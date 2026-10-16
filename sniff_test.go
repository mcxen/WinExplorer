package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsMostlyText(t *testing.T) {
	if !isMostlyText([]byte("hello world\nplain lines\n")) {
		t.Fatal("plain text not detected")
	}
	// UTF-8 bytes count as printable; a NUL anywhere kills it.
	if !isMostlyText([]byte("你好，世界")) {
		t.Fatal("utf-8 not detected")
	}
	if isMostlyText([]byte{0x00, 0x01, 0x02, 'a'}) {
		t.Fatal("NUL bytes are binary")
	}
	// 85% threshold: one control byte in 16 is still text.
	if !isMostlyText([]byte("sixteen bytes !\x01")) {
		t.Fatal("mostly printable should pass")
	}
}

func TestHexDump(t *testing.T) {
	out := hexDump([]byte{0x41, 0x42, 0x00, 0xff, 0x7e})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "00000000  41 42 00 ff 7e") {
		t.Fatalf("offset/bytes = %q", lines[0])
	}
	// Non-printables render as dots; printables as themselves.
	if !strings.HasSuffix(lines[0], "|AB..~|") {
		t.Fatalf("ascii column = %q", lines[0])
	}
	// A full 16-byte row plus a 1-byte row.
	b := make([]byte, 17)
	for i := range b {
		b[i] = byte(i)
	}
	lines = strings.Split(strings.TrimRight(hexDump(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("two rows expected, got %q", lines)
	}
	if !strings.Contains(lines[1], "00000010  10") {
		t.Fatalf("second row = %q", lines[1])
	}
}

func TestSniffKind(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-sniff")
	defer os.RemoveAll(dir)

	// A Makefile: no extension, but text.
	mk := filepath.Join(dir, "Makefile")
	os.WriteFile(mk, []byte("all:\n\tgo build ./...\n"), 0o644)
	k, text, full, err := sniffKind(mk, 22)
	if err != nil || k != previewText || !strings.HasPrefix(text, "all:") || full != 22 {
		t.Fatalf("text sniff = %v %q %d %v", k, text, full, err)
	}

	// A binary blob: hex.
	blob := filepath.Join(dir, "blob.bin")
	os.WriteFile(blob, []byte{0x00, 0x01, 0x02, 0xfe, 0x00, 0x7f, 0x00, 0x0b}, 0o644)
	k, text, _, err = sniffKind(blob, 8)
	if err != nil || k != previewHex || !strings.Contains(text, "00 01 02 fe") {
		t.Fatalf("hex sniff = %v %q %v", k, text, err)
	}

	// buildPreview wires the sniff for unknown kinds.
	a := &App{}
	st := a.buildPreview(blob)
	if st.kind != previewHex || st.text == "" {
		t.Fatalf("buildPreview unknown = %v %q", st.kind, st.text)
	}
	st = a.buildPreview(mk)
	if st.kind != previewText || st.text != "all:\n\tgo build ./...\n" {
		t.Fatalf("buildPreview text = %v %q", st.kind, st.text)
	}
}
