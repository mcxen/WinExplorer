package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// searchindex.go is Everything's index: one walk of the machine whose entries
// stay in memory and land in index.json beside state.json, so the Ctrl+K
// palette answers on the keystroke instead of walking the disks per query.
// Everything is an index with a live shell in front of it; this is that index,
// built by walking rather than by reading the NTFS master file table.
//
// The cache is the 启动后缓存 this palette is built on: startSearchIndex reads the
// last walk's entries (Ctrl+K searches them at once, warm) and walks the volumes
// again in the background, swapping the fresh snapshot in and writing it back. A
// machine with more than searchIndexCap entries keeps the first ones walked — the
// user's home first, so the cap cuts the system's own trees before the folders
// the user searches for — and the footer says 未完整 rather than passing the
// list off as the whole machine, with the live walk filling the short index in. A
// quit in the middle of a build saves what the walk has read so far
// (flushSearchIndex), so even a first launch cut short leaves a warm start behind.
//
// The pane's own search box (search.go) is untouched: a listing view wants the
// live walk, streamed. The index serves the palette, which is the query tool,
// and the palette falls back to that same live walk whenever the index is off,
// empty, or has never reached the folder being searched — see
// searchIndexQuery.
const (
	// searchIndexWalkers is how many roots one build walks at once. Fewer than
	// the search's own walkers: a build runs in the background for minutes and
	// should not own the disk.
	searchIndexWalkers = 2
	// searchIndexVersion is the cache file's format, refused when it differs.
	searchIndexVersion = 1
)

// searchIndexCap, searchIndexTimeout and searchIndexRefresh bound a build:
// how many entries it keeps, how long it may run, and how old a cache may be
// before startup walks again. Vars so the tests can shrink them.
var (
	searchIndexCap     = 200000
	searchIndexTimeout = 10 * time.Minute
	searchIndexRefresh = 6 * time.Hour
)

// searchIndexCacheFile is where the index persists, beside statestore.go's
// state.json. A var so tests can point it at a scratch file.
var searchIndexCacheFile = func() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "index.json")
}()

// indexEntry is one indexed path: what a hit needs to be listed and matched,
// with the stat the walk already paid for and no more — the name and the folder
// come out of the path when a hit is built.
type indexEntry struct {
	path   string
	ext    string // lower, no dot; empty for folders
	size   int64
	mod    int64 // Unix seconds; 0 when the walk could not stat it
	isDir  bool
	hidden bool // a dot-prefixed name, the walk's hidden rule (search.go)
}

// entry is the indexEntry as the views take an Entry, splitting the folder and
// the name back out of the path.
func (e indexEntry) entry() Entry {
	out := Entry{
		Name:  filepath.Base(e.path),
		Path:  e.path,
		Dir:   filepath.Dir(e.path),
		IsDir: e.isDir,
		Size:  e.size,
	}
	if e.mod > 0 {
		out.ModTime = time.Unix(e.mod, 0)
	}
	if !e.isDir {
		out.Ext = e.ext
	}
	return out
}

// indexEntryFrom is the same entry as the app's own listing sees it, for
// searchIndexNote — no stat, the listing already read one.
func indexEntryFrom(e Entry) indexEntry {
	out := indexEntry{
		path:   e.Path,
		ext:    strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), ".")),
		size:   e.Size,
		isDir:  e.IsDir,
		hidden: strings.HasPrefix(e.Name, "."),
	}
	if !e.ModTime.IsZero() {
		out.mod = e.ModTime.Unix()
	}
	return out
}

// searchIndexStore is the process-wide index: the snapshot a query reads, what
// the last build knew of itself, and the folders the window's own listings
// folded in.
//
// items is read under the read lock for the length of a query — the map is
// upserted in place by the listings, so a query that ranged it without the lock
// would race them. A listing therefore waits for a running scan, which is tens
// of milliseconds on a full index, once per navigation.
type searchIndexStore struct {
	mu sync.RWMutex

	items    map[string]indexEntry
	built    time.Time
	complete bool
	enabled  bool
	building bool

	// progress counts the entries the running build has collected, which the
	// footer reads while it walks; it is touched without mu.
	progress atomic.Int64

	// dirs is what the window's own listings folded in: each folder the pane
	// listed this run with the entries that listing showed, so the next
	// listing replaces them and a build that finishes meanwhile keeps them.
	dirs map[string][]indexEntry

	// pending is the build in flight, swapped into items when it ends — or
	// saved as it stands when the app quits first. Its own mutex: the walkers
	// write it while queries read items, and the two must not wait a scan.
	pendingMu sync.Mutex
	pending   map[string]indexEntry
}

