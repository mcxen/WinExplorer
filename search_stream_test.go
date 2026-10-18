package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// search_stream_test.go drives the streaming walk and the query language
// together, over real temp trees: batching, the flush cadence, the cap and
// the Everything-style queries end to end.

// streamAll collects a streamed walk's batches, recording how many batches
// and at which totals they arrived — the incremental behavior is what this
// file pins down.
func streamAll(root, query string, hidden bool, deadline time.Time) (hits []Entry, totals []int) {
	return streamRoots([]string{root}, query, hidden, deadline)
}

// streamRoots is streamAll over all the roots of a 全局 walk at once.
func streamRoots(roots []string, query string, hidden bool, deadline time.Time) (hits []Entry, totals []int) {
	q := parseSearchQuery(query)
	streamMatches(roots, q, hidden, deadline, func(batch []Entry, more bool) {
		hits = append(hits, batch...)
		totals = append(totals, len(hits))
	})
	return hits, totals
}

func TestStreamMatchesBatchesAndEnds(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, "hit-"+itoa(i)+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, totals := streamAll(root, "hit", false, time.Now().Add(5*time.Second))
	if len(hits) != 40 {
		t.Fatalf("hits = %d, want 40", len(hits))
	}
	// The final callback must arrive even when the last batch is empty,
	// with more=false, so the caller learns the walk ended.
	if len(totals) < 1 {
		t.Fatal("no batches were emitted")
	}
	if totals[len(totals)-1] != 40 {
		t.Fatalf("last total = %d, want 40", totals[len(totals)-1])
	}
	// The results concatenate back into one list, each hit carrying its
	// Dir for the 位置 column.
	for _, h := range hits {
		if h.Dir != root {
			t.Fatalf("hit %v lost its Dir", h)
		}
	}
}

