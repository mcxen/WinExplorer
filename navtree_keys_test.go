package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// navtree_keys_test.go drives the navigation pane's keyboard cursor through
// the real window. The tests keep navigation separate from cursor movement:
// arrows and Home/End only choose a row, while Enter/Space opens it.

func navRowByKey(t *testing.T, a *App, key string) navRow {
	t.Helper()
	for _, row := range a.navRows() {
		if row.key == key {
			return row
		}
	}
	t.Fatalf("no navigation row %q", key)
	return navRow{}
}

func clickNavRow(t *testing.T, tt *ui.Tester, a *App, key string) {
	t.Helper()
	row := navRowByKey(t, a, key)
	for i, candidate := range a.navRows() {
		if candidate.key == key {
			a.navTree.list.ScrollIntoView(i)
			break
		}
	}
	tt.Frame()
	tt.Frame()
	r, ok := tt.Find(row.label)
	if !ok {
		t.Fatalf("no visible navigation row %q (%s)", key, row.label)
	}
	tt.ClickAt(r.X+r.W-24, r.Y+r.H/2)
}

func TestNavTreeKeysMoveWithoutNavigating(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	sub := filepath.Join(dir, "B-sub")
	if err := tt.Click("展开 " + filepath.Base(dir)); err != nil {
		t.Fatalf("could not open the current folder: %v", err)
	}
	clickNavRow(t, tt, a, "navtree-"+sub)
	start := a.cur().path
	if !a.navTree.focused {
		t.Fatal("clicking a tree row did not focus the navigation pane")
	}

	rows := a.navRows()
	bIndex := navTreeCursorIndex(rows, a.navTree.cursor, a.navTree.row)
	tt.Key(0, ui.KeyUp)
	rows = a.navRows()
	if a.navTree.cursor != rows[bIndex-1].key {
		t.Fatalf("Up cursor = %q, want %q", a.navTree.cursor, rows[bIndex-1].key)
	}
	tt.Key(0, ui.KeyDown)
	if a.navTree.cursor != "navtree-"+sub {
		t.Fatalf("Down cursor = %q, want the clicked row", a.navTree.cursor)
	}
	if a.cur().path != start {
		t.Fatalf("moving the cursor navigated to %q, want %q", a.cur().path, start)
	}

	tt.Key(0, ui.KeyEnd)
	rows = a.navRows()
	if rows[navTreeCursorIndex(rows, a.navTree.cursor, -1)].kind == navRowSection {
		t.Fatal("End landed on a section heading")
	}
	tt.Key(0, ui.KeyHome)
	if a.navTree.cursor != "nav-home" {
		t.Fatalf("Home cursor = %q, want nav-home", a.navTree.cursor)
	}
	if a.cur().path != start {
		t.Fatalf("Home/End navigated to %q, want %q", a.cur().path, start)
	}
}

func TestNavTreeKeysExpandCollapseAndActivate(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	sub := filepath.Join(dir, "a-sub")
	twist := "展开 " + filepath.Base(dir)
	if err := tt.Click(twist); err != nil {
		t.Fatalf("could not open the current folder: %v", err)
	}
	if err := tt.Click(twist); err != nil {
		t.Fatalf("could not close the current folder: %v", err)
	}
	start := a.cur().path

	tt.Key(0, ui.KeyRight)
	if !a.navTree.open[dir] {
		t.Fatal("Right did not expand the focused folder")
	}
	tt.Key(0, ui.KeyRight)
	if a.navTree.cursor != "navtree-"+sub {
		t.Fatalf("Right cursor = %q, want first child %q", a.navTree.cursor, sub)
	}
	if a.cur().path != start {
		t.Fatalf("entering a child navigated to %q, want %q", a.cur().path, start)
	}

	tt.Key(0, ui.KeyLeft)
	if a.navTree.cursor != "navtree-"+dir {
		t.Fatalf("Left cursor = %q, want parent %q", a.navTree.cursor, dir)
	}
	tt.Key(0, ui.KeyLeft)
	if a.navTree.open[dir] {
		t.Fatal("Left did not collapse the focused folder")
	}

	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyRight)
	tt.Key(0, ui.KeyEnter)
	if a.cur().path != sub {
		t.Fatalf("Enter navigated to %q, want %q", a.cur().path, sub)
	}

	sibling := filepath.Join(dir, "B-sub")
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeySpace)
	if a.cur().path != sibling {
		t.Fatalf("Space navigated to %q, want %q", a.cur().path, sibling)
	}
}

func TestNavTreeKeysLeaveOnTabAndFilePane(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	if err := tt.Click("展开 " + filepath.Base(dir)); err != nil {
		t.Fatalf("could not open the current folder: %v", err)
	}
	if !a.navTree.focused {
		t.Fatal("the navigation twist did not take focus")
	}
	tt.Key(0, ui.KeyTab)
	if a.navTree.focused {
		t.Fatal("Tab left the navigation pane focused")
	}

	if err := tt.Click("展开 " + filepath.Base(dir)); err != nil {
		t.Fatalf("could not close the current folder: %v", err)
	}
	file, ok := tt.Find("file.txt")
	if !ok {
		t.Fatalf("file.txt is not laid out: %v", tt.Texts())
	}
	tt.ClickAt(file.X+file.W/2, file.Y+file.H/2)
	if a.navTree.focused {
		t.Fatal("clicking the file pane left the navigation pane focused")
	}
}

func TestNavRowsCarryTreeDepth(t *testing.T) {
	a, _, dir := navTreeApp(t)
	sub := filepath.Join(dir, "a-sub")
	a.navTree.open[dir] = true
	rows := a.navRows()
	depths := map[string]int{}
	for _, row := range rows {
		depths[row.key] = row.depth
	}
	if got := depths["nav-thispc"]; got != -1 {
		t.Fatalf("This PC depth = %d, want -1", got)
	}
	rootDepth, ok := depths["navtree-/"]
	if !ok || rootDepth != 0 {
		t.Fatalf("root depth = %d (present %v), want 0", rootDepth, ok)
	}
	if got, want := depths["navtree-"+dir], depths["navtree-"+sub]-1; got != want {
		t.Fatalf("folder depth = %d, child depth = %d; want child one deeper", got, depths["navtree-"+sub])
	}
}
