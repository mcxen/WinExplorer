//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// sysDiskHealth reads the Linux machine's block devices from sysfs, merging
// smartctl output when it is installed.
func sysDiskHealth() []DiskInfo {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil
	}
	var disks []DiskInfo
	for _, e := range entries {
		name := e.Name()
		// Skip loop, ram, dm- and zram devices: they are not physical.
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "zram") ||
			strings.HasPrefix(name, "sr") {
			continue
		}
		base := filepath.Join("/sys/block", name)
		d := DiskInfo{
			Name:        name,
			Interface:   ifaceFromName(name),
			Temperature: -1,
			Health:      "未知",
		}
		if b, err := os.ReadFile(filepath.Join(base, "device", "model")); err == nil {
			d.Model = strings.TrimSpace(string(b))
		}
		if b, err := os.ReadFile(filepath.Join(base, "device", "serial")); err == nil {
			d.Serial = strings.TrimSpace(string(b))
		}
		if b, err := os.ReadFile(filepath.Join(base, "size")); err == nil {
			if n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
				d.Size = n * 512
			}
		}
		if b, err := os.ReadFile(filepath.Join(base, "queue", "rotational")); err == nil {
			if strings.TrimSpace(string(b)) == "0" {
				d.Media = "固态硬盘"
			} else {
				d.Media = "机械硬盘"
			}
		}
		// NVMe devices carry their own interface name.
		if strings.HasPrefix(name, "nvme") {
			d.Interface = "NVMe"
		}
		// SMART health from sysfs when the device reports it.
		if b, err := os.ReadFile(filepath.Join(base, "device", "smart", "health")); err == nil {
			s := strings.TrimSpace(string(b))
			if s == "PASSED" || s == "passed" {
				d.Health = "良好"
				d.HealthOK = true
			} else if s == "FAILED" || s == "failed" {
				d.Health = "警告"
			}
		}
		// Mount capacity: find the first mounted partition.
		fillLinuxVolume(name, &d)
		disks = append(disks, d)
	}
	smartctlLinux(disks)
	return disks
}

// fillLinuxVolume attaches the mounted volume's capacity to d when one of its
// partitions is mounted.
func fillLinuxVolume(name string, d *DiskInfo) {
	out, err := exec.Command("df", "-B1", "--output=source,avail,size").Output()
	if err != nil {
		return
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		src := fields[0]
		if !strings.HasPrefix(src, "/dev/") {
			continue
		}
		if !strings.HasPrefix(filepath.Base(src), name) {
			continue
		}
		avail, err1 := strconv.ParseInt(fields[1], 10, 64)
		total, err2 := strconv.ParseInt(fields[2], 10, 64)
		if err1 == nil && err2 == nil && total > 0 {
			d.Free, d.Total = avail, total
		}
		return
	}
}

// ifaceFromName guesses the interface from the device node's name.
func ifaceFromName(name string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "NVMe"
	case strings.HasPrefix(name, "sd"):
		return "SATA"
	case strings.HasPrefix(name, "hd"):
		return "IDE"
	case strings.HasPrefix(name, "vd"):
		return "SATA"
	case strings.HasPrefix(name, "mmc"):
		return "eMMC"
	}
	return "未知"
}

// smartctlLinux merges smartctl output into the disks it recognises.
func smartctlLinux(disks []DiskInfo) {
	smartctl, err := exec.LookPath("smartctl")
	if err != nil {
		return
	}
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
	}
}