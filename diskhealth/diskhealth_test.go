package diskhealth

import (
	"testing"
	"time"
)

// TestSmartKeysCounter checks the paired-counter reading: a drive that
// reports only the low word reads as that word, a missing pair reads as
// unreported rather than as zero, and the high word shifts into place.
func TestSmartKeysCounter(t *testing.T) {
	k := smartKeys{
		"DATA_UNITS_READ_0":    10,
		"DATA_UNITS_WRITTEN_0": 5,
		"DATA_UNITS_WRITTEN_1": 1,
	}
	if v, ok := k.counter(keyDataUnitsRead); !ok || v != 10 {
		t.Fatalf("low word only = %v,%v, want 10,true", v, ok)
	}
	if v, ok := k.counter(keyDataUnitsWritten); !ok || v != 5+(1<<32) {
		t.Fatalf("paired counter = %v,%v, want %d", v, ok, 5+(1<<32))
	}
	if _, ok := k.counter(keyMediaErrors); ok {
		t.Fatal("absent counter reported as present")
	}
}

// TestNvmeUnit pins the unit size the spec defines and this machine
// measured: one unit is 1000 sectors of 512 bytes.
func TestNvmeUnit(t *testing.T) {
	if nvmeUnit != 512000 {
		t.Fatalf("nvmeUnit = %d, want 512000", nvmeUnit)
	}
}

// TestDiskFromSMART checks the macOS mapping from diskutil's SMART keys to
// the model: kelvin to Celsius, the wear figures, and 未知 for what the
// drive did not report.
func TestDiskFromSMART(t *testing.T) {
	info := &diskInfo{
		MediaName:   "APPLE SSD AP0256Z",
		BusProtocol: "Apple Fabric",
		SolidState:  true,
		Internal:    true,
		Size:        251000193024,
		SMARTStatus: "Verified",
		SMART: smartKeys{
			"DATA_UNITS_READ_0":         1000,
			"DATA_UNITS_WRITTEN_0":      2000,
			"POWER_ON_HOURS_0":          1624,
			"POWER_CYCLES_0":            153,
			"UNSAFE_SHUTDOWNS_0":        18,
			"MEDIA_ERRORS_0":            0,
			"PERCENTAGE_USED":           4,
			"AVAILABLE_SPARE":           100,
			"AVAILABLE_SPARE_THRESHOLD": 99,
			"TEMPERATURE":               322,
		},
	}
	d := diskFromInfo(wholeDisk{ID: "disk0", Size: info.Size}, info)

	if d.Bus != "NVMe" {
		t.Fatalf("bus = %q, want NVMe for Apple Fabric", d.Bus)
	}
	if d.ReadBytes != 1000*nvmeUnit || d.WriteBytes != 2000*nvmeUnit {
		t.Fatalf("bytes = %d/%d", d.ReadBytes, d.WriteBytes)
	}
	if d.PowerOnHours != 1624 || d.PowerCycles != 153 || d.UnsafeShutdowns != 18 {
		t.Fatalf("counters = %+v", d)
	}
	// Zero media errors is an answer, not a missing field.
	if d.MediaErrors != 0 {
		t.Fatalf("media errors = %d, want 0", d.MediaErrors)
	}
	if !d.LifeKnown || d.LifeUsed != 4 || d.SparePercent != 100 {
		t.Fatalf("life = %+v", d)
	}
	// 322 K is 48.85 °C; the label rounds to whole degrees.
	if !d.HasTemperature() || d.TemperatureLabel() != "49 °C" {
		t.Fatalf("temperature = %v %q", d.TempKnown, d.TemperatureLabel())
	}
	if d.Health != HealthGood {
		t.Fatalf("health = %v, want good", d.Health)
	}
	if d.HealthNote != "SMART 已校验" {
		t.Fatalf("health note = %q", d.HealthNote)
	}
}

// TestDiskFromBareInfo checks a drive that reports little: the counters
// stay at their "unknown" sentinels so the page leaves them out, and the
// health is 未知 rather than 良好.
func TestDiskFromBareInfo(t *testing.T) {
	d := diskFromInfo(wholeDisk{ID: "disk9", Size: 1000}, &diskInfo{})
	if d.ReadBytes != -1 || d.PowerOnHours != -1 || d.MediaErrors != -1 {
		t.Fatalf("sentinels = %+v", d)
	}
	if d.LifeKnown || d.HasTemperature() {
		t.Fatalf("a bare drive reported life/temperature: %+v", d)
	}
	if d.Health != HealthUnknown {
		t.Fatalf("health = %v, want unknown", d.Health)
	}
	if d.TemperatureLabel() != "未知" || d.LifeLabel() != "未知" {
		t.Fatalf("labels = %q / %q", d.TemperatureLabel(), d.LifeLabel())
	}
}

