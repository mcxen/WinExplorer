package diskhealth

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
)

// smartctlJSON is the subset of `smartctl -a -j`'s report this package
// reads. smartctl writes the same field names on every OS, so one struct
// serves macOS and Linux.
type smartctlJSON struct {
	ModelName    string `json:"model_name"`
	SerialNumber string `json:"serial_number"`
	Firmware     string `json:"firmware_version"`
	SmartStatus  struct {
		Passed bool `json:"passed"`
	} `json:"smart_status"`
	Temperature struct {
		Current float64 `json:"current"`
	} `json:"temperature"`
	PowerOnTime struct {
		Hours int64 `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount int64 `json:"power_cycle_count"`
	UnsafeShutdown  int64 `json:"unsafe_shutdown_count"`
	Nvme            struct {
		DataUnitsRead           int64 `json:"data_units_read"`
		DataUnitsWritten        int64 `json:"data_units_written"`
		PercentageUsed          int64 `json:"percentage_used"`
		AvailableSpare          int64 `json:"available_spare"`
		AvailableSpareThreshold int64 `json:"available_spare_threshold"`
		MediaErrors             int64 `json:"media_errors"`
	} `json:"nvme_smart_health_information_log"`
}

// nvmeUnit is one DATA_UNITS_* tick: 1000 sectors of 512 bytes, per the
// NVMe spec. Measured on this machine against a 369,107,968-byte read:
// 724 units were counted, i.e. 370.7 MB, the spec's decimal rounding.
const nvmeUnit = 1000 * 512

// smartctlMerge runs smartctl against each disk and fills in whatever the
// platform's own probe could not reach — usually temperature, counters and
// wear on drives whose controller hides them. It reports whether any disk
// answered, so the report can say which tools it drew on.
func smartctlMerge(ctx context.Context, disks []Disk) bool {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		return false
	}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		used bool
	)
	for i := range disks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := exec.CommandContext(ctx, path, "-a", "-j", "/dev/"+disks[i].ID).Output()
			// smartctl exits non-zero for a drive that answered but
			// reported a problem, so the JSON is what matters, not err.
			if len(out) == 0 || (err != nil && !json.Valid(out)) {
				return
			}
			var j smartctlJSON
			if json.Unmarshal(out, &j) != nil {
				return
			}
			mergeSmartctl(&disks[i], &j)
			mu.Lock()
			used = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return used
}

// mergeSmartctl folds one drive's smartctl report into its model. Fields
// smartctl did not answer keep the platform's values.
func mergeSmartctl(d *Disk, j *smartctlJSON) {
	if j.ModelName != "" {
		d.Model = j.ModelName
	}
	if j.SerialNumber != "" {
		d.Serial = j.SerialNumber
	}
	if j.Firmware != "" {
		d.Firmware = j.Firmware
	}
	if j.Temperature.Current > 0 {
		d.Temperature = j.Temperature.Current
		d.TempKnown = true
	}
	if j.PowerOnTime.Hours > 0 {
		d.PowerOnHours = j.PowerOnTime.Hours
	}
	if j.PowerCycleCount > 0 {
		d.PowerCycles = j.PowerCycleCount
	}
	if j.UnsafeShutdown > 0 {
		d.UnsafeShutdowns = j.UnsafeShutdown
	}
	// smartctl's NVMe log uses the same 1000 × 512-byte unit.
	if j.Nvme.DataUnitsRead > 0 {
		d.ReadBytes = j.Nvme.DataUnitsRead * nvmeUnit
	}
	if j.Nvme.DataUnitsWritten > 0 {
		d.WriteBytes = j.Nvme.DataUnitsWritten * nvmeUnit
	}
	if j.Nvme.PercentageUsed > 0 || j.Nvme.AvailableSpare > 0 {
		d.LifeKnown = true
		d.LifeUsed = float64(j.Nvme.PercentageUsed)
		d.SparePercent = float64(j.Nvme.AvailableSpare)
		d.SpareThreshold = float64(j.Nvme.AvailableSpareThreshold)
	}
	if j.Nvme.MediaErrors > 0 {
		d.MediaErrors = j.Nvme.MediaErrors
	}
	if !j.SmartStatus.Passed {
		d.Health = HealthFailing
		d.HealthNote = "SMART 检测未通过"
	}
}
