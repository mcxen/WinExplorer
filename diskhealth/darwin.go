//go:build darwin

package diskhealth

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// This file reads a Mac's disks. The facts come from diskutil's plist
// output, which on Apple Silicon carries the whole NVMe SMART set
// (DATA_UNITS_*, PERCENTAGE_USED, AVAILABLE_SPARE, POWER_ON_HOURS…) that
// system_profiler's NVMe section does not expose. system_profiler fills
// what diskutil leaves out — the serial and firmware numbers some Macs
// withhold, and Intel Macs' SATA drives — and smartctl, when installed,
// adds a second opinion and a real temperature on drives whose controller
// hides it.

// probeTimeout bounds one probe: diskutil is fast, but a wedged external
// enclosure's SMART read can block for seconds.
const probeTimeout = 4 * time.Second

// probe reads every physical disk on this Mac.
func probe() Report {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	r := Report{At: time.Now(), Sources: []string{"diskutil"}}

	tree := readDiskTree(ctx)
	if tree == nil {
		return Report{At: r.At, Notes: []string{"diskutil 没有返回磁盘列表"}}
	}
	// One call each, not one per disk: the containers, and the profiler
	// sections that fill in what diskutil omits.
	containers := readAPFSContainers(ctx)
	spNVMe := sysProfilerNVMe(ctx)
	spSATA := sysProfilerSATA(ctx)

	for _, whole := range tree.wholeDisks() {
		info := readDiskInfo(ctx, whole.ID)
		if info == nil || info.Virtual() {
			// A synthesised disk (an APFS container's own node) is not a
			// drive; the container's physical stores are.
			continue
		}
		d := diskFromInfo(whole, info)
		d.Volumes = containers.volumesFor(whole.ID)
		d.Free, d.Total = containers.capacityFor(whole.ID, d.Volumes)
		spNVMe.merge(&d)
		spSATA.merge(&d)
		r.Disks = append(r.Disks, d)
	}

	if len(r.Disks) > 0 {
		r.Sources = append(r.Sources, "system_profiler")
	}
	// smartctl is the fallback for what the Apple tools withheld; it runs
	// on the device nodes the disks now carry.
	if smartctlMerge(ctx, r.Disks) {
		r.Sources = append(r.Sources, "smartctl")
	}
	return r
}

// wholeDisk is one physical drive's place in diskutil's tree.
type wholeDisk struct {
	ID   string
	Size int64
}

// — diskutil list —

// diskTree is the part of `diskutil list -plist` the probe reads.
type diskTree struct {
	AllDisksAndPartitions []diskNode `json:"AllDisksAndPartitions"`
}

type diskNode struct {
	DeviceIdentifier string `json:"DeviceIdentifier"`
	Content          string `json:"Content"`
	Size             int64  `json:"Size"`
	// APFSPhysicalStores is non-empty for an APFS container node, which
	// is synthesised rather than physical.
	APFSPhysicalStores []storeNode `json:"APFSPhysicalStores"`
}

type storeNode struct {
	DeviceIdentifier string `json:"DeviceIdentifier"`
}

// wholeDisks drops the synthesised APFS container nodes, leaving drives.
func (t *diskTree) wholeDisks() []wholeDisk {
	out := make([]wholeDisk, 0, len(t.AllDisksAndPartitions))
	for _, d := range t.AllDisksAndPartitions {
		if len(d.APFSPhysicalStores) > 0 || d.DeviceIdentifier == "" {
			continue
		}
		out = append(out, wholeDisk{ID: d.DeviceIdentifier, Size: d.Size})
	}
	return out
}

func readDiskTree(ctx context.Context) *diskTree {
	var t diskTree
	if !plistJSON(ctx, &t, "diskutil", "list", "-plist") {
		return nil
	}
	return &t
}

// — diskutil apfs list —

// apfsList is the part of `diskutil apfs list -plist` the probe reads.
// A disk's real capacity lives here: an APFS container pools its volumes
// into one shared space, so summing the volumes would count the same
// blocks over and over.
type apfsList struct {
	Containers []apfsContainer `json:"Containers"`
}

type apfsContainer struct {
	ContainerReference string       `json:"ContainerReference"`
	CapacityCeiling    int64        `json:"CapacityCeiling"`
	CapacityFree       int64        `json:"CapacityFree"`
	PhysicalStores     []storeNode  `json:"PhysicalStores"`
	Volumes            []apfsVolume `json:"Volumes"`
}

