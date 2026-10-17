package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildRecycleInfo writes a version-2 $I metadata file for orig.
func buildRecycleInfo(orig string, size uint64, deleted time.Time) []byte {
	runes := []rune(orig)
	b := &bytes.Buffer{}
	binary.Write(b, binary.LittleEndian, uint32(2))
	binary.Write(b, binary.LittleEndian, size)
	binary.Write(b, binary.LittleEndian, winFiletime(deleted))
	binary.Write(b, binary.LittleEndian, uint32(len(runes)))
	for _, r := range runes {
		binary.Write(b, binary.LittleEndian, uint16(r))
	}
	return b.Bytes()
}

// winFiletime is in recycleinfo.go; the test uses it.

func TestParseRecycleInfo(t *testing.T) {
	orig := `C:\Users\mcx\文档 报告.txt`
	data := buildRecycleInfo(orig, 1234, time.Date(2026, 10, 17, 1, 2, 3, 0, time.UTC))
	got, ok := parseRecycleInfo(data)
	if !ok || got != orig {
		t.Fatalf("parse = %q %v", got, ok)
	}
	// Deletion time round-trips to the second.
	when := recycleDeletedAt(data)
	d, _ := time.Parse("2006-01-02 15:04:05", "2026-10-17 01:02:03")
	if when.Unix() != d.Unix() {
		t.Fatalf("deleted at = %v", when)
	}

	// A v1 header (Windows Vista) and truncated files are refused, not
	// misread.
	binary.LittleEndian.PutUint32(data, 1)
	if _, ok := parseRecycleInfo(data); ok {
		t.Fatal("v1 accepted")
	}
	binary.LittleEndian.PutUint32(data, 2)
	if _, ok := parseRecycleInfo(data[:20]); ok {
		t.Fatal("short file accepted")
	}
	// A length past the file's end is refused.
	binary.LittleEndian.PutUint32(data[20:], 1<<20)
	if _, ok := parseRecycleInfo(data); ok {
		t.Fatal("overflowing length accepted")
	}
}

func TestTrashinfoEscape(t *testing.T) {
	path := "/Users/mcx/100% 完成/报告.txt"
	esc := trashinfoEscape(path)
	if esc != "/Users/mcx/100%25 完成/报告.txt" {
		t.Fatalf("escape = %q", esc)
	}
	if back := trashinfoUnescape(esc); back != path {
		t.Fatalf("unescape = %q", back)
	}
}

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
