package diskhealth

import (
	"encoding/json"
	"testing"
)

// TestMergeSmartctlJSON feeds a fixed smartctl report through the shared
// decoder. It keeps the Linux path covered without requiring smartctl or a
// real drive, matching the fields smartctl writes on every platform.
func TestMergeSmartctlJSON(t *testing.T) {
	fixture := []byte(`{
		"model_name": "TEST NVME",
		"serial_number": "SN-42",
		"firmware_version": "1.2.3",
		"smart_status": {"passed": true},
		"temperature": {"current": 41},
		"power_on_time": {"hours": 1624},
		"power_cycle_count": 153,
		"unsafe_shutdown_count": 18,
		"nvme_smart_health_information_log": {
			"data_units_read": 1000,
			"data_units_written": 2000,
			"percentage_used": 4,
			"available_spare": 100,
			"available_spare_threshold": 99,
			"media_errors": 2
		}
	}`)
	var report smartctlJSON
	if err := json.Unmarshal(fixture, &report); err != nil {
		t.Fatal(err)
	}
	d := Disk{Health: HealthUnknown, ReadBytes: -1, WriteBytes: -1,
		PowerOnHours: -1, PowerCycles: -1, UnsafeShutdowns: -1, MediaErrors: -1}
	mergeSmartctl(&d, &report)

	if d.Model != "TEST NVME" || d.Serial != "SN-42" || d.Firmware != "1.2.3" {
		t.Fatalf("identity = %+v", d)
	}
	if !d.TempKnown || d.Temperature != 41 {
		t.Fatalf("temperature = %v/%v", d.Temperature, d.TempKnown)
	}
	if d.PowerOnHours != 1624 || d.PowerCycles != 153 || d.UnsafeShutdowns != 18 {
		t.Fatalf("counters = %+v", d)
	}
	if d.ReadBytes != 1000*nvmeUnit || d.WriteBytes != 2000*nvmeUnit {
		t.Fatalf("byte counters = %d/%d", d.ReadBytes, d.WriteBytes)
	}
	if !d.LifeKnown || d.LifeUsed != 4 || d.SparePercent != 100 || d.SpareThreshold != 99 {
		t.Fatalf("life = %+v", d)
	}
	// A passed SMART verdict above a platform value of 未知 remains 未知:
	// mergeSmartctl only overrides health when the drive fails.
	if d.MediaErrors != 2 || d.Health != HealthUnknown {
		t.Fatalf("health/media = %v/%d", d.Health, d.MediaErrors)
	}
}