type apfsVolume struct {
	DeviceIdentifier string   `json:"DeviceIdentifier"`
	Name             string   `json:"Name"`
	Roles            []string `json:"Roles"`
	CapacityQuota    int64    `json:"CapacityQuota"`
	CapacityInUse    int64    `json:"CapacityInUse"`
	FileVault        bool     `json:"FileVault"`
	Locked           bool     `json:"Locked"`
}

// belongsTo reports whether a store of the container is a partition of
// the disk: "disk0s2" belongs to "disk0".
func (c apfsContainer) belongsTo(disk string) bool {
	for _, s := range c.PhysicalStores {
		if strings.HasPrefix(s.DeviceIdentifier, disk+"s") {
			return true
		}
	}
	return false
}

// volumesFor lists the volumes the disk carries, mounted or not.
func (l *apfsList) volumesFor(disk string) []Volume {
	var out []Volume
	for _, c := range l.Containers {
		if !c.belongsTo(disk) {
			continue
		}
		for _, v := range c.Volumes {
			mount, fs, free := deviceMount(v.DeviceIdentifier)
			out = append(out, Volume{
				Name:       v.Name,
				MountPoint: mount,
				FS:         fs,
				Role:       strings.Join(v.Roles, ","),
				Size:       c.CapacityCeiling,
				Free:       free,
				Used:       v.CapacityInUse,
			})
		}
	}
	return out
}

// capacityFor is the disk's free and total space: its containers' pools,
// counted once each. Free is -1 when no container reported it, which the
// caller reads as 未知 rather than as "full".
func (l *apfsList) capacityFor(disk string, vols []Volume) (free, total int64) {
	free = -1
	for _, c := range l.Containers {
		if !c.belongsTo(disk) {
			continue
		}
		total += c.CapacityCeiling
		if c.CapacityFree >= 0 {
			if free < 0 {
				free = 0
			}
			free += c.CapacityFree
		}
	}
	if total > 0 {
		return free, total
	}
	// Not APFS (or no container): whatever partitions are mounted answer.
	f, t := mountCapacity(disk)
	if t > 0 {
		return f, t
	}
	return -1, 0
}

func readAPFSContainers(ctx context.Context) *apfsList {
	var l apfsList
	if !plistJSON(ctx, &l, "diskutil", "apfs", "list", "-plist") {
		return &apfsList{}
	}
	return &l
}

// — diskutil info —

// diskInfo is the subset of `diskutil info -plist` the probe reads.
type diskInfo struct {
	MediaName         string
	BusProtocol       string
	SolidState        bool
	Internal          bool
	Removable         bool
	Size              int64
	SMARTStatus       string
	VirtualOrPhysical string
	MediaType         string
	SMART             smartKeys
}

// diskInfoJSON mirrors diskutil's own PascalCase keys.
type diskInfoJSON struct {
	DeviceIdentifier  string     `json:"DeviceIdentifier"`
	MediaName         string     `json:"MediaName"`
	BusProtocol       string     `json:"BusProtocol"`
	SolidState        bool       `json:"SolidState"`
	Internal          bool       `json:"Internal"`
	Removable         bool       `json:"Removable"`
	RemovableMedia    bool       `json:"RemovableMedia"`
	Size              int64      `json:"Size"`
	TotalSize         int64      `json:"TotalSize"`
	SMARTStatus       string     `json:"SMARTStatus"`
	VirtualOrPhysical string     `json:"VirtualOrPhysical"`
	MediaType         string     `json:"MediaType"`
	SMART             *smartKeys `json:"SMARTDeviceSpecificKeysMayVaryNotGuaranteed"`
}

// smartKeys is the NVMe SMART block diskutil exposes. The meanings are
// fixed by the NVMe spec:
//
//	DATA_UNITS_READ/WRITTEN  1000 × 512-byte sectors per unit
//	TEMPERATURE              kelvin
//	PERCENTAGE_USED          share of the drive's rated writes consumed
//	AVAILABLE_SPARE          spare blocks left, percent
//
// The _0/_1 pairs are a 128-bit counter's low and high words; Apple's
// drives so far report only the low word.
// smartKeys is the NVMe SMART block diskutil exposes, kept as raw
// key/value pairs: a drive that does not report a counter must read as
// 未知, and a struct would make that indistinguishable from a zero.
type smartKeys map[string]int64

