package main

import (
	"os"
	"path/filepath"
	"testing"

	"sunvale/internal/rename"
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

func TestRenamePlanMarksCollisions(t *testing.T) {
	a, dir := newBatchApp(t, "x.txt", "y.txt", "existing.txt")
	a.selectOnly(filepath.Join(dir, "x.txt"))
	a.toggleSelect(filepath.Join(dir, "y.txt"))
	a.doRenameStart()

	// A target an unselected item holds is a clash.
	a.batch.mode = rename.ModeFindReplace
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
