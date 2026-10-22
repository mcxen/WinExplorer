//go:build windows

package diskhealth

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// This file reads Windows' physical disks through the PowerShell Storage
// cmdlets. Get-PhysicalDisk carries identity, bus, media and health;
// Get-StorageReliabilityCounter carries the temperature, the power-on
// hours and the wear figure; Get-Disk carries the serial number. The
// SMART lifetime byte counters are not exposed by these cmdlets, so they
// stay 未知 rather than being guessed from the read-error count (which is
// what an earlier version did).

const probeTimeout = 12 * time.Second

// createNoWindow is Windows' CREATE_NO_WINDOW. The shipped binary is linked
// with -H=windowsgui, so it owns no console. A bare exec of powershell.exe
// then allocates a new visible one. The tray polls this probe every few
// seconds while disk capacity or speed is shown, which is the default, so
// the window kept opening on its own.
const createNoWindow = 0x08000000

func probe() Report {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	r := Report{At: time.Now(), Sources: []string{"PowerShell Storage"}}

	out, err := quietCommand(ctx, "powershell", "-NoProfile", "-NonInteractive",
		"-Command", diskProbePS).Output()
	if err != nil || len(out) == 0 {
		return Report{At: r.At, Notes: []string{"PowerShell 没有返回磁盘信息"}}
	}
	var raw []diskProbeRow
	if json.Unmarshal(out, &raw) != nil {
		return Report{At: r.At, Notes: []string{"无法解析 PowerShell 的磁盘信息"}}
	}
	vols := windowsVolumes()
	for _, row := range raw {
		d := row.toDisk()
		d.Volumes, d.Free, d.Total = volsForLetters(vols, row.Letters, d.Size)
		r.Disks = append(r.Disks, d)
	}
	return r
}

// quietCommand starts a console program without a window. HideWindow covers
// the show-state; CREATE_NO_WINDOW stops the console from being allocated
// at all, which is what keeps a GUI parent from flashing powershell.exe.
func quietCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

// diskProbeRow is one row the PowerShell probe emits.
type diskProbeRow struct {
	Index        int      `json:"index"`
	Model        string   `json:"model"`
	Serial       string   `json:"serial"`
	Bus          string   `json:"bus"`
	Media        string   `json:"media"`
	Size         int64    `json:"size"`
	Temperature  float64  `json:"temperature"`
	Health       string   `json:"health"`
	Operational  string   `json:"operational"`
	Wear         int64    `json:"wear"`
	PowerOnHours int64    `json:"powerOnHours"`
	Firmware     string   `json:"firmware"`
	Letters      []string `json:"letters"`
}

func (r diskProbeRow) toDisk() Disk {
	d := Disk{
		ID:              "PhysicalDrive" + itoa(r.Index),
		Model:           strings.TrimSpace(r.Model),
		Serial:          strings.TrimSpace(r.Serial),
		Firmware:        strings.TrimSpace(r.Firmware),
		Bus:             busLabel(r.Bus),
		Media:           mediaLabel(r.Media),
		Size:            r.Size,
		Health:          healthLabel(r.Health, r.Operational),
		HealthNote:      r.Health,
		ReadBytes:       -1,
		WriteBytes:      -1,
		ReadOps:         -1,
		WriteOps:        -1,
		PowerCycles:     -1,
		UnsafeShutdowns: -1,
		MediaErrors:     -1,
		Source:          "PowerShell Storage",
	}
	if r.Temperature > 0 {
		d.Temperature = r.Temperature
		d.TempKnown = true
	}
	if r.PowerOnHours > 0 {
		d.PowerOnHours = r.PowerOnHours
	}
	// Wear is a percentage the Storage stack reports for SSDs only.
	if r.Wear > 0 || d.Media == "固态硬盘" {
		d.LifeKnown = true
		d.LifeUsed = float64(r.Wear)
	}
	return d
}

