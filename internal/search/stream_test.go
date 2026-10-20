package search

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

func streamRoots(roots []string, query string, hidden bool, deadline time.Time, max int) (hits []fsentry.Entry, totals []int) {
	Stream(roots, searchquery.Parse(query), Options{
		Hidden: hidden, Deadline: deadline, Max: max,
	}, func(batch []fsentry.Entry, more bool) {
		hits = append(hits, batch...)
		totals = append(totals, len(hits))
	})
	return hits, totals
}

func namesOf(hits []fsentry.Entry) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, filepath.Base(hit.Path))
	}
	return out
}

func TestStreamBatchesAndAlwaysEnds(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(root, "hit-"+strconv.Itoa(i)+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, totals := streamRoots([]string{root}, "hit", false, time.Now().Add(5*time.Second), 0)
	if len(hits) != 40 {
		t.Fatalf("hits = %d, want 40", len(hits))
	}
	if len(totals) == 0 || totals[len(totals)-1] != 40 {
		t.Fatalf("stream totals = %v, want a final total of 40", totals)
	}
	for _, hit := range hits {
		if hit.Dir != root {
			t.Fatalf("hit %v lost its folder", hit)
		}
	}
}

func TestStreamQueryLanguage(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"docs", "pics"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"docs/report.txt", "docs/report.md", "pics/report.png", "notes.txt",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{query: "report ext:txt", want: []string{"report.txt"}},
		{query: "path:pics report", want: []string{"report.png"}},
		{query: "folder:rep", want: nil},
		{query: "report md", want: []string{"report.md"}},
		{query: "case:REPORT", want: nil},
		{query: "case:report", want: []string{"report.txt", "report.md", "report.png"}},
	} {
		hits, _ := streamRoots([]string{root}, tc.query, false, deadline, 0)
		got := namesOf(hits)
		if !equalStrings(got, tc.want) && !sameStringSet(got, tc.want) {
			t.Fatalf("%q = %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestStreamSharedCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		if err := os.WriteFile(filepath.Join(root, "many-"+strconv.Itoa(i)+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, _ := streamRoots([]string{root}, "many", false, time.Now().Add(5*time.Second), 3)
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want the shared cap of 3", len(hits))
	}
}

func TestStreamManyRoots(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(a, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(a, "hit-a.txt"),
		filepath.Join(a, "sub", "hit-a2.txt"),
		filepath.Join(b, "hit-b.txt"),
	} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, totals := streamRoots([]string{a, b}, "hit", false, time.Now().Add(5*time.Second), 0)
	if len(hits) != 3 || totals[len(totals)-1] != 3 {
		t.Fatalf("hits = %v, totals = %v", namesOf(hits), totals)
	}
	dirs := map[string]bool{}
	for _, hit := range hits {
		dirs[hit.Dir] = true
	}
	if !dirs[a] || !dirs[filepath.Join(a, "sub")] || !dirs[b] {
		t.Fatalf("hits lost their folders: %v", dirs)
	}
}

func TestPruneDir(t *testing.T) {
	for _, path := range platformPrune() {
		if !pruneDir(path) {
			t.Fatalf("%q is on the platform prune list but was not pruned", path)
		}
	}
	if pruneDir(t.TempDir()) {
		t.Fatal("a plain directory was pruned")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}
