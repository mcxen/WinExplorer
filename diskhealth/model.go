// Package diskhealth reads the machine's physical disks: identity, health,
// SMART counters, temperature and the volumes each one carries.
//
// The facts come from the platform: macOS uses diskutil's plists (which
// expose the full NVMe SMART set on Apple Silicon), Linux reads sysfs and
// merges smartctl, Windows queries the Storage cmdlets. Everything above
// that boundary — the model below, the cache and the throughput sampling —
// is shared, so the page renders the same whatever it runs on.
package diskhealth

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Health is the drive's overall state, as its SMART data supports telling.
type Health int

const (
	HealthUnknown Health = iota
	HealthGood
	HealthWarning
	HealthFailing
)

// String is the label the disks page shows.
func (h Health) String() string {
	switch h {
	case HealthGood:
		return "良好"
	case HealthWarning:
		return "警告"
	case HealthFailing:
		return "故障"
	}
	return "未知"
}

// Volume is one mounted volume on a disk. APFS makes these many-to-one: a
// single physical store carries several volumes that share one container.
type Volume struct {
	Name       string // volume name, e.g. "Macintosh HD - Data"
	MountPoint string // "" when the volume is not mounted
	FS         string // apfs, hfs, exfat…
	Role       string // APFS role: System, Data, Preboot, Recovery, VM…
	Size       int64  // bytes
	Free       int64  // bytes, -1 when not reported
	Used       int64  // bytes, -1 when not reported
}

// Disk is one physical drive.
type Disk struct {
	ID       string // stable identifier: "disk0", "/dev/nvme0n1", "PhysicalDrive0"
	Model    string
	Serial   string
	Firmware string
	Bus      string // NVMe / SATA / USB / Apple Fabric…
	Media    string // 固态硬盘 / 机械硬盘
	Size     int64  // bytes, 0 when unknown
	// Internal is false for drives the machine does not boot from — USB
	// and Thunderbolt enclosures report themselves this way.
	Internal  bool
	Removable bool

	Health     Health
	HealthNote string // the raw status, e.g. "SMART Verified"

	// Temperature in °C; TempKnown is false when the drive has no sensor
	// or the platform cannot reach it.
	Temperature float64
	TempKnown   bool

	// Life is the SSD's wear budget: percentage of rated writes consumed,
	// spare blocks left and the vendor's threshold for both. A spinning
	// disk leaves LifeKnown false.
	LifeUsed       float64
	SparePercent   float64
	SpareThreshold float64
	LifeKnown      bool

	// Counters; -1 when the platform does not report them.
	PowerOnHours    int64
	PowerCycles     int64
	UnsafeShutdowns int64
	MediaErrors     int64
	ReadBytes       int64 // lifetime bytes read
	WriteBytes      int64 // lifetime bytes written
	ReadOps         int64 // lifetime read commands
	WriteOps        int64 // lifetime write commands

	// Free and Total aggregate the volumes mounted from this disk: for
	// APFS that is the container's capacity, not the sum of the volumes,
	// which would count the shared space several times.
	Free  int64
	Total int64

	// ReadBps and WriteBps are averaged over the interval between two
	// probes; RateKnown says whether a second probe has landed, so the
	// page can say 采样中 rather than 未知 before it has.
	ReadBps   float64
	WriteBps  float64
	RateKnown bool

	Volumes []Volume
	Source  string // where the facts came from, for the page's footnote
}

// Report is one probe of the whole machine.
type Report struct {
	At      time.Time
	Disks   []Disk
	Notes   []string // capability notes: a missing tool, an unreadable drive
	Sources []string // the tools the report drew on
	// History is each disk's recent throughput samples, oldest first,
	// keyed by Disk.ID. The page plots them; a disk new to the report has
	// an empty slice.
	History map[string][]Sample
	// HistorySpan is how much time the samples cover, for the chart's
	// x-axis caption.
	HistorySpan time.Duration
}

// Sample is one throughput measurement: the rate over the interval that
// ended at At.
type Sample struct {
	At    time.Time
	Read  float64 // bytes per second
	Write float64
}

