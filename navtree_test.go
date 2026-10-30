package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// navtree_test.go drives the navigation pane's folder tree: the lazy read behind
// a folder, the sync that follows the tab, and the rows the pane draws.

// navTreeApp is an app over a folder of two subfolders, a file and a hidden
// folder, with 此电脑 expanded so the tree is on screen.
func navTreeApp(t *testing.T) (*App, *ui.Tester, string) {
	t.Helper()
	a, tt, dir := keynavWindow(t, 1)
	for _, name := range []string{"a-sub", "B-sub", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	a.navPCExpanded = true
	// These tests drive 展开到打开的文件夹, off by default since 0.3.6.
	on := true
	a.applySettings(func(x *appSettings) { x.TreeExpand = &on })
	a.navTree.synced = "" // follow the folder keynavWindow opened
	// A real pane takes width — the test app starts at 0 — or its rows have no
	// box to click or paint.
	a.navWidth = navMinWidth
	a.reload()
	// The tree's reads are off the view path now: open the chain and gather
	// the rows once outside the view, as an app that has just started does.
	a.navTreeFollow()
	a.navRows()
	// A few frames: the tree's rows need one to be laid out with a box, and the
	// reveal that scrolls the current folder's row into the pane needs the next.
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	return a, tt, dir
}

// TestNavTreeChildrenAreFoldersAndCached: the read behind a node lists folders
// only, by name, hidden ones per the setting, and is kept until dropped.
func TestNavTreeChildrenAreFoldersAndCached(t *testing.T) {
	a, _, dir := navTreeApp(t)
	got := namesOfEntries(a.navChildren(dir))
	if len(got) != 2 || got[0] != "a-sub" || got[1] != "B-sub" {
		t.Fatalf("children = %v, want the two folders", got)
	}
	// The read is kept: a folder created now does not appear until it is
	// dropped, which is what a reload does.
	if err := os.MkdirAll(filepath.Join(dir, "c-new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := namesOfEntries(a.navChildren(dir)); len(got) != 2 {
		t.Fatalf("children = %v, want the read kept", got)
	}
	a.navChildrenDrop(dir)
	if got := namesOfEntries(a.navChildren(dir)); len(got) != 3 {
		t.Fatalf("children = %v, want the new folder after the drop", got)
	}
	// 隐藏的项目 shows the dot-folders too.
	a.showHidden = true
	a.navChildrenClear()
	if got := namesOfEntries(a.navChildren(dir)); len(got) != 4 {
		t.Fatalf("children with hidden = %v, want four", got)
	}
}

// namesOfEntries lists entries by name, for the tree's own order.
func namesOfEntries(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

// TestNavSyncOpensTheAncestors: following a tab into a nested folder opens the
// chain above it — up to the volume, not past it — and a special page is left
// out of the tree.
func TestNavSyncOpensTheAncestors(t *testing.T) {
	a, _, dir := navTreeApp(t)
	deep := filepath.Join(dir, "a-sub", "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	a.navigate(deep)
	a.navTreeFollow()
	if !a.navTree.open[dir] || !a.navTree.open[filepath.Join(dir, "a-sub")] {
		t.Fatalf("the ancestors are not open: %v", a.navTree.open)
	}
	if a.navTree.open[deep] {
		t.Fatal("the sync opened the current folder itself")
	}
	if a.navTree.synced != deep || a.navTree.reveal != deep {
		t.Fatalf("the tree follows %q, reveals %q", a.navTree.synced, a.navTree.reveal)
	}
	// The climb stops at the volume root: /Volumes is not opened for a folder
	// on the volume under it.
	if a.navTree.open["/Volumes"] {
		t.Fatal("the sync climbed past the volume root")
	}
	// A special page has no row to reveal.
	a.navigate(pathThisPC)
	a.navTreeFollow()
	if a.navTree.reveal != "" {
		t.Fatalf("a special page asked for a reveal: %q", a.navTree.reveal)
	}
}

// TestNavTreeRowFollowsTheWindow: the pane draws the chain to the current
// folder, marks its row selected, and navigates when a tree row is clicked.
func TestNavTreeRowFollowsTheWindow(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	sub := filepath.Join(dir, "a-sub")
	a.navigate(sub)
	a.navTreeFollow()
	a.navRows()
	// The rows for the new place take a frame to be laid out, and the reveal
	// that scrolls the current folder's row into view the one after that.
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	if a.navTree.synced != sub {
		t.Fatalf("the tree follows %q, want %q", a.navTree.synced, sub)
	}
	// The current folder's row is the pane's, marked selected and in sight: the
	// pane's list is virtualized, so its place in that list is the check.
	if a.navTree.row < 0 {
		t.Fatalf("the pane lists no row for the current folder %q", sub)
	}
	first, last := a.navTree.list.Visible()
	if a.navTree.row < first || a.navTree.row > last {
		t.Fatalf("the current folder's row %d is out of sight (%d-%d)", a.navTree.row, first, last)
	}
	// The row carries the selection colour. Its twist is the one text the pane's
	// breadcrumb does not share, so it names the row to look at.
	chev, ok := tt.Find("展开 " + filepath.Base(sub))
	if !ok {
		t.Fatalf("the tree does not draw the current folder's row: %v", tt.Texts())
	}
	sel := colorSelected()
	if visualV2 {
		// v2 paints the chosen row as a white chip on the wash, not tokSel.
		sel = colorContent()
	}
	if n := colorPixels(tt.Image(), ui.Rect{X: 8, Y: chev.Y, W: 100, H: chev.H}, sel); n == 0 {
		t.Fatalf("the row at %v is not marked selected (v2=%v)", chev, visualV2)
	}
	// Clicking a tree row navigates there: the folder beside the current one,
	// scrolled into sight first, since the reveal scrolls only as far as the
	// current row needs. The click lands past the row's
	// twist — the name's end — since the row's own middle is where the chevron
	// sits on an indented folder.
	sibling := filepath.Join(dir, "B-sub")
	for i, r := range a.navRows() {
		if r.path == sibling {
			a.navTree.list.ScrollIntoView(i)
		}
	}
	tt.Frame()
	tt.Frame()
	row, ok := tt.Find("B-sub")
	if !ok {
		t.Fatalf("no tree row for the sibling folder: %v", tt.Texts())
	}
	tt.ClickAt(row.X+row.W-24, row.Y+row.H/2)
	if a.cur().path != sibling {
		t.Fatalf("clicking the tree row left the tab in %q, want %q", a.cur().path, sibling)
	}
}

// TestNavTreeChevronOpensWithoutNavigating: the chevron opens a folder's
// children — read then, not before — and leaves the tab where it was.
func TestNavTreeChevronOpensWithoutNavigating(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	sub := filepath.Join(dir, "a-sub")
	deep := filepath.Join(sub, "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// The tab is in dir, and a tree shows the current folder without opening
	// it: open it first to have a-sub's row, as a click on its chevron would.
	if _, read := a.navTree.kids[dir]; read {
		t.Fatal("the tree read a collapsed folder")
	}
	if err := tt.Click("展开 " + filepath.Base(dir)); err != nil {
		t.Fatalf("no chevron for the current folder: %v", err)
	}
	if !a.navTree.open[dir] {
		t.Fatal("the chevron did not open the folder")
	}
	if _, read := a.navTree.kids[sub]; read {
		t.Fatal("the tree read a folder nobody opened")
	}
	// The open folder's children are read off the view path; the row shows
	// on the next frame after that read.
	folderNames.wait(dir)
	tt.Frame()
	start := a.cur().path
	if err := tt.Click("展开 a-sub"); err != nil {
		t.Fatalf("no chevron for the folder: %v", err)
	}
	if !a.navTree.open[sub] {
		t.Fatal("the chevron did not open the folder")
	}
	if a.cur().path != start {
		t.Fatalf("the chevron navigated to %q", a.cur().path)
	}
	folderNames.wait(sub)
	tt.Frame()
	if !tt.HasText("deeper") {
		t.Fatalf("the opened folder shows no children: %v", tt.Texts())
	}
	// Clicking it again closes the folder.
	if err := tt.Click("展开 a-sub"); err != nil {
		t.Fatal(err)
	}
	if a.navTree.open[sub] {
		t.Fatal("the chevron did not close the folder")
	}
}
