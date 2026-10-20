package rename

import (
	"os"
	"path/filepath"
	"testing"
)

func expand(pattern string, n int, e item) string {
	rows := []Row{{Name: e.Name, IsDir: e.IsDir}}
	Replan(rows, Options{Mode: ModeTemplate, Pattern: pattern, Start: n}, "")
	return rows[0].To
}

func rowsOf(items []item) []Row {
	rows := make([]Row, len(items))
	for i, e := range items {
		rows[i] = Row{Name: e.Name, IsDir: e.IsDir}
	}
	return rows
}

func TestExpandRenamePattern(t *testing.T) {
	e := item{Name: "照片 (2).jpg"}
	got := expand("{n} {name}", 5, e)
	if got != "5 照片 (2).jpg" {
		t.Fatalf("{n} {name} = %q", got)
	}
	got = expand("{stem}-{n:3}.{ext}", 7, e)
	if got != "照片 (2)-007.jpg" {
		t.Fatalf("padded = %q", got)
	}
	// A folder keeps its whole name as the stem, with no extension.
	d := item{Name: "项目文件夹", IsDir: true}
	if got := expand("[{stem}]", 1, d); got != "[项目文件夹]" {
		t.Fatalf("dir = %q", got)
	}
	// A file with no extension either.
	f := item{Name: "Makefile"}
	if got := expand("[{stem}]", 1, f); got != "[Makefile]" {
		t.Fatalf("noext = %q", got)
	}
	// {n:0} and {n:x} fall back to plain numbers.
	if got := expand("{n:0}", 3, f); got != "3" {
		t.Fatalf("bad width = %q", got)
	}
	if got := expand("{n:x}", 3, f); got != "3" {
		t.Fatalf("non-numeric width = %q", got)
	}
}

func TestRenameRegexAndFindReplaceModes(t *testing.T) {
	rows := []item{
		{Name: "IMG_001.jpg"},
		{Name: "IMG_002.jpg"},
		{Name: "notes.txt"},
	}
	r := rowsOf(rows)

	Replan(r, Options{Mode: ModeRegex, Pattern: `^IMG_(\d+)\.jpg$`, Replace: "photo-$1.jpg"}, "")
	if r[0].To != "photo-001.jpg" || r[1].To != "photo-002.jpg" || r[2].To != "notes.txt" {
		t.Fatalf("regex targets = %q %q %q", r[0].To, r[1].To, r[2].To)
	}

	Replan(r, Options{Mode: ModeFindReplace, Pattern: "IMG_", Replace: "pic_"}, "")
	if r[0].To != "pic_001.jpg" || r[2].To != "notes.txt" {
		t.Fatalf("find/replace targets = %q %q", r[0].To, r[2].To)
	}

	// An invalid regexp marks every row instead of silently keeping names.
	r = rowsOf(rows)
	Replan(r, Options{Mode: ModeRegex, Pattern: `(`, Replace: "x"}, "")
	for i, row := range r {
		if row.Err == "" {
			t.Fatalf("row %d not marked for an invalid regexp: %+v", i, row)
		}
	}
}

func TestRenameApplyNameSwap(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-swap")
	t.Cleanup(func() { os.RemoveAll(dir) })
	for name, body := range map[string]string{"a.txt": "A", "b.txt": "B"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Swap the two names: the two-pass move must not lose either file.
	rows := []Row{{Name: "a.txt", To: "b.txt"}, {Name: "b.txt", To: "a.txt"}}
	moved, failed := Apply(dir, rows)
	if len(failed) != 0 || len(moved) != 2 {
		t.Fatalf("swap: moved %d failed %v", len(moved), failed)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "A" {
		t.Fatalf("a.txt did not become b.txt: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "B" {
		t.Fatalf("b.txt did not become a.txt: %q", b)
	}
}
