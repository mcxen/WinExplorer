package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestEditMenuCommandsReachThePane covers the macOS clipboard fix: the Edit
// menu takes Cmd+C/X/V before the key reaches the view and delivers a
// "copy"/"cut"/"paste" command to the focused element instead, so the file
// rows and gallery tiles take those commands themselves. A focused text
// field still takes them first, which is why this is bound on the rows and
// not on the window.
//
// selectAll is the probe: it goes through the same path as copy, and unlike
// copy it does not touch the system clipboard, which a headless tester
// cannot service.
func TestEditMenuCommandsReachThePane(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hi"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if r, ok := tt.Find("a.txt"); ok {
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	} else {
		t.Fatalf("the row is not built: %v", tt.Texts())
	}
	tt.Frame()
	if n := len(a.selectedEntries()); n != 1 {
		t.Fatalf("the click selected %d entries, want 1", n)
	}

	// As macOS delivers Cmd+A: an edit command to the focused element.
	tt.Command("selectAll")
	tt.Frame()
	if n := len(a.selectedEntries()); n != 2 {
		t.Fatalf("the Edit menu's selectAll selected %d entries, want both", n)
	}
}

// TestPaneCommandNames checks the mapping from the Edit menu's command names
// to the pane's actions, including the names macOS may send for paste.
func TestPaneCommandNames(t *testing.T) {
	dir := t.TempDir()
	a := newStoreApp(t, dir)
	a.navigate(dir)

	// Every command the Edit menu can send while the listing is focused is
	// one the pane handles; an unknown name is refused rather than swallowed.
	for _, name := range []string{"copy", "cut", "paste", "pasteAndMatchStyle", "selectAll", "undo", "delete"} {
		if !paneHandlesCommand(name) {
			t.Fatalf("the pane does not handle %q", name)
		}
	}
	for _, name := range []string{"bold", "alignCenter", "redo", "nonsense"} {
		if paneHandlesCommand(name) {
			t.Fatalf("the pane claims to handle %q", name)
		}
	}
	if a.paneCommand(ui.InputEvent{Kind: ui.InputKeyDown, Text: "copy"}) {
		t.Fatal("a key event was taken as an edit command")
	}
}
