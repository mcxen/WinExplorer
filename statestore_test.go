package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStateDirMigration covers the rename from winexplorer to sunvale: state
// written under the old directory is still loaded, and the first save lands in
// the new one.
func TestStateDirMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir on this platform")
	}
	if !strings.HasPrefix(cfg, home) {
		t.Skip("HOME does not redirect the user config dir on this platform")
	}

	legacy := filepath.Join(cfg, legacyStateDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	pin := filepath.Join(cfg, "旧收藏")
	recent := filepath.Join(cfg, "最近.txt")
	seed, err := json.Marshal(persistedState{Pinned: []string{pin}, Recent: []string{recent}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "state.json"), seed, 0o644); err != nil {
		t.Fatal(err)
	}

	st := loadState()
	if len(st.Pinned) != 1 || st.Pinned[0] != pin {
		t.Fatalf("pinned after migration = %v, want [%s]", st.Pinned, pin)
	}
	if len(st.Recent) != 1 || st.Recent[0] != recent {
		t.Fatalf("recent after migration = %v, want [%s]", st.Recent, recent)
	}

	st.pushRecent(filepath.Join(cfg, "新文件.txt"))
	if _, err := os.Stat(filepath.Join(cfg, stateDirName, "state.json")); err != nil {
		t.Fatalf("state not rewritten to %s: %v", stateDirName, err)
	}
}

// stateHome moves HOME to a temp directory and returns the user config
// dir, skipping the test where HOME does not move it.
func stateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir on this platform")
	}
	if !strings.HasPrefix(cfg, home) {
		t.Skip("HOME does not redirect the user config dir on this platform")
	}
	return cfg
}

// TestSettingsRoundTrip covers the view settings: updateSettings persists
// them to state.json and they survive a reload through loadState.
func TestSettingsRoundTrip(t *testing.T) {
	stateHome(t)

	st := loadState()
	st.updateSettings(func(s *appSettings) {
		s.ShowHidden = true
		s.Compact = true
		s.Mode = int(ViewList)
	})
	got := st.settings()
	if !got.ShowHidden || !got.Compact || got.Mode != int(ViewList) {
		t.Fatalf("settings after update = %+v, want ShowHidden/Compact on and mode %d", got, ViewList)
	}
	// Fields the update left alone keep the defaults the first change
	// started from.
	if !got.ShowExt || got.ItemCheckboxes || got.PreviewOpen {
		t.Fatalf("untouched settings = %+v, want ShowExt on and the rest off", got)
	}

	s := loadState().settings()
	if !s.ShowHidden || !s.Compact || s.Mode != int(ViewList) || !s.ShowExt {
		t.Fatalf("reloaded settings = %+v, want the ones updateSettings saved", s)
	}
}

// TestSettingsApplyToAllWindows covers the settings dialog's write-through:
// a change made in one window reaches every open window's fields and lands
// in the state file.
func TestSettingsApplyToAllWindows(t *testing.T) {
	stateHome(t)

	dir := t.TempDir()
	a, b := newStoreApp(t, dir), newStoreApp(t, dir)
	st := &persistedState{}
	a.store, b.store = st, st
	registerApp(a)
	registerApp(b)
	defer func() { unregisterApp(a); unregisterApp(b) }()

	a.applySettings(func(s *appSettings) { s.ShowHidden = true; s.Compact = true })
	if !a.showHidden || !a.compact {
		t.Fatal("the change did not apply to the window that made it")
	}
	if !b.showHidden || !b.compact {
		t.Fatal("the change did not reach the other open window")
	}
	s := loadState().settings()
	if !s.ShowHidden || !s.Compact {
		t.Fatalf("settings on disk = %+v, want ShowHidden and Compact on", s)
	}
}

// TestSettingsDefaults covers a state file written before the settings field
// existed: loading it yields the defaults a fresh app starts with.
func TestSettingsDefaults(t *testing.T) {
	cfg := stateHome(t)

	// A store with no settings at all reports the defaults.
	if got := (&persistedState{}).settings(); got != *defaultSettings() {
		t.Fatalf("settings of an empty store = %+v, want the defaults %+v", got, *defaultSettings())
	}

	dir := filepath.Join(cfg, stateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := []byte(`{"pinned":["/tmp/旧收藏"],"recent":["/tmp/最近.txt"]}`)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), seed, 0o644); err != nil {
		t.Fatal(err)
	}

	s := loadState().settings()
	if s != *defaultSettings() {
		t.Fatalf("settings without a settings field = %+v, want the defaults %+v", s, *defaultSettings())
	}
	if !s.ShowExt || s.Mode != int(ViewDetails) {
		t.Fatalf("defaults = %+v, want ShowExt on and mode %d", s, ViewDetails)
	}
}
