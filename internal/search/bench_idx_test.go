package search

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

// TestIndexBench measures docs/specs/23-search-index.md's table. It writes a
// 200k-file tree, so it only runs with SUNVALE_IDX_BENCH=1.
func TestIndexBench(t *testing.T) {
	if os.Getenv("SUNVALE_IDX_BENCH") == "" {
		t.Skip("set SUNVALE_IDX_BENCH=1")
	}
	words := []string{"report", "invoice", "photo", "budget", "draft", "notes", "backup", "design", "summary", "client",
		"project", "meeting", "archive", "export", "final", "review", "schedule", "contract", "readme", "config"}
	exts := []string{"txt", "pdf", "jpg", "docx", "go", "md", "png", "xlsx"}
	rng := rand.New(rand.NewSource(1))
	root := t.TempDir()
	var dirs []string
	for a := 0; a < 40; a++ {
		for b := 0; b < 50; b++ {
			dir := filepath.Join(root, words[a%20]+"_"+strconv.Itoa(a), words[b%20]+"-"+strconv.Itoa(b))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			dirs = append(dirs, dir)
			for f := 0; f < 100; f++ {
				name := fmt.Sprintf("%s_%s_%05d.%s", words[rng.Intn(20)], words[rng.Intn(20)], rng.Intn(100000), exts[rng.Intn(len(exts))])
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	cache := filepath.Join(t.TempDir(), "idx")
	wait := func(ix *Index) {
		for ix.Status().Building {
			time.Sleep(time.Millisecond)
		}
	}
	base := heapMB()
	ix := NewIndex(IndexConfig{CacheFile: cache, MaxEntries: 1_000_000})
	ix.Enable()
	t0 := time.Now()
	ix.Rebuild([]string{root})
	time.Sleep(time.Millisecond)
	wait(ix)
	t.Logf("cold build: %v, entries %d, file %.1f MB", time.Since(t0).Round(time.Millisecond), ix.Status().Total, float64(fileSize(cache))/(1<<20))
	t.Logf("heap after build: %.1f MB over base", heapMB()-base)

	base = heapMB()
	warm := NewIndex(IndexConfig{CacheFile: cache, MaxEntries: 1_000_000})
	t0 = time.Now()
	warm.Start([]string{root})
	t.Logf("load: %v, heap held by loaded index: %.1f MB", time.Since(t0).Round(time.Microsecond), heapMB()-base)

	for _, q := range []string{"pho", "invoi", "budget_fin", "00042", "_final_000", "e"} {
		query := searchquery.Parse(q)
		var n int
		t0 = time.Now()
		const reps = 20
		for i := 0; i < reps; i++ {
			hits, _ := warm.Query([]string{root}, query, false)
			n = len(hits)
		}
		idx := time.Since(t0) / reps
		t0 = time.Now()
		var walked int
		Stream([]string{root}, query, Options{Deadline: time.Now().Add(time.Minute)}, func(b []fsentry.Entry, _ bool) { walked += len(b) })
		t.Logf("query %-11q index %v (%d hits)  walk %v (%d hits)", q, idx.Round(time.Microsecond), n, time.Since(t0).Round(time.Millisecond), walked)
	}

	// 1% of the folders change: one new file each.
	future := time.Now().Add(5 * time.Second)
	for i := 0; i < len(dirs); i += 100 {
		_ = os.WriteFile(filepath.Join(dirs[i], "fresh_"+strconv.Itoa(i)+".txt"), nil, 0o644)
		_ = os.Chtimes(dirs[i], future, future)
	}
	t0 = time.Now()
	warm.Rebuild([]string{root})
	time.Sleep(time.Millisecond)
	wait(warm)
	st := warm.Status()
	t.Logf("refresh after 1%% change: %v, entries %d", time.Since(t0).Round(time.Millisecond), st.Total)
}