// TestWornDriveWarns checks that a drive past its spare threshold stops
// reading 良好 even though SMART still says Verified.
func TestWornDriveWarns(t *testing.T) {
	info := &diskInfo{
		SMARTStatus: "Verified",
		SMART: smartKeys{
			"PERCENTAGE_USED":           90,
			"AVAILABLE_SPARE":           5,
			"AVAILABLE_SPARE_THRESHOLD": 99,
		},
	}
	d := diskFromInfo(wholeDisk{ID: "disk0"}, info)
	if d.Health != HealthWarning {
		t.Fatalf("health = %v, want warning below the spare threshold", d.Health)
	}
	if d.HealthNote != "备用块低于阈值" {
		t.Fatalf("note = %q", d.HealthNote)
	}
}

// TestFailingSMART checks that a failing status outranks a healthy spare
// pool.
func TestFailingSMART(t *testing.T) {
	info := &diskInfo{
		SMARTStatus: "Failing",
		SMART: smartKeys{
			"AVAILABLE_SPARE":           100,
			"AVAILABLE_SPARE_THRESHOLD": 99,
		},
	}
	if d := diskFromInfo(wholeDisk{ID: "disk0"}, info); d.Health != HealthFailing {
		t.Fatalf("health = %v, want failing", d.Health)
	}
}

// TestMisSetTemperatureIgnored checks that a sensor reading below 200 K —
// clearly not a real drive temperature — is treated as unavailable rather
// than shown as -100 °C.
func TestMisSetTemperatureIgnored(t *testing.T) {
	info := &diskInfo{SMART: smartKeys{"TEMPERATURE": 120}}
	if d := diskFromInfo(wholeDisk{ID: "disk0"}, info); d.HasTemperature() {
		t.Fatalf("mis-set sensor reported %.1f °C", d.Temperature)
	}
}

// TestCapacityAndUsedRatio checks the capacity arithmetic, including the
// unmounted case where free is -1.
func TestCapacityAndUsedRatio(t *testing.T) {
	d := Disk{Total: 100, Free: 25}
	if d.UsedBytes() != 75 || d.UsedRatio() != 0.75 {
		t.Fatalf("used = %d %v", d.UsedBytes(), d.UsedRatio())
	}
	// Free unknown: no ratio, and the bar draws nothing.
	d = Disk{Total: 100, Free: -1}
	if d.UsedRatio() != 0 || d.UsedBytes() != 0 {
		t.Fatalf("unknown free produced %v / %d", d.UsedRatio(), d.UsedBytes())
	}
	// A free figure above total (a resized pool) clamps instead of
	// drawing a bar past its track.
	d = Disk{Total: 100, Free: 150}
	if r := d.UsedRatio(); r != 0 {
		t.Fatalf("ratio = %v, want 0", r)
	}
}

// TestParseMountLine covers both mount table shapes and the kernel's
// octal escapes.
func TestParseMountLine(t *testing.T) {
	cases := []struct {
		line string
		want mountEntry
		ok   bool
	}{
		{
			"/dev/disk3s5 on /System/Volumes/Data (apfs, local, journaled, nobrowse)",
			mountEntry{From: "/dev/disk3s5", On: "/System/Volumes/Data", FS: "apfs"}, true,
		},
		{
			"/dev/sda1 / ext4 rw,relatime 0 0",
			mountEntry{From: "/dev/sda1", On: "/", FS: "ext4"}, true,
		},
		{
			"/dev/disk4s1 on /Volumes/Macintosh\\040HD (exfat, local, nodev)",
			mountEntry{From: "/dev/disk4s1", On: "/Volumes/Macintosh HD", FS: "exfat"}, true,
		},
		{"# a comment", mountEntry{}, false},
		{"", mountEntry{}, false},
	}
	for _, c := range cases {
		got, ok := parseMountLine(c.line)
		if ok != c.ok {
			t.Fatalf("parseMountLine(%q) ok = %v, want %v", c.line, ok, c.ok)
		}
		if got != c.want {
			t.Fatalf("parseMountLine(%q) = %+v, want %+v", c.line, got, c.want)
		}
	}
}

// TestParseMountsSkipsNoise checks that a whole table parses and the
// non-mount lines do not become entries.
func TestParseMountsSkipsNoise(t *testing.T) {
	text := "# comment\n/dev/sda1 / ext4 rw 0 0\n\n/dev/sdb1 /mnt/data xfs rw 0 0\n"
	got := parseMounts(text)
	if len(got) != 2 {
		t.Fatalf("parsed %d entries, want 2: %+v", len(got), got)
	}
	if got[1].On != "/mnt/data" || got[1].FS != "xfs" {
		t.Fatalf("second entry = %+v", got[1])
	}
}