var searchIndex searchIndexStore

// startSearchIndex turns the index on, reads the cache and walks again when
// that cache is worth replacing. main calls it in the background once the first
// window is up; every test that never calls it keeps the palette on its live
// walk.
func startSearchIndex() {
	searchIndex.mu.Lock()
	if searchIndex.enabled {
		searchIndex.mu.Unlock()
		return
	}
	searchIndex.enabled = true
	searchIndex.mu.Unlock()
	loadSearchIndex()
	if searchIndexStale() {
		searchIndexRebuild()
	}
}

// searchIndexOn reports whether the index is in use.
func searchIndexOn() bool {
	searchIndex.mu.RLock()
	defer searchIndex.mu.RUnlock()
	return searchIndex.enabled
}

// searchIndexStale reports whether the snapshot on hand is worth replacing: an
// empty one, one that is not the whole machine, or one older than
// searchIndexRefresh.
func searchIndexStale() bool {
	searchIndex.mu.RLock()
	defer searchIndex.mu.RUnlock()
	return len(searchIndex.items) == 0 || !searchIndex.complete ||
		time.Since(searchIndex.built) > searchIndexRefresh
}

// searchIndexRebuild walks the machine and swaps the result in, then writes it
// to the cache. One build runs at a time: a request while one is in flight is
// the request already being answered.
func searchIndexRebuild() {
	searchIndex.mu.Lock()
	if searchIndex.building || !searchIndex.enabled {
		searchIndex.mu.Unlock()
		return
	}
	searchIndex.building = true
	searchIndex.progress.Store(0)
	searchIndex.pendingMu.Lock()
	searchIndex.pending = make(map[string]indexEntry, 4096)
	searchIndex.pendingMu.Unlock()
	searchIndex.mu.Unlock()
	go func() {
		complete := walkSearchIndex()
		searchIndex.mu.Lock()
		searchIndex.pendingMu.Lock()
		items := searchIndex.pending
		searchIndex.pending = nil
		searchIndex.pendingMu.Unlock()
		if items == nil {
			items = map[string]indexEntry{}
		}
		// What the listings folded in while the walk ran is fresher than the
		// walk's own read of those folders: it goes over the new snapshot.
		for _, entries := range searchIndex.dirs {
			for _, e := range entries {
				items[e.path] = e
			}
		}
		searchIndex.items = items
		searchIndex.built = time.Now()
		searchIndex.complete = complete
		searchIndex.mu.Unlock()
		saveSearchIndex()
		// The build is over only once its cache is written: while the flag
		// stands, the build is still touching the store (and the tests' own
		// scratch cache file), and the footer's 正在重建 is honest either way.
		searchIndex.mu.Lock()
		searchIndex.building = false
		searchIndex.progress.Store(0)
		searchIndex.mu.Unlock()
	}()
}

// searchIndexRoots are what a build walks, in the order it walks them: the
// user's home first — so a cap the machine's size reaches cuts the tail, the
// system's own trees, rather than the folders the user searches for — then
// every mounted volume.
func searchIndexRoots() (first []string, roots []string) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if st, err := os.Stat(home); err == nil && st.IsDir() {
			first = append(first, filepath.Clean(home))
		}
	}
	for _, r := range globalSearchRoots() {
		if slices.Contains(first, r) {
			continue
		}
		roots = append(roots, r)
	}
	return first, roots
}

