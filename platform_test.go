package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopMimeExts(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-desk")
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "image-editor.desktop")
	os.WriteFile(f, []byte(
		"[Desktop Entry]\nName=Editor\nExec=editor %f\nMimeType=image/png;*.png;*.jpg;image/jpeg;\n"), 0o644)
	got := desktopMimeExts(f)
	if len(got) != 2 || got[0] != "png" || got[1] != "jpg" {
		t.Fatalf("exts = %v", got)
	}
	// A file without the line yields none.
	other := filepath.Join(dir, "plain.desktop")
	os.WriteFile(other, []byte("[Desktop Entry]\nName=Plain\n"), 0o644)
	if e := desktopMimeExts(other); len(e) != 0 {
		t.Fatalf("plain = %v", e)
	}
}

func TestDesktopExec(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-desk2")
	defer os.RemoveAll(dir)
	for _, tc := range []struct {
		file, exec string
		want       []string
	}{
		{"a.desktop", "editor %f", []string{"/x/a.png"}},
		{"b.desktop", "tool %F --flag", []string{"/x/a.png", "--flag"}},
		{"c.desktop", "web %u", []string{"/x/a.png"}},
		{"d.desktop", "plain", nil},
	} {
		os.WriteFile(filepath.Join(dir, tc.file), []byte(
			"[Desktop Entry]\nName=X\nExec="+tc.exec+"\n"), 0o644)
	}
	// The Exec line's field codes take the file; a missing app reports
	// false rather than a half-built command.
	if cmd, ok := desktopExecIn([]string{dir}, "a", "/x/a.png"); !ok || len(cmd.Args) != 2 || cmd.Args[1] != "/x/a.png" {
		t.Fatalf("a: %v %v", cmd, ok)
	}
	if cmd, ok := desktopExecIn([]string{dir}, "b", "/x/a.png"); !ok || len(cmd.Args) != 3 || cmd.Args[2] != "--flag" {
		t.Fatalf("b: %v", cmd.Args)
	}
	if _, ok := desktopExecIn([]string{dir}, "nope", "/x/a.png"); ok {
		t.Fatal("missing app accepted")
	}
}
