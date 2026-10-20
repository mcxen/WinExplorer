package main

import (
	"strings"
	"testing"
	"time"

	"sunvale/diskhealth"
)

// TestDiskPageRenders boots the real window view on the 磁盘健康 page with
// a fixed report. The page's layout must not depend on the host having
// readable drives, smartctl, or any particular probe timing.
func TestDiskPageRenders(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	t.Cleanup(a.disksUnwatch)
	report := diskhealth.Report{
		At:      time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
		Sources: []string{"测试数据"},
		Disks: []diskhealth.Disk{{
			ID:           "disk-test",
			Model:        "TEST SSD",
			Bus:          "NVMe",
			Media:        "固态硬盘",
			Size:         512 << 30,
			Internal:     true,
			Health:       diskhealth.HealthGood,
			Temperature:  42,
			TempKnown:    true,
			LifeKnown:    true,
			LifeUsed:     8,
			SparePercent: 100,
			Free:         256 << 30,
			Total:        512 << 30,
			Volumes: []diskhealth.Volume{{
				Name: "System", MountPoint: "/", Size: 512 << 30,
			}},
		}},
	}
	a.diskRead = func() diskhealth.Report { return report }
	a.diskWatch = func() {}
	a.navigate(pathDisks)
	tt.Frame()

	for _, text := range []string{"磁盘健康", "温度", "检测项", "卷构成"} {
		if !tt.HasText(text) {
			t.Fatalf("the page does not show %q: %v", text, tt.Texts())
		}
	}
	// The wear check names itself by what the drive reports: 写入寿命 when it
	// has the counter, 寿命 with 未报告 when it does not — which is every CI
	// runner and most virtual disks.
	if !tt.HasText("写入寿命") && !tt.HasText("寿命") {
		t.Fatalf("the page shows no wear check: %v", tt.Texts())
	}
	if !tt.HasText("可用") && !tt.HasText("已用") {
		t.Fatalf("no capacity line on the card: %v", tt.Texts())
	}
}

// TestDiskWatchStopsOnLeaving checks the page's background probe is torn
// down when the tab leaves: a window must not keep polling disks behind a
// folder it is showing.
func TestDiskWatchStopsOnLeaving(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	a.navigate(pathDisks)
	tt.Frame()
	if a.diskWatch == nil {
		t.Fatal("the page did not start watching")
	}
	a.navigate(dir)
	tt.Frame()
	if a.diskWatch != nil {
		t.Fatal("leaving the page left the probe running")
	}
}

// TestDiskWatchStopsWhenPluginDisabled checks the plugin's own switch stops
// the polling, not just the page's rendering.
func TestDiskWatchStopsWhenPluginDisabled(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	a.navigate(pathDisks)
	tt.Frame()
	if a.diskWatch == nil {
		t.Fatal("the page did not start watching")
	}
	a.pluginSetEnabled(diskPluginID, false)
	tt.Frame()
	if a.diskWatch != nil {
		t.Fatal("switching the plugin off left the probe running")
	}
	if !tt.HasText("插件已停用") {
		t.Fatalf("the page does not say it is off: %v", tt.Texts())
	}
	// Switching it back on while the page is showing resumes the probe.
	a.pluginSetEnabled(diskPluginID, true)
	tt.Frame()
	if a.diskWatch == nil {
		t.Fatal("switching the plugin back on did not resume the probe")
	}
	a.disksUnwatch()
}

// TestDiskReportCopiesTheFacts checks the copied report names the drive and
// carries the fields the page shows, so a bug report is readable without
// a screenshot.
func TestDiskReportCopiesTheFacts(t *testing.T) {
	r := diskhealth.Report{
		At:      time.Date(2026, 10, 18, 21, 0, 0, 0, time.UTC),
		Sources: []string{"diskutil"},
		Notes:   []string{"未安装 smartctl"},
		Disks: []diskhealth.Disk{{
			ID:              "disk0",
			Model:           "APPLE SSD AP0256Z",
			Serial:          "0ba028bba048461d",
			Firmware:        "2914.0.1",
			Bus:             "NVMe",
			Media:           "固态硬盘",
			Size:            251000193024,
			Internal:        true,
			Health:          diskhealth.HealthGood,
			HealthNote:      "SMART 已校验",
			Temperature:     50,
			TempKnown:       true,
			LifeKnown:       true,
			LifeUsed:        4,
			SparePercent:    100,
			PowerOnHours:    1624,
			PowerCycles:     153,
			UnsafeShutdowns: 18,
			MediaErrors:     0,
			ReadBytes:       134042065408000,
			WriteBytes:      56923208192000,
			Free:            13842112512,
			Total:           251000147968,
			Volumes: []diskhealth.Volume{{
				Name: "Macintosh HD - Data", MountPoint: "/System/Volumes/Data",
				Role: "Data", Size: 245107195904,
			}},
		}},
	}
	got := diskReport(r)
	for _, want := range []string{
		"APPLE SSD AP0256Z", "disk0", "NVMe", "固态硬盘",
		"0ba028bba048461d", "2914.0.1", "良好", "50 °C", "已用 4%",
		"1624", "153", "18", "介质错误：0", "未安装 smartctl",
		"Macintosh HD - Data", "Data",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("report lacks %q:\n%s", want, got)
		}
	}
}

// TestDiskReportUnknownFields checks that a drive which reports little is
// described as 未知 rather than as zero.
func TestDiskReportUnknownFields(t *testing.T) {
	got := diskReport(diskhealth.Report{
		Disks: []diskhealth.Disk{{
			ID: "disk9", Temperature: 0, TempKnown: false,
			ReadBytes: -1, WriteBytes: -1, PowerOnHours: -1,
			MediaErrors: -1, Free: -1,
		}},
	})
	if strings.Contains(got, "温度：0 °C") {
		t.Fatalf("a sensorless drive reported 0 °C:\n%s", got)
	}
	if !strings.Contains(got, "温度：未知") || !strings.Contains(got, "介质错误：未知") {
		t.Fatalf("unknown fields not labelled 未知:\n%s", got)
	}
}
