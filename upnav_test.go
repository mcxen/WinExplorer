package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// navFixture builds a small tree: <root>/a/b, plus a sibling folder, so a
// test can walk down and back up.
func navFixture(t *testing.T) (a *App, root, aDir, bDir, sibling string) {
	t.Helper()
	root = t.TempDir()
	aDir = filepath.Join(root, "alpha")
	bDir = filepath.Join(aDir, "beta")
	sibling = filepath.Join(root, "gamma")
	for _, d := range []string{bDir, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a = newStoreApp(t, root)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	return a, root, aDir, bDir, sibling
}

// selectedPaths is the tab's selection as a slice.
func selectedPaths(a *App) []string {
	out := make([]string, 0, len(a.cur().selected))
	for p := range a.cur().selected {
		out = append(out, p)
	}
	return out
}

// TestUpSelectsTheFolderLeft: 上一级 lands on the parent with the folder it
// came out of selected — Explorer's behavior — so the trip back down is one
// click.
func TestUpSelectsTheFolderLeft(t *testing.T) {
	a, _, aDir, bDir, _ := navFixture(t)
	a.navigate(bDir)
	a.up()

	if got := a.cur().path; got != aDir {
		t.Fatalf("上一级 went to %q, want %q", got, aDir)
	}
	if sel := selectedPaths(a); len(sel) != 1 || sel[0] != bDir {
		t.Fatalf("selection = %v, want just %q", sel, bDir)
	}
	if a.cur().cursor != bDir || a.cur().anchor != bDir {
		t.Fatalf("cursor/anchor = %q/%q, want the selected folder", a.cur().cursor, a.cur().anchor)
	}

	// A second 上一级 selects alpha in the root, one level further up.
	a.up()
	if got := a.cur().path; got != filepath.Dir(aDir) {
		t.Fatalf("second 上一级 went to %q", got)
	}
	if sel := selectedPaths(a); len(sel) != 1 || sel[0] != aDir {
		t.Fatalf("second selection = %v, want just %q", sel, aDir)
	}
}

// TestForwardAndBackSelectTheFolderLeft: 后退 and 前进 behave the same way —
// the folder just left is selected on the listing it lands on. The walk is
// parent → child, so each landing lists the other end.
func TestForwardAndBackSelectTheFolderLeft(t *testing.T) {
	a, _, aDir, bDir, _ := navFixture(t)
	a.navigate(aDir)
	a.navigate(bDir)

	a.back()
	if got := a.cur().path; got != aDir {
		t.Fatalf("后退 went to %q, want %q", got, aDir)
	}
	if sel := selectedPaths(a); len(sel) != 1 || sel[0] != bDir {
		t.Fatalf("后退 selection = %v, want just %q", sel, bDir)
	}

	a.forward()
	if got := a.cur().path; got != bDir {
		t.Fatalf("前进 went to %q, want %q", got, bDir)
	}
	// bDir is empty, so its parent's folder (aDir) is not in it: nothing to
	// select, which is the honest answer rather than a stale row.
	if sel := selectedPaths(a); len(sel) != 0 {
		t.Fatalf("前进 selection = %v, want none in an empty folder", sel)
	}
}

// TestNavigateClearsThePendingSelection: an ordinary navigation selects
// nothing, so the rule cannot leak into opening a folder from the listing.
func TestNavigateClearsThePendingSelection(t *testing.T) {
	a, root, aDir, _, _ := navFixture(t)
	a.up() // root has no folder to select but the pending path is consumed
	a.navigate(aDir)
	if sel := selectedPaths(a); len(sel) != 0 {
		t.Fatalf("a plain navigation left %v selected", sel)
	}
	if got := a.cur().selectPath; got != "" {
		t.Fatalf("the pending selection survived as %q", got)
	}
	_ = root
}

// TestUpSelectsNothingWhenTheFolderIsGone: the row is only selected when the
// listing still holds it, so a folder deleted in between leaves a clean
// state rather than a selection pointing nowhere.
func TestUpSelectsNothingWhenTheFolderIsGone(t *testing.T) {
	a, _, aDir, bDir, _ := navFixture(t)
	a.navigate(bDir)
	if err := os.Remove(bDir); err != nil {
		t.Fatal(err)
	}
	a.up()
	if got := a.cur().path; got != aDir {
		t.Fatalf("上一级 went to %q, want %q", got, aDir)
	}
	if sel := selectedPaths(a); len(sel) != 0 {
		t.Fatalf("a deleted folder was selected: %v", sel)
	}
}

// TestUpRaisesTheSelectedRowIntoView: the folder selected by 上一级 is
// scrolled into sight, which matters in a listing longer than the pane. The
// key drives the whole path (handleKeys → up → navigate), because up on its
// own leaves the reveal request where handleKeys would have cleared it.
func TestUpRaisesTheSelectedRowIntoView(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		if err := os.Mkdir(filepath.Join(root, dirName(i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	deep := filepath.Join(root, "d55")
	if err := os.MkdirAll(filepath.Join(deep, "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newStoreApp(t, root)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.navigate(deep)
	tt.Frame()

	tt.Key(0, ui.KeyBackspace) // 上一级
	for i := 0; i < 4; i++ {
		tt.Frame()
	}

	if got := a.cur().path; got != root {
		t.Fatalf("上一级 went to %q, want %q", got, root)
	}
	if !a.cur().selected[deep] {
		t.Fatal("the folder left behind is not selected")
	}
	idx := entryIndexOf(a.cur().entries, deep)
	first, last := a.cur().details.Visible()
	if idx < first || idx > last {
		t.Fatalf("the selected row %d is out of sight (%d..%d)", idx, first, last)
	}
}

// dirName names the fixture's folders so they sort predictably.
func dirName(i int) string { return "d" + string(rune('0'+i/10)) + string(rune('0'+i%10)) }

// TestUpFromASpecialPage: the special pages have no parent and no selection
// to make.
func TestUpFromASpecialPage(t *testing.T) {
	a, _, _, _, _ := navFixture(t)
	a.navigate(pathHome)
	a.up()
	if got := a.cur().path; got != pathHome {
		t.Fatalf("上一级 left the home page for %q", got)
	}
	if sel := selectedPaths(a); len(sel) != 0 {
		t.Fatalf("the home page kept a selection: %v", sel)
	}
}
