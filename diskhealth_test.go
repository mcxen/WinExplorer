package main

import "testing"

// TestDiskWarnTemp checks the high-temperature warning's settings: the
// defaults (on, 50 °C), the stored overrides, the fall-back on values the
// 插件 page never offers, and the persistence through state.json.
func TestDiskWarnTemp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	if limit, on := a.diskWarnTemp(); !on || limit != 50 {
		t.Fatalf("defaults: limit=%v on=%v, want 50 true", limit, on)
	}
	a.pluginSet(diskPluginID, "tempWarn", "off")
	a.pluginSet(diskPluginID, "tempLimit", "60")
	if limit, on := a.diskWarnTemp(); on || limit != 60 {
		t.Fatalf("overrides: limit=%v on=%v, want 60 false", limit, on)
	}
	// A value the settings page never offers falls back to the default.
	a.pluginSet(diskPluginID, "tempLimit", "99")
	if limit, _ := a.diskWarnTemp(); limit != 50 {
		t.Fatalf("unknown limit = %v, want 50", limit)
	}
	a.pluginSet(diskPluginID, "tempLimit", "40")
	if got := loadState().pluginSetting(diskPluginID, "tempLimit"); got != "40" {
		t.Fatalf("reloaded tempLimit = %q, want 40", got)
	}
}

// TestDiskHot checks the hot-disk rule the cards colour by: a sensorless
// disk never warns, a switched-off warning never warns, and the threshold
// is inclusive.
func TestDiskHot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	cold := DiskInfo{Name: "disk0", Temperature: 40}
	hot := DiskInfo{Name: "disk0", Temperature: 60}
	if a.diskHot(cold) {
		t.Fatal("40 °C marked hot at the default threshold")
	}
	if !a.diskHot(hot) {
		t.Fatal("60 °C not hot at the default threshold")
	}
	if a.diskHot(DiskInfo{Temperature: -1}) {
		t.Fatal("sensorless disk marked hot")
	}
	a.pluginSet(diskPluginID, "tempWarn", "off")
	if a.diskHot(hot) {
		t.Fatal("hot disk warned with the warning off")
	}
	a.pluginSet(diskPluginID, "tempWarn", "on")
	a.pluginSet(diskPluginID, "tempLimit", "40")
	if !a.diskHot(cold) {
		t.Fatal("threshold not inclusive")
	}
}

// TestDiskSettingsDeclared checks the disk plugin declares its settings
// through the registry's interface, so the 插件 page renders them without
// the disk plugin rendering anything itself.
func TestDiskSettingsDeclared(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	rows := a.pluginSettings(findPlugin(diskPluginID))
	if len(rows) != 2 {
		t.Fatalf("disk plugin declares %d settings, want 2", len(rows))
	}
	if rows[0].Kind != PluginSettingSwitch || rows[0].Name != "高温警告" {
		t.Fatalf("first row = %+v, want the 高温警告 switch", rows[0])
	}
	if rows[1].Kind != PluginSettingSelect || rows[1].Value != "50" {
		t.Fatalf("second row = %+v, want the threshold select at 50", rows[1])
	}
	// The declared rows write through the hooks: flipping the switch and
	// picking a threshold move the settings the cards read.
	rows[0].Change("off")
	rows[1].Change("60")
	if limit, on := a.diskWarnTemp(); on || limit != 60 {
		t.Fatalf("after Change: limit=%v on=%v, want 60 false", limit, on)
	}
	// A plugin switched off contributes no settings to the page.
	a.pluginSetEnabled(diskPluginID, false)
	if rows := a.pluginSettings(findPlugin(diskPluginID)); rows != nil {
		t.Fatalf("disk plugin still declares settings while off: %v", rows)
	}
}
