package search

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

// memTree writes dirs*per empty files under a temp dir.
func memTree(t *testing.T, dirs, per int) string {
	t.Helper()
	root := t.TempDir()
	for d := 0; d < dirs; d++ {
		dir := filepath.Join(root, "d"+strconv.Itoa(d))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < per; f++ {
			if err := os.WriteFile(filepath.Join(dir, "file_"+strconv.Itoa(f)+".txt"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func heapMB() float64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

// TestSearchMemoryBounded runs many searches over a 100k-file tree and checks
// that stale walks stop and that browsing many folders keeps the index bounded.
func TestSearchMemoryBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("large tree")
	}
	root := memTree(t, 1000, 100)
	base := heapMB()
	g0 := runtime.NumGoroutine()

	// Typing: each keystroke starts a broad walk and abandons the previous one.
	// Each new query cancels the one before, as the palette and the tab do.
	var stop context.CancelFunc = func() {}
	for i := 0; i < 40; i++ {
		stop()
		ctx, cancel := context.WithCancel(context.Background())
		stop = cancel
		q := searchquery.Parse("file_" + strconv.Itoa(i%10))
		go Stream([]string{root}, q, Options{Ctx: ctx, Deadline: time.Now().Add(30 * time.Second), Max: DefaultMaxResults},
			func([]fsentry.Entry, bool) {})
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	time.Sleep(200 * time.Millisecond)
	g1, h1 := runtime.NumGoroutine(), heapMB()
	t.Logf("after 40 abandoned searches: goroutines %d (start %d), heap %.1f MB (base %.1f)", g1, g0, h1, base)
	if g1 > g0+5 {
		t.Errorf("abandoned searches still running: %d goroutines, started with %d", g1, g0)
	}

	// Browsing: the pane notes every folder it lists into the index.
	ix := NewIndex(IndexConfig{MaxEntries: 20000})
	ix.Enable()
	for d := 0; d < 1000; d++ {
		dir := filepath.Join(root, "d"+strconv.Itoa(d))
		des, _ := os.ReadDir(dir)
		entries := make([]fsentry.Entry, 0, len(des))
		for _, de := range des {
			p := filepath.Join(dir, de.Name())
			entries = append(entries, fsentry.Entry{Name: de.Name(), Path: p, Dir: dir, Ext: "txt"})
		}
		ix.Note(dir, entries)
	}
	for i := 0; i < 200; i++ {
		ix.Query([]string{root}, searchquery.Parse("file_"+strconv.Itoa(i)), false)
	}
	st := ix.Status()
	t.Logf("index after noting 1000 folders: %d items (cap %d), heap %.1f MB", st.Total, 20000, heapMB())
	if st.Total > 20000 {
		t.Errorf("noted listings grew the index past its cap: %d items", st.Total)
	}
	runtime.KeepAlive(ix)
}