// TestIsPartitionOf covers the three partition naming schemes Linux uses.
func TestIsPartitionOf(t *testing.T) {
	cases := []struct {
		device, disk string
		want         bool
	}{
		{"/dev/sda1", "sda", true},
		{"/dev/sda", "sda", false},
		{"/dev/nvme0n1p1", "nvme0n1", true},
		{"/dev/nvme0n1", "nvme0n1", false},
		{"/dev/mmcblk0p2", "mmcblk0", true},
		{"/dev/sdb1", "sda", false},
		{"/dev/sda12", "sda", true},
		{"/dev/sdabc", "sda", false},
	}
	for _, c := range cases {
		if got := isPartitionOf(c.device, c.disk); got != c.want {
			t.Fatalf("isPartitionOf(%q,%q) = %v, want %v", c.device, c.disk, got, c.want)
		}
	}
}

// TestAddThroughput checks the sampled rates: a delta over the elapsed
// seconds, nothing for a disk new to the probe, and nothing for a counter
// that went backwards.
func TestAddThroughput(t *testing.T) {
	prev := map[string]Disk{
		"disk0": {ID: "disk0", ReadBytes: 1000, WriteBytes: 2000},
		"disk1": {ID: "disk1", ReadBytes: 5000, WriteBytes: 5000},
	}
	now := time.Now()
	disks := []Disk{
		{ID: "disk0", ReadBytes: 3000, WriteBytes: 4000},
		{ID: "disk1", ReadBytes: 4000, WriteBytes: 5000}, // read went backwards
		{ID: "disk2", ReadBytes: 100, WriteBytes: 100},   // new to this probe
	}
	addThroughput(disks, prev, now.Add(-2*time.Second), now)

	if disks[0].ReadBps != 1000 || disks[0].WriteBps != 1000 {
		t.Fatalf("disk0 rates = %v/%v, want 1000/1000", disks[0].ReadBps, disks[0].WriteBps)
	}
	if disks[1].ReadBps != 0 || disks[1].WriteBps != 0 {
		t.Fatalf("disk1 rates = %v/%v, want 0/0", disks[1].ReadBps, disks[1].WriteBps)
	}
	if disks[2].ReadBps != 0 {
		t.Fatalf("new disk got a rate: %v", disks[2].ReadBps)
	}
}

// TestAddThroughputWithoutHistory checks the first probe: with nothing to
// compare against, no rate is invented.
func TestAddThroughputWithoutHistory(t *testing.T) {
	disks := []Disk{{ID: "disk0", ReadBytes: 1000}}
	addThroughput(disks, nil, time.Time{}, time.Now())
	if disks[0].ReadBps != 0 {
		t.Fatalf("first probe produced a rate: %v", disks[0].ReadBps)
	}
}

// TestHealthLabels pins the display strings the page and the copied report
// share.
func TestHealthLabels(t *testing.T) {
	cases := map[Health]string{
		HealthGood:    "良好",
		HealthWarning: "警告",
		HealthFailing: "故障",
		HealthUnknown: "未知",
	}
	for h, want := range cases {
		if got := h.String(); got != want {
			t.Fatalf("%d.String() = %q, want %q", h, got, want)
		}
	}
}

// TestChecksReportEveryMeasurableIndicator checks the diagnostics list: a
// healthy drive passes, the temperature check follows the caller's
// threshold, and a switched-off threshold (0) does not fail a warm drive.
func TestChecksReportEveryMeasurableIndicator(t *testing.T) {
	d := Disk{
		Health: HealthGood, HealthNote: "SMART 已校验",
		Temperature: 49, TempKnown: true,
		LifeKnown: true, LifeUsed: 4, SparePercent: 100, SpareThreshold: 99,
		MediaErrors: 0, PowerOnHours: 1624,
		Total: 100, Free: 5,
	}
	checks := d.Checks(50)
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	for _, name := range []string{"SMART 状态", "温度", "备用块", "写入寿命", "介质错误", "通电时长", "容量使用"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("check %q missing: %+v", name, checks)
		}
	}
	if byName["温度"].State != CheckPass {
		t.Fatalf("49 °C at a 50 °C threshold = %v", byName["温度"].State)
	}
	// Capacity at 95% is a watch item, with its explanation.
	if byName["容量使用"].State != CheckWarn || byName["容量使用"].Note == "" {
		t.Fatalf("95%% capacity = %+v", byName["容量使用"])
	}

	// A threshold below the reading flags it; a zero threshold means the
	// user switched the warning off, which must not fail the drive.
	if c := byName["温度"]; d.Checks(40)[1].State != CheckWarn {
		t.Fatalf("49 °C at a 40 °C threshold = %v", c.State)
	}
	for _, c := range d.Checks(0) {
		if c.Name == "温度" && c.State != CheckPass {
			t.Fatalf("temperature failed with the warning off: %+v", c)
		}
	}
}

