package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAbsFrom(t *testing.T) {
	root := t.TempDir()
	got, err := AbsFrom(root, "a/b")
	if err != nil || got != filepath.Join(root, "a", "b") {
		t.Fatalf("relative = %q %v", got, err)
	}
	if got, err := AbsFrom(root, "/tmp/x"); err != nil || got != filepath.Clean("/tmp/x") {
		t.Fatalf("absolute = %q %v", got, err)
	}
	if _, err := AbsFrom("", "relative"); err == nil {
		t.Fatal("an empty base accepted a relative path")
	}
}

func TestFreeNameAndPlaceItem(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	free, err := FreeName(dir, "report.txt", false)
	if err != nil || free != "report - 副本.txt" {
		t.Fatalf("free name = %q %v", free, err)
	}
	if got, err := FreeName(dir, "new.txt", false); err != nil || got != "new.txt" {
		t.Fatalf("unused name = %q %v", got, err)
	}

	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyDst := filepath.Join(dir, "copy")
	if err := PlaceItem(src, copyDst, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(copyDst, "a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("copied data = %q %v", data, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("copy removed source: %v", err)
	}

	moveDst := filepath.Join(dir, "moved")
	if err := PlaceItem(src, moveDst, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("move left source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moveDst, "a.txt")); err != nil {
		t.Fatalf("move lost content: %v", err)
	}
}
