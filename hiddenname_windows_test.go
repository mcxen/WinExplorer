//go:build windows

package main

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

// TestWindowsHiddenAttributeCompletion covers the attribute Explorer uses for
// ordinary hidden names: AppData stays out of completion until the setting
// asks for hidden items.
func TestWindowsHiddenAttributeCompletion(t *testing.T) {
	dir := t.TempDir()
	hiddenPath := filepath.Join(dir, "AppData")
	if err := os.Mkdir(hiddenPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "Visible"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := syscall.UTF16PtrFromString(hiddenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.SetFileAttributes(p, syscall.FILE_ATTRIBUTE_NORMAL)
	})

	names, hidden, err := readFolderNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !hidden["AppData"] || hidden["Visible"] {
		t.Fatalf("hidden map = %v for %v", hidden, names)
	}
	all := dirNames{names: names, hidden: hidden}
	if got := all.visibleNames(false); !slices.Equal(got, []string{"Visible"}) {
		t.Fatalf("completion without hidden items = %v", got)
	}
	if got := all.visibleNames(true); !slices.Contains(got, "AppData") || !slices.Contains(got, "Visible") {
		t.Fatalf("completion with hidden items = %v", got)
	}
}
