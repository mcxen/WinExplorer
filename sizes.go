package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// sizes.go is the details view's 大小 column for folders: Explorer leaves a
// folder's size blank until it is asked for; here every folder a listing
// shows is queued for a background walk, four walkers total the subtrees off
// the UI thread, and the tallies land in a process-wide cache — memory
// first, mirrored to sizes.json — so the next visit shows the size at once.
// A folder whose ModTime no longer matches its cached tally is queued again,
// as Explorer recomputes after a change. The engine is process-wide, so
// every window and tab shares one queue and one cache.

const (
	dirStatWorkers      = 4                      // walkers reading folders at once
	dirStatMemMax       = 8192                   // memory cache entries, oldest evicted
	dirStatDiskMax      = 4096                   // entries kept in sizes.json
	dirStatBatchEntries = 64                     // results collected before the UI is told
	dirStatBatchDelay   = 150 * time.Millisecond // the batch's window
	dirStatSaveEvery    = 5 * time.Second        // the saver's dirty check
	dirStatResultsCap   = 256                    // results buffered between walkers and collector
)

// dirStatTimeout and dirStatMaxNodes bound one walk, as search's deadline
// and usage's node cap bound theirs; vars so tests can shrink them.
var (
	dirStatTimeout  = 30 * time.Second
	dirStatMaxNodes = 250000
)

// dirStatsCacheFile is where the cache persists, beside statestore.go's
// state.json. A var so tests can point it at a scratch file.
var dirStatsCacheFile = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "sizes.json")
}()

// dirStat is one folder's tally: the bytes of every file under it and the
// file and subfolder counts, with when the walk finished and the folder's
// own Lstat ModTime as the walk saw it — the stale check compares that with
// the listing's, so a symlinked folder is not mistaken for changed on every
// visit.
type dirStat struct {
	Size  int64
	Files int
	Dirs  int
	At    time.Time
	Mod   time.Time
}

// dirStatResult is one finished walk on its way to the cache and the UI.
type dirStatResult struct {
	path string
	stat dirStat
}

// dirStatDiskEntry is one cached folder in sizes.json.
type dirStatDiskEntry struct {
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	Files int       `json:"files"`
	Dirs  int       `json:"dirs"`
	At    time.Time `json:"at"`
	Mod   time.Time `json:"mod"`
}

// dirStatDiskFile is sizes.json: a version and the entries, oldest first.
type dirStatDiskFile struct {
	Version int                `json:"version"`
	Entries []dirStatDiskEntry `json:"entries"`
}

var (
	// dirStatsMu guards everything below it and the queue's state.
	dirStatsMu   sync.Mutex
	dirStatsCond = sync.NewCond(&dirStatsMu)

	// dirStatsMem is the in-memory cache, every window's lookup.
	dirStatsMem = map[string]dirStat{}

	// dirStatsQueue is the paths waiting for a walker; dirStatsPending is
	// the same paths as a set for dedup, dirStatsRunning the ones a walker
	// holds, and dirStatsRetry the one-shot reruns refreshDirStats asked
	// for mid-walk.
	dirStatsQueue   []string
	dirStatsPending = map[string]bool{}
	dirStatsRunning = map[string]bool{}
	dirStatsRetry   = map[string]bool{}

	// dirStatInFlight counts finished walks the collector has not applied
	// to the cache yet; idle means queue, walkers and this are all empty.
	dirStatInFlight int
	dirStatsDirty   bool

	dirStatResults = make(chan dirStatResult, dirStatResultsCap)

	dirStatsOnce sync.Once
)

// ensureDirStatsStarted loads the disk cache and starts the walkers, the
// collector and the saver once per process. The public entry points call it,
// so nothing else needs wiring.
func ensureDirStatsStarted() {
	dirStatsOnce.Do(func() {
		dirStatsMu.Lock()
		loadDirStatsLocked()
		dirStatsMu.Unlock()
		for i := 0; i < dirStatWorkers; i++ {
			go dirStatWorker()
		}
		go dirStatCollector()
		go dirStatSaver()
	})
}

