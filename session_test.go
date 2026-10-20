package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestVisitsAreOrderedDedupedAndCapped covers 最近位置's store rules in one
// pass: newest first, a revisit moves the folder to the head, special pages
// are not folders, a forgotten folder leaves the list, and the history
// stops at maxVisited.
func TestVisitsAreOrderedDedupedAndCapped(t *testing.T) {
	st := &persistedState{}
	st.pushVisit("/a")
	st.pushVisit("/b")
	st.pushVisit("/c")
	if got := st.visitList(0); len(got) != 3 || got[0] != "/c" || got[1] != "/b" || got[2] != "/a" {
		t.Fatalf("visited = %v, want [/c /b /a]", got)
	}
	st.pushVisit("/a")
	if got := st.visitList(2); len(got) != 2 || got[0] != "/a" || got[1] != "/c" {
		t.Fatalf("after revisiting /a the newest two are %v, want [/a /c]", got)
	}
	st.pushVisit(pathHome)
	if got := st.visitList(0); len(got) != 3 {
		t.Fatalf("a special page was recorded: %v", got)
	}
	st.forgetVisit("/c")
	if got := st.visitList(0); len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("after forgetting /c visited = %v, want [/a /b]", got)
	}

	for i := 0; i < maxVisited+10; i++ {
		st.pushVisit(filepath.Join("/dir", string(rune('a'+i%26)), string(rune('0'+i/26))))
	}
	if n := len(st.visitList(0)); n > maxVisited {
		t.Fatalf("history kept %d folders, want at most %d", n, maxVisited)
	}
}

// TestRecentPlaceRows: navigating records the folder, the sidebar shows it,
// and it leaves out both the folder the tab is already in — that row is
// above, selected — and folders that no longer exist, which it drops.
func TestRecentPlaceRows(t *testing.T) {
	dir := listingDir(t)
	sub := filepath.Join(dir, "sub")
	gone := filepath.Join(dir, "gone")
	a := newStoreApp(t, dir)
	a.navigate(sub)
	if a.cur().path != sub {
		t.Fatalf("navigate went to %q, want %q", a.cur().path, sub)
	}
	if got := a.store.visitList(1); len(got) != 1 || got[0] != sub {
		t.Fatalf("最近位置 = %v, want %q first", got, sub)
	}
	a.store.pushVisit(gone)
	if rows := a.recentPlaceRows(); len(rows) != 0 {
		t.Fatalf("最近位置 rows = %v, want none: the only visit is the current folder", rows)
	}
	if got := a.store.visitList(0); len(got) != 1 {
		t.Fatalf("the missing folder was not dropped: %v", got)
	}

	sibling := filepath.Join(dir, "sibling")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	a.store.pushVisit(sibling)
	rows := a.recentPlaceRows()
	if len(rows) != 1 || rows[0].label != "sibling" || rows[0].path != sibling {
		t.Fatalf("最近位置 rows = %+v, want the sibling folder", rows)
	}
}

// TestSessionStore covers the session record's store rules: a round trip
// keeps every field, 清除会话记录 wipes it and keeps it wiped, and a tab's
// folder survives restore only while it exists (a special page always does;
// anything else falls back to 主文件夹).
func TestSessionStore(t *testing.T) {
	st := &persistedState{}
	st.setSession(&sessionState{Windows: []windowSession{
		{X: 10, Y: 20, W: 900, H: 600, Tabs: []string{"/a", "/b"}, Active: 1},
		{W: 800, H: 500, Tabs: []string{"/c"}},
	}})
	out := st.session()
	if out == nil || len(out.Windows) != 2 {
		t.Fatalf("session = %+v, want two windows", out)
	}
	w := out.Windows[0]
	if w.X != 10 || w.Y != 20 || w.W != 900 || w.H != 600 || w.Active != 1 ||
		len(w.Tabs) != 2 || w.Tabs[1] != "/b" {
		t.Fatalf("first window = %+v", w)
	}

	st.clearSession()
	if st.session() != nil {
		t.Fatal("the session survived 清除")
	}
	if !st.sessionClearedNow() {
		t.Fatal("the cleared flag is not set")
	}
	st.setSession(nil)
	if st.session() != nil {
		t.Fatal("a nil set brought the session back")
	}

	dir := listingDir(t)
	if got := restoreTabPath(dir); got != dir {
		t.Fatalf("restoreTabPath(%q) = %q", dir, got)
	}
	if got := restoreTabPath(pathThisPC); got != pathThisPC {
		t.Fatalf("restoreTabPath(%q) = %q", pathThisPC, got)
	}
	if got := restoreTabPath(filepath.Join(dir, "gone")); got != pathHome {
		t.Fatalf("a missing folder restored as %q, want %q", got, pathHome)
	}
}

