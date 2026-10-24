package main

import (
	"path/filepath"
	"testing"
)

// TestMergeKnownFolderPaths covers the redirect-over-default layering: the
// shell's resolution (OneDrive's known-folder move, on Windows) wins per
// folder, and anything unresolved falls back to the home-relative default
// instead of pointing the sidebar nowhere.
func TestMergeKnownFolderPaths(t *testing.T) {
	home := "/home/u"
	oneDrive := filepath.Join(home, "OneDrive")
	got := mergeKnownFolderPaths(home, map[string]string{
		"documents": filepath.Join(oneDrive, "Documents"),
		"pictures":  filepath.Join(oneDrive, "Pictures"),
		"downloads": "relative/is-ignored",
	})
	if got["documents"] != filepath.Join(oneDrive, "Documents") {
		t.Errorf("redirect not honored: documents = %q", got["documents"])
	}
	if got["pictures"] != filepath.Join(oneDrive, "Pictures") {
		t.Errorf("redirect not honored: pictures = %q", got["pictures"])
	}
	if got["desktop"] != filepath.Join(home, "Desktop") {
		t.Errorf("fallback lost: desktop = %q", got["desktop"])
	}
	if got["downloads"] != filepath.Join(home, "Downloads") {
		t.Errorf("relative redirect must fall back: downloads = %q", got["downloads"])
	}
	// No redirects at all (macOS, Linux, wasm): every folder is the
	// home-relative default.
	all := mergeKnownFolderPaths(home, nil)
	for _, k := range knownFolderKeys {
		if all[k.key] != filepath.Join(home, k.def) {
			t.Errorf("nil redirects: %s = %q, want %q", k.key, all[k.key], filepath.Join(home, k.def))
		}
	}
}