// diskProbePS gathers everything in one pass and emits compact JSON. The
// letters array maps each disk to the drive letters of its partitions, so
// the Go side can attach capacities without a second round trip.
const diskProbePS = `
$ErrorActionPreference = 'SilentlyContinue'
$out = @()
foreach ($d in Get-PhysicalDisk | Sort-Object DeviceId) {
    $rc = $d | Get-StorageReliabilityCounter
    $pd = Get-Disk -Number $d.DeviceId
    $letters = @()
    if ($pd) {
        $letters = @(Get-Partition -DiskNumber $d.DeviceId |
            Where-Object { $_.DriveLetter } |
            ForEach-Object { [string]$_.DriveLetter })
    }
    $temp = 0.0
    $hours = 0
    $wear = 0
    if ($rc) {
        if ($rc.Temperature -ne $null -and $rc.Temperature -ne 0) { $temp = [double]$rc.Temperature }
        if ($rc.PowerOnHours -ne $null) { $hours = [int64]$rc.PowerOnHours }
        if ($rc.Wear -ne $null) { $wear = [int64]$rc.Wear }
    }
    $out += [PSCustomObject]@{
        index = [int]$d.DeviceId
        model = [string]$d.FriendlyName
        serial = [string]$pd.SerialNumber
        bus = [string]$d.BusType
        media = [string]$d.MediaType
        size = [int64]$d.Size
        temperature = $temp
        health = [string]$d.HealthStatus
        operational = [string]$d.OperationalStatus
        wear = $wear
        powerOnHours = $hours
        firmware = [string]$pd.FirmwareVersion
        letters = $letters
    }
}
$out | ConvertTo-Json -Compress
`

// busLabel normalises the bus names the page shows.
func busLabel(b string) string {
	switch {
	case b == "":
		return "未知"
	case strings.Contains(b, "NVMe"), strings.Contains(b, "NVM"):
		return "NVMe"
	case strings.Contains(b, "SATA"), strings.Contains(b, "ATA"):
		return "SATA"
	case strings.Contains(b, "USB"):
		return "USB"
	case strings.Contains(b, "SCSI"), strings.Contains(b, "SAS"):
		return "SAS"
	case strings.Contains(b, "IDE"), strings.Contains(b, "PATA"):
		return "IDE"
	}
	return b
}

// mediaLabel says 固态硬盘 / 机械硬盘.
func mediaLabel(m string) string {
	switch {
	case strings.Contains(m, "SSD"), strings.Contains(m, "Solid"):
		return "固态硬盘"
	case strings.Contains(m, "HDD"), strings.Contains(m, "Hard"):
		return "机械硬盘"
	case m == "SCM", m == "Scm":
		return "存储级内存"
	}
	if m == "" {
		return "未知"
	}
	return m
}

// healthLabel maps the Storage stack's two status fields. Operational
// status is the drive's own; HealthStatus is the stack's verdict.
func healthLabel(health, operational string) Health {
	switch {
	case strings.Contains(health, "Unhealthy"):
		return HealthFailing
	case strings.Contains(health, "Warning"):
		return HealthWarning
	case strings.Contains(operational, "Failed"), strings.Contains(operational, "Lost Communication"):
		return HealthFailing
	case strings.Contains(health, "Healthy"), strings.Contains(operational, "OK"):
		return HealthGood
	}
	return HealthUnknown
}

// windowsVolume is one mounted drive letter.
type windowsVolume struct {
	Letter string
	FS     string
	Free   int64
	Total  int64
}

// windowsVolumes lists the ready volumes once, so attaching them to disks
// costs no further PowerShell calls.
func windowsVolumes() map[string]windowsVolume {
	mounts, err := platformMounts()
	if err != nil {
		return nil
	}
	out := make(map[string]windowsVolume, len(mounts))
	for _, m := range mounts {
		f, t, ok := capacityOf(m.On)
		if !ok {
			continue
		}
		letter := strings.TrimRight(m.On, `:\`)
		out[letter] = windowsVolume{Letter: letter, FS: m.FS, Free: f, Total: t}
	}
	return out
}

// volsForLetters attaches the drive letters' capacity to the disk. The
// volume map is read once per probe, not once per disk.
func volsForLetters(all map[string]windowsVolume, letters []string, diskSize int64) (vols []Volume, free, total int64) {
	free = -1
	for _, l := range letters {
		v, ok := all[l]
		if !ok {
			continue
		}
		vols = append(vols, Volume{
			Name:       l + ":",
			MountPoint: l + `:\`,
			FS:         v.FS,
			Size:       v.Total,
			Free:       v.Free,
			Used:       v.Total - v.Free,
		})
		if free < 0 {
			free = 0
		}
		free += v.Free
		total += v.Total
	}
	if total == 0 {
		total = diskSize
	}
	return vols, free, total
}

// itoa avoids strconv for one small conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
