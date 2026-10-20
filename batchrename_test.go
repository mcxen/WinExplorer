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
	a.batchReplan()
	if len(a.batch.rows) != 3 {
		t.Fatalf("preview = %d rows", len(a.batch.rows))
	}
	if a.batch.rows[0].To != "1 a.txt" || a.batch.rows[2].To != "3 c.txt" {
		t.Fatalf("rows = %+v", a.batch.rows)
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
	a.batchReplan()
	if !a.batchHasClash() {
		t.Fatal("collision with an unselected item not flagged")
	}

	// Two selected items after one name clash with each other too.
	a.batch.pattern = "same.txt"
	a.batchReplan()
	rows := 0
	for _, r := range a.batch.rows {
		if r.Err != "" {
			rows++
		}
	}
	if rows < 1 {
		t.Fatalf("duplicate-target clash not flagged: %+v", a.batch.rows)
	}

	// Invalid names clash: dot-dot, path separators.
	a.batch.pattern = ".."
	a.batchReplan()
	if !a.batchHasClash() {
		t.Fatal("dot-dot not flagged")
	}
	a.batch.pattern = "a/b"
	a.batchReplan()
	if !a.batchHasClash() {
		t.Fatal("path separator not flagged")
	}

	// Commit with clashes applies the clean rows only; {ext} keeps the
	// extension a rename needs.
	a.batch.pattern = "{stem}-新.{ext}"
	a.batchReplan()
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

func TestRenameRowsReorderRenumbers(t *testing.T) {
	a, dir := newBatchApp(t, "a.txt", "b.txt", "c.txt")
	a.selectAll()
	a.doRenameStart()
	if !a.batch.open || len(a.batch.rows) != 3 {
		t.Fatalf("dialog rows = %+v", a.batch.rows)
	}

	// Defaults number the rows in view order.
	if a.batch.rows[0].To != "1 a.txt" || a.batch.rows[2].To != "3 c.txt" {
		t.Fatalf("initial targets = %q, %q", a.batch.rows[0].To, a.batch.rows[2].To)
	}

	// Moving the last row to the front renumbers everything: the number is
	// the row's place, not its identity.
	a.batchMoveRows([]int{2}, 0)
	if got := []string{a.batch.rows[0].Name, a.batch.rows[1].Name, a.batch.rows[2].Name}; got[0] != "c.txt" || got[2] != "b.txt" {
		t.Fatalf("rows after the drag = %v", got)
	}
	if a.batch.rows[0].To != "1 c.txt" || a.batch.rows[1].To != "2 a.txt" || a.batch.rows[2].To != "3 b.txt" {
		t.Fatalf("renumbered targets = %q, %q, %q",
			a.batch.rows[0].To, a.batch.rows[1].To, a.batch.rows[2].To)
	}

	// Committing applies the order the drag made.
	a.commitBatchRename()
	for _, want := range []string{"1 c.txt", "2 a.txt", "3 b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("dragged order not applied (%s): %v", want, err)
		}
	}
}

func TestRenameManualRowPinsUntilReset(t *testing.T) {
	a, dir := newBatchApp(t, "a.txt", "b.txt", "c.txt")
	a.selectAll()
	a.doRenameStart()

	// A typed row keeps its name while the pattern changes under it.
	a.batch.rows[1].To = "keep.txt"
	a.batch.rows[1].Manual = true
	a.batch.pattern = "{stem}_x.{ext}"
	a.batchReplan()
	if a.batch.rows[1].To != "keep.txt" || !a.batch.rows[1].Manual {
		t.Fatalf("manual row lost its name: %+v", a.batch.rows[1])
	}
	if a.batch.rows[0].To != "a_x.txt" {
		t.Fatalf("pattern row = %q", a.batch.rows[0].To)
	}

	// Resetting hands the row back to the pattern.
	a.batchResetRow(1)
	if a.batch.rows[1].Manual || a.batch.rows[1].To != "b_x.txt" {
		t.Fatalf("reset row = %+v", a.batch.rows[1])
	}

	// The pinned name is what lands on disk.
	a.batch.rows[1].To = "keep.txt"
	a.batch.rows[1].Manual = true
	a.batchReplan()
	a.commitBatchRename()
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("manual name not applied: %v", err)
	}
}

func TestRenameRegexAndFindReplaceModes(t *testing.T) {
	rows := []Entry{
		{Name: "IMG_001.jpg", Ext: "jpg"},
		{Name: "IMG_002.jpg", Ext: "jpg"},
		{Name: "notes.txt", Ext: "txt"},
	}
	r := renameRowsFromEntries(rows)

	renameReplan(r, renameOptions{Mode: renameModeRegex, Pattern: `^IMG_(\d+)\.jpg$`, Replace: "photo-$1.jpg"}, "")
	if r[0].To != "photo-001.jpg" || r[1].To != "photo-002.jpg" || r[2].To != "notes.txt" {
		t.Fatalf("regex targets = %q %q %q", r[0].To, r[1].To, r[2].To)
	}

	renameReplan(r, renameOptions{Mode: renameModeFindReplace, Pattern: "IMG_", Replace: "pic_"}, "")
	if r[0].To != "pic_001.jpg" || r[2].To != "notes.txt" {
		t.Fatalf("find/replace targets = %q %q", r[0].To, r[2].To)
	}

	// An invalid regexp marks every row instead of silently keeping names.
	r = renameRowsFromEntries(rows)
	renameReplan(r, renameOptions{Mode: renameModeRegex, Pattern: `(`, Replace: "x"}, "")
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
	rows := []renameRow{{Name: "a.txt", To: "b.txt"}, {Name: "b.txt", To: "a.txt"}}
	moved, failed := renameApply(dir, rows)
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

func TestRenamePlanMarksCollisions(t *testing.T) {
	a, dir := newBatchApp(t, "x.txt", "y.txt", "existing.txt")
	a.selectOnly(filepath.Join(dir, "x.txt"))
	a.toggleSelect(filepath.Join(dir, "y.txt"))
	a.doRenameStart()

	// A target an unselected item holds is a clash.
	a.batch.mode = renameModeFindReplace
	a.batch.pattern, a.batch.replace = "x.txt", "existing.txt"
	a.batchReplan()
	if a.batch.rows[0].Err == "" {
		t.Fatalf("clash with an unselected item not marked: %+v", a.batch.rows[0])
	}

	// Two rows wanting one name: the first keeps it.
	a.batch.pattern, a.batch.replace = "x.txt", "y.txt"
	a.batch.rows[1].To, a.batch.rows[1].Manual = "y.txt", false
	a.batchReplan()
	marked := 0
	for _, r := range a.batch.rows {
		if r.Err != "" {
			marked++
		}
	}
	if marked == 0 {
		t.Fatalf("duplicate targets not marked: %+v", a.batch.rows)
	}
}
