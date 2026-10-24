//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowsShellDeleteToRecycleBin regression: the delete list handed to
// SHFileOperationW must be double-NUL terminated, and the buffer builder
// once rejected its own embedded NUL, so every delete failed with EINVAL
// before the shell was ever called. This exercises the real path: a temp
// file moves into the volume's bin, and the before/after index maps it.
func TestWindowsShellDeleteToRecycleBin(t *testing.T) {
	file := filepath.Join(t.TempDir(), "sunvale-删除回归.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, key, err := sysTrashMove(file)
	if err != nil {
		t.Fatalf("sysTrashMove: %v", err)
	}
	t.Cleanup(func() {
		if payload != "" {
			_ = sysTrashRemove(payload)
		}
	})
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("original still present after delete: %v", err)
	}
	if payload == "" || key == "" {
		t.Fatal("deleted item did not surface in any volume's bin index")
	}
	if base := filepath.Base(payload); !strings.HasPrefix(base, "$R") {
		t.Fatalf("payload = %q, want a $R entry", payload)
	}
}

// TestWindowsTrashRecyclableLocal pins the confirm guard's baseline: an
// ordinary local folder must report recyclable so local deletes never see
// the permanent-delete dialog.
func TestWindowsTrashRecyclableLocal(t *testing.T) {
	if !sysTrashRecyclable(t.TempDir()) {
		t.Fatal("local temp directory reported unrecyclable")
	}
}