func TestStreamMatchesQueryLanguage(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.MkdirAll(filepath.Join(root, "pics"), 0o755)
	os.WriteFile(filepath.Join(root, "docs", "report.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "docs", "report.md"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "pics", "report.png"), nil, 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), nil, 0o644)

	// ext: + a term.
	hits, _ := streamAll(root, "report ext:txt", false, time.Now().Add(5*time.Second))
	if len(hits) != 1 || filepath.Base(hits[0].Path) != "report.txt" {
		t.Fatalf("ext:txt hits = %v", namesOf(hits))
	}
	// path: narrows by folder.
	hits, _ = streamAll(root, "path:pics report", false, time.Now().Add(5*time.Second))
	if len(hits) != 1 || filepath.Base(hits[0].Path) != "report.png" {
		t.Fatalf("path:pics hits = %v", namesOf(hits))
	}
	// folder: keeps directories only.
	hits, _ = streamAll(root, "folder:rep", false, time.Now().Add(5*time.Second))
	if len(hits) != 0 {
		t.Fatalf("folder:rep matched files: %v", namesOf(hits))
	}
	// AND of two terms.
	hits, _ = streamAll(root, "report md", false, time.Now().Add(5*time.Second))
	if len(hits) != 1 || filepath.Base(hits[0].Path) != "report.md" {
		t.Fatalf("AND hits = %v", namesOf(hits))
	}
	// case: is positional.
	hits, _ = streamAll(root, "case:REPORT", false, time.Now().Add(5*time.Second))
	if len(hits) != 0 {
		t.Fatalf("case:REPORT matched lowercase names: %v", namesOf(hits))
	}
	hits, _ = streamAll(root, "case:report", false, time.Now().Add(5*time.Second))
	if len(hits) != 3 {
		t.Fatalf("case:report hits = %d, want 3 (txt, md, png)", len(hits))
	}
}

func TestStreamMatchesCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < searchMaxResults+50; i++ {
		if err := os.WriteFile(filepath.Join(root, "many-"+itoa(i)+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, _ := streamAll(root, "many", false, time.Now().Add(30*time.Second))
	if len(hits) != searchMaxResults {
		t.Fatalf("hits = %d, want the %d cap", len(hits), searchMaxResults)
	}
}

func TestSearchStatusLabelShape(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	os.WriteFile(filepath.Join(root, "alpha.txt"), nil, 0o644)
	a.reload()
	tab := a.cur()
	tab.query = "alpha"
	a.updateSearch()
	// The walk's batches reach the tab in a frame (frameQueue): wait for it to
	// finish by pumping updateSearch, which applies them.
	waitSearch(t, a)
	label := a.searchStatusLabel()
	if !strings.Contains(label, "找到 1 个项目") {
		t.Fatalf("label = %q, want the count", label)
	}
	if !strings.Contains(label, "(") {
		t.Fatalf("label = %q, want the elapsed time", label)
	}
	if !strings.Contains(label, "alpha") {
		t.Fatalf("label = %q, want the query", label)
	}
	// No query, no label.
	tab.query = ""
	if a.searchStatusLabel() != "" {
		t.Fatalf("empty query label = %q", a.searchStatusLabel())
	}
}

func namesOf(hits []Entry) []string {
	var out []string
	for _, h := range hits {
		out = append(out, filepath.Base(h.Path))
	}
	return out
}

// TestStreamMatchesManyRoots walks several roots at once — the shape of the
// 全局 scope, whose roots are the machine's volumes: every root's hits arrive,
// each keeps its own Dir for the 位置 column, and the stream ends exactly once.
func TestStreamMatchesManyRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(a, "sub"), 0o755)
	os.WriteFile(filepath.Join(a, "hit-a.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(a, "sub", "hit-a2.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(b, "hit-b.txt"), nil, 0o644)
	hits, totals := streamRoots([]string{a, b}, "hit", false, time.Now().Add(5*time.Second))
	if len(hits) != 3 {
		t.Fatalf("hits = %v, want the three under both roots", namesOf(hits))
	}
	dirs := map[string]bool{}
	for _, h := range hits {
		dirs[h.Dir] = true
	}
	if !dirs[a] || !dirs[filepath.Join(a, "sub")] || !dirs[b] {
		t.Fatalf("hits lost their folders: %v", dirs)
	}
	if totals[len(totals)-1] != 3 {
		t.Fatalf("last total = %d, want 3", totals[len(totals)-1])
	}
}

// TestSearchRootsScope pins what each scope walks: the tab's own folder, or the
// machine's volumes (sysVolumes) — listed once each, existing directories.
func TestSearchRootsScope(t *testing.T) {
	root := t.TempDir()
	a := newTestApp(t, root)
	if got := a.searchRoots(scopeFolder); len(got) != 1 || got[0] != root {
		t.Fatalf("folder roots = %v, want just %q", got, root)
	}
	vols := a.searchRoots(scopeGlobal)
	if len(vols) == 0 {
		t.Fatal("no volume roots: a 全局 search would find nothing")
	}
	seen := map[string]bool{}
	for _, v := range vols {
		if seen[v] {
			t.Fatalf("volume %q listed twice: %v", v, vols)
		}
		seen[v] = true
		if st, err := os.Stat(v); err != nil || !st.IsDir() {
			t.Fatalf("volume root %q is not a directory", v)
		}
	}
	// A special page has no folder to search.
	a.navigate(pathThisPC)
	if got := a.searchRoots(scopeFolder); got != nil {
		t.Fatalf("special folder roots = %v, want none", got)
	}
}

// TestSearchPruneDir pins the platform's prune list: exactly those directories
// are skipped, and nothing else is. The entries are mount points the volume
// roots walk themselves and virtual filesystems — walking them twice, or at
// all, is duplication or endless.
func TestSearchPruneDir(t *testing.T) {
	for _, p := range sysSearchPrune() {
		if !searchPruneDir(p) {
			t.Fatalf("%q is on the platform's prune list but not pruned", p)
		}
	}
	if searchPruneDir(t.TempDir()) {
		t.Fatal("a plain directory was pruned")
	}
}

// TestUpdateSearchRecordsScopeAndRestarts pins the walk's key: a query and a
// scope start one walk, and calling again with both unchanged starts nothing.
func TestUpdateSearchRecordsScopeAndRestarts(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "alpha.txt"), nil, 0o644)
	a := newTestApp(t, root)
	a.reload()
	tab := a.cur()
	tab.query = "alpha"
	a.updateSearch()
	if tab.search.scope != scopeFolder {
		t.Fatalf("walk scope = %v, want the folder scope", tab.search.scope)
	}
	waitSearch(t, a)
	if len(tab.search.results) != 1 {
		t.Fatalf("results = %v, want alpha.txt", namesOf(tab.search.results))
	}
	started := tab.search.started
	a.updateSearch() // same query, same scope: nothing to start
	if tab.search.started != started {
		t.Fatal("an unchanged query started a second walk")
	}
	if tab.search.done.IsZero() {
		t.Fatal("the finished walk forgot its time")
	}
}

// waitSearch waits for the active tab's walk to finish, bounded so a wedged walk
// fails the test instead of hanging it. updateSearch is the pump: the walk hands
// its batches to the frame that owns the tab's state (frameQueue), and calling
// it again with the query unchanged applies them without starting another walk.
func waitSearch(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.updateSearch()
		if !a.cur().search.busy {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the search walk never finished")
}

// TestSearchResultsFollowThePaneSort: the pane lists a search in the tab's own
// order — folders first, then the 名称 / 大小 key, ascending or descending — so
// its column headers keep working on a search, as Everything's window sorts its
// own results.
func TestSearchResultsFollowThePaneSort(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"a-big.txt": "12345", "a-small.txt": "1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newTestApp(t, dir)
	a.sortKey, a.sortAsc = SortName, true
	a.reload()
	a.cur().query = "a"
	a.updateSearch()
	waitSearch(t, a)

	sameNames := func(want []string) bool {
		got := namesOf(a.visibleEntries())
		if len(got) != len(want) {
			return false
		}
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	// 名称 ascending: the folder first, then the files by name.
	if !sameNames([]string{"a-folder", "a-big.txt", "a-small.txt"}) {
		t.Fatalf("name order = %v", namesOf(a.visibleEntries()))
	}
	// Descending flips the files; the folder stays first, as Explorer's does.
	a.sortAsc = false
	if !sameNames([]string{"a-folder", "a-small.txt", "a-big.txt"}) {
		t.Fatalf("descending name order = %v", namesOf(a.visibleEntries()))
	}
	// 大小 ascending: the folder first, then the smaller file.
	a.sortKey, a.sortAsc = SortSize, true
	if !sameNames([]string{"a-folder", "a-small.txt", "a-big.txt"}) {
		t.Fatalf("size order = %v", namesOf(a.visibleEntries()))
	}
}
