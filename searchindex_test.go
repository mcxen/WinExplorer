package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/fsentry"
	"sunvale/internal/search"
)

// searchindex_test.go covers the application boundary: how the Ctrl+K palette
// uses the index and what its footer says. Index storage and walking are tested
// in internal/search.

var searchIndexTestCacheFile string

func resetSearchIndex(t *testing.T) {
	t.Helper()
	old, oldFile := searchIndex, searchIndexTestCacheFile
	searchIndexTestCacheFile = filepath.Join(t.TempDir(), "index.json")
	clearSearchIndex()
	t.Cleanup(func() {
		searchIndex, searchIndexTestCacheFile = old, oldFile
	})
}

func clearSearchIndex() {
	searchIndex = search.NewIndex(search.IndexConfig{
		CacheFile: searchIndexTestCacheFile,
	})
}

func enableSearchIndex() { searchIndex.Enable() }

// buildTestIndex runs the real build over only the requested test roots.
func buildTestIndex(t *testing.T, roots ...string) {
	t.Helper()
	enableSearchIndex()
	searchIndex.Rebuild(roots)
	waitSearchIndexBuild(t)
}

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

func TestSearchBoxAnswersFromTheIndex(t *testing.T) {
	resetSearchIndex(t)
	a, tt, dir := keynavWindow(t, 3)
	buildTestIndex(t, dir)

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !tt.HasText("索引 3 项") {
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
		t.Fatalf("the footer has no 重建索引 button: %v", tt.Texts())
	}
}

func TestSearchBoxRebuildButtonWalksAgain(t *testing.T) {
	resetSearchIndex(t)
	searchIndex = search.NewIndex(search.IndexConfig{
		CacheFile: searchIndexTestCacheFile, MaxEntries: 1,
	})
	_, tt, dir := keynavWindow(t, 2)
	buildTestIndex(t, dir)

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if err := tt.Click("重建索引"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	waitSearchIndexBuild(t)
	st := searchIndexState()
	if st.building || st.total != 1 || st.complete {
		t.Fatalf("capped rebuild status = %+v", st)
	}
	if !tt.HasText("未完整") {
		t.Fatalf("the footer does not say the index is short: %v", tt.Texts())
	}
}

func TestSearchBoxTopsUpAShortIndex(t *testing.T) {
	resetSearchIndex(t)
	a, tt, dir := keynavWindow(t, 1)
	sub := filepath.Join(dir, "sub")
	writeFiles(t, sub, map[string]string{"indexed.txt": "hi"})
	a.reload()
	enableSearchIndex()
	searchIndex.Note(sub, []fsentry.Entry{{
		Name: "indexed.txt", Path: filepath.Join(sub, "indexed.txt"), Ext: "txt",
		Size: 2, ModTime: time.Now(),
	}})

	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("txt")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	got := namesOf(a.searchBox.results)
	want := []string{"f000.txt", "indexed.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("the topped-up list is %v, want %v", got, want)
	}
	if a.searchBox.src != sourceIndex {
		t.Fatalf("the box answered from source %v, want the index", a.searchBox.src)
	}
	if !tt.HasText("未完整") {
		t.Fatalf("the footer does not say the index is short: %v", tt.Texts())
	}
}

func TestSearchBoxListsAHitOnce(t *testing.T) {
	b := &searchBoxState{}
	b.addHits([]Entry{{Name: "same.txt", Path: "/a/same.txt"}, {Name: "other.txt", Path: "/a/other.txt"}})
	b.cursor, b.cursorPath = 1, "/a/same.txt"
	b.addHits([]Entry{{Name: "same.txt", Path: "/a/same.txt"}, {Name: "later.txt", Path: "/a/later.txt"}})
	if len(b.results) != 3 {
		t.Fatalf("deduplicated list = %v, want one row per hit", namesOf(b.results))
	}
	if b.results[b.cursor].Path != "/a/same.txt" {
		t.Fatalf("the cursor moved off its hit to %q", b.results[b.cursor].Path)
	}
}

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
		t.Fatalf("source = %v, want the live walk", a.searchBox.src)
	}
	if len(a.searchBox.results) != 1 {
		t.Fatalf("the box found %v, want inner.txt", namesOf(a.searchBox.results))
	}
	if err := tt.Click("打开所在文件夹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.searchBox.open || a.cur().path != sub || !a.isSelected(filepath.Join(sub, "inner.txt")) {
		t.Fatal("the location button did not reveal the hit")
	}
}

func TestGroupThousands(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{0, "0"}, {12, "12"}, {999, "999"}, {1000, "1,000"},
		{1284532, "1,284,532"}, {-1234, "-1,234"},
	} {
		if got := groupThousands(tc.n); got != tc.want {
			t.Fatalf("groupThousands(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFormatElapsedReadsAtBothScales(t *testing.T) {
	if got := formatElapsed(1500 * time.Millisecond); got != "1.5s" {
		t.Fatalf("walk elapsed = %q", got)
	}
	if got := formatElapsed(250 * time.Microsecond); got != "250µs" {
		t.Fatalf("index elapsed = %q", got)
	}
}