// walkSearchIndex walks searchIndexRoots into the build's pending map, and
// reports whether it saw the whole machine: the cap and the deadline end a walk
// early, and a walk that ended early is not everything there is.
func walkSearchIndex() bool {
	first, roots := searchIndexRoots()
	if len(first) == 0 && len(roots) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), searchIndexTimeout)
	defer cancel()
	add := func(e indexEntry) {
		searchIndex.pendingMu.Lock()
		if searchIndex.pending != nil {
			searchIndex.pending[e.path] = e
		}
		searchIndex.pendingMu.Unlock()
	}
	// The phases go in order: the home tree, then the volumes with the home
	// tree skipped — the same files, walked once.
	skip := map[string]bool{}
	for _, root := range first {
		skip[root] = true
	}
	walkIndexList(ctx, first, nil, add)
	walkIndexList(ctx, roots, skip, add)
	searchIndex.pendingMu.Lock()
	n := len(searchIndex.pending)
	searchIndex.pendingMu.Unlock()
	return indexComplete(ctx, n)
}

// indexComplete reports whether a walk that has just ended saw the whole
// machine: the deadline must not have fired, and the cap must not have cut the
// entries short.
func indexComplete(ctx context.Context, collected int) bool {
	return ctx.Err() == nil && collected < searchIndexCap
}

// walkIndexList walks every root of list, searchIndexWalkers at once — one
// volume does not hold up another's — handing every entry read to add. The
// subtrees in skip are not entered, which is how the second phase of a build
// walks a tree the first already covered without reading it twice.
func walkIndexList(ctx context.Context, list []string, skip map[string]bool, add func(indexEntry)) {
	var (
		wg   sync.WaitGroup
		slot = make(chan struct{}, searchIndexWalkers)
	)
	for _, root := range list {
		wg.Add(1)
		slot <- struct{}{}
		go func(root string) {
			defer wg.Done()
			defer func() { <-slot }()
			indexWalkRoot(ctx, root, skip, add)
		}(root)
	}
	wg.Wait()
}

// indexWalkRoot reads one root into add, skipping the platform's prune list
// (searchPruneDir) and the subtrees skip names, as the live walk's own roots
// do. Every entry goes in, hidden ones too: the query filters those by the
// app's 隐藏的项目 setting, so toggling it needs no rebuild.
func indexWalkRoot(ctx context.Context, root string, skip map[string]bool, add func(indexEntry)) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // the root itself is unreadable
			}
			return nil // an unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || searchIndex.progress.Load() >= int64(searchIndexCap) {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root && d.IsDir() {
			if searchPruneDir(path) || skip[path] {
				return filepath.SkipDir
			}
		}
		isDir := d.IsDir()
		if !isDir && d.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(path); err == nil && st.IsDir() {
				isDir = true
			}
		}
		e := indexEntry{path: path, isDir: isDir, hidden: strings.HasPrefix(name, ".")}
		if !isDir {
			e.ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		}
		if info, err := d.Info(); err == nil {
			e.size, e.mod = info.Size(), info.ModTime().Unix()
		}
		searchIndex.progress.Add(1)
		add(e)
		return nil
	})
}

// searchIndexNote folds a folder's listing into the index: the entries the pane
// just listed replace what the index held for that folder, so a file the user
// created, renamed or deleted is searchable from the palette the moment the
// pane shows it — Everything's journal, from the one source this app is sure of
// (its own listings). A no-op while the index is off.
func searchIndexNote(dir string, entries []Entry) {
	searchIndex.mu.Lock()
	defer searchIndex.mu.Unlock()
	if !searchIndex.enabled || dir == "" {
		return
	}
	if searchIndex.items == nil {
		searchIndex.items = map[string]indexEntry{}
	}
	if searchIndex.dirs == nil {
		searchIndex.dirs = map[string][]indexEntry{}
	}
	for _, old := range searchIndex.dirs[dir] {
		delete(searchIndex.items, old.path)
	}
	kept := make([]indexEntry, 0, len(entries))
	for _, e := range entries {
		if e.Path == "" || e.Name == "" || e.Path == dir {
			continue
		}
		ie := indexEntryFrom(e)
		searchIndex.items[ie.path] = ie
		kept = append(kept, ie)
	}
	searchIndex.dirs[dir] = kept
}

