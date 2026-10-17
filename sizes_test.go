package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Package tests walk temp trees, and the saver would mirror their folders
// into the user's real sizes.json: keep the cache file in the test's own
// scratch path instead.
func init() {
	dirStatsCacheFile = filepath.Join(os.TempDir(),
		fmt.Sprintf("sunvale-sizes-test-%d.json", os.Getpid()))
}

// writeTree makes parent dirs as needed and writes a file of n bytes.
func writeTree(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWalkDirStat checks the walk itself: exact totals, hidden files
// counted, symlinks never followed — not even a symlinked root, which walks
// its target but keeps the link's own ModTime — and the node cap keeping a
// partial tally.
func TestWalkDirStat(t *testing.T) {
	root := t.TempDir()
	writeTree(t, filepath.Join(root, "a.txt"), 3)
	writeTree(t, filepath.Join(root, ".hidden"), 4)
	writeTree(t, filepath.Join(root, "sub", "b.bin"), 5)
	writeTree(t, filepath.Join(root, "sub", "deep", "c.txt"), 7)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "a.txt"), filepath.Join(root, "file-link")); err != nil {
		t.Skip("cannot symlink:", err)
	}
	if err := os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "dir-link")); err != nil {
		t.Skip("cannot symlink:", err)
	}

	st, ok := walkDirStat(root)
	if !ok {
		t.Fatal("walk failed")
	}
	// 3+4+5+7: the hidden file counts, neither link does.
	if st.Size != 19 || st.Files != 4 || st.Dirs != 3 {
		t.Fatalf("stat = %+v, want size 19 files 4 dirs 3 (sub, deep, empty)", st)
	}
	if fi, err := os.Lstat(root); err != nil || !st.Mod.Equal(fi.ModTime()) {
		t.Fatalf("mod = %v, lstat = %v (%v)", st.Mod, fi, err)
	}

	// A symlinked root walks its target but reports the link's own Lstat
	// ModTime, the one the listing shows.
	link := filepath.Join(root, "dir-link")
	st, ok = walkDirStat(link)
	if !ok || st.Size != 12 || st.Files != 2 || st.Dirs != 1 {
		t.Fatalf("linked root = %+v ok=%v, want size 12 files 2 dirs 1", st, ok)
	}
	if fi, err := os.Lstat(link); err != nil || !st.Mod.Equal(fi.ModTime()) {
		t.Fatalf("linked mod = %v, lstat = %v (%v)", st.Mod, fi, err)
	}

	// A link that resolves nowhere caches an empty tally, so navigation
	// stops queueing it; a missing or plain-file root caches nothing.
	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "gone"), broken); err != nil {
		t.Skip("cannot symlink:", err)
	}
	if st, ok = walkDirStat(broken); !ok || st.Size != 0 || st.Files != 0 || st.Dirs != 0 {
		t.Fatalf("broken link = %+v ok=%v, want an empty cached tally", st, ok)
	}
	if _, ok := walkDirStat(filepath.Join(root, "nope")); ok {
		t.Fatal("missing root cached")
	}
	if _, ok := walkDirStat(filepath.Join(root, "a.txt")); ok {
		t.Fatal("plain file cached")
	}

	// The node cap stops a huge tree but still caches what it collected:
	// ReadDir sorts, so .hidden and a.txt are the first two entries.
	oldMax := dirStatMaxNodes
	dirStatMaxNodes = 2
	defer func() { dirStatMaxNodes = oldMax }()
	if st, ok = walkDirStat(root); !ok || st.Size != 7 || st.Files != 2 || st.Dirs != 0 {
		t.Fatalf("capped walk = %+v ok=%v, want the partial tally size 7 files 2", st, ok)
	}
}

