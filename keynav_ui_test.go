package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// keynavWindow boots the real window view in a headless tester over a folder
// of n files named 00.txt…, and returns the app, the tester and the folder.
func keynavWindow(t *testing.T, n int) (*App, *ui.Tester, string) {
	t.Helper()
	dir, _ := os.MkdirTemp("", "we-keynav-ui")
	t.Cleanup(func() { os.RemoveAll(dir) })
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, fmt.Sprintf("f%03d.txt", i))
		if err := os.WriteFile(name, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, dir)
	// The bare test app takes grouping's zero value, which is 分组依据>
	// 名称, and iconZoom's, which would size the icons to nothing:
	// newAppWithStore turns the first off and the second to 1.
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	return a, ui.NewTester(a.view, 1200, 800), dir
}

// TestArrowKeysDriveTheWindow exercises the shortcut table, the pane's
// selection and its views together in a laid-out window: the keys reach
// handleKeys, and a modal keeps them while it is up.
func TestArrowKeysDriveTheWindow(t *testing.T) {
	a, tt, dir := keynavWindow(t, 4)

	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down", "f000.txt")
	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down", "f001.txt")
	tt.Key(ui.Shift, ui.KeyDown)
	navIs(t, a, "Shift+Down", "f001.txt", "f002.txt")
	if want := filepath.Join(dir, "f001.txt"); a.cur().anchor != want {
		t.Fatalf("anchor = %q, want %q", a.cur().anchor, want)
	}
	tt.Key(0, ui.KeyEnd)
	navIs(t, a, "End", "f003.txt")
	tt.Key(ui.Shift, ui.KeyHome)
	navIs(t, a, "Shift+Home", "f000.txt", "f001.txt", "f002.txt", "f003.txt")
	tt.Key(0, ui.KeyUp)
	navIs(t, a, "Up after Shift+Home", "f000.txt")

	// 空格 opens the quick preview, and while it is up the arrows stay with
	// it rather than moving the pane's selection.
	tt.Key(0, ui.KeySpace)
	if !a.quickLook {
		t.Fatal("空格 did not open the quick preview")
	}
	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down behind the quick preview", "f000.txt")
	tt.Key(0, ui.KeyEscape)
	if a.quickLook {
		t.Fatal("Escape did not close the quick preview")
	}
	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down after the quick preview", "f001.txt")
}

// TestArrowKeysScrollTheWindow walks a listing taller than the pane and
// checks the moved-to row comes into view, through the ListState the
// details view keeps.
func TestArrowKeysScrollTheWindow(t *testing.T) {
	a, tt, dir := keynavWindow(t, 200)

	visible := func() (int, int) { return a.cur().details.Visible() }
	want := filepath.Join(dir, "f199.txt")
	tt.Key(0, ui.KeyEnd)
	navIs(t, a, "End", "f199.txt")
	first, last := visible()
	if row := entryIndexOf(a.visibleEntries(), want); row < first || row > last {
		t.Fatalf("End left rows %d..%d in view, want the row of f199.txt (%d)", first, last, row)
	}
	tt.Key(0, ui.KeyHome)
	navIs(t, a, "Home", "f000.txt")
	if first, _ = visible(); first != 0 {
		t.Fatalf("Home left rows from %d, want the first", first)
	}

	// A screenful down moves the selection and keeps it in view.
	page := a.navPage()
	wantName := fmt.Sprintf("f%03d.txt", page)
	tt.Key(0, ui.KeyPageDown)
	navIs(t, a, "Page Down", wantName)
	first, last = visible()
	if row := entryIndexOf(a.visibleEntries(), filepath.Join(dir, wantName)); row < first || row > last {
		t.Fatalf("the moved-to row %d is not in view (%d..%d)", row, first, last)
	}
}

// TestArrowKeysStepIconRowsInTheWindow checks the arrows' row step against
// the columns the grid actually lays out, and that the tile stepped to comes
// into view.
func TestArrowKeysStepIconRowsInTheWindow(t *testing.T) {
	a, tt, dir := keynavWindow(t, 200)
	a.mode = ViewExtraLarge
	tt.Frame()

	// The tiles of a row: the boxes of the first few, which share a top.
	first, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatal("the first tile is not built")
	}
	cols := 1
	for i := 1; i < 200; i++ {
		r, ok := tt.Find(fmt.Sprintf("f%03d.txt", i))
		if !ok || r.Y != first.Y {
			break
		}
		cols++
	}
	if a.cur().gridCols != cols {
		t.Fatalf("the arrows step by %d columns, the grid lays %d", a.cur().gridCols, cols)
	}

	tt.Key(0, ui.KeyDown) // the first tile
	tt.Key(0, ui.KeyDown) // and down a row
	want := fmt.Sprintf("f%03d.txt", cols)
	if got := navNames(t, a); len(got) != 1 || got[0] != want {
		t.Fatalf("Down a row selected %v, want [%s]", got, want)
	}
	if _, ok := tt.Find(want); !ok {
		t.Fatalf("%s is not on show after the move", want)
	}

	// Walking down scrolls the grid along with the selection: the row it
	// lands on is built and in the window, and the rows it left are not.
	for i := 0; i < 40; i++ {
		tt.Key(0, ui.KeyDown)
	}
	sel := navNames(t, a)
	if len(sel) != 1 {
		t.Fatalf("selection = %v", sel)
	}
	if row := entryIndexOf(a.visibleEntries(), filepath.Join(dir, sel[0])); row < 0 {
		t.Fatalf("%s is not in the listing", sel[0])
	}
	box, ok := tt.Find(sel[0])
	if !ok {
		t.Fatalf("%s is not built after the move: the grid did not follow it", sel[0])
	}
	if box.Y < 0 || box.Y+box.H > 800 {
		t.Fatalf("%s sits at y=%v, outside the window", sel[0], box.Y)
	}
	if _, ok := tt.Find("f000.txt"); ok {
		t.Fatal("the grid did not scroll: the first tile is still built")
	}
}