// TestSessionCaptureAndRestore walks a window's record through both ends: an
// app captures its tabs and active one, a saved window comes back with those
// tabs in order, the right one active and its box queued, and a tab whose
// folder is gone opens on 主文件夹.
func TestSessionCaptureAndRestore(t *testing.T) {
	dir := listingDir(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(filepath.Join(dir, "sibling"), 0o755); err != nil {
		t.Fatal(err)
	}

	live := newStoreApp(t, dir)
	live.openTab(sub)
	ws, ok := live.windowSession()
	if !ok || len(ws.Tabs) != 2 || ws.Active != 1 {
		t.Fatalf("window session = %+v (ok %v), want two tabs with the second active", ws, ok)
	}

	store := &persistedState{Pinned: defaultPins()}
	saved := windowSession{
		X: 5, Y: 6, W: 700, H: 500,
		Tabs:   []string{dir, sub, pathThisPC},
		Active: 1,
	}
	a := newSessionApp(store, saved, 0)
	if len(a.tabs) != 3 ||
		a.tabs[0].path != dir || a.tabs[1].path != sub || a.tabs[2].path != pathThisPC {
		t.Fatalf("restored tabs = %+v, want the saved three in order", a.tabs)
	}
	if a.activeTab != 1 {
		t.Fatalf("active tab = %d, want 1", a.activeTab)
	}
	if a.geom == nil || *a.geom != (windowGeom{x: 5, y: 6, w: 700, h: 500}) {
		t.Fatalf("window box = %+v, want the saved one", a.geom)
	}
	if a.sessionSlot != 0 {
		t.Fatalf("session slot = %d, want 0", a.sessionSlot)
	}

	missing := newSessionApp(store, windowSession{Tabs: []string{
		filepath.Join(dir, "gone"), dir,
	}}, 0)
	if got := missing.tabs[0].path; got != pathHome {
		t.Fatalf("a missing tab restored as %q, want %q", got, pathHome)
	}
}

// TestSessionHistoryRoundTrip keeps each tab's back/forward history beside
// its path, caps it at 50 entries while retaining the current position, and
// drops deleted folders while keeping histIdx on the restored current path.
func TestSessionHistoryRoundTrip(t *testing.T) {
	dir := listingDir(t)
	for _, name := range []string{"current", "later"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, dir)
	a.tabs[0].history = []string{filepath.Join(dir, "gone"), filepath.Join(dir, "current"), filepath.Join(dir, "later")}
	a.tabs[0].histIdx = 1
	ws, ok := a.windowSession()
	if !ok || len(ws.History) != 1 || len(ws.HistIdx) != 1 || ws.HistIdx[0] != 1 {
		t.Fatalf("captured history = %+v (ok %v)", ws, ok)
	}

	store := &persistedState{Pinned: defaultPins(), path: filepath.Join(t.TempDir(), "state.json")}
	store.setSession(&sessionState{Windows: []windowSession{ws}})
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var disk persistedState
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Session == nil || len(disk.Session.Windows) != 1 || len(disk.Session.Windows[0].History) != 1 {
		t.Fatalf("stored session = %+v", disk.Session)
	}

	// A missing folder leaves the list, while the existing current folder
	// remains the histIdx entry after the index shifts.
	restored := newSessionApp(store, windowSession{
		Tabs:    []string{filepath.Join(dir, "current")},
		History: [][]string{{filepath.Join(dir, "gone"), filepath.Join(dir, "current"), filepath.Join(dir, "later")}},
		HistIdx: []int{1},
	}, 0)
	if got := restored.tabs[0].history; len(got) != 2 || got[0] != filepath.Join(dir, "current") || got[1] != filepath.Join(dir, "later") {
		t.Fatalf("restored history = %v, want missing entry removed", got)
	}
	if restored.tabs[0].histIdx != 0 || restored.tabs[0].history[restored.tabs[0].histIdx] != restored.tabs[0].path {
		t.Fatalf("restored histIdx = %d history = %v path = %q", restored.tabs[0].histIdx, restored.tabs[0].history, restored.tabs[0].path)
	}

	long := make([]string, 80)
	for i := range long {
		long[i] = filepath.Join(dir, "h", string(rune('a'+i%26)), string(rune('0'+i/26)))
	}
	capped, idx := capTabHistory(long, 60)
	if len(capped) != maxTabHistory || idx != maxTabHistory-1 || capped[idx] != long[60] {
		t.Fatalf("capped history = %d entries at %d, want %d at the current entry", len(capped), idx, maxTabHistory)
	}

	old := newSessionApp(store, windowSession{Tabs: []string{dir}}, 0)
	if len(old.tabs) != 1 || len(old.tabs[0].history) != 1 || old.tabs[0].histIdx != 0 {
		t.Fatalf("old session without history = %+v", old.tabs[0])
	}
}

// TestParallelFor runs the shared worker pool over the shapes its callers
// use: every index visited exactly once, a short list on fewer workers, and
// no goroutine for an empty one.
func TestParallelFor(t *testing.T) {
	seen := make([]int, 20)
	parallelFor(len(seen), 4, func(i int) { seen[i]++ })
	for i, n := range seen {
		if n != 1 {
			t.Fatalf("index %d ran %d times, want once", i, n)
		}
	}
	parallelFor(3, 8, func(i int) { seen[i]++ })
	for i := 0; i < 3; i++ {
		if seen[i] != 2 {
			t.Fatalf("index %d ran %d times over both passes", i, seen[i])
		}
	}
	parallelFor(0, 4, func(int) { t.Fatal("an empty range ran a call") })
}
