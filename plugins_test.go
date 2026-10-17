package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestPluginDefaultsAllEnabled checks the fresh store records nothing: a
// nil plugins list is every registered plugin on, first-run behavior being
// today's. A nil store is every plugin on too.
func TestPluginDefaultsAllEnabled(t *testing.T) {
	st := &persistedState{}
	if list := st.pluginList(); list != nil {
		t.Fatalf("pluginList = %v, want nil", list)
	}
	for _, p := range registeredPlugins() {
		if !pluginOn(st, p.ID) {
			t.Fatalf("pluginOn(%q) = false on a fresh store", p.ID)
		}
	}
	if !pluginOn(nil, "gif") {
		t.Fatal("pluginOn(nil) = false")
	}
}

// TestPluginSetRoundTrip turns a plugin off and back on, going through the
// state.json a fresh loadState reads. HOME and XDG_CONFIG_HOME move to
// temp directories for the test, so the round trip stays out of the
// developer's own state.
func TestPluginSetRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())

	a.store.setPlugin("dups", false)
	if a.pluginEnabled("dups") {
		t.Fatal("dups still on after setPlugin off")
	}
	// setPlugin writes through saveLocked, so the reloaded store keeps it
	// off, and only the off ids are recorded.
	st := loadState()
	if pluginOn(st, "dups") {
		t.Fatal("reloaded store has dups on")
	}
	if got := st.pluginList(); len(got) != 1 || got[0] != "dups" {
		t.Fatalf("pluginList = %v, want [dups]", got)
	}
	// Turning it back on drops the record; with every plugin on the list
	// is nil again, and the next loadState sees that.
	st.setPlugin("dups", true)
	if !pluginOn(st, "dups") {
		t.Fatal("dups still off after setPlugin on")
	}
	if list := st.pluginList(); list != nil {
		t.Fatalf("pluginList = %v, want nil", list)
	}
	again := loadState()
	if got := again.pluginList(); got != nil {
		t.Fatalf("reloaded pluginList = %v, want nil", got)
	}
	for _, p := range registeredPlugins() {
		if !pluginOn(again, p.ID) {
			t.Fatalf("pluginOn(%q) = false after re-enable", p.ID)
		}
	}
	// Turning an off plugin off again does not duplicate the record.
	st.setPlugin("hash", false)
	st.setPlugin("hash", false)
	if got := st.pluginList(); len(got) != 1 || got[0] != "hash" {
		t.Fatalf("pluginList = %v, want [hash]", got)
	}
}

// TestPluginEnabledApp checks the app-level helper against its store, and
// pluginSetEnabled going through the same store. A nil store — newTestApp's
// apps have none — keeps every plugin on.
func TestPluginEnabledApp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	for _, p := range registeredPlugins() {
		if !a.pluginEnabled(p.ID) {
			t.Fatalf("pluginEnabled(%q) = false, want default on", p.ID)
		}
	}
	a.store.setPlugin("gif", false)
	if a.pluginEnabled("gif") {
		t.Fatal("pluginEnabled(gif) = true after setPlugin off")
	}
	if !a.pluginEnabled("dups") {
		t.Fatal("pluginEnabled(dups) = false, other plugins untouched")
	}
	// pluginSetEnabled persists through the same store; the headless
	// repaint is a no-op.
	a.pluginSetEnabled("dups", false)
	if a.pluginEnabled("dups") {
		t.Fatal("pluginSetEnabled did not turn dups off")
	}
	if !slices.Contains(a.store.pluginList(), "dups") {
		t.Fatal("pluginSetEnabled did not record dups")
	}
	if !(&App{}).pluginEnabled("remote") {
		t.Fatal("nil store should have every plugin on")
	}
	// An unknown id toggles without crashing and without touching hooks.
	a.pluginSetEnabled("no-such-plugin", false)
}

// TestPluginRegisterDuplicatePanics checks the registry rejects a second
// plugin with the same id.
func TestPluginRegisterDuplicatePanics(t *testing.T) {
	RegisterPlugin(Plugin{ID: "test-dup"})
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate id did not panic")
		}
	}()
	RegisterPlugin(Plugin{ID: "test-dup"})
}