// TestChecksOmitWhatTheDriveWithheld checks that a drive reporting little
// gets 未知 rows rather than zero-valued passes.
func TestChecksOmitWhatTheDriveWithheld(t *testing.T) {
	d := Disk{Health: HealthUnknown, ReadBytes: -1, WriteBytes: -1,
		PowerOnHours: -1, MediaErrors: -1}
	checks := d.Checks(50)
	for _, c := range checks {
		if c.State == CheckPass && (c.Name == "介质错误" || c.Name == "通电时长") {
			t.Fatalf("unreported %s read as a pass: %+v", c.Name, c)
		}
	}
}

// TestScoreBands checks the score's arithmetic and its bands: a clean
// drive scores 100, a watch item costs a little, a failure costs a lot,
// and a drive with nothing measurable has no score at all.
func TestScoreBands(t *testing.T) {
	clean := Disk{
		Health: HealthGood, HealthNote: "SMART 已校验",
		Temperature: 40, TempKnown: true,
		LifeKnown: true, LifeUsed: 4, SparePercent: 100, SpareThreshold: 99,
		MediaErrors: 0, PowerOnHours: 1600, Total: 100, Free: 50,
	}
	if got, ok := clean.Score(50); !ok || got != 100 {
		t.Fatalf("clean drive score = %d,%v, want 100,true", got, ok)
	}
	// Nearly full: the capacity check's warning costs 6 points.
	full := clean
	full.Free = 2
	if got, _ := full.Score(50); got != 94 {
		t.Fatalf("nearly full score = %d, want 94", got)
	}
	// A failing SMART verdict caps the score regardless of everything else.
	failing := clean
	failing.Health = HealthFailing
	if got, _ := failing.Score(50); got > 40 {
		t.Fatalf("failing drive scored %d, want at most 40", got)
	}
	// Nothing measured: no score to show.
	if _, ok := (Disk{Health: HealthUnknown, ReadBytes: -1, WriteBytes: -1,
		PowerOnHours: -1, MediaErrors: -1, Total: 0}).Score(50); ok {
		t.Fatal("a drive with no measurable indicator produced a score")
	}
}

// TestScoreLabels pins the band names the badge shows.
func TestScoreLabels(t *testing.T) {
	cases := map[int]string{100: "健康", 90: "健康", 85: "良好", 70: "良好", 60: "注意", 50: "注意", 20: "危险"}
	for score, want := range cases {
		if got := ScoreLabel(score); got != want {
			t.Fatalf("ScoreLabel(%d) = %q, want %q", score, got, want)
		}
	}
}

// TestAppendHistory checks the sample ring: only disks with a known rate
// contribute, and the list is capped.
func TestAppendHistory(t *testing.T) {
	at := time.Now()
	disks := []Disk{
		{ID: "a", ReadBps: 10, WriteBps: 5, RateKnown: true},
		{ID: "b", ReadBps: 0, WriteBps: 0}, // no rate yet
	}
	hist := appendHistory(nil, disks, at)
	if len(hist["a"]) != 1 || hist["a"][0].Read != 10 {
		t.Fatalf("history a = %+v", hist["a"])
	}
	if _, ok := hist["b"]; ok {
		t.Fatal("a disk without a rate got a sample")
	}
	// The cap holds: historyLen samples at most.
	for i := 0; i < historyLen+10; i++ {
		hist = appendHistory(hist, disks, at.Add(time.Duration(i)*time.Second))
	}
	if got := len(hist["a"]); got != historyLen {
		t.Fatalf("history kept %d samples, want %d", got, historyLen)
	}
}

// TestHistorySpan checks the window the chart's caption reports.
func TestHistorySpan(t *testing.T) {
	at := time.Now()
	hist := map[string][]Sample{
		"a": {{At: at, Read: 1}, {At: at.Add(2 * time.Second), Read: 2}},
		"b": {{At: at, Read: 1}},
	}
	if got := historySpan(hist); got != 2*time.Second {
		t.Fatalf("span = %v, want 2s", got)
	}
	if got := historySpan(map[string][]Sample{"b": hist["b"]}); got != 0 {
		t.Fatalf("single sample span = %v, want 0", got)
	}
}
