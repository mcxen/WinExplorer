package main

import (
	"os"
	"path/filepath"
	"testing"
)

func newBatchApp(t *testing.T, names ...string) (*App, string) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "we-batch")
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, n := range names {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	a := newTestApp(t, dir)
	return a, dir
}

func TestExpandRenamePattern(t *testing.T) {
	e := Entry{Name: "照片 (2).jpg", Ext: "jpg"}
	got := expandRenamePattern("{n} {name}", 5, e)
	if got != "5 照片 (2).jpg" {
		t.Fatalf("{n} {name} = %q", got)
	}
	got = expandRenamePattern("{stem}-{n:3}.{ext}", 7, e)
	if got != "照片 (2)-007.jpg" {
		t.Fatalf("padded = %q", got)
	}
	// A folder keeps its whole name as the stem, with no extension.
	d := Entry{Name: "项目文件夹", IsDir: true}
	if got := expandRenamePattern("[{stem}]", 1, d); got != "[项目文件夹]" {
		t.Fatalf("dir = %q", got)
	}
	// A file with no extension either.
	f := Entry{Name: "Makefile"}
	if got := expandRenamePattern("[{stem}]", 1, f); got != "[Makefile]" {
		t.Fatalf("noext = %q", got)
	}
	// {n:0} and {n:x} fall back to plain numbers.
	if got := expandRenamePattern("{n:0}", 3, f); got != "3" {
		t.Fatalf("bad width = %q", got)
	}
	if got := expandRenamePattern("{n:x}", 3, f); got != "3" {
		t.Fatalf("non-numeric width = %q", got)
	}
}

func TestBatchRenameFlow(t *testing.T) {
	a, dir := newBatchApp(t, "a.txt", "b.txt", "c.txt")
	a.selectAll()
	if len(a.selectedEntries()) != 3 {
		t.Fatalf("selection = %d", len(a.selectedEntries()))
	}

	// F2 on a multiple selection opens the batch dialog, not inline.
	a.doRenameStart()
	if !a.batch.open {
		t.Fatal("batch dialog did not open")
	}
	if a.batch.pattern != "{n} {name}" {
		t.Fatalf("default pattern = %q", a.batch.pattern)
	}

	// The preview shows every row, numbered from the start value.
	a.batch.start = 1
	a.batch.preview = a.batchPreview()
	if len(a.batch.preview) != 3 {
		t.Fatalf("preview = %d rows", len(a.batch.preview))
	}
	if a.batch.preview[0].to != "1 a.txt" || a.batch.preview[2].to != "3 c.txt" {
		t.Fatalf("rows = %+v", a.batch.preview)
	}
	if a.batchHasClash() {
		t.Fatal("clean preview flagged")
	}

	// Commit renames all three and records one undo.
	a.commitBatchRename()
	if a.batch.open {
		t.Fatal("dialog still open")
	}
	for i, want := range []string{"1 a.txt", "2 b.txt", "3 c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("row %d not renamed (%s): %v", i, want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("old name remains")
	}
	if !a.canUndo() {
		t.Fatal("no undo recorded")
	}
	// Ctrl+Z puts every name back.
	a.doUndo()
	for _, want := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("undo did not restore %s: %v", want, err)
		}
	}
}

func TestBatchRenameClashes(t *testing.T) {
	a, dir := newBatchApp(t, "x.txt", "y.txt", "existing.txt")
	// Select only x and y; existing.txt stays as the obstacle.
	a.selectOnly(filepath.Join(dir, "x.txt"))
	a.toggleSelect(filepath.Join(dir, "y.txt"))
	a.doRenameStart()
	if !a.batch.open {
		t.Fatal("dialog did not open")
	}

	// A target that an unselected item holds is a clash.
	a.batch.pattern = "existing.txt"
	a.batch.preview = a.batchPreview()
	if !a.batchHasClash() {
		t.Fatal("collision with an unselected item not flagged")
	}

	// Two selected items after one name clash with each other too.
	a.batch.pattern = "same.txt"
	a.batch.preview = a.batchPreview()
	rows := 0
	for _, r := range a.batch.preview {
		if r.clash {
			rows++
		}
	}
	if rows < 1 {
		t.Fatalf("duplicate-target clash not flagged: %+v", a.batch.preview)
	}

	// Invalid names clash: dot-dot, path separators.
	a.batch.pattern = ".."
	a.batch.preview = a.batchPreview()
	if !a.batchHasClash() {
		t.Fatal("dot-dot not flagged")
	}
	a.batch.pattern = "a/b"
	a.batch.preview = a.batchPreview()
	if !a.batchHasClash() {
		t.Fatal("path separator not flagged")
	}

	// Commit with clashes applies the clean rows only; {ext} keeps the
	// extension a rename needs.
	a.batch.pattern = "{stem}-新.{ext}"
	a.batch.preview = a.batchPreview()
	a.commitBatchRename()
	if _, err := os.Stat(filepath.Join(dir, "x-新.txt")); err != nil {
		t.Fatalf("clean row not applied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "y-新.txt")); err != nil {
		t.Fatalf("second clean row not applied: %v", err)
	}
}

func TestRenameSingleKeepsInline(t *testing.T) {
	a, dir := newBatchApp(t, "one.txt")
	a.selectOnly(filepath.Join(dir, "one.txt"))
	a.doRenameStart()
	if a.batch.open {
		t.Fatal("single selection must rename inline")
	}
	if a.cur().renaming != "one.txt" {
		t.Fatalf("inline rename not started: %q", a.cur().renaming)
	}
}