// HasTemperature reports whether the drive's temperature is known.
func (d Disk) HasTemperature() bool { return d.TempKnown && !math.IsNaN(d.Temperature) }

// UsedBytes is the space in use across the disk's volumes, 0 when unknown.
func (d Disk) UsedBytes() int64 {
	if d.Total <= 0 || d.Free < 0 {
		return 0
	}
	used := d.Total - d.Free
	if used < 0 {
		return 0
	}
	return used
}

// UsedRatio is the share of the disk's capacity in use, in [0,1].
func (d Disk) UsedRatio() float64 {
	if d.Total <= 0 || d.Free < 0 {
		return 0
	}
	r := float64(d.Total-d.Free) / float64(d.Total)
	return math.Min(math.Max(r, 0), 1)
}

// TemperatureLabel is "51 °C", or 未知 without a sensor.
func (d Disk) TemperatureLabel() string {
	if !d.HasTemperature() {
		return "未知"
	}
	return fmt.Sprintf("%.0f °C", d.Temperature)
}

// LifeLabel is the SSD wear, e.g. "已用 4%".
func (d Disk) LifeLabel() string {
	if !d.LifeKnown {
		return "未知"
	}
	return fmt.Sprintf("已用 %.0f%%", d.LifeUsed)
}

// SpareLabel is the spare-block pool, e.g. "100%（阈值 99%）".
func (d Disk) SpareLabel() string {
	if !d.LifeKnown {
		return "未知"
	}
	if d.SpareThreshold > 0 {
		return fmt.Sprintf("%.0f%%（阈值 %.0f%%）", d.SparePercent, d.SpareThreshold)
	}
	return fmt.Sprintf("%.0f%%", d.SparePercent)
}

// Subtitle is the identity line under the model: bus · media · serial.
func (d Disk) Subtitle() string {
	parts := make([]string, 0, 4)
	for _, s := range []string{d.Bus, d.Media} {
		if s != "" && s != "未知" {
			parts = append(parts, s)
		}
	}
	if d.Serial != "" {
		parts = append(parts, "序列号 "+d.Serial)
	}
	if d.Firmware != "" {
		parts = append(parts, "固件 "+d.Firmware)
	}
	return strings.Join(parts, " · ")
}

// CheckState is how one diagnostic indicator reads.
type CheckState int

const (
	// CheckUnknown means the platform did not report what the check needs.
	CheckUnknown CheckState = iota
	// CheckPass is a healthy reading.
	CheckPass
	// CheckWarn is a reading drifting toward trouble.
	CheckWarn
	// CheckFail is a reading that means the drive is failing.
	CheckFail
)

// String is the short label a badge shows.
func (s CheckState) String() string {
	switch s {
	case CheckPass:
		return "正常"
	case CheckWarn:
		return "注意"
	case CheckFail:
		return "异常"
	}
	return "未知"
}

// Check is one diagnostic indicator: what was measured, what it read, and
// how that reads. The page lists them; the score is derived from them, so
// the two never disagree.
type Check struct {
	Name  string
	Value string
	State CheckState
	// Note explains a non-pass reading; empty for a pass.
	Note string
}

// Thresholds the diagnostics evaluate against. They are the page's own
// standards, not the firmware's: the drive reports numbers, the app says
// what they mean. Zero disables a threshold.
const (
	// warnWearPercent is where an SSD's consumed write budget starts to
	// matter. Drives are rated to 100%, but a drive past 80% has little
	// headroom left for another few years of writes.
	warnWearPercent = 80.0
	// warnCapacityPercent flags a volume pool running out of room.
	warnCapacityPercent = 90.0
	// warnPowerOnHours is roughly five years of continuous use, when a
	// consumer drive's failure rate starts to climb.
	warnPowerOnHours = 5 * 365 * 24
)

