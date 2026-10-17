//go:build darwin

package main

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

// sysDiskHealth reads the Mac's physical disks. SMART status and temperature
// come from system_profiler (SATA) and the NVMe section; smartctl, when the
// user has it installed, adds lifetime read/write counters and a reliable
// temperature for both kinds.
func sysDiskHealth() []DiskInfo {
	raw := sysProfilerDisks()
	nvme := sysProfilerNVMe()
	out := []DiskInfo{}

	// The SATA/ATA branch: each device under the SMART tree.
	for _, s := range raw {
		if !s.SMART {
			continue
		}
		d := DiskInfo{
			Name:        s.Name,
			Model:       s.Model,
			Serial:      s.Serial,
			Interface:   "SATA",
			Media:       "机械硬盘",
			Temperature: -1,
			Health:      "未知",
		}
		if len(s.BayName) > 0 {
			d.Name = s.BayName[0].Name
		}
		d.Health = macHealth(s.SMARTStatus)
		d.HealthOK = s.SMARTStatus == "Verified" || s.SMARTStatus == "Passed"
		if s.SMARTStatus != "Verified" && s.SMARTStatus != "Passed" &&
			s.SMARTStatus != "" {
			d.Health = "警告"
		}
		if s.SMARTFatalError {
			d.Health = "警告"
			d.HealthOK = false
		}
		d.Size = macSize(s.BayName)
		fillVolume(d.Name, &d)
		out = append(out, d)
	}

	// NVMe branch: each device under the NVMe tree.
	for _, n := range nvme {
		d := DiskInfo{
			Name:        n.Name,
			Model:       n.Model,
			Serial:      n.Serial,
			Interface:   "NVMe",
			Media:       "固态硬盘",
			Temperature: -1,
			Health:      "未知",
		}
		if len(n.DeviceNames) > 0 {
			d.Name = n.DeviceNames[0].Name
		}
		if n.Temp != "" {
			if t, err := strconv.ParseFloat(strings.TrimSuffix(n.Temp, "°C"), 64); err == nil {
				d.Temperature = t
			}
		}
		d.Size = macSize(n.DeviceNames)
		fillVolume(d.Name, &d)
		out = append(out, d)
	}

	// smartctl, when available, adds cumulative read/write and a real
	// temperature for ATA disks.
	smartctlDisks(out)
	return out
}

// fillVolume attaches the mounted volume's capacity to d when its device
// name matches a mounted volume.
func fillVolume(name string, d *DiskInfo) {
	vols := sysVolumes()
	for _, v := range vols {
		if strings.Contains(v.Path, name) || strings.HasSuffix(v.Path, name) {
			if free, total, ok := volumeCap(v.Path); ok {
				d.Free, d.Total = free, total
			}
			return
		}
	}
}

// sysProfilerDisks runs system_profiler's SATA section and returns the SMART
// devices it lists.
func sysProfilerDisks() []spSATA {
	out, err := exec.Command("system_profiler", "SPSerialATADataType", "-json").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var root struct {
		Disks []spSATA `json:"SPSerialATADataType"`
	}
	_ = json.Unmarshal(out, &root)
	return root.Disks
}

// spSATA is one device in system_profiler's SATA output.
type spSATA struct {
	Name              string  `json:"_name"`
	Model             string  `json:"Model"`
	Serial            string  `json:"SerialNumber"`
	SMART             bool    `json:"SMART"`
	SMARTStatus       string  `json:"SMARTStatus"`
	SMARTFatalError   bool    `json:"SMARTFatalError"`
	Capacity          string  `json:"Size"`
	BayName           []spBay `json:"bay_name"`
	DeviceDescription []spBay `json:"device_description"`
}

type spBay struct {
	Name  string `json:"_name"`
	Size  string `json:"Size"`
	Media string `json:"Media"`
}

// macSize parses system_profiler's "128.0 GB" capacity string into bytes.
func macSize(bays []spBay) int64 {
	for _, b := range bays {
		if b.Size != "" {
			if n, err := parseProfilerSize(b.Size); err == nil {
				return n
			}
		}
	}
	return 0
}

func parseProfilerSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "TB"):
		mult = 1e12
		s = strings.TrimSuffix(s, "TB")
	case strings.HasSuffix(s, "GB"):
		mult = 1e9
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		mult = 1e6
		s = strings.TrimSuffix(s, "MB")
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

// sysProfilerNVMe runs system_profiler's NVMe section.
func sysProfilerNVMe() []spNVMe {
	out, err := exec.Command("system_profiler", "SPNVMeDataType", "-json").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var root struct {
		Disks []spNVMe `json:"SPNVMeDataType"`
	}
	_ = json.Unmarshal(out, &root)
	return root.Disks
}

type spNVMe struct {
	Name        string  `json:"_name"`
	Model       string  `json:"Model"`
	Serial      string  `json:"SerialNumber"`
	Temp        string  `json:"Temperature"`
	DeviceNames []spBay `json:"device_names"`
}

// macHealth maps system_profiler's SMART status to the display string.
func macHealth(s string) string {
	switch {
	case s == "Verified", s == "Passed":
		return "良好"
	case s == "Failing":
		return "警告"
	}
	if s != "" {
		return "警告"
	}
	return "未知"
}

// smartctlDisks merges smartctl data into the disks it recognises.
func smartctlDisks(disks []DiskInfo) {
	smartctl, err := exec.LookPath("smartctl")
	if err != nil {
		return
	}
	// Find the device names: /dev/disk0, /dev/disk1, ...
	for i := range disks {
		dev := "/dev/" + disks[i].Name
		out, err := exec.Command(smartctl, "-A", "-j", dev).Output()
		if err != nil {
			continue
		}
		var j struct {
			Temperature struct {
				Current float64 `json:"current"`
			} `json:"temperature"`
			SmartStatus struct {
				Passed bool `json:"passed"`
			} `json:"smart_status"`
			ModelName    string `json:"model_name"`
			SerialNumber string `json:"serial_number"`
			UserCapacity struct {
				Bytes int64 `json:"bytes"`
			} `json:"user_capacity"`
		}
		if err := json.Unmarshal(out, &j); err != nil {
			continue
		}
		if j.Temperature.Current > 0 {
			disks[i].Temperature = j.Temperature.Current
		}
		if j.SmartStatus.Passed {
			disks[i].Health = "良好"
			disks[i].HealthOK = true
		} else {
			disks[i].Health = "警告"
			disks[i].HealthOK = false
		}
		if j.ModelName != "" {
			disks[i].Model = j.ModelName
		}
		if j.SerialNumber != "" {
			disks[i].Serial = j.SerialNumber
		}
		if j.UserCapacity.Bytes > 0 {
			disks[i].Size = j.UserCapacity.Bytes
		}
	}
}
