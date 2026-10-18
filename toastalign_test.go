package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// toastalign_test.go pins the alignment work: the inline rename on
// components.Text (item 4) and the toast feedback (item 6).

// TestRenameInlineCommitsAndCancels: the inline rename — components.Text's
// Editable binding — types into the buffer, commits on Enter and cancels on
// Escape, which the editor hands to the window's shortcuts.
func TestRenameInlineCommitsAndCancels(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	file := filepath.Join(dir, "f000.txt")
	a.selectOnly(file)
	a.doRenameStart()
	if a.cur().renaming != "f000.txt" {
		t.Fatalf("rename did not start: %q", a.cur().renaming)
	}
	tt.Frame()
	// The editor holds the focus: typing reaches the buffer, not the window.
	tt.Type("-draft")
	if got := a.cur().renameBuf; got != "f000.txt-draft" {
		t.Fatalf("typed text did not reach the rename buffer: %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.cur().renaming != "" {
		t.Fatalf("Enter did not commit: renaming %q", a.cur().renaming)
	}
	if _, err := os.Stat(filepath.Join(dir, "f000.txt-draft")); err != nil {
		t.Fatalf("the rename did not land on disk: %v", err)
	}

	// Escape cancels: the buffer goes, the file stays.
	a.selectOnly(filepath.Join(dir, "f000.txt-draft"))
	a.doRenameStart()
	tt.Frame()
	tt.Type("-more")
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.cur().renaming != "" || a.cur().renameBuf != "" {
		t.Fatalf("Escape did not cancel: renaming %q buffer %q", a.cur().renaming, a.cur().renameBuf)
	}
	if _, err := os.Stat(filepath.Join(dir, "f000.txt-draft-more")); err == nil {
		t.Fatal("the cancelled rename landed on disk")
	}
	if _, err := os.Stat(filepath.Join(dir, "f000.txt-draft")); err != nil {
		t.Fatalf("the cancelled rename lost the original: %v", err)
	}
}

// TestOperationOutcomeToasts: a delete shows its outcome as a toast, with the
// 撤销 button the undo stack earned it.
func TestOperationOutcomeToasts(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	a.selectOnly(filepath.Join(dir, "f000.txt"))
	a.doDelete()
	tt.Frame()
	if a.statusMsg == "" {
		t.Fatal("the delete left no status message")
	}
	if !tt.HasText("撤销") {
		t.Fatalf("the toast carries no 撤销 button: %v", tt.Texts())
	}
	// The undo behind the toast puts the file back.
	if r, ok := tt.Find("撤销"); ok {
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	} else {
		t.Fatal("no 撤销 to click")
	}
	tt.Frame()
	if _, err := os.Stat(filepath.Join(dir, "f000.txt")); err != nil {
		t.Fatalf("the toast's 撤销 did not restore the file: %v", err)
	}
}
