package search

import (
	"os"
	"path/filepath"
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
	ix.Rebuild(roots)
	waitBuilt(ix)
}

func waitBuilt(ix *Index) {
	ix.mu.RLock()
	done := ix.done
	ix.mu.RUnlock()
	if done != nil {
		<-done
	}
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

	if err := os.WriteFile(ix.cacheFile, []byte("SVIX0001 an older format"), 0o644); err != nil {
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
	buildIndex(t, ix, root)
	if st := ix.Status(); st.Total != 3 || st.Complete {
		t.Fatalf("capped build status = %+v, want 3 partial rows", st)
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

	warm := NewIndex(IndexConfig{CacheFile: ix.cacheFile})
	warm.Start([]string{root})
	if !warm.Enabled() || warm.Stale() || warm.Status().Building {
		t.Fatalf("warm start status = %+v", warm.Status())
	}
	if hits, ok := queryNames(t, warm, root, "cached", false); !ok || len(hits) != 1 {
		t.Fatalf("warm cache answered %v with %v hits", ok, len(hits))
	}
}

func TestIndexRefreshReusesUnchangedFolders(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 20; i++ {
		files["d"+string(rune('a'+i))+"/f.txt"] = ""
	}
	writeFiles(t, root, files)
	buildIndex(t, ix, root)
	if st := ix.Status(); st.Rescanned != 21 {
		t.Fatalf("cold build read %d folders, want 21", st.Rescanned)
	}
	// One folder changes: a file appears, another goes.
	writeFiles(t, root, map[string]string{"dc/new-one.md": ""})
	if err := os.Remove(filepath.Join(root, "dd", "f.txt")); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	for _, d := range []string{"dc", "dd"} {
		_ = os.Chtimes(filepath.Join(root, d), future, future)
	}
	ix.Rebuild([]string{root})
	waitBuilt(ix)
	if st := ix.Status(); st.Rescanned != 2 {
		t.Fatalf("refresh read %d folders, want the 2 changed", st.Rescanned)
	}
	if hits, _ := queryNames(t, ix, root, "new-one", false); len(hits) != 1 {
		t.Fatalf("new file missing after refresh: %v", namesOf(hits))
	}
	if hits, _ := queryNames(t, ix, filepath.Join(root, "dd"), "f.txt", false); len(hits) != 0 {
		t.Fatalf("deleted file survived refresh: %v", namesOf(hits))
	}
	if hits, _ := queryNames(t, ix, root, "f.txt", false); len(hits) != 19 {
		t.Fatalf("reused rows = %d, want 19", len(hits))
	}
}

func TestIndexSkipsExcludedAndMatchesShortAndWide(t *testing.T) {
	ix := testIndex(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"node_modules/pkg/index.js": "",
		"src/index.go":              "",
		"文档/季度报告.docx":              "",
		"x.y":                       "",
	})
	buildIndex(t, ix, root)
	for q, want := range map[string]int{"index": 1, "x.": 2, "季度报告": 1, "报告": 1, "INDEX.GO": 1, "zzz": 0} {
		hits, ok := queryNames(t, ix, root, q, false)
		if !ok || len(hits) != want {
			t.Fatalf("%q = %v (answered %v), want %d", q, namesOf(hits), ok, want)
		}
	}
	if _, ok := queryNames(t, ix, filepath.Join(root, "node_modules"), "index", false); ok {
		t.Fatal("an excluded folder claimed coverage")
	}
}

func TestIndexRootsPreferHome(t *testing.T) {
	ix := NewIndex(IndexConfig{CacheFile: filepath.Join(t.TempDir(), "index.json"), PreferHome: true})
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
