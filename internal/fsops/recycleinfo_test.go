package fsops

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// buildRecycleInfo writes a version-2 $I metadata file for orig.
func buildRecycleInfo(orig string, size uint64, deleted time.Time) []byte {
	runes := []rune(orig)
	b := &bytes.Buffer{}
	binary.Write(b, binary.LittleEndian, uint32(2))
	binary.Write(b, binary.LittleEndian, size)
	binary.Write(b, binary.LittleEndian, WinFiletime(deleted))
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
	got, ok := ParseRecycleInfo(data)
	if !ok || got != orig {
		t.Fatalf("parse = %q %v", got, ok)
	}
	// Deletion time round-trips to the second.
	when := RecycleDeletedAt(data)
	d, _ := time.Parse("2006-01-02 15:04:05", "2026-10-17 01:02:03")
	if when.Unix() != d.Unix() {
		t.Fatalf("deleted at = %v", when)
	}

	// A v1 header (Windows Vista) and truncated files are refused, not
	// misread.
	binary.LittleEndian.PutUint32(data, 1)
	if _, ok := ParseRecycleInfo(data); ok {
		t.Fatal("v1 accepted")
	}
	binary.LittleEndian.PutUint32(data, 2)
	if _, ok := ParseRecycleInfo(data[:20]); ok {
		t.Fatal("short file accepted")
	}
	// A length past the file's end is refused.
	binary.LittleEndian.PutUint32(data[20:], 1<<20)
	if _, ok := ParseRecycleInfo(data); ok {
		t.Fatal("overflowing length accepted")
	}
}

func TestTrashinfoEscape(t *testing.T) {
	path := "/Users/mcx/100% 完成/报告.txt"
	esc := TrashinfoEscape(path)
	if esc != "/Users/mcx/100%25 完成/报告.txt" {
		t.Fatalf("escape = %q", esc)
	}
	if back := TrashinfoUnescape(esc); back != path {
		t.Fatalf("unescape = %q", back)
	}
}
