package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// navJumpApp is a window whose pane lists a pinned folder, three recent ones
// and the tree, with the places living outside the folder the tab is in so
// their names name pane rows only.
func navJumpApp(t *testing.T) (*App, *ui.Tester, map[string]string) {
	t.Helper()
	a, tt, _ := navTreeApp(t)
	others, err := os.MkdirTemp("", "we-navjump")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(others) })
	places := map[string]string{}
	for _, n := range []string{"固定甲", "最近乙", "最近丙", "最近丁"} {
		p := filepath.Join(others, n)
		if err := os.MkdirAll(filepath.Join(p, "子目录"), 0o755); err != nil {
			t.Fatal(err)
		}
		places[n] = p
	}
	a.store.Pinned = append(a.store.Pinned, places["固定甲"])
	for _, n := range []string{"最近丁", "最近丙", "最近乙"} {
		a.store.pushVisit(places[n])
	}
	a.navTree.list.ScrollTo(0, ui.Start)
	for i := 0; i < 4; i++ {
		tt.Frame()
	}
	return a, tt, places
}

func navRowIndex(a *App, key string) int {
	for i, r := range a.navRows() {
		if r.key == key {
			return i
		}
	}
	return -1
}

func navRowY(t *testing.T, tt *ui.Tester, label string) float32 {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no row %q in %v", label, tt.Texts())
	}
	return r.Y
}

// TestNavRecentClickHoldsStill: clicking a 最近位置 row keeps that row, the
// rows around it and the scroll where they were, lights the clicked row up
// rather than its twin in the tree, and leaves the tree closed.
func TestNavRecentClickHoldsStill(t *testing.T) {
	a, tt, places := navJumpApp(t)
	first, last := a.navTree.list.Visible()
	// The tab's title shows the folder's name too, so rows are told apart by
	// their place in the list, and 此电脑 by its twist.
	idx := navRowIndex(a, "nav-recent-"+places["最近丙"])
	pc := navRowY(t, tt, "展开此电脑")
	openBefore := len(a.navTree.open)
	if err := tt.Click("最近丙"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	if a.cur().path != places["最近丙"] {
		t.Fatalf("went to %q", a.cur().path)
	}
	if a.navTree.clicked != "nav-recent-"+places["最近丙"] {
		t.Fatalf("clicked row %q", a.navTree.clicked)
	}
	if got := navRowIndex(a, "nav-recent-"+places["最近丙"]); got != idx {
		t.Fatalf("the clicked row moved from row %d to %d", idx, got)
	}
	if got := navRowY(t, tt, "展开此电脑"); got != pc {
		t.Fatalf("此电脑 moved from %v to %v", pc, got)
	}
	if f, l := a.navTree.list.Visible(); f != first || l != last {
		t.Fatalf("visible rows %d-%d, were %d-%d", f, l, first, last)
	}
	if len(a.navTree.open) != openBefore || a.navTree.open[filepath.Dir(places["最近丙"])] {
		t.Fatalf("a recent click opened the tree: %v", a.navTree.open)
	}
	rows := a.navRows()
	if a.navTree.row < 0 || rows[a.navTree.row].key != "nav-recent-"+places["最近丙"] {
		t.Fatalf("selected row %d, want the recent one", a.navTree.row)
	}
	// A pinned click does the same, and the recent row it left stays put.
	if err := tt.Click("固定甲"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		tt.Frame()
	}
	if got := navRowIndex(a, "nav-recent-"+places["最近丙"]); got != idx {
		t.Fatalf("leaving a recent row moved it from row %d to %d", idx, got)
	}
	if f, l := a.navTree.list.Visible(); f != first || l != last {
		t.Fatalf("visible rows %d-%d after the pinned click, were %d-%d", f, l, first, last)
	}
	if a.navTree.open[filepath.Dir(places["固定甲"])] {
		t.Fatal("a quick-access click opened the tree")
	}
}

// TestNavRecentsHoldTheirOrder: within a window 最近位置 keeps its order as
// the user moves between its folders; a folder joins at the end once left,
// and 刷新 sorts it newest first again.
func TestNavRecentsHoldTheirOrder(t *testing.T) {
	a, tt, places := navJumpApp(t)
	order := func() []string {
		var out []string
		for _, r := range a.recentPlaceRows() {
			out = append(out, r.label)
		}
		return out
	}
	before := order()
	if len(before) < 3 || before[0] != "最近乙" || before[1] != "最近丙" || before[2] != "最近丁" {
		t.Fatalf("recents %v", before)
	}
	for _, n := range []string{"最近丁", "最近乙", "最近丙"} {
		a.navFromPane("nav-recent-"+places[n], places[n], false)
		a.navigate(places[n])
		tt.Frame()
	}
	after := order()
	for i, n := range before {
		if after[i] != n {
			t.Fatalf("recents reordered: %v, were %v", after, before)
		}
	}
	a.navRecentReset()
	if got := order(); got[0] != "最近乙" {
		t.Fatalf("after reset the newest is not first: %v", got)
	}
}

// TestNavTreeClickStillSyncs: a tree row and the file pane still open the
// tree down to the folder, and a folder reached from the file pane selects
// the first row naming it.
func TestNavTreeClickStillSyncs(t *testing.T) {
	a, tt, dir := navTreeApp(t)
	deep := filepath.Join(dir, "a-sub", "deeper")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	a.navFromPane("nav-recent-x", "/elsewhere", false)
	a.navigate(deep)
	tt.Frame()
	if !a.navTree.open[filepath.Join(dir, "a-sub")] {
		t.Fatal("the file pane's navigation did not open the tree")
	}
	if a.navTree.clicked != "" {
		t.Fatalf("a stale click survived: %q", a.navTree.clicked)
	}
}