// lookupDirStat returns the folder's cached tally, if any.
func lookupDirStat(path string) (dirStat, bool) {
	ensureDirStatsStarted()
	dirStatsMu.Lock()
	defer dirStatsMu.Unlock()
	st, ok := dirStatsMem[path]
	return st, ok
}

// dirStatInProgress reports whether the folder is queued for a walk or one
// is running.
func dirStatInProgress(path string) bool {
	dirStatsMu.Lock()
	defer dirStatsMu.Unlock()
	return dirStatsPending[path] || dirStatsRunning[path]
}

// enqueueDirStat schedules a background walk of the folder, unless one is
// already queued or running. Special pages never have a tally.
func enqueueDirStat(path string) {
	ensureDirStatsStarted()
	if path == "" || specialPath(path) {
		return
	}
	dirStatsMu.Lock()
	defer dirStatsMu.Unlock()
	if dirStatsPending[path] || dirStatsRunning[path] {
		return
	}
	dirStatsPending[path] = true
	dirStatsQueue = append(dirStatsQueue, path)
	dirStatsCond.Signal()
}

// refreshDirStats drops the cached tallies of root and everything under it,
// then walks root again. A walk already queued or running cannot see the
// invalidation, so a one-shot rerun is arranged for it: the refresh always
// ends with fresh data.
func refreshDirStats(root string) {
	ensureDirStatsStarted()
	if root == "" || specialPath(root) {
		return
	}
	prefix := root + string(filepath.Separator)
	dirStatsMu.Lock()
	defer dirStatsMu.Unlock()
	for p := range dirStatsMem {
		if p == root || strings.HasPrefix(p, prefix) {
			delete(dirStatsMem, p)
		}
	}
	if dirStatsPending[root] || dirStatsRunning[root] {
		dirStatsRetry[root] = true
		return
	}
	dirStatsPending[root] = true
	dirStatsQueue = append(dirStatsQueue, root)
	dirStatsCond.Signal()
}

// flushDirStats writes the cache to disk when anything changed, as the app
// quits.
func flushDirStats() {
	dirStatsMu.Lock()
	defer dirStatsMu.Unlock()
	if !dirStatsDirty {
		return
	}
	dirStatsDirty = false
	saveDirStatsLocked()
}

