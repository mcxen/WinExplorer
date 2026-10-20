package search

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

func testIndex(t *testing.T) *Index {
	t.Helper()
	return NewIndex(IndexConfig{CacheFile: filepath.Join(t.TempDir(), "index.json")})
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

func buildIndex(t *testing.T, ix *Index, roots ...string) {
	t.Helper()
	ix.Enable()
	ix.progress.Store(0)
	items := map[string]indexEntry{}
	var mu sync.Mutex
	ix.walkList(context.Background(), roots, nil, func(e indexEntry) {
		mu.Lock()
		items[e.path] = e
		mu.Unlock()
	})
	ix.mu.Lock()
	ix.items = items
	ix.built = time.Now()
	ix.complete = true
	ix.estimate = len(items)
	ix.mu.Unlock()
}

func queryNames(t *testing.T, ix *Index, root, raw string, hidden bool) ([]fsentry.Entry, bool) {
	t.Helper()
	return ix.Query([]string{root}, searchquery.Parse(raw), hidden)
}

func TestIndexWalksAndAnswers(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"alpha.txt":       "12345",
		"docs/beta.md":    "x",
		".hidden/.deep":   "",
		".secret.txt":     "",
		"docs/inner1.txt": "",
	})
	buildIndex(t, ix, root)

	hit := func(query string, hidden bool) []fsentry.Entry {
		t.Helper()
		hits, ok := queryNames(t, ix, root, query, hidden)
		if !ok {
			t.Fatalf("the index did not answer %q", query)
		}
		return hits
	}
	hits := hit("alpha", false)
	if len(hits) != 1 || hits[0].Name != "alpha.txt" {
		t.Fatalf("alpha = %v", namesOf(hits))
	}
	if hits[0].Dir != root || hits[0].Size != 5 || hits[0].IsDir || hits[0].Ext != "txt" {
		t.Fatalf("hit lost listing data: %+v", hits[0])
	}
	if hits[0].ModTime.IsZero() {
		t.Fatal("hit has no modification time")
	}
	for _, tc := range []struct {
		query string
		want  string
	}{
		{query: "ext:md beta", want: "beta.md"},
		{query: "path:docs inner1", want: "inner1.txt"},
		{query: "folder:docs", want: "docs"},
	} {
		got := namesOf(hit(tc.query, false))
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%q = %v, want %q", tc.query, got, tc.want)
		}
	}
	if got := namesOf(hit("txt folder:", false)); len(got) != 0 {
		t.Fatalf("txt folder: matched files: %v", got)
	}
	if got := namesOf(hit("secret", false)); len(got) != 0 {
		t.Fatalf("hidden hit leaked: %v", got)
	}
	if got := namesOf(hit("secret", true)); len(got) != 1 || got[0] != ".secret.txt" {
		t.Fatalf("hidden query = %v", got)
	}
}

func TestIndexOnlyAnswersCoveredRoots(t *testing.T) {
	ix := testIndex(t)
	root, other := t.TempDir(), t.TempDir()
	writeFiles(t, root, map[string]string{"here.txt": ""})
	if _, ok := queryNames(t, ix, root, "here", false); ok {
		t.Fatal("an empty index answered")
	}
	buildIndex(t, ix, root)
	if _, ok := queryNames(t, ix, other, "here", false); ok {
		t.Fatal("the index answered for a root it never walked")
	}
	if hits, ok := queryNames(t, ix, root, "nothing", false); !ok || len(hits) != 0 {
		t.Fatalf("covered empty result: %v hits, answered %v", len(hits), ok)
	}
	parent := filepath.Dir(root)
	if hits, ok := ix.Query([]string{parent}, searchquery.Parse("here"), false); !ok || len(hits) != 1 {
		t.Fatalf("parent query: %v hits, answered %v", len(hits), ok)
	}
}