// TestDirStatListingIntegration checks the pipeline headless: reload queues
// the folders it lists, the walk lands in the cache, and the entries the
// window shows pick the size up.
func TestDirStatListingIntegration(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	writeTree(t, filepath.Join(sub, "a.txt"), 100)
	writeTree(t, filepath.Join(sub, "inner", "b.bin"), 50)
	writeTree(t, filepath.Join(root, "c.txt"), 7)

	a := newTestApp(t, root)
	registerApp(a)
	t.Cleanup(func() { unregisterApp(a) })
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}

	st, ok := lookupDirStat(sub)
	if !ok || st.Size != 150 || st.Files != 2 || st.Dirs != 1 {
		t.Fatalf("sub stat = %+v ok=%v, want size 150 files 2 dirs 1", st, ok)
	}
	if fi, err := os.Lstat(sub); err != nil || !st.Mod.Equal(fi.ModTime()) {
		t.Fatalf("mod = %v, lstat = %v (%v)", st.Mod, fi, err)
	}
	var entry *Entry
	for i := range a.cur().entries {
		if a.cur().entries[i].Path == sub {
			entry = &a.cur().entries[i]
		}
	}
	if entry == nil || entry.Size != 150 {
		t.Fatalf("entry for sub = %+v, want the walked size 150", entry)
	}
	if dirStatInProgress(sub) {
		t.Fatal("sub still in progress after idle")
	}

	// Empty and special paths never enqueue; pending and running folders
	// report in progress, anything else does not.
	enqueueDirStat("")
	enqueueDirStat(pathHome)
	if !waitDirStatsIdle(time.Second) {
		t.Fatal("idle broke on an empty or special enqueue")
	}
	dirStatsMu.Lock()
	dirStatsPending["/we-pending"] = true
	dirStatsRunning["/we-running"] = true
	dirStatsMu.Unlock()
	t.Cleanup(func() {
		dirStatsMu.Lock()
		delete(dirStatsPending, "/we-pending")
		delete(dirStatsRunning, "/we-running")
		dirStatsMu.Unlock()
	})
	if !dirStatInProgress("/we-pending") || !dirStatInProgress("/we-running") {
		t.Fatal("pending or running folder not in progress")
	}
	if dirStatInProgress("/we-idle") {
		t.Fatal("idle folder reports in progress")
	}
}

// TestDirStatStaleRequeue checks the stale detection: a folder whose ModTime
// moved since its tally is queued again on the next reload, and the entries
// pick the fresh size up once the walk lands.
func TestDirStatStaleRequeue(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	writeTree(t, filepath.Join(sub, "a.txt"), 10)
	writeTree(t, filepath.Join(root, "top.txt"), 5)

	a := newTestApp(t, root)
	registerApp(a)
	t.Cleanup(func() { unregisterApp(a) })
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}

	// A new file moves the folder's ModTime, so reload shows the cached
	// size and queues a fresh walk behind it.
	writeTree(t, filepath.Join(sub, "b.txt"), 32)
	a.reload()
	if st, ok := lookupDirStat(sub); !ok || st.Size != 10 {
		t.Fatalf("stale cache = %+v ok=%v, want the old size 10", st, ok)
	}
	var entry *Entry
	for i := range a.cur().entries {
		if a.cur().entries[i].Path == sub {
			entry = &a.cur().entries[i]
		}
	}
	if entry == nil || entry.Size != 10 {
		t.Fatalf("entry right after reload = %+v, want the cached size 10", entry)
	}

	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	if st, ok := lookupDirStat(sub); !ok || st.Size != 42 {
		t.Fatalf("fresh cache = %+v ok=%v, want size 42", st, ok)
	}
	if entry == nil || entry.Size != 42 {
		t.Fatalf("entry after the walk = %+v, want size 42", entry)
	}
}

// TestRefreshDirStats checks the invalidation: the subtree's cache is
// dropped, the root is walked again — also when the refresh lands while a
// walk is still in flight, which is what the one-shot rerun covers.
func TestRefreshDirStats(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "proj")
	writeTree(t, filepath.Join(f, "sub", "a.txt"), 10)

	// Cache both the folder and its subfolder.
	enqueueDirStat(f)
	enqueueDirStat(filepath.Join(f, "sub"))
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	if st, ok := lookupDirStat(filepath.Join(f, "sub")); !ok || st.Size != 10 {
		t.Fatalf("sub stat = %+v ok=%v", st, ok)
	}

	// A change deep in the tree leaves the folder's own ModTime alone, so
	// the stale check cannot see it; the refresh drops the subtree and
	// rewalks the root.
	writeTree(t, filepath.Join(f, "sub", "b.txt"), 40)
	refreshDirStats(f)
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	if st, ok := lookupDirStat(f); !ok || st.Size != 50 {
		t.Fatalf("root after refresh = %+v ok=%v, want size 50", st, ok)
	}
	if _, ok := lookupDirStat(filepath.Join(f, "sub")); ok {
		t.Fatal("subtree cache survived the refresh")
	}

	// The same refresh landing around a walk in flight still ends with
	// fresh data, whichever side of the walk it caught.
	g := filepath.Join(root, "mid")
	writeTree(t, filepath.Join(g, "d", "x.txt"), 5)
	enqueueDirStat(g)
	writeTree(t, filepath.Join(g, "d", "y.txt"), 70)
	refreshDirStats(g)
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	if st, ok := lookupDirStat(g); !ok || st.Size != 75 {
		t.Fatalf("mid-walk refresh = %+v ok=%v, want size 75", st, ok)
	}
}

