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
