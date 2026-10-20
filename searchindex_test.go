package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// searchindex_test.go drives the index both ways: the engine on its own — a
// walk of a temp tree, the query language over its entries, the cache file —
// and the Ctrl+K palette on it, which is what the index is for: a 全部磁盘 that
// answers on the keystroke, warm from the last launch's cache.

// The tests walk temp trees, and a build would mirror them into the user's real
// index.json: keep the cache file in the test's own scratch path for the whole
// binary, as sizes_test.go does for sizes.json. resetSearchIndex narrows it
// further per test.
func init() {
	searchIndexCacheFile = filepath.Join(os.TempDir(),
		"sunvale-index-test-"+itoa(os.Getpid())+".json")
}

// resetSearchIndex clears the process-wide index and points its cache at a
// scratch file, restoring both when the test ends. The index outlives an App,
// so a test that left one enabled would answer the next test's queries.
func resetSearchIndex(t *testing.T) {
	t.Helper()
	old := searchIndexCacheFile
	searchIndexCacheFile = filepath.Join(t.TempDir(), "index.json")
	clearSearchIndex()
	t.Cleanup(func() {
		searchIndexCacheFile = old
		clearSearchIndex()
	})
}

// clearSearchIndex empties the store, as a launch that has not started it yet.
func clearSearchIndex() {
	searchIndex.mu.Lock()
	searchIndex.items = nil
	searchIndex.built = time.Time{}
	searchIndex.complete = false
	searchIndex.enabled = false
	searchIndex.building = false
	searchIndex.estimate = 0
	searchIndex.dirs = nil
	searchIndex.mu.Unlock()
	searchIndex.pendingMu.Lock()
	searchIndex.pending = nil
	searchIndex.pendingMu.Unlock()
	searchIndex.progress.Store(0)
}

// buildTestIndex walks roots into the store's snapshot, as a finished build
// does, and marks the result the whole machine.
func buildTestIndex(t *testing.T, roots ...string) {
	t.Helper()
	enableSearchIndex()
	searchIndex.progress.Store(0)
	items := map[string]indexEntry{}
	var mu sync.Mutex
	walkIndexList(context.Background(), roots, nil, func(e indexEntry) {
		mu.Lock()
		items[e.path] = e
		mu.Unlock()
	})
	searchIndex.mu.Lock()
	searchIndex.items = items
	searchIndex.built = time.Now()
	searchIndex.complete = true
	searchIndex.estimate = len(items)
	searchIndex.mu.Unlock()
}

// enableSearchIndex turns the index on without walking, the state
// searchIndexNote and startSearchIndex's cache read leave behind.
func enableSearchIndex() {
	searchIndex.mu.Lock()
	searchIndex.enabled = true
	searchIndex.mu.Unlock()
}

// writeFiles writes name/body pairs under root, making the folders they need:
// one call describes a whole little tree. (sizes_test.go's writeTree writes one
// file of n zero bytes.)
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSearchIndexWalksAndAnswers: a walk turns a tree into entries the query
// language matches, each carrying the folder, the size and the date a hit
// shows. Hidden entries are indexed too — the query filters them by the app's
// 显示隐藏的项目 setting, so toggling it needs no rebuild.
// TestSearchBoxAnswersFromTheIndex: with an index covering the folder, Ctrl+K
// answers on the keystroke — from the index, not a walk — and the footer says
// so, with 重建索引 beside it.
func TestSearchBoxAnswersFromTheIndex(t *testing.T) {
	resetSearchIndex(t)
	a, tt, dir := keynavWindow(t, 3)
	buildTestIndex(t, dir)

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	// With nothing typed yet the footer is the index's own line: how many
	// entries it holds — the folder's three files and the folder itself.
	if !tt.HasText("索引 4 项") {
		t.Fatalf("the footer does not count the index: %v", tt.Texts())
	}
	a.searchBox.scope = scopeFolder
	tt.Type("f00")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if a.searchBox.src != sourceIndex {
		t.Fatalf("the box answered from source %v, want the index", a.searchBox.src)
	}
	if len(a.searchBox.results) != 3 {
		t.Fatalf("the index answered with %v, want the folder's three", namesOf(a.searchBox.results))
	}
	if !tt.HasText("索引命中") {
		t.Fatalf("the footer does not say the index answered: %v", tt.Texts())
	}
	if !tt.HasText("重建索引") {
		t.Fatalf("the footer has no 重建索引 button while the index is on: %v", tt.Texts())
	}
	if _, ok := tt.Find("重建索引"); !ok {
		t.Fatal("the 重建索引 button is in no frame")
	}
}

// TestSearchBoxRebuildButtonWalksAgain: the footer's 重建索引 starts a build —
// the palette's own way to make the cache current again after the disks moved
// under it — and a build the cap cuts short says 未完整 rather than passing
// itself off as the machine.
func TestSearchBoxRebuildButtonWalksAgain(t *testing.T) {
	resetSearchIndex(t)
	_, tt, dir := keynavWindow(t, 2)
	buildTestIndex(t, dir)
	old := searchIndexCap
	searchIndexCap = 1 // the walk stops at its first entry, not on the machine
	t.Cleanup(func() { searchIndexCap = old })

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if err := tt.Click("重建索引"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	waitSearchIndexBuild(t)
	st := searchIndexState()
	if st.building {
		t.Fatal("the build the button started is still running")
	}
	if st.total != 1 {
		t.Fatalf("the capped build left %d entries, want 1", st.total)
	}
	if st.complete {
		t.Fatal("a build that stopped at the cap called itself the whole machine")
	}
	if !tt.HasText("未完整") {
		t.Fatalf("the footer does not say the index is short: %v", tt.Texts())
	}
}

// waitSearchIndexBuild waits for the build in flight to end, bounded so a
// wedged walk fails the test instead of hanging it.
func waitSearchIndexBuild(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if !searchIndexState().building {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the index build never finished")
}

// TestSearchBoxTopsUpAShortIndex: an index that is not the whole machine still
// answers on the keystroke, and the walk fills in what it does not hold — with
// a file both of them find listed once, and the footer saying which is which.
func TestSearchBoxTopsUpAShortIndex(t *testing.T) {
	resetSearchIndex(t)
	a, tt, dir := keynavWindow(t, 1)
	sub := filepath.Join(dir, "sub")
	writeFiles(t, sub, map[string]string{"indexed.txt": "hi"})
	a.reload()
	// The index holds the subfolder's file only, and is not the machine.
	buildTestIndex(t, sub)
	searchIndex.mu.Lock()
	searchIndex.complete = false
	searchIndex.mu.Unlock()

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("txt")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	// The walk added the file the index never saw, and the file both of them
	// hold — the index's answer, the walk's answer — is listed once.
	got := namesOf(a.searchBox.results)
	want := []string{"f000.txt", "indexed.txt"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("the topped-up list is %v, want %v", got, want)
	}
	if a.searchBox.src != sourceIndex {
		t.Fatalf("the box answered from source %v, want the index that was topped up", a.searchBox.src)
	}
	if !tt.HasText("未完整") {
		t.Fatalf("the footer does not say the index is short: %v", tt.Texts())
	}
}

// TestSearchBoxListsAHitOnce: a hit two engines found — the index answered it,
// the walk behind found it again — is one row, and the cursor's hit stays the
// one it was on as the rest streams in.
func TestSearchBoxListsAHitOnce(t *testing.T) {
	b := &searchBoxState{}
	b.addHits([]Entry{{Name: "same.txt", Path: "/a/same.txt"}, {Name: "other.txt", Path: "/a/other.txt"}})
	b.cursor, b.cursorPath = 1, "/a/same.txt"
	b.addHits([]Entry{{Name: "same.txt", Path: "/a/same.txt"}, {Name: "later.txt", Path: "/a/later.txt"}})
	got := namesOf(b.results)
	if len(got) != 3 {
		t.Fatalf("the list is %v, want one row per hit", got)
	}
	if b.results[b.cursor].Path != "/a/same.txt" {
		t.Fatalf("the cursor moved off its hit to %q", b.results[b.cursor].Path)
	}
}

// TestSearchBoxLocationButtonJumps: the 位置 button on a hit's row is the
// mouse's Ctrl+Enter — the tab goes to the hit's folder with the hit selected —
// and the walk is what answers when no index covers the folder.
func TestSearchBoxLocationButtonJumps(t *testing.T) {
	resetSearchIndex(t)
	a, tt, dir := keynavWindow(t, 1)
	sub := filepath.Join(dir, "sub")
	writeFiles(t, sub, map[string]string{"inner.txt": "hi"})
	a.reload()

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("inner")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if a.searchBox.src != sourceWalk {
		t.Fatalf("with no index the box answered from source %v, want the walk", a.searchBox.src)
	}
	if len(a.searchBox.results) != 1 {
		t.Fatalf("the box found %v, want inner.txt", namesOf(a.searchBox.results))
	}
	if !tt.HasText("实时遍历") {
		t.Fatalf("the footer does not say the walk answered: %v", tt.Texts())
	}
	if err := tt.Click("打开所在文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("the box stayed open after 位置")
	}
	if a.cur().path != sub {
		t.Fatalf("the tab is in %q, want the hit's folder %q", a.cur().path, sub)
	}
	if !a.isSelected(filepath.Join(sub, "inner.txt")) {
		t.Fatal("the hit is not selected in its folder")
	}
}