// waitDirStatsIdle waits for the queue, the walkers and the collector to
// finish, so tests can read the cache and the entries. It reports whether
// everything drained before the timeout.
func waitDirStatsIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		dirStatsMu.Lock()
		idle := len(dirStatsQueue) == 0 && len(dirStatsRunning) == 0 &&
			len(dirStatsRetry) == 0 && dirStatInFlight == 0
		dirStatsMu.Unlock()
		if idle {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// dirStatWorker takes folders off the queue and walks them. The result goes
// to the collector; a folder that cannot be read produces nothing, so it is
// walked again the next time it shows up in a listing.
func dirStatWorker() {
	for {
		dirStatsMu.Lock()
		for len(dirStatsQueue) == 0 {
			dirStatsCond.Wait()
		}
		path := dirStatsQueue[0]
		dirStatsQueue = dirStatsQueue[1:]
		delete(dirStatsPending, path)
		dirStatsRunning[path] = true
		dirStatsMu.Unlock()

		st, ok := walkDirStat(path)

		dirStatsMu.Lock()
		delete(dirStatsRunning, path)
		if dirStatsRetry[path] {
			// The walk overlapped an invalidation: run it once more.
			delete(dirStatsRetry, path)
			dirStatsPending[path] = true
			dirStatsQueue = append(dirStatsQueue, path)
			dirStatsCond.Signal()
		}
		if ok {
			dirStatInFlight++
		}
		dirStatsMu.Unlock()
		if ok {
			dirStatResults <- dirStatResult{path: path, stat: st}
		}
	}
}

// dirStatCollector gathers finished walks and applies them in batches, so a
// large listing updates the UI and the cache in a few hops instead of one
// per folder. A batch goes out at 64 results or 150ms, whichever comes
// first.
func dirStatCollector() {
	var batch []dirStatResult
	var due <-chan time.Time
	for {
		if len(batch) == 0 {
			batch = append(batch, <-dirStatResults)
			due = time.After(dirStatBatchDelay)
			continue
		}
		select {
		case r := <-dirStatResults:
			batch = append(batch, r)
			if len(batch) >= dirStatBatchEntries {
				flushDirStatBatch(batch)
				batch, due = nil, nil
			}
		case <-due:
			flushDirStatBatch(batch)
			batch, due = nil, nil
		}
	}
}

// flushDirStatBatch lands one batch in the memory cache, then tells the UI.
// The engine lock is free before the UI hop and the main-thread fn touches
// no engine state, so neither side ever waits on the other.
func flushDirStatBatch(batch []dirStatResult) {
	dirStatsMu.Lock()
	for _, r := range batch {
		dirStatsMem[r.path] = r.stat
	}
	dirStatsEvictLocked()
	dirStatsDirty = true
	dirStatsMu.Unlock()
	dirStatUpdateUI(batch)
	dirStatsMu.Lock()
	dirStatInFlight -= len(batch)
	dirStatsMu.Unlock()
}

// dirStatUpdateUI applies a batch on the main thread; headless, where the
// app never runs, it lands right here, as repaint does.
func dirStatUpdateUI(batch []dirStatResult) {
	sizes := make(map[string]int64, len(batch))
	for _, r := range batch {
		sizes[r.path] = r.stat.Size
	}
	fn := func() { applyDirStatSizes(sizes) }
	if mygo.App.IsReady() {
		mygo.RunOnMain(fn)
	} else {
		fn()
	}
}

// applyDirStatSizes fills the folders' sizes into every window's listings
// and search results, then repaints. Only folders in the batch move; the
// rest keep what they had.
func applyDirStatSizes(sizes map[string]int64) {
	appsMu.Lock()
	windows := append([]*App(nil), apps...)
	appsMu.Unlock()
	for _, a := range windows {
		for _, t := range a.tabs {
			for i := range t.entries {
				if e := &t.entries[i]; e.IsDir {
					if sz, ok := sizes[e.Path]; ok {
						e.Size = sz
					}
				}
			}
			for i := range t.search.results {
				if e := &t.search.results[i]; e.IsDir {
					if sz, ok := sizes[e.Path]; ok {
						e.Size = sz
					}
				}
			}
		}
	}
	repaint()
}

// walkDirStat totals one folder's subtree: the bytes of every regular file
// under it — hidden files included — plus the file and subfolder counts.
// Symlinks are never followed inside the tree; a symlinked root, which the
// listing shows as a folder, is resolved to its target once, with the
// resolved path seeded into seen so a loop cannot walk forever. A root that
// cannot be resolved is cached empty, so navigation stops queueing it; a
// root that is missing or unreadable produces no cache entry, so it is
// walked again once it can be read. The deadline and node cap bound a huge
// tree, and a walk over the limit still caches what it collected. Mod is the
// root path's Lstat ModTime — the same clock the listing shows, so a
// symlinked folder is not judged stale on every visit.
func walkDirStat(path string) (dirStat, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return dirStat{}, false // gone: nothing to cache
	}
	mod := info.ModTime()
	root := path
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			// An unresolvable link: cache the empty tally.
			return dirStat{At: time.Now(), Mod: mod}, true
		}
		if st, err := os.Stat(resolved); err != nil || !st.IsDir() {
			return dirStat{}, false // the link leads nowhere folder-like
		}
		root = resolved
	case info.IsDir():
	default:
		return dirStat{}, false // a plain file has no folder tally
	}
	var st dirStat
	deadline := time.Now().Add(dirStatTimeout)
	seen := map[string]bool{root: true}
	queue := []string{root}
	nodes := 0
	for len(queue) > 0 && nodes < dirStatMaxNodes && !time.Now().After(deadline) {
		dir := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(dir)
		if err != nil {
			if dir == root {
				return dirStat{}, false // unreadable root: no cache
			}
			continue // unreadable subfolder: skip it, keep walking
		}
		for _, e := range entries {
			if nodes >= dirStatMaxNodes || time.Now().After(deadline) {
				break // over the limit: keep the partial tally
			}
			nodes++
			t := e.Type()
			if t&os.ModeSymlink != 0 {
				continue // links inside the tree are never followed
			}
			if t.IsDir() {
				st.Dirs++
				child := filepath.Join(dir, e.Name())
				if !seen[child] {
					seen[child] = true
					queue = append(queue, child)
				}
				continue
			}
			if !t.IsRegular() {
				continue // sockets, devices and the like hold no bytes
			}
			if fi, err := e.Info(); err == nil {
				st.Size += fi.Size()
				st.Files++
			}
		}
	}
	st.At = time.Now()
	st.Mod = mod
	return st, true
}

// dirStatsEvictLocked trims the memory cache to its cap, dropping the
// oldest tallies first. Called with dirStatsMu held.
func dirStatsEvictLocked() {
	for len(dirStatsMem) > dirStatMemMax {
		oldest := ""
		var oldestAt time.Time
		for p, st := range dirStatsMem {
			if oldest == "" || st.At.Before(oldestAt) {
				oldest, oldestAt = p, st.At
			}
		}
		delete(dirStatsMem, oldest)
	}
}

// dirStatSaver writes the cache back every few seconds when something
// changed, so an ordinary quit loses at most the last few tallies.
func dirStatSaver() {
	tick := time.NewTicker(dirStatSaveEvery)
	defer tick.Stop()
	for range tick.C {
		dirStatsMu.Lock()
		if !dirStatsDirty {
			dirStatsMu.Unlock()
			continue
		}
		dirStatsDirty = false
		saveDirStatsLocked()
		dirStatsMu.Unlock()
	}
}

// loadDirStatsLocked reads the disk cache into memory. A missing or corrupt
// file is an empty cache, as statestore.go treats state.json. Called with
// dirStatsMu held.
func loadDirStatsLocked() {
	if dirStatsCacheFile == "" {
		return
	}
	data, err := os.ReadFile(dirStatsCacheFile)
	if err != nil {
		return
	}
	var f dirStatDiskFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != 1 {
		return
	}
	for _, e := range f.Entries {
		if e.Path == "" {
			continue
		}
		dirStatsMem[e.Path] = dirStat{
			Size: e.Size, Files: e.Files, Dirs: e.Dirs, At: e.At, Mod: e.Mod,
		}
	}
	dirStatsEvictLocked()
}

// saveDirStatsLocked writes the cache to disk, keeping the newest entries
// when it is over the file's cap. Errors are ignored: a lost tally is only
// recomputed. Called with dirStatsMu held.
func saveDirStatsLocked() {
	if dirStatsCacheFile == "" {
		return
	}
	entries := make([]dirStatDiskEntry, 0, len(dirStatsMem))
	for p, st := range dirStatsMem {
		entries = append(entries, dirStatDiskEntry{
			Path: p, Size: st.Size, Files: st.Files, Dirs: st.Dirs, At: st.At, Mod: st.Mod,
		})
	}
	// Oldest first, so the tail of the cap is what stays.
	slices.SortStableFunc(entries, func(a, b dirStatDiskEntry) int {
		return a.At.Compare(b.At)
	})
	if len(entries) > dirStatDiskMax {
		entries = entries[len(entries)-dirStatDiskMax:]
	}
	if err := os.MkdirAll(filepath.Dir(dirStatsCacheFile), 0o755); err != nil {
		return
	}
	// Compact, unlike state.json: the cache can hold thousands of entries
	// and is rewritten whenever it changes.
	data, err := json.Marshal(dirStatDiskFile{Version: 1, Entries: entries})
	if err != nil {
		return
	}
	_ = os.WriteFile(dirStatsCacheFile, data, 0o644)
}