// searchIndexQuery answers a query from the snapshot: every entry under one of
// roots that the query matches, hidden entries skipped unless asked, capped at
// searchMaxResults and in the palette's order — by name, as Everything lists
// its hits. ok is false when the index cannot answer — it is off, empty, or
// holds nothing under roots — and the caller walks the disk instead, which is
// how a folder the index never reached still gets its results.
func searchIndexQuery(roots []string, query searchQuery, hidden bool) ([]Entry, bool) {
	searchIndex.mu.RLock()
	defer searchIndex.mu.RUnlock()
	if !searchIndex.enabled || len(searchIndex.items) == 0 || len(roots) == 0 {
		return nil, false
	}
	var (
		hits  []Entry
		under int
	)
	for _, e := range searchIndex.items {
		if !underAnyRoot(e.path, roots) {
			continue
		}
		under++
		if e.hidden && !hidden {
			continue
		}
		if !query.matches(filepath.Base(e.path), e.path, e.ext, e.isDir) {
			continue
		}
		hits = append(hits, e.entry())
		if len(hits) >= searchMaxResults {
			break
		}
	}
	if under == 0 {
		// The scope is not in the index at all: a folder no walk reached.
		return nil, false
	}
	sortHitsByName(hits)
	return hits, true
}

// searchIndexWhole reports whether the snapshot on hand is the whole machine: a
// build that saw everything, not one the cap or a quit cut short. A short
// snapshot still answers — it is what the last run read — but the palette walks
// on top of it.
func searchIndexWhole() bool {
	searchIndex.mu.RLock()
	defer searchIndex.mu.RUnlock()
	return searchIndex.complete
}

// underAnyRoot reports whether path is one of roots or lives inside one.
func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if underRoot(path, root) {
			return true
		}
	}
	return false
}

