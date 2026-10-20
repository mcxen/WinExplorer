//go:build darwin

package diskhealth

import "testing"

// These tests exercise diskutil's macOS-specific key mapping. Keeping them
// behind the platform build tag lets the shared diskhealth tests compile
// and run on Linux, where diskInfo and smartKeys do not exist.

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
