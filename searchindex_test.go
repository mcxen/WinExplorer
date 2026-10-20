package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/searchquery"
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
func TestSearchIndexWalksAndAnswers(t *testing.T) {
	resetSearchIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"alpha.txt":       "12345",
		"docs/beta.md":    "x",
		".hidden/.deep":   "",
		".secret.txt":     "",
		"docs/inner1.txt": "",
	})
	buildTestIndex(t, root)

	hit := func(query string, hidden bool) []Entry {
		t.Helper()
		hits, ok := searchIndexQuery([]string{root}, searchquery.Parse(query), hidden)
		if !ok {
			t.Fatalf("the index did not answer %q over its own tree", query)
		}
		return hits
	}
	hits := hit("alpha", false)
	if len(hits) != 1 || hits[0].Name != "alpha.txt" {
		t.Fatalf("alpha = %v, want alpha.txt", namesOf(hits))
	}
	if hits[0].Dir != root || hits[0].Size != 5 || hits[0].IsDir || hits[0].Ext != "txt" {
		t.Fatalf("the hit lost what a row shows: %+v", hits[0])
	}
	if hits[0].ModTime.IsZero() {
		t.Fatal("the hit has no modification date")
	}
	// The query language is the walk's own: ext:, path:, folder: and AND.
	if got := namesOf(hit("ext:md beta", false)); len(got) != 1 || got[0] != "beta.md" {
		t.Fatalf("ext:md beta = %v", got)
	}
	if got := namesOf(hit("path:docs inner1", false)); len(got) != 1 || got[0] != "inner1.txt" {
		t.Fatalf("path:docs inner1 = %v", got)
	}
	if got := namesOf(hit("folder:docs", false)); len(got) != 1 || got[0] != "docs" {
		t.Fatalf("folder:docs = %v", got)
	}
	if got := hit("txt folder:", false); len(got) != 0 {
		t.Fatalf("txt folder: = %v, want no files", namesOf(got))
	}
	// Hidden entries are in the index but out of the default listing.
	if got := hit("secret", false); len(got) != 0 {
		t.Fatalf("secret without 显示隐藏的项目 = %v", namesOf(got))
	}
	if got := hit("secret", true); len(got) != 1 || got[0].Name != ".secret.txt" {
		t.Fatalf("secret with 显示隐藏的项目 = %v", namesOf(got))
	}
}

// TestSearchIndexOnlyAnswersWhatItCovers: the palette walks the disks whenever
// the index cannot answer — it is off, it is empty, or the folder searched is
// one no walk ever reached. A covered folder answers, even when the answer is
// nothing: an empty result must not send the box off to walk the machine.
func TestSearchIndexOnlyAnswersWhatItCovers(t *testing.T) {
	resetSearchIndex(t)
	root, other := t.TempDir(), t.TempDir()
	writeFiles(t, root, map[string]string{"here.txt": ""})

	if _, ok := searchIndexQuery([]string{root}, searchquery.Parse("here"), false); ok {
		t.Fatal("the index answered while it was off")
	}
	buildTestIndex(t, root)
	if _, ok := searchIndexQuery([]string{other}, searchquery.Parse("here"), false); ok {
		t.Fatal("the index answered for a folder it never walked")
	}
	hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("here"), false)
	if !ok || len(hits) != 1 {
		t.Fatalf("a covered folder: %v hits, answered %v", len(hits), ok)
	}
	if hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("nothing"), false); !ok || len(hits) != 0 {
		t.Fatalf("a covered folder with no hits: %v hits, answered %v", len(hits), ok)
	}
	// A root that holds the indexed one answers for it: the index covers a
	// folder's whole subtree, not only the folder itself.
	parent := filepath.Dir(root)
	if hits, ok := searchIndexQuery([]string{parent}, searchquery.Parse("here"), false); !ok || len(hits) != 1 {
		t.Fatalf("the parent folder came back with %v hits, answered %v", len(hits), ok)
	}
	// A root the index holds nothing under is somebody else's to walk.
	sibling, err := os.MkdirTemp(parent, "sibling")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sibling)
	if _, ok := searchIndexQuery([]string{sibling}, searchquery.Parse("here"), false); ok {
		t.Fatal("the index answered for a sibling folder it never walked")
	}
}