func TestIndexCacheRoundTrip(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"sub/kept.txt": "12345"})
	buildIndex(t, ix, root)
	ix.save()

	loaded := NewIndex(IndexConfig{CacheFile: ix.cacheFile})
	loaded.Enable()
	loaded.load()
	hits, ok := queryNames(t, loaded, root, "kept", false)
	if !ok || len(hits) != 1 {
		t.Fatalf("loaded cache: %v hits, answered %v", len(hits), ok)
	}
	if hits[0].Size != 5 || hits[0].Ext != "txt" || hits[0].ModTime.IsZero() {
		t.Fatalf("loaded hit lost data: %+v", hits[0])
	}
	if st := loaded.Status(); !st.Complete || st.Built.IsZero() || st.Estimate != st.Total {
		t.Fatalf("loaded status = %+v", st)
	}

	if err := os.WriteFile(ix.cacheFile, []byte(`{"version":99,"entries":[{"p":"/x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	foreign := NewIndex(IndexConfig{CacheFile: ix.cacheFile})
	foreign.Enable()
	foreign.load()
	if _, ok := queryNames(t, foreign, root, "kept", false); ok {
		t.Fatal("a foreign cache version answered")
	}
}

func TestIndexCapStopsTheWalk(t *testing.T) {
	ix := testIndex(t)
	ix.maxEntries = 3
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		writeFiles(t, root, map[string]string{"file-" + string(rune('a'+i)) + ".txt": ""})
	}
	ix.Enable()
	ix.progress.Store(0)
	ix.pendingMu.Lock()
	ix.pending = map[string]indexEntry{}
	ix.pendingMu.Unlock()
	ix.walkList(context.Background(), []string{root}, nil, func(e indexEntry) {
		ix.pendingMu.Lock()
		ix.pending[e.path] = e
		ix.pendingMu.Unlock()
	})
	ix.pendingMu.Lock()
	collected := len(ix.pending)
	ix.pendingMu.Unlock()
	if collected != 3 {
		t.Fatalf("walk collected %d entries, want the cap of 3", collected)
	}
}

func TestIndexNoteFollowsListings(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	ix.Enable()
	entry := func(name string, size int64) fsentry.Entry {
		return fsentry.Entry{Name: name, Path: filepath.Join(root, name), Size: size, ModTime: time.Now()}
	}
	ix.Note(root, []fsentry.Entry{entry("gone.txt", 1), entry("kept.txt", 2)})
	if hits, ok := queryNames(t, ix, root, "gone", false); !ok || len(hits) != 1 {
		t.Fatalf("listing did not reach index: %v hits, answered %v", len(hits), ok)
	}
	ix.Note(root, []fsentry.Entry{entry("kept.txt", 2), entry("fresh.txt", 3)})
	if hits, _ := queryNames(t, ix, root, "gone", false); len(hits) != 0 {
		t.Fatalf("deleted file remained: %v", namesOf(hits))
	}
	if hits, _ := queryNames(t, ix, root, "fresh", false); len(hits) != 1 || hits[0].Size != 3 {
		t.Fatalf("created file missing: %v", namesOf(hits))
	}
}

func TestIndexStartsFromCompleteCache(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"cached.txt": "hi"})
	buildIndex(t, ix, root)
	ix.save()

	warm := NewIndex(IndexConfig{CacheFile: ix.cacheFile})
	warm.Start([]string{root})
	if !warm.Enabled() || warm.Stale() || warm.Status().Building {
		t.Fatalf("warm start status = %+v", warm.Status())
	}
	if hits, ok := queryNames(t, warm, root, "cached", false); !ok || len(hits) != 1 {
		t.Fatalf("warm cache answered %v with %v hits", ok, len(hits))
	}
}

func TestIndexFlushesBuildInFlight(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	half := filepath.Join(root, "half.txt")
	ix.Enable()
	ix.mu.Lock()
	ix.building = true
	ix.pending = map[string]indexEntry{half: {path: half, size: 3, mod: time.Now().Unix()}}
	ix.mu.Unlock()
	ix.Flush()
	if st := ix.Status(); st.Total != 1 || st.Complete {
		t.Fatalf("flushed status = %+v", st)
	}

	loaded := NewIndex(IndexConfig{CacheFile: ix.cacheFile})
	loaded.Enable()
	loaded.load()
	if hits, ok := queryNames(t, loaded, root, "half", false); !ok || len(hits) != 1 {
		t.Fatalf("flushed cache: %v hits, answered %v", len(hits), ok)
	}
}

func TestIndexRootsPreferHome(t *testing.T) {
	ix := testIndex(t)
	first, rest := ix.roots([]string{t.TempDir()})
	if len(first) == 0 {
		t.Skip("no home directory")
	}
	for _, root := range rest {
		if root == first[0] {
			t.Fatalf("home root %q was walked twice", root)
		}
	}
}