// Checks evaluates the drive's indicators. warnTemp is the user's
// high-temperature threshold; 0 disables the temperature check.
func (d Disk) Checks(warnTemp float64) []Check {
	checks := []Check{
		{Name: "SMART 状态", Value: orDash(d.HealthNote), State: stateFromHealth(d.Health)},
	}
	if d.HasTemperature() {
		st := CheckPass
		note := ""
		if warnTemp > 0 && d.Temperature >= warnTemp {
			st = CheckWarn
			note = fmt.Sprintf("达到设定阈值 %.0f °C", warnTemp)
		}
		checks = append(checks, Check{
			Name: "温度", Value: d.TemperatureLabel(), State: st, Note: note,
		})
	} else {
		checks = append(checks, Check{Name: "温度", Value: "无传感器", State: CheckUnknown})
	}
	if d.LifeKnown {
		st, note := CheckPass, ""
		if d.SpareThreshold > 0 && d.SparePercent < d.SpareThreshold {
			st = CheckFail
			note = "低于厂商阈值，备用块耗尽"
		}
		checks = append(checks, Check{
			Name: "备用块", Value: d.SpareLabel(), State: st, Note: note,
		})
		st, note = CheckPass, ""
		if d.LifeUsed >= warnWearPercent {
			st = CheckWarn
			note = "写入寿命已用较多"
		}
		checks = append(checks, Check{
			Name: "写入寿命", Value: d.LifeLabel(), State: st, Note: note,
		})
	} else {
		checks = append(checks, Check{
			Name: "寿命", Value: "未报告", State: CheckUnknown,
		})
	}
	if d.MediaErrors >= 0 {
		st, note := CheckPass, ""
		if d.MediaErrors > 0 {
			st = CheckWarn
			note = "出现介质错误，数据可能已受影响"
		}
		checks = append(checks, Check{
			Name: "介质错误", Value: fmt.Sprintf("%d 次", d.MediaErrors),
			State: st, Note: note,
		})
	}
	if d.PowerOnHours >= 0 {
		st, note := CheckPass, ""
		if d.PowerOnHours >= warnPowerOnHours {
			st = CheckWarn
			note = "通电时长已超过五年"
		}
		checks = append(checks, Check{
			Name: "通电时长", Value: fmt.Sprintf("%d 小时", d.PowerOnHours),
			State: st, Note: note,
		})
	}
	if d.Total > 0 {
		used := d.UsedRatio() * 100
		st, note := CheckPass, ""
		if used >= warnCapacityPercent {
			st = CheckWarn
			note = "可用空间偏低"
		}
		checks = append(checks, Check{
			Name: "容量使用", Value: fmt.Sprintf("%.0f%%", used), State: st, Note: note,
		})
	}
	return checks
}

func stateFromHealth(h Health) CheckState {
	switch h {
	case HealthGood:
		return CheckPass
	case HealthWarning:
		return CheckWarn
	case HealthFailing:
		return CheckFail
	}
	return CheckUnknown
}

// Score is a 0–100 health score derived from the checks: it starts at 100
// and loses points for what is wrong, weighted by how much it matters. A
// failed check costs far more than a watch item, and an unknown reading
// costs nothing — a drive that reports less is not a drive in worse shape.
// The second return is false when nothing could be measured at all, in
// which case there is no score to show.
func (d Disk) Score(warnTemp float64) (int, bool) {
	checks := d.Checks(warnTemp)
	score := 100.0
	measured := false
	for _, c := range checks {
		switch c.State {
		case CheckPass:
			measured = true
		case CheckWarn:
			measured = true
			// Wear and capacity are the slow-moving ones: a warning there
			// is worth less than a failing SMART verdict.
			switch c.Name {
			case "容量使用", "通电时长", "写入寿命":
				score -= 6
			default:
				score -= 12
			}
		case CheckFail:
			measured = true
			score -= 45
		}
	}
	// The drive's own verdict caps the score: a failing SMART status cannot
	// read as 90 just because everything else looked fine.
	if d.Health == HealthFailing && score > 40 {
		score = 40
	}
	if !measured {
		return 0, false
	}
	if score < 0 {
		score = 0
	}
	return int(math.Round(score)), true
}

// ScoreLabel names the score's band, for the ring's caption and the badge.
func ScoreLabel(score int) string {
	switch {
	case score >= 90:
		return "健康"
	case score >= 70:
		return "良好"
	case score >= 50:
		return "注意"
	}
	return "危险"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
