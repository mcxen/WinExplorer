package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// search_stream_test.go keeps application-level search behavior: status text,
// scope selection and the tab's frame-owned stream state. The reusable walker
// has its own black-box tests in internal/search.

func namesOf(hits []Entry) []string {
	var out []string
	for _, hit := range hits {
		out = append(out, filepath.Base(hit.Path))
	}
	return out
}

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
	for _, volume := range vols {
		if seen[volume] {
			t.Fatalf("volume %q listed twice: %v", volume, vols)
		}
		seen[volume] = true
		if st, err := os.Stat(volume); err != nil || !st.IsDir() {
			t.Fatalf("volume root %q is not a directory", volume)
		}
	}
	a.navigate(pathThisPC)
	if got := a.searchRoots(scopeFolder); got != nil {
		t.Fatalf("special folder roots = %v, want none", got)
	}
}

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
	a.updateSearch()
	if tab.search.started != started {
		t.Fatal("an unchanged query started a second walk")
	}
	if tab.search.done.IsZero() {
		t.Fatal("the finished walk forgot its time")
	}
}

// waitSearch pumps the frame queue while the background walk is active.
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
		return equalStrings(namesOf(a.visibleEntries()), want)
	}
	if !sameNames([]string{"a-folder", "a-big.txt", "a-small.txt"}) {
		t.Fatalf("name order = %v", namesOf(a.visibleEntries()))
	}
	a.sortAsc = false
	if !sameNames([]string{"a-folder", "a-small.txt", "a-big.txt"}) {
		t.Fatalf("descending name order = %v", namesOf(a.visibleEntries()))
	}
	a.sortKey, a.sortAsc = SortSize, true
	if !sameNames([]string{"a-folder", "a-small.txt", "a-big.txt"}) {
		t.Fatalf("size order = %v", namesOf(a.visibleEntries()))
	}
}