// TestSearchIndexCacheRoundTrip: what a build writes, the next launch reads —
// every field a hit shows, and the flags saying whether the walk saw the whole
// machine.
func TestSearchIndexCacheRoundTrip(t *testing.T) {
	resetSearchIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"sub/kept.txt": "12345"})
	buildTestIndex(t, root)
	saveSearchIndex()
	if _, err := os.Stat(searchIndexCacheFile); err != nil {
		t.Fatalf("the build wrote no cache: %v", err)
	}

	// Emptied, as a fresh process starts: the cache is the only copy.
	clearSearchIndex()
	enableSearchIndex()
	if hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("kept"), false); ok {
		t.Fatalf("an empty index answered with %v", namesOf(hits))
	}
	loadSearchIndex()
	hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("kept"), false)
	if !ok || len(hits) != 1 {
		t.Fatalf("the loaded cache has %v hits (answered %v)", len(hits), ok)
	}
	got := hits[0]
	want := filepath.Join(root, "sub", "kept.txt")
	if got.Path != want || got.Name != "kept.txt" || got.Dir != filepath.Join(root, "sub") {
		t.Fatalf("the loaded hit is %+v, want %q", got, want)
	}
	if got.Size != 5 || got.Ext != "txt" || got.IsDir || got.ModTime.IsZero() {
		t.Fatalf("the loaded hit lost a column: %+v", got)
	}
	st := searchIndexState()
	if !st.complete || st.built.IsZero() {
		t.Fatalf("the cache forgot its own state: complete=%v built=%v", st.complete, st.built)
	}
	// A folder the walk saw is in the cache as a folder.
	if hits, _ := searchIndexQuery([]string{root}, searchquery.Parse("folder:sub"), false); len(hits) != 1 || !hits[0].IsDir {
		t.Fatalf("the folder did not survive the round trip: %v", namesOf(hits))
	}
	// A cache from another format is ignored, as a corrupt state.json is.
	if err := os.WriteFile(searchIndexCacheFile, []byte(`{"version":99,"entries":[{"p":"/x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	clearSearchIndex()
	enableSearchIndex()
	loadSearchIndex()
	if hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("kept"), false); ok {
		t.Fatalf("a foreign cache version answered with %v", namesOf(hits))
	}
}

// TestSearchIndexCapStopsTheWalk: the cap ends a walk where it is, and a walk
// that stopped at the cap — or ran out of time — is not the whole machine, which
// is what the footer's 未完整 says.
func TestSearchIndexCapStopsTheWalk(t *testing.T) {
	resetSearchIndex(t)
	old := searchIndexCap
	searchIndexCap = 3
	t.Cleanup(func() { searchIndexCap = old })
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		writeFiles(t, root, map[string]string{"file-" + itoa(i) + ".txt": ""})
	}
	searchIndex.progress.Store(0)
	read := 0
	walkIndexList(context.Background(), []string{root}, nil, func(indexEntry) { read++ })
	if read != searchIndexCap {
		t.Fatalf("the walk read %d entries, want the cap's %d", read, searchIndexCap)
	}
	if indexComplete(context.Background(), read) {
		t.Fatal("a walk that stopped at the cap called itself the whole machine")
	}
	if !indexComplete(context.Background(), 1) {
		t.Fatal("a short walk is not the whole machine")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if indexComplete(ctx, 1) {
		t.Fatal("a walk that ran out of time called itself the whole machine")
	}
}

// TestSearchIndexNoteFollowsTheListings: the pane's own listing replaces what
// the index held for that folder, so a file the user created or deleted in view
// is searchable — or gone — at once.
func TestSearchIndexNoteFollowsTheListings(t *testing.T) {
	resetSearchIndex(t)
	root := t.TempDir()
	enableSearchIndex()
	path := func(name string) string { return filepath.Join(root, name) }
	entry := func(name string, size int64) Entry {
		return Entry{Name: name, Path: path(name), Size: size, ModTime: time.Now()}
	}
	searchIndexNote(root, []Entry{entry("gone.txt", 1), entry("kept.txt", 2)})
	if hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("gone"), false); !ok || len(hits) != 1 {
		t.Fatalf("the listing did not reach the index: %v hits, answered %v", len(hits), ok)
	}
	// The user deletes gone.txt and creates fresh.txt; the next listing is the
	// index's word on this folder.
	searchIndexNote(root, []Entry{entry("kept.txt", 2), entry("fresh.txt", 3)})
	if hits, _ := searchIndexQuery([]string{root}, searchquery.Parse("gone"), false); len(hits) != 0 {
		t.Fatalf("a deleted file stayed in the index: %v", namesOf(hits))
	}
	hits, _ := searchIndexQuery([]string{root}, searchquery.Parse("fresh"), false)
	if len(hits) != 1 || hits[0].Size != 3 {
		t.Fatalf("a created file is not in the index: %v", namesOf(hits))
	}
	// The index stays off in a process that never started it: no listing, no
	// index, no surprise for a test that never asked for one.
	clearSearchIndex()
	searchIndexNote(root, []Entry{entry("quiet.txt", 1)})
	if searchIndexState().total != 0 {
		t.Fatal("a listing reached an index that was never started")
	}
}

// TestStartSearchIndexWarmsFromTheCache is the launch itself: startup reads the
// last walk's cache and searches it at once, and walks the disks again only
// when that cache is worth replacing — missing, short, or older than
// searchIndexRefresh.
func TestStartSearchIndexWarmsFromTheCache(t *testing.T) {
	resetSearchIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"cached.txt": "hi"})
	buildTestIndex(t, root)
	saveSearchIndex()

	// A fresh process: nothing in memory, the cache is all it has.
	clearSearchIndex()
	startSearchIndex()
	if !searchIndexOn() {
		t.Fatal("startSearchIndex left the index off")
	}
	if searchIndexStale() {
		t.Fatal("a fresh, complete cache reads as stale")
	}
	hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("cached"), false)
	if !ok || len(hits) != 1 {
		t.Fatalf("the launched index answered %v with %v hits", ok, len(hits))
	}
	if searchIndexState().building {
		t.Fatal("a fresh cache still started a walk of the disks")
	}

	// The same launch, a cache older than the refresh window: the disks are
	// walked again, and the capped walk says it is not the whole machine.
	old := searchIndexCap
	searchIndexCap = 1 // the machine is the real one: keep the walk to a step
	t.Cleanup(func() { searchIndexCap = old })
	clearSearchIndex()
	enableSearchIndex()
	loadSearchIndex()
	searchIndex.mu.Lock()
	searchIndex.built = time.Now().Add(-2 * searchIndexRefresh)
	searchIndex.mu.Unlock()
	saveSearchIndex()
	clearSearchIndex()
	startSearchIndex()
	if !searchIndexState().building {
		t.Fatal("a stale cache did not start a build")
	}
	waitSearchIndexBuild(t)
	if st := searchIndexState(); st.complete {
		t.Fatal("the capped walk called itself the whole machine")
	}
}

// TestFlushSearchIndexSavesTheWalkInFlight is the quit path: a build cut short
// by the app closing still leaves what it read as the cache — marked 未完整, so
// the next launch knows to walk again — while a launch with no build running
// writes nothing at all.
func TestFlushSearchIndexSavesTheWalkInFlight(t *testing.T) {
	resetSearchIndex(t)
	root := t.TempDir()
	half := filepath.Join(root, "half.txt")
	enableSearchIndex()
	searchIndex.mu.Lock()
	searchIndex.building = true
	searchIndex.pending = map[string]indexEntry{half: {path: half, size: 3, mod: time.Now().Unix()}}
	searchIndex.mu.Unlock()

	flushSearchIndex()
	st := searchIndexState()
	if st.total != 1 || st.complete {
		t.Fatalf("the flush left %d entries, complete=%v", st.total, st.complete)
	}
	clearSearchIndex()
	enableSearchIndex()
	loadSearchIndex()
	if hits, ok := searchIndexQuery([]string{root}, searchquery.Parse("half"), false); !ok || len(hits) != 1 {
		t.Fatalf("the flushed cache has %v hits (answered %v)", len(hits), ok)
	}

	// Nothing in flight: the cache file on disk stays the finished one.
	before, err := os.ReadFile(searchIndexCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	flushSearchIndex()
	after, err := os.ReadFile(searchIndexCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a flush with no build in flight rewrote the cache")
	}
}

// TestSearchIndexSummaryWords is the footer's line on the index: the count with
// the cache's age, 未完整 when the walk did not see the machine, and the walk in
// flight beside it. Silent while the index is off.
func TestSearchIndexSummaryWords(t *testing.T) {
	resetSearchIndex(t)
	if got := searchIndexSummary(); got != "" {
		t.Fatalf("an index that was never started said %q", got)
	}
	enableSearchIndex()
	searchIndex.mu.Lock()
	searchIndex.items = map[string]indexEntry{"/a": {path: "/a"}, "/b": {path: "/b"}}
	searchIndex.built = time.Now().Add(-2 * time.Hour)
	searchIndex.complete = true
	searchIndex.mu.Unlock()
	if got := searchIndexSummary(); !strings.Contains(got, "索引 2 条") || !strings.Contains(got, "小时前的缓存") {
		t.Fatalf("the summary is %q", got)
	}
	searchIndex.mu.Lock()
	searchIndex.complete = false
	searchIndex.building = true
	searchIndex.mu.Unlock()
	searchIndex.progress.Store(7)
	got := searchIndexSummary()
	if !strings.Contains(got, "未完整") || !strings.Contains(got, "正在重建 7 条") {
		t.Fatalf("a running, incomplete index says %q", got)
	}
}

// TestFormatElapsedReadsAtBothScales: a walk is timed in seconds, an index hit
// in microseconds — Everything's own line, at our scale.
func TestFormatElapsedReadsAtBothScales(t *testing.T) {
	if got := formatElapsed(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("a walk took %q, want 1.5s", got)
	}
	if got := formatElapsed(250 * time.Microsecond); got != "250µs" {
		t.Fatalf("an index hit took %q, want 250µs", got)
	}
	if got := formatElapsed(0); got == "" || strings.Contains(got, "-") {
		t.Fatalf("an instant search took %q", got)
	}
}

// TestSearchIndexRootsPreferHome: a build walks the user's home first, so a
// machine larger than the cap keeps the folders the user searches for, and the
// volumes after it — without walking the home tree twice.
func TestSearchIndexRootsPreferHome(t *testing.T) {
	first, roots := searchIndexRoots()
	if len(first) == 0 {
		t.Skip("no home directory to prefer")
	}
	if !filepath.IsAbs(first[0]) {
		t.Fatalf("the first root is %q, want an absolute path", first[0])
	}
	for _, root := range roots {
		if root == first[0] {
			t.Fatalf("the volumes walk %q again", root)
		}
	}
}

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
	if !tt.HasText("索引 4 条") {
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
	want := []string{"later.txt", "other.txt", "same.txt"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("the list is %v, want %v", got, want)
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