// TestArrowKeysLeaveTheSearchFieldAlone: while the search field has the
// keyboard focus the arrows belong to it — ↑/↓ and the Page keys are not
// its own, so they do nothing rather than moving the pane's selection out
// from under the query — and a click on a row hands them back.
func TestArrowKeysLeaveTheSearchFieldAlone(t *testing.T) {
	a, tt, _ := keynavWindow(t, 4)
	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down", "f000.txt")

	tt.Key(ui.Ctrl, ui.KeyE) // 聚焦搜索框
	tt.Type("f")
	if a.cur().query != "f" {
		t.Fatalf("the search field took no text: query = %q", a.cur().query)
	}
	if !a.fieldFocused {
		t.Fatal("the focused search field is not reported as such")
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyPageDown)
	tt.Key(ui.Shift, ui.KeyDown)
	navIs(t, a, "the arrows in the search field", "f000.txt")

	// Clicking a row moves the keyboard back to the pane.
	if r, ok := tt.Find("f001.txt"); ok {
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	}
	if a.fieldFocused {
		t.Fatal("the search field still holds the focus")
	}
	tt.Key(0, ui.KeyDown)
	navIs(t, a, "Down after the click", "f002.txt")
}

// TestArrowKeysScrollAGroupedIconView: 分组依据 lays each group's tiles in a
// grid inside one plain scroll, so the arrows still step by a row and the
// tile stepped to raises itself into view.
func TestArrowKeysScrollAGroupedIconView(t *testing.T) {
	a, tt, _ := keynavWindow(t, 120)
	a.mode, a.grouping = ViewLargeIcons, SortName
	tt.Frame()

	first, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatal("the first tile is not built")
	}
	cols := 1
	for i := 1; i < 120; i++ {
		r, ok := tt.Find(fmt.Sprintf("f%03d.txt", i))
		if !ok || r.Y != first.Y {
			break
		}
		cols++
	}
	if a.cur().gridCols != cols {
		t.Fatalf("the arrows step by %d columns, the grouped grid lays %d", a.cur().gridCols, cols)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	if got := navNames(t, a); len(got) != 1 || got[0] != fmt.Sprintf("f%03d.txt", cols) {
		t.Fatalf("Down a row selected %v, want one row on", got)
	}

	// Walking down brings the tiles into view.
	for i := 0; i < 20; i++ {
		tt.Key(0, ui.KeyDown)
	}
	sel := navNames(t, a)
	if len(sel) != 1 {
		t.Fatalf("selection = %v", sel)
	}
	box, ok := tt.Find(sel[0])
	if !ok || box.W <= 0 {
		t.Fatalf("%s does not show after the move: the grouped view did not follow it", sel[0])
	}
	if box.Y < 0 || box.Y+box.H > 800 {
		t.Fatalf("%s sits at y=%v, outside the window", sel[0], box.Y)
	}
	// Every tile of a group is built, so what says the view scrolled is the
	// first one being clipped away rather than moved off.
	if first, ok := tt.Find("f000.txt"); ok && first.W > 0 {
		t.Fatal("the grouped view did not scroll: the first tile still shows")
	}
}

// TestArrowKeysScrollTheListView: 列表 keeps a place of its own, and the
// arrows scroll it as they do the details view.
func TestArrowKeysScrollTheListView(t *testing.T) {
	a, tt, dir := keynavWindow(t, 200)
	a.mode = ViewList
	tt.Frame()

	tt.Key(0, ui.KeyEnd)
	navIs(t, a, "End", "f199.txt")
	first, last := a.cur().list.Visible()
	if row := entryIndexOf(a.visibleEntries(), filepath.Join(dir, "f199.txt")); row < first || row > last {
		t.Fatalf("End left rows %d..%d in view, want the row of f199.txt (%d)", first, last, row)
	}
	tt.Key(0, ui.KeyHome)
	navIs(t, a, "Home", "f000.txt")
	if first, _ := a.cur().list.Visible(); first != 0 {
		t.Fatalf("Home left rows from %d, want the first", first)
	}
}
