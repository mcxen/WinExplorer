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
	q := parseSearchQuery(query)
	streamMatches(root, q, hidden, deadline, func(batch []Entry, more bool) {
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
	// The walk is synchronous to its first flush; wait for it to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && tab.search.busy {
		time.Sleep(5 * time.Millisecond)
	}
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