// TestPluginHooksGatedByEnabled registers a fake plugin contributing every
// hook kind, and checks the collectors gather its items only while it is
// enabled.
func TestPluginHooksGatedByEnabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	RegisterPlugin(Plugin{
		ID:   "test-hooks",
		Name: "测试钩子",
		MenuItems: func(a *App, sel []Entry) []PluginMenuItem {
			return []PluginMenuItem{{Label: "测试菜单", Run: func() {}}}
		},
		MoreItems: func(a *App) []PluginMenuItem {
			return []PluginMenuItem{{Label: "测试更多", Run: func() {}}}
		},
		NavItems: func(a *App) []PluginNavItem {
			return []PluginNavItem{{Key: "nav-test", Label: "测试导航", Run: func() {}}}
		},
		MetaRows: func(a *App, e Entry) []PluginMetaRow {
			return []PluginMetaRow{{Key: "测试行", Val: "v"}}
		},
	})
	hasLabel := func(items []PluginMenuItem, label string) bool {
		return slices.ContainsFunc(items, func(it PluginMenuItem) bool { return it.Label == label })
	}
	a := newStoreApp(t, t.TempDir())
	if !hasLabel(a.pluginMenuItems(nil), "测试菜单") {
		t.Fatal("fake plugin's menu item missing while enabled")
	}
	if !hasLabel(a.pluginMoreItems(), "测试更多") {
		t.Fatal("fake plugin's overflow item missing while enabled")
	}
	if !slices.ContainsFunc(a.pluginNavItems(), func(it PluginNavItem) bool { return it.Label == "测试导航" }) {
		t.Fatal("fake plugin's nav item missing while enabled")
	}
	if !slices.ContainsFunc(a.pluginMetaRows(Entry{Path: "/x/a.txt"}), func(r PluginMetaRow) bool { return r.Key == "测试行" }) {
		t.Fatal("fake plugin's meta row missing while enabled")
	}
	// Switched off, every collector drops it — the built-in plugins'
	// contributions are unaffected.
	a.pluginSetEnabled("test-hooks", false)
	if hasLabel(a.pluginMenuItems(nil), "测试菜单") ||
		hasLabel(a.pluginMoreItems(), "测试更多") ||
		slices.ContainsFunc(a.pluginNavItems(), func(it PluginNavItem) bool { return it.Label == "测试导航" }) ||
		slices.ContainsFunc(a.pluginMetaRows(Entry{Path: "/x/a.txt"}), func(r PluginMetaRow) bool { return r.Key == "测试行" }) {
		t.Fatal("fake plugin still contributes while disabled")
	}
	if !hasLabel(a.pluginMenuItems(nil), "可视化查看占用") {
		t.Fatal("usage plugin's menu item missing while enabled")
	}
	a.pluginSetEnabled("test-hooks", true)
	if !hasLabel(a.pluginMoreItems(), "测试更多") {
		t.Fatal("fake plugin's overflow item missing after re-enable")
	}
}

// TestPluginSetEnabledCallsHooks checks the switch runs the registered
// OnEnable/OnDisable hooks, with no real plugin behind them.
func TestPluginSetEnabledCallsHooks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var enabled, disabled int
	RegisterPlugin(Plugin{
		ID:        "test-lifecycle",
		Name:      "测试生命周期",
		OnEnable:  func(a *App) { enabled++ },
		OnDisable: func(a *App) { disabled++ },
	})
	a := newStoreApp(t, t.TempDir())
	a.pluginSetEnabled("test-lifecycle", false)
	if disabled != 1 || enabled != 0 {
		t.Fatalf("after off: OnDisable=%d OnEnable=%d, want 1/0", disabled, enabled)
	}
	if a.pluginEnabled("test-lifecycle") {
		t.Fatal("plugin still enabled after off")
	}
	a.pluginSetEnabled("test-lifecycle", true)
	if enabled != 1 || disabled != 1 {
		t.Fatalf("after on: OnEnable=%d OnDisable=%d, want 1/1", enabled, disabled)
	}
	if !a.pluginEnabled("test-lifecycle") {
		t.Fatal("plugin still disabled after on")
	}
}

// TestPluginSettingsRoundTrip checks the per-plugin settings store: reads
// default empty, writes persist through state.json, old state files
// without the field read as empty and initialize lazily on write.
func TestPluginSettingsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	if got := a.pluginGet("fake", "key"); got != "" {
		t.Fatalf("pluginGet on unset = %q, want empty", got)
	}
	a.pluginSet("fake", "key", "val")
	if got := a.pluginGet("fake", "key"); got != "val" {
		t.Fatalf("pluginGet = %q, want val", got)
	}
	if got := loadState().pluginSetting("fake", "key"); got != "val" {
		t.Fatalf("reloaded pluginSetting = %q, want val", got)
	}
	// A store from a state file that predates the field: nil maps.
	var old persistedState
	if got := old.pluginSetting("x", "y"); got != "" {
		t.Fatalf("nil PluginSettings read = %q, want empty", got)
	}
	old.setPluginSetting("x", "y", "z")
	if got := old.pluginSetting("x", "y"); got != "z" {
		t.Fatalf("after lazy init pluginSetting = %q, want z", got)
	}
	data, err := json.Marshal(&old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"pluginSettings"`) {
		t.Fatalf("state JSON misses pluginSettings: %s", data)
	}
	// An app without a store reads empty and writes nowhere.
	if got := (&App{}).pluginGet("fake", "key"); got != "" {
		t.Fatalf("nil store pluginGet = %q, want empty", got)
	}
	(&App{}).pluginSet("fake", "key", "val")
}

// TestGifPluginGatesPlayback checks the gif player's entry: with the
// plugin off, gifFor declines, and the callers keep the decoded still.
func TestGifPluginGatesPlayback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "动画.gif")
	if err := writeAnimatedGif(path); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, t.TempDir())
	a.store.setPlugin("gif", false)
	if bm := a.gifFor(path); bm != nil {
		t.Fatal("gifFor started a player with the plugin off")
	}
}

// writeAnimatedGif writes a two-frame gif, the smallest one a player
// plays.
func writeAnimatedGif(path string) error {
	pal := color.Palette{color.RGBA{0, 0, 0, 255}, color.RGBA{255, 255, 255, 255}}
	g := &gif.GIF{}
	for i := range 2 {
		img := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
		img.Set(i, i, pal[1])
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, g)
}
