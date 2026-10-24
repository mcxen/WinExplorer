package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDesktopMimeExts(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-desk")
	defer os.RemoveAll(dir)
	f := filepath.Join(dir, "image-editor.desktop")
	os.WriteFile(f, []byte(
		"[Desktop Entry]\nName=Editor\nExec=editor %f\nMimeType=image/png;*.png;*.jpg;image/jpeg;\n"), 0o644)
	got := desktopMimeExts(f)
	// image/png and image/jpeg resolve through the MIME table (jpeg keeps
	// both its extensions), *.png and *.jpg stay glob patterns; the
	// duplicates collapse.
	slices.Sort(got)
	if !slices.Equal(got, []string{"jpeg", "jpg", "png"}) {
		t.Fatalf("exts = %v", got)
	}
	// A file without the line yields none.
	other := filepath.Join(dir, "plain.desktop")
	os.WriteFile(other, []byte("[Desktop Entry]\nName=Plain\n"), 0o644)
	if e := desktopMimeExts(other); len(e) != 0 {
		t.Fatalf("plain = %v", e)
	}
}

// TestMimeExtsTable covers the shared-mime-info fallback: known types map
// to their extensions, unknown types to a readable subtype, junk to none.
func TestMimeExtsTable(t *testing.T) {
	if got := mimeExts("application/pdf"); !slices.Equal(got, []string{"pdf"}) {
		t.Fatalf("pdf = %v", got)
	}
	if got := mimeExts("application/x-keepass"); !slices.Equal(got, []string{"keepass"}) {
		t.Fatalf("x-keepass = %v", got)
	}
	if got := mimeExts("no-slash"); got != nil {
		t.Fatalf("no-slash = %v", got)
	}
	if got := mimeExts("application/x-reallylongsubtype"); got != nil {
		t.Fatalf("long subtype = %v", got)
	}
}

// TestDesktopNameAndExecByName covers the menu's show-name launch path:
// Name= (localized when the locale matches) names the app in the menu,
// and desktopExecIn finds the launcher by that name when launching.
func TestDesktopNameAndExecByName(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-desk3")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "writer.desktop"), []byte(
		"[Desktop Entry]\nName=LibreOffice Writer\nName[zh_CN]=Writer 文字\nExec=lowriter %f\n"), 0o644)
	t.Setenv("LANG", "zh_CN.UTF-8")
	name, ok := desktopName(filepath.Join(dir, "writer.desktop"))
	if !ok || name != "Writer 文字" {
		t.Fatalf("localized name = %q %v", name, ok)
	}
	t.Setenv("LANG", "C")
	if name, ok := desktopName(filepath.Join(dir, "writer.desktop")); !ok || name != "LibreOffice Writer" {
		t.Fatalf("fallback name = %q %v", name, ok)
	}
	// Launching by the shown name resolves through desktopAppIds.
	if _, ok := desktopExecIn([]string{dir}, "LibreOffice Writer", "/x/a.pdf"); !ok {
		t.Fatal("exec by display name failed")
	}
	if cmd, ok := desktopExecIn([]string{dir}, "writer", "/x/a.pdf"); !ok || cmd.Args[1] != "/x/a.pdf" {
		t.Fatalf("exec by id = %v %v", cmd, ok)
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
