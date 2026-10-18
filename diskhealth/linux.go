//go:build linux

package diskhealth

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// This file reads a Linux machine's block devices from sysfs. sysfs alone
// gives the identity, the media type and SMART pass/fail; everything
// richer — counters, temperature, wear — needs smartctl, because the
// kernel exposes no portable attribute interface.

const probeTimeout = 6 * time.Second

func probe() Report {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	r := Report{At: time.Now(), Sources: []string{"/sys/block"}}
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return Report{At: r.At, Notes: []string{"无法读取 /sys/block"}}
	}
	for _, e := range entries {
		name := e.Name()
		if !isPhysicalBlock(name) {
			continue
		}
		d := sysfsDisk(name)
		d.Volumes, d.Free, d.Total = linuxVolumes(name)
		r.Disks = append(r.Disks, d)
	}
	if smartctlMerge(ctx, r.Disks) {
		r.Sources = append(r.Sources, "smartctl")
	}
	return r
}

// isPhysicalBlock keeps the real devices and drops the kernel's synthetic
// ones: loop mounts, RAM disks, device-mapper, zram, software RAID, the
// network block devices and the optical drive.
func isPhysicalBlock(name string) bool {
	for _, p := range []string{"loop", "ram", "dm-", "zram", "sr", "md", "nbd"} {
		if strings.HasPrefix(name, p) {
			return false
		}
	}
	return true
}

// sysfsDisk reads one device's identity and health from sysfs.
func sysfsDisk(name string) Disk {
	base := filepath.Join("/sys/block", name)
	d := Disk{
		ID:              name,
		Bus:             busFromName(name),
		Media:           mediaFromRotational(base),
		Health:          HealthUnknown,
		ReadBytes:       -1,
		WriteBytes:      -1,
		ReadOps:         -1,
		WriteOps:        -1,
		PowerOnHours:    -1,
		PowerCycles:     -1,
		UnsafeShutdowns: -1,
		MediaErrors:     -1,
		Source:          "/sys/block",
	}
	d.Model = readTrimmed(filepath.Join(base, "device", "model"))
	d.Serial = readTrimmed(filepath.Join(base, "device", "serial"))
	d.Firmware = readTrimmed(filepath.Join(base, "device", "rev"))
	if n, err := strconv.ParseInt(readTrimmed(filepath.Join(base, "size")), 10, 64); err == nil {
		d.Size = n * 512 // sysfs size counts 512-byte sectors
	}
	switch readTrimmed(filepath.Join(base, "device", "smart", "health")) {
	case "PASSED", "passed", "OK":
		d.Health = HealthGood
		d.HealthNote = "SMART PASSED"
	case "FAILED", "failed":
		d.Health = HealthFailing
		d.HealthNote = "SMART FAILED"
	}
	if readTrimmed(filepath.Join(base, "removable")) == "1" {
		d.Removable = true
	}
	// NVMe publishes its temperature in millidegrees through hwmon, and
	// its controller counters are not exposed by sysfs at all.
	if t, err := strconv.ParseFloat(
		readTrimmed(filepath.Join(base, "device", "hwmon", "hwmon0", "temp1_input")), 64); err == nil && t > 0 {
		d.Temperature = t / 1000
		d.TempKnown = true
	}
	d.Internal = !d.Removable
	return d
}

// mediaFromRotational says 固态硬盘 for non-rotating media. A device
// without the file (some USB bridges) is assumed solid-state: reporting
// 机械硬盘 for a flash drive would be the worse guess.
func mediaFromRotational(base string) string {
	if readTrimmed(filepath.Join(base, "queue", "rotational")) == "1" {
		return "机械硬盘"
	}
	return "固态硬盘"
}

// busFromName guesses the bus from the kernel's device name. It is a
// convention, not a fact: sdX is SCSI, which covers SATA, USB and SAS
// alike, so SATA is the common case rather than a certainty.
func busFromName(name string) string {
	switch {
	case strings.HasPrefix(name, "nvme"):
		return "NVMe"
	case strings.HasPrefix(name, "mmc"):
		return "eMMC"
	case strings.HasPrefix(name, "vd"):
		return "VirtIO"
	case strings.HasPrefix(name, "hd"):
		return "IDE"
	case strings.HasPrefix(name, "sd"):
		return "SATA"
	}
	return "未知"
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// linuxVolumes lists the volumes mounted from a disk and sums their
// capacity. Partitions are named <disk>1 (SATA), <disk>p1 (NVMe, mmc), so
// the match is a prefix plus a digit or a "p".
func linuxVolumes(disk string) (vols []Volume, free, total int64) {
	mounts, err := readMounts()
	if err != nil {
		return nil, -1, 0
	}
	free = -1
	for _, m := range mounts {
		if !isPartitionOf(m.From, disk) {
			continue
		}
		f, t, ok := capacityOf(m.On)
		if !ok {
			f, t = -1, 0
		}
		vols = append(vols, Volume{
			Name:       m.On,
			MountPoint: m.On,
			FS:         m.FS,
			Size:       t,
			Free:       f,
			Used:       t - f,
		})
		if f < 0 || t <= 0 {
			continue
		}
		if free < 0 {
			free = 0
		}
		free += f
		total += t
	}
	return vols, free, total
}