// get returns the key's value and whether the drive reported it.
func (k smartKeys) get(name string) (int64, bool) {
	v, ok := k[name]
	return v, ok
}

// counter reads a paired 128-bit counter (the spec's _0 and _1 low and
// high words) and reports whether the drive reported it at all.
func (k smartKeys) counter(name string) (int64, bool) {
	lo, ok := k.get(name + "_0")
	if !ok {
		return 0, false
	}
	hi, _ := k.get(name + "_1")
	return combine(lo, hi), true
}

// The SMART keys read here, named as diskutil writes them. DATA_UNITS_*
// count 1000 × 512-byte sectors per unit and TEMPERATURE is kelvin; the
// rest are plain counters.
const (
	keyDataUnitsRead     = "DATA_UNITS_READ"
	keyDataUnitsWritten  = "DATA_UNITS_WRITTEN"
	keyHostReadCommands  = "HOST_READ_COMMANDS"
	keyHostWriteCommands = "HOST_WRITE_COMMANDS"
	keyPowerOnHours      = "POWER_ON_HOURS"
	keyPowerCycles       = "POWER_CYCLES"
	keyUnsafeShutdowns   = "UNSAFE_SHUTDOWNS"
	keyMediaErrors       = "MEDIA_ERRORS"
	keyPercentageUsed    = "PERCENTAGE_USED"
	keyAvailableSpare    = "AVAILABLE_SPARE"
	keySpareThreshold    = "AVAILABLE_SPARE_THRESHOLD"
	keyTemperature       = "TEMPERATURE"
)

func readDiskInfo(ctx context.Context, id string) *diskInfo {
	var j diskInfoJSON
	if !plistJSON(ctx, &j, "diskutil", "info", "-plist", id) {
		return nil
	}
	var smart smartKeys
	if j.SMART != nil {
		smart = *j.SMART
	}
	return &diskInfo{
		MediaName:         j.MediaName,
		BusProtocol:       j.BusProtocol,
		SolidState:        j.SolidState,
		Internal:          j.Internal,
		Removable:         j.Removable || j.RemovableMedia,
		Size:              firstNonZero(j.Size, j.TotalSize),
		SMARTStatus:       j.SMARTStatus,
		VirtualOrPhysical: j.VirtualOrPhysical,
		MediaType:         j.MediaType,
		SMART:             smart,
	}
}

// Virtual reports whether diskutil calls this a synthesised disk — an
// APFS container's own node rather than a drive.
func (i *diskInfo) Virtual() bool { return i.VirtualOrPhysical == "Virtual" }

// diskFromInfo turns one whole disk's diskutil data into the shared model.
func diskFromInfo(whole wholeDisk, info *diskInfo) Disk {
	d := Disk{
		ID:              whole.ID,
		Model:           info.MediaName,
		Bus:             busLabel(info.BusProtocol),
		Media:           mediaLabel(info),
		Size:            firstNonZero(info.Size, whole.Size),
		Internal:        info.Internal,
		Removable:       info.Removable,
		Source:          "diskutil",
		Health:          healthFromSMARTStatus(info.SMARTStatus),
		HealthNote:      healthNote(info.SMARTStatus),
		ReadBytes:       -1,
		WriteBytes:      -1,
		ReadOps:         -1,
		WriteOps:        -1,
		PowerOnHours:    -1,
		PowerCycles:     -1,
		UnsafeShutdowns: -1,
		MediaErrors:     -1,
	}
	if s := info.SMART; len(s) > 0 {
		if v, ok := s.counter(keyDataUnitsRead); ok {
			d.ReadBytes = v * nvmeUnit
		}
		if v, ok := s.counter(keyDataUnitsWritten); ok {
			d.WriteBytes = v * nvmeUnit
		}
		if v, ok := s.counter(keyHostReadCommands); ok {
			d.ReadOps = v
		}
		if v, ok := s.counter(keyHostWriteCommands); ok {
			d.WriteOps = v
		}
		if v, ok := s.counter(keyPowerOnHours); ok {
			d.PowerOnHours = v
		}
		if v, ok := s.counter(keyPowerCycles); ok {
			d.PowerCycles = v
		}
		if v, ok := s.counter(keyUnsafeShutdowns); ok {
			d.UnsafeShutdowns = v
		}
		if v, ok := s.counter(keyMediaErrors); ok {
			d.MediaErrors = v
		}
		// PERCENTAGE_USED and AVAILABLE_SPARE are meaningful at zero: a
		// healthy drive has used none of its wear budget.
		used, okUsed := s.get(keyPercentageUsed)
		spare, okSpare := s.get(keyAvailableSpare)
		if okUsed && okSpare {
			d.LifeKnown = true
			d.LifeUsed = float64(used)
			d.SparePercent = float64(spare)
			threshold, _ := s.get(keySpareThreshold)
			d.SpareThreshold = float64(threshold)
		}
		// The field is kelvin. A reading below 200 K is a mis-set sensor;
		// showing it would be worse than saying 未知.
		if k, ok := s.get(keyTemperature); ok && k > 200 {
			d.Temperature = float64(k) - 273.15
			d.TempKnown = true
		}
	}
	// SMARTVerified alone says the drive answered; a failing status is
	// what changes the health, and a drive with no SMART is 未知.
	if info.SMARTStatus == "Failing" {
		d.Health = HealthFailing
	}
	if d.LifeKnown && d.SpareThreshold > 0 && d.SparePercent < d.SpareThreshold {
		// Worn past its spare threshold: the drive still works, but the
		// page should stop calling it 良好.
		d.Health = HealthWarning
		d.HealthNote = "备用块低于阈值"
	}
	return d
}