// underRoot reports whether path is root itself or inside it.
func underRoot(path, root string) bool {
	if path == root {
		return true
	}
	if root == "" {
		return false
	}
	if strings.HasSuffix(root, string(filepath.Separator)) {
		return strings.HasPrefix(path, root)
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// searchIndexStatus is what the footer says of the index, as one read.
type searchIndexStatus struct {
	on       bool
	total    int
	built    time.Time
	complete bool
	building bool
	progress int
}

// searchIndexState reads the status under the lock.
func searchIndexState() searchIndexStatus {
	searchIndex.mu.RLock()
	defer searchIndex.mu.RUnlock()
	return searchIndexStatus{
		on:       searchIndex.enabled,
		total:    len(searchIndex.items),
		built:    searchIndex.built,
		complete: searchIndex.complete,
		building: searchIndex.building,
		progress: int(searchIndex.progress.Load()),
	}
}

// searchIndexSummary is the footer's word on the index: how many entries it
// holds and how old the cache is, with a walk in flight beside it. Empty while
// the index is off.
func searchIndexSummary() string {
	st := searchIndexState()
	if !st.on {
		return ""
	}
	var parts []string
	if st.total > 0 {
		parts = append(parts, "索引 "+itoa(st.total)+" 条"+searchIndexAge(st.built)+searchIndexCapNote(st))
	}
	if st.building {
		parts = append(parts, "正在重建 "+itoa(st.progress)+" 条")
	}
	if len(parts) == 0 {
		return "索引正在建立"
	}
	return strings.Join(parts, " · ")
}

// searchIndexAge is how old the cache is, in the footer's words.
func searchIndexAge(built time.Time) string {
	if built.IsZero() {
		return ""
	}
	switch d := time.Since(built); {
	case d < time.Minute:
		return "（刚写入的缓存）"
	case d < time.Hour:
		return "（" + itoa(int(d.Minutes())) + " 分钟前的缓存）"
	case d < 24*time.Hour:
		return "（" + itoa(int(d.Hours())) + " 小时前的缓存）"
	default:
		return "（" + itoa(int(d.Hours()/24)) + " 天前的缓存）"
	}
}

// searchIndexCapNote is what the footer adds when the last walk did not see the
// whole machine.
func searchIndexCapNote(st searchIndexStatus) string {
	if st.complete {
		return ""
	}
	return "（未完整）"
}

// flushSearchIndex saves the walk in flight as it stands, for OnWillQuit: a
// build that a quit cuts short still leaves a warm cache, marked 未完整 so the
// next launch knows to walk again. A build that had not collected anything is
// left alone.
func flushSearchIndex() {
	searchIndex.mu.RLock()
	building := searchIndex.building
	searchIndex.mu.RUnlock()
	if !building {
		return
	}
	searchIndex.mu.Lock()
	searchIndex.pendingMu.Lock()
	pending := searchIndex.pending
	searchIndex.pendingMu.Unlock()
	if len(pending) > 0 {
		if searchIndex.items == nil {
			searchIndex.items = map[string]indexEntry{}
		}
		for path, e := range pending {
			searchIndex.items[path] = e
		}
		searchIndex.built = time.Now()
		searchIndex.complete = false
	}
	searchIndex.mu.Unlock()
	saveSearchIndex()
}

// indexDiskEntry is one entry of index.json, with short keys: the name, the
// folder and the extension come back out of the path when the file is read.
type indexDiskEntry struct {
	Path   string `json:"p"`
	Size   int64  `json:"s,omitempty"`
	Mod    int64  `json:"m,omitempty"`
	Dir    bool   `json:"d,omitempty"`
	Hidden bool   `json:"h,omitempty"`
}

// indexDiskFile is index.json: a version, what the walk that wrote it knew of
// itself, and the entries.
type indexDiskFile struct {
	Version  int              `json:"version"`
	Built    string           `json:"built"`
	Complete bool             `json:"complete"`
	Entries  []indexDiskEntry `json:"entries"`
}

// loadSearchIndex reads the cached index into memory. A missing, unreadable or
// foreign-version file is an empty index, as statestore.go treats state.json:
// the build that follows writes a fresh one.
func loadSearchIndex() {
	if searchIndexCacheFile == "" {
		return
	}
	data, err := os.ReadFile(searchIndexCacheFile)
	if err != nil {
		return
	}
	var f indexDiskFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != searchIndexVersion {
		return
	}
	items := make(map[string]indexEntry, len(f.Entries))
	for _, d := range f.Entries {
		if d.Path == "" {
			continue
		}
		e := indexEntry{
			path: d.Path, size: d.Size, mod: d.Mod, isDir: d.Dir, hidden: d.Hidden,
		}
		if !e.isDir {
			e.ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(filepath.Base(d.Path)), "."))
		}
		items[d.Path] = e
	}
	built, err := time.Parse(time.RFC3339, f.Built)
	if err != nil {
		built = time.Time{}
	}
	searchIndex.mu.Lock()
	searchIndex.items = items
	searchIndex.built = built
	searchIndex.complete = f.Complete
	searchIndex.mu.Unlock()
}

// saveSearchIndex writes the snapshot to the cache — to a temporary file and
// then the rename, so the file is never read half-written. Errors are ignored:
// a lost cache is a walk the next launch pays for.
func saveSearchIndex() {
	if searchIndexCacheFile == "" {
		return
	}
	searchIndex.mu.RLock()
	entries := make([]indexDiskEntry, 0, len(searchIndex.items))
	for _, e := range searchIndex.items {
		entries = append(entries, indexDiskEntry{
			Path: e.path, Size: e.size, Mod: e.mod, Dir: e.isDir, Hidden: e.hidden,
		})
	}
	f := indexDiskFile{
		Version:  searchIndexVersion,
		Built:    searchIndex.built.Format(time.RFC3339),
		Complete: searchIndex.complete,
	}
	searchIndex.mu.RUnlock()
	// By path, so the file is the same for the same index: a repack of the
	// same entries writes the same bytes.
	slices.SortFunc(entries, func(a, b indexDiskEntry) int { return strings.Compare(a.Path, b.Path) })
	f.Entries = entries
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(searchIndexCacheFile), 0o755); err != nil {
		return
	}
	tmp := searchIndexCacheFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, searchIndexCacheFile)
}
