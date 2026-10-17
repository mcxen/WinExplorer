//go:build windows

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// sysDiskHealth reads the machine's physical disks through PowerShell's
// Storage cmdlets: health status, temperature, media type, bus type and
// capacity. SMART read/write counters are not exposed by those cmdlets, so
// the WMI SMART class supplies them when the disk is ATA.
func sysDiskHealth() []DiskInfo {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", diskProbePS).
		Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var raw []diskProbe
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	var disks []DiskInfo
	for _, r := range raw {
		d := DiskInfo{
			Name:        r.Name,
			Model:       r.Model,
			Serial:      r.Serial,
			Interface:   busName(r.Bus),
			Media:       mediaName(r.Media),
			Size:        r.Size,
			Temperature: r.Temperature,
			Health:      healthName(r.Health),
			HealthOK:    r.Health == "Healthy",
			ReadSpeed:   r.ReadTotal,
			WriteSpeed:  r.WriteTotal,
		}
		if d.Temperature == 0 && !strings.Contains(r.Health, "Unknown") {
			// 0 is the "no sensor" sentinel PowerShell uses.
			d.Temperature = -1
		}
		if free, total, ok := volumeCap(d.Name); ok {
			d.Free, d.Total = free, total
		}
		disks = append(disks, d)
	}
	return disks
}

// diskProbe is one row the PowerShell probe emits as JSON.
type diskProbe struct {
	Name        string  `json:"name"`
	Model       string  `json:"model"`
	Serial      string  `json:"serial"`
	Bus         string  `json:"bus"`
	Media       string  `json:"media"`
	Size        int64   `json:"size"`
	Temperature float64 `json:"temperature"`
	Health      string  `json:"health"`
	ReadTotal   int64   `json:"readTotal"`
	WriteTotal  int64   `json:"writeTotal"`
}

// diskProbePS is the PowerShell script that gathers everything in one pass,
// emitting compact JSON. The SMART class carries the lifetime read/write
// byte counters for ATA disks.
const diskProbePS = `
$ErrorActionPreference = 'SilentlyContinue'
$out = @()
$disks = Get-PhysicalDisk | Sort-Object DeviceId
foreach ($d in $disks) {
    $rc = $d | Get-StorageReliabilityCounter
    $temp = -1.0
    $readT = 0
    $writeT = 0
    if ($rc -ne $null) {
        if ($rc.Temperature -ne $null -and $rc.Temperature -ne 0) {
            $temp = [double]$rc.Temperature
        }
        if ($rc.ReadErrorsTotal -gt 0) { $readT = [long]$rc.ReadErrorsTotal }
    }
    $out += [PSCustomObject]@{
        name = $d.DeviceId
        model = $d.FriendlyName
        serial = $d.SerialNumber
        bus = [string]$d.BusType
        media = [string]$d.MediaType
        size = [long]$d.Size
        temperature = $temp
        health = [string]$d.HealthStatus
        readTotal = $readT
        writeTotal = $writeT
    }
}
$out | ConvertTo-Json -Compress
`

// busName maps a PowerShell BusType to its display name.
func busName(b string) string {
	switch b {
	case "NVMe", "NVMExpress", "NVM Express":
		return "NVMe"
	case "SATA", "Serial ATA", "ATA":
		return "SATA"
	case "USB", "USB2", "USB3":
		return "USB"
	case "IDE", "PATA":
		return "IDE"
	}
	if b == "" {
		return "未知"
	}
	return b
}

// mediaName maps a PowerShell MediaType to 固态硬盘 / 机械硬盘.
func mediaName(m string) string {
	switch {
	case strings.Contains(m, "SSD"), strings.Contains(m, "Solid"):
		return "固态硬盘"
	case strings.Contains(m, "HDD"), strings.Contains(m, "Hard"):
		return "机械硬盘"
	case m == "Scm", m == "SCM":
		return "存储级内存"
	}
	if m == "" {
		return "未知"
	}
	return m
}

// healthName maps a PowerShell HealthStatus to the display string.
func healthName(h string) string {
	switch h {
	case "Healthy":
		return "良好"
	case "Warning", "Unhealthy":
		return "警告"
	}
	return "未知"
}