// combine folds a counter's low and high words.
func combine(lo, hi int64) int64 {
	if hi <= 0 {
		return lo
	}
	return lo + hi*(1<<32)
}

// healthFromSMARTStatus maps diskutil's SMARTStatus string.
func healthFromSMARTStatus(s string) Health {
	switch s {
	case "Verified", "Passed":
		return HealthGood
	case "Failing":
		return HealthFailing
	}
	return HealthUnknown
}

// healthNote renders diskutil's raw SMART status for the card's detail
// line, in the app's language.
func healthNote(status string) string {
	switch status {
	case "Verified":
		return "SMART 已校验"
	case "Passed":
		return "SMART 通过"
	case "Failing":
		return "SMART 即将失效"
	}
	if status == "" {
		return ""
	}
	return "SMART " + status
}

// busLabel normalises the protocol names the page shows.
func busLabel(p string) string {
	switch {
	case p == "":
		return "未知"
	case strings.Contains(p, "Apple Fabric"):
		// Apple Silicon's internal SSD: NVMe over Apple's own fabric.
		return "NVMe"
	case strings.Contains(p, "NVMe"), strings.Contains(p, "NVM"):
		return "NVMe"
	case strings.Contains(p, "SATA"), strings.Contains(p, "Serial ATA"):
		return "SATA"
	case strings.Contains(p, "USB"):
		return "USB"
	case strings.Contains(p, "Thunderbolt"):
		return "Thunderbolt"
	case strings.Contains(p, "PCI"):
		return "PCIe"
	}
	return p
}

// mediaLabel says 固态硬盘 / 机械硬盘.
func mediaLabel(i *diskInfo) string {
	if i.SolidState {
		return "固态硬盘"
	}
	if strings.Contains(i.MediaType, "光盘") {
		return "光盘"
	}
	return "机械硬盘"
}

func firstNonZero(a, b int64) int64 {
	if a != 0 {
		return a
	}
	return b
}

// — volumes and capacity —

// deviceMount resolves a volume identifier to its mount point through the
// mount table, with the filesystem and the free space there. A volume
// with no mount point (Preboot, Recovery, VM) returns "", "", -1.
func deviceMount(id string) (mount, fs string, free int64) {
	return mountOf("/dev/" + id)
}

// mountCapacity is the free and total of whatever partitions of the disk
// are mounted, for disks with no APFS container.
func mountCapacity(disk string) (free, total int64) {
	mounts, err := getfsstat()
	if err != nil {
		return 0, 0
	}
	for _, m := range mounts {
		if !strings.HasPrefix(m.From, "/dev/"+disk+"s") {
			continue
		}
		f, t, ok := capacityOf(m.On)
		if ok {
			free += f
			total += t
		}
	}
	return free, total
}

// — system_profiler —

// profilerNVMe is system_profiler's SPNVMeDataType. Its shape changed in
// macOS 12: each controller now carries an _items array of drives, where
// older systems listed the drives directly. Only the current shape is
// read; a system that still uses the old one leaves diskutil's facts to
// stand, which are the same numbers.
type profilerNVMe struct {
	Controllers []struct {
		Items []struct {
			Name        string `json:"_name"`
			BSDName     string `json:"bsd_name"`
			Model       string `json:"device_model"`
			Serial      string `json:"device_serial"`
			Revision    string `json:"device_revision"`
			SizeBytes   int64  `json:"size_in_bytes"`
			SMARTStatus string `json:"smart_status"`
		} `json:"_items"`
	} `json:"SPNVMeDataType"`
}

