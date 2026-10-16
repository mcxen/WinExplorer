package main

import (
	"os"
	"path/filepath"
	"testing"
)

// navApp makes a listing of four files, a.txt through d.txt, and returns the
// app showing it and the folder.
func navApp(t *testing.T) (*App, string) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "we-keynav")
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(t, dir)
	// newAppWithStore starts ascending; the bare test app takes the zero
	// value, which would walk the listing backwards.
	a.sortAsc = true
	if got := len(a.visibleEntries()); got != 4 {
		t.Fatalf("listing = %d, want 4", got)
	}
	return a, dir
}

func navNames(t *testing.T, a *App) []string {
	t.Helper()
	var out []string
	for _, e := range a.selectedEntries() {
		out = append(out, e.Name)
	}
	return out
}

func navIs(t *testing.T, a *App, what string, want ...string) {
	t.Helper()
	got := navNames(t, a)
	if len(got) != len(want) {
		t.Fatalf("%s selected %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s selected %v, want %v", what, got, want)
		}
	}
}

func TestArrowKeysStepTheSelection(t *testing.T) {
	a, dir := navApp(t)

	// Down from nothing takes the first item, then walks the listing, one
	// item selected at a time and asked to show itself.
	for i, name := range []string{"a.txt", "b.txt", "c.txt"} {
		a.moveSelection(1, 0, false)
		navIs(t, a, "Down", name)
		if want := filepath.Join(dir, name); a.cur().reveal != want {
			t.Fatalf("step %d left reveal = %q, want %q", i, a.cur().reveal, want)
		}
	}
	// Up walks back, replacing the selection rather than extending it.
	a.moveSelection(-1, 0, false)
	navIs(t, a, "Up", "b.txt")

	// The ends hold rather than wrapping.
	a.moveSelectionEnd(false, false)
	a.moveSelection(-1, 0, false)
	navIs(t, a, "Up at the top", "a.txt")
	a.moveSelectionEnd(true, false)
	a.moveSelection(1, 0, false)
	navIs(t, a, "Down at the bottom", "d.txt")

	// Up from nothing takes the last item.
	a.clearSelection()
	a.moveSelection(-1, 0, false)
	navIs(t, a, "Up from nothing", "d.txt")
}

func TestShiftArrowsExtendFromTheAnchor(t *testing.T) {
	a, dir := navApp(t)
	a.moveSelectionEnd(false, false) // anchor a.txt
	a.moveSelection(1, 0, false)     // b.txt
	a.moveSelection(1, 0, true)      // b.txt..c.txt
	navIs(t, a, "Shift+Down", "b.txt", "c.txt")
	a.moveSelection(1, 0, true)  // out to d.txt
	a.moveSelection(-1, 0, true) // and back in to c.txt
	navIs(t, a, "Shift+Up", "b.txt", "c.txt")
	if want := filepath.Join(dir, "b.txt"); a.cur().anchor != want {
		t.Fatalf("anchor = %q, want %q: Shift must not move it", a.cur().anchor, want)
	}

	// Shift+End takes the rest of the listing; a plain step drops it.
	a.moveSelectionEnd(true, true)
	navIs(t, a, "Shift+End", "b.txt", "c.txt", "d.txt")
	a.moveSelection(1, 0, false)
	navIs(t, a, "a plain step", "d.txt")
}

func TestArrowKeysStepIconRowsAndPages(t *testing.T) {
	a, _ := navApp(t)
	a.mode = ViewMediumIcons
	a.cur().gridCols = 3
	a.cur().gridRows = 2

	a.moveSelection(1, 0, false) // a.txt
	a.moveSelection(1, 0, false) // down a row of three: d.txt
	navIs(t, a, "Down a row", "d.txt")
	a.moveSelection(0, 1, false) // Right, with nowhere to go
	navIs(t, a, "Right at the end", "d.txt")

	a.moveSelectionEnd(false, false)
	a.moveSelection(0, 1, false) // Right across the row: b.txt
	navIs(t, a, "Right", "b.txt")
	a.moveSelectionPage(1, false) // a screenful of six, held to the last
	navIs(t, a, "Page Down", "d.txt")
}

func TestArrowKeysIgnoreSidewaysInTheRowViews(t *testing.T) {
	a, _ := navApp(t)
	a.mode = ViewDetails
	a.moveSelection(1, 0, false)
	a.moveSelection(0, 1, false)
	navIs(t, a, "Right in 详细信息", "a.txt")
	// Without a laid-out list a page is one row, so Page Down steps once.
	a.moveSelectionPage(1, false)
	navIs(t, a, "Page Down", "b.txt")
}

// TestRevealRowSkipsGroupHeaders: 分组依据 puts a header over each group, so
// the row the view scrolls to is not the entry's listing index.
func TestRevealRowSkipsGroupHeaders(t *testing.T) {
	a, dir := navApp(t)
	a.grouping = SortName
	rows := a.groupedRows(a.visibleEntries())
	if len(rows) == 0 || rows[0].header == "" {
		t.Fatalf("no group header over %d rows", len(rows))
	}
	if got := rowIndexOf(rows, filepath.Join(dir, "a.txt")); got != 1 {
		t.Fatalf("the first entry sits at row %d, want 1 under its header", got)
	}
	if got := rowIndexOf(rows, ""); got != -1 {
		t.Fatalf("an empty path matched a row: %d", got)
	}
	if got := rowIndexOf(rows, filepath.Join(dir, "nope")); got != -1 {
		t.Fatalf("a missing entry sits at row %d, want -1", got)
	}
	if got := entryIndexOf(a.visibleEntries(), filepath.Join(dir, "d.txt")); got != 3 {
		t.Fatalf("the last entry is at %d, want 3", got)
	}
}