// TestDirStatsDiskRoundTrip checks the sizes.json mirror: what was walked is
// saved, an empty memory cache reads it back, and both caps keep the newest
// entries.
func TestDirStatsDiskRoundTrip(t *testing.T) {
	old := dirStatsCacheFile
	dirStatsCacheFile = filepath.Join(t.TempDir(), "sizes.json")
	defer func() { dirStatsCacheFile = old }()

	root := t.TempDir()
	dir := filepath.Join(root, "keep")
	writeTree(t, filepath.Join(dir, "a.txt"), 64)

	// Drain the engine first, then start from an empty cache so this folder
	// is the only thing on disk.
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	dirStatsMu.Lock()
	dirStatsMem = map[string]dirStat{}
	dirStatsMu.Unlock()
	enqueueDirStat(dir)
	if !waitDirStatsIdle(10 * time.Second) {
		t.Fatal("dir stats did not go idle")
	}
	flushDirStats()

	data, err := os.ReadFile(dirStatsCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	var f dirStatDiskFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != 1 {
		t.Fatalf("cache file = %s (%v)", data, err)
	}
	if len(f.Entries) != 1 || f.Entries[0].Path != dir || f.Entries[0].Size != 64 {
		t.Fatalf("entries = %+v, want only %s with size 64", f.Entries, dir)
	}

	// A reload of an empty memory cache reads it back.
	dirStatsMu.Lock()
	dirStatsMem = map[string]dirStat{}
	loadDirStatsLocked()
	st, ok := dirStatsMem[dir]
	dirStatsMu.Unlock()
	if !ok || st.Size != 64 || st.Files != 1 {
		t.Fatalf("reloaded = %+v ok=%v", st, ok)
	}
	wantAt := st.At

	// Over either cap the oldest tallies drop first: the memory cache holds
	// dirStatMemMax entries, the file dirStatDiskMax.
	dirStatsMu.Lock()
	base := time.Now().Add(-24 * time.Hour)
	for i := 0; i < dirStatMemMax+100; i++ {
		dirStatsMem[fmt.Sprintf("/cap/%d", i)] = dirStat{
			At: base.Add(time.Duration(i) * time.Second),
		}
	}
	dirStatsDirty = true
	saveDirStatsLocked()
	dirStatsEvictLocked()
	n := len(dirStatsMem)
	_, oldestKept := dirStatsMem["/cap/0"]
	_, newestKept := dirStatsMem[fmt.Sprintf("/cap/%d", dirStatMemMax+99)]
	dirStatsDirty = false
	dirStatsMu.Unlock()
	if n != dirStatMemMax {
		t.Fatalf("memory cache = %d entries, want %d", n, dirStatMemMax)
	}
	if oldestKept {
		t.Fatal("oldest entry survived the memory eviction")
	}
	if !newestKept {
		t.Fatal("newest entry dropped from the memory cache")
	}

	data, err = os.ReadFile(dirStatsCacheFile)
	if err != nil {
		t.Fatal(err)
	}
	f = dirStatDiskFile{}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	var diskStat *dirStatDiskEntry
	for i := range f.Entries {
		paths[f.Entries[i].Path] = true
		if f.Entries[i].Path == dir {
			diskStat = &f.Entries[i]
		}
	}
	if len(f.Entries) != dirStatDiskMax {
		t.Fatalf("disk cache = %d entries, want %d", len(f.Entries), dirStatDiskMax)
	}
	if paths["/cap/0"] {
		t.Fatal("oldest entry survived on disk")
	}
	if !paths[fmt.Sprintf("/cap/%d", dirStatMemMax+99)] {
		t.Fatal("newest entry missing on disk")
	}
	if diskStat == nil || diskStat.Size != 64 || !diskStat.At.Equal(wantAt) {
		t.Fatalf("walked folder on disk = %+v, want size 64 at %v", diskStat, wantAt)
	}

	// Leave the engine clean for whatever test follows.
	dirStatsMu.Lock()
	dirStatsMem = map[string]dirStat{}
	dirStatsMu.Unlock()
}
