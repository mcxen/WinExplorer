package main

import (
	"bytes"
	"image/png"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"sunvale/diskhealth"
)

// The tray itself cannot be made here: NewTray panics before the app is
// ready, and these tests never run App.Run. What the tray shows, though,
// is trayMenuItems — a pure function of the report and the settings — so
// the menu's shape is pinned here, plus the settings mapping and the
// dispatcher that fans diskhealth's one update slot out.

func TestTrayIconPNG(t *testing.T) {
	icon, err := trayIconPNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatalf("tray icon does not decode as PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("tray icon is %dx%d, want 32x32", b.Dx(), b.Dy())
	}
}

// trayReport is a report the menu tests build: two disks, one with rates
// known, one without, and the sizes the capacity rows quote.
func trayReport() diskhealth.Report {
	return diskhealth.Report{
		Disks: []diskhealth.Disk{
			{
				ID: "disk0", Model: "APPLE SSD AP1024Z", Total: 994662800000,
				Free: 497331400000, ReadBps: 12.5e6, WriteBps: 3.2e6, RateKnown: true,
			},
			{ID: "disk1", Total: 2e12, Free: 1.4e12},
		},
	}
}

func trayLabels(items []*mygo.MenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Type == mygo.MenuItemSeparator {
			continue
		}
		out = append(out, it.Label)
	}
	for _, it := range items {
		if it.Label == "" && it.Type == mygo.MenuItemSeparator {
			out = append(out, "")
		}
	}
	return out
}

// TestTrayMenuShape pins the menu: the palette and window items, the disk
// readout rows when the settings show them, the switches that mirror the
// settings, and quit at the foot. Disabled readout rows are labels, not
// actions — clicking one must not run anything.
func TestTrayMenuShape(t *testing.T) {
	on := appSettings{} // both defaults on
	items := trayMenuItems(trayReport(), on)
	labels := trayLabels(items)

	// formatBytes is 1024-based with no decimal at or above 10, and the
	// rates are 1024-based MB: the labels quote what those produce.
	mustHave := []string{"快速搜索…", "显示晴谷", "新建窗口",
		"APPLE SSD AP1024Z — 可用 463 GB / 共 926 GB（50%）",
		"disk1 — 可用 1.3 TB / 共 1.8 TB（30%）",
		"磁盘读写：读 11.9 MB/s · 写 3.1 MB/s",
		"刷新磁盘信息",
		"显示磁盘容量", "显示读写速度", "退出晴谷"}
	for _, want := range mustHave {
		found := false
		for _, l := range labels {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("menu lost %q; has %v", want, labels)
		}
	}
	// The readout rows are disabled labels; the switches and actions are
	// not. Clicking a disabled item is a no-op everywhere, and the tests
	// say so by not finding the disk rows clickable.
	for _, it := range items {
		if strings.Contains(it.Label, "可用") || strings.HasPrefix(it.Label, "磁盘读写") {
			if !it.Disabled {
				t.Errorf("readout row %q is clickable", it.Label)
			}
			if it.Click != nil {
				t.Errorf("readout row %q carries a click", it.Label)
			}
		}
	}
	// The switches mirror the settings.
	for _, it := range items {
		if it.Label == "显示磁盘容量" || it.Label == "显示读写速度" {
			if !it.Checked || it.Type != mygo.MenuItemCheckbox {
				t.Errorf("switch %q not checked for the on settings", it.Label)
			}
		}
	}

	// Both settings off: the disk section goes — no rows, no separator
	// doubling — and the switches stay, unchecked.
	off := appSettings{NoTrayDiskCap: true, NoTrayDiskSpeed: true}
	items = trayMenuItems(trayReport(), off)
	labels = trayLabels(items)
	for _, bad := range []string{"可用", "磁盘读写", "刷新磁盘信息"} {
		for _, l := range labels {
			if strings.Contains(l, bad) {
				t.Errorf("menu with both off still shows %q (%v)", bad, l)
			}
		}
	}
	for _, it := range items {
		if it.Label == "显示磁盘容量" || it.Label == "显示读写速度" {
			if it.Checked {
				t.Errorf("switch %q checked for the off settings", it.Label)
			}
		}
	}

	// Only capacity on: the speed row is gone, the capacity rows stay.
	half := appSettings{NoTrayDiskSpeed: true}
	items = trayMenuItems(trayReport(), half)
	labels = trayLabels(items)
	hasCap, hasSpeed := false, false
	for _, l := range labels {
		if strings.Contains(l, "可用") && strings.Contains(l, "（") {
			hasCap = true
		}
		if strings.HasPrefix(l, "磁盘读写") {
			hasSpeed = true
		}
	}
	if !hasCap || hasSpeed {
		t.Fatalf("capacity-only menu: cap=%v speed=%v (%v)", hasCap, hasSpeed, labels)
	}
}

// TestTrayMenuEmptyReport pins the placeholders: a probe that has not
// landed says 读取中, one that found no sizes says 未报告, and a report
// without rates says 采样中 — a section asked for is never silently empty.
func TestTrayMenuEmptyReport(t *testing.T) {
	items := trayMenuItems(diskhealth.Report{}, appSettings{})
	labels := trayLabels(items)
	found := false
	for _, l := range labels {
		if l == "磁盘容量：读取中…" || l == "磁盘容量：未报告" {
			found = true
		}
		if strings.HasPrefix(l, "磁盘读写") {
			if l != "磁盘读写：采样中…" {
				t.Fatalf("speed row without rates = %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("no capacity placeholder among %v", labels)
	}
}

// TestTrayDiskShown pins the settings mapping: on by default, stored as
// negations so a state file from before the switches reads as showing
// both, and each switch flips only its own half.
func TestTrayDiskShown(t *testing.T) {
	if s := trayDiskShown(appSettings{}); !s.capOn || !s.speedOn {
		t.Fatalf("defaults = %+v, want both on", s)
	}
	if s := trayDiskShown(appSettings{NoTrayDiskCap: true}); s.capOn || !s.speedOn {
		t.Fatalf("cap off = %+v", s)
	}
	if s := trayDiskShown(appSettings{NoTrayDiskSpeed: true}); !s.capOn || s.speedOn {
		t.Fatalf("speed off = %+v", s)
	}
}

// TestDiskUpdateDispatcher pins the fan-out: several listeners hear one
// landing, a removed one does not, and the listeners run in the order
// they were added. Headless — no App.Run — the notify runs them on its
// own goroutine, which is exactly the path the tests exercise.
func TestDiskUpdateDispatcher(t *testing.T) {
	var mu sync.Mutex
	var order []int
	add := func(tag int) func() {
		return diskUpdateOn(func() {
			mu.Lock()
			order = append(order, tag)
			mu.Unlock()
		})
	}
	off := add(1)
	add(2)
	add(3)
	diskUpdateNotify()
	mu.Lock()
	got := append([]int(nil), order...)
	mu.Unlock()
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("listeners ran %v, want [1 2 3]", got)
	}
	off()
	order = nil
	diskUpdateNotify()
	mu.Lock()
	got = append([]int(nil), order...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("after removal ran %v, want [2 3]", got)
	}
}