func sysProfilerNVMe(ctx context.Context) *profilerNVMe {
	var p profilerNVMe
	if !plistJSON(ctx, &p, "system_profiler", "SPNVMeDataType", "-json") {
		return &profilerNVMe{}
	}
	return &p
}

// merge fills the serial and firmware diskutil omitted, matching by BSD
// name — the one identifier both tools report the same way.
func (p *profilerNVMe) merge(d *Disk) {
	for _, c := range p.Controllers {
		for _, it := range c.Items {
			if it.BSDName != d.ID {
				continue
			}
			if d.Serial == "" {
				d.Serial = it.Serial
			}
			if d.Firmware == "" {
				d.Firmware = it.Revision
			}
			if d.Model == "" {
				d.Model = it.Model
			}
			if d.Size == 0 {
				d.Size = it.SizeBytes
			}
			if d.Health == HealthUnknown {
				d.Health = healthFromSMARTStatus(it.SMARTStatus)
				if it.SMARTStatus != "" {
					d.HealthNote = it.SMARTStatus
				}
			}
			return
		}
	}
}

// profilerSATA is system_profiler's SPSerialATADataType — Intel Macs'
// internal drives and SATA enclosures.
type profilerSATA struct {
	Disks []struct {
		Model       string `json:"Model"`
		Serial      string `json:"SerialNumber"`
		Revision    string `json:"Revision"`
		SMARTStatus string `json:"SMARTStatus"`
		Fatal       bool   `json:"SMARTFatalError"`
		Devices     []struct {
			Size string `json:"Size"`
		} `json:"device_description"`
	} `json:"SPSerialATADataType"`
}

func sysProfilerSATA(ctx context.Context) *profilerSATA {
	var p profilerSATA
	if !plistJSON(ctx, &p, "system_profiler", "SPSerialATADataType", "-json") {
		return &profilerSATA{}
	}
	return &p
}

// merge fills a SATA drive diskutil identified only partly. The SATA
// section names drives by bay, not by BSD name, so matching is by serial
// when both sides have one and otherwise by model.
func (p *profilerSATA) merge(d *Disk) {
	for _, s := range p.Disks {
		if s.Serial != "" && d.Serial != "" && s.Serial != d.Serial {
			continue
		}
		if s.Model != "" && d.Model != "" && s.Model != d.Model {
			continue
		}
		if s.Serial != "" && d.Serial == "" {
			d.Serial = s.Serial
		}
		if s.Revision != "" && d.Firmware == "" {
			d.Firmware = s.Revision
		}
		if d.Model == "" {
			d.Model = s.Model
		}
		if d.Bus == "未知" {
			d.Bus = "SATA"
		}
		if s.SMARTStatus != "" && d.Health == HealthUnknown {
			d.Health = healthFromSMARTStatus(s.SMARTStatus)
			d.HealthNote = s.SMARTStatus
		}
		if s.Fatal {
			d.Health = HealthFailing
			d.HealthNote = "SMART 致命错误"
		}
		if d.Size == 0 {
			for _, dev := range s.Devices {
				if n, err := parseDecimalSize(dev.Size); err == nil && n > 0 {
					d.Size = n
					break
				}
			}
		}
		return
	}
}

// — helpers —

// plistJSON runs a command that prints a plist and decodes it as JSON,
// with plutil doing the conversion so no plist library is needed.
func plistJSON(ctx context.Context, dst any, name string, args ...string) bool {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil || len(out) == 0 {
		return false
	}
	conv := exec.CommandContext(ctx, "plutil", "-convert", "json", "-o", "-", "-")
	conv.Stdin = strings.NewReader(string(out))
	js, err := conv.Output()
	if err != nil || len(js) == 0 {
		return false
	}
	return json.Unmarshal(js, dst) == nil
}

// parseDecimalSize parses system_profiler's "128.0 GB" into bytes, with
// the decimal prefixes Apple reports.
func parseDecimalSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult, s = 1e12, strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult, s = 1e9, strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult, s = 1e6, strings.TrimSuffix(s, "MB")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * mult), nil
}
