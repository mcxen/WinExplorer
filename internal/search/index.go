package search

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sunvale/internal/fsentry"
	"sunvale/internal/searchquery"
)

// IndexConfig bounds one persistent index. The application supplies its cache
// path and roots; the package owns all storage and build state.
type IndexConfig struct {
	CacheFile  string
	MaxEntries int
	Timeout    time.Duration
	Refresh    time.Duration
	Walkers    int
}

// Status is one atomic view of the index for progress and fallback decisions.
type Status struct {
	On       bool
	Total    int
	Built    time.Time
	Complete bool
	Building bool
	Progress int
	Estimate int
}

// Index is a process-local filename index. Queries read the completed snapshot
// while a rebuild fills a pending map, so a long walk never blocks a lookup.
type Index struct {
	mu sync.RWMutex

	cacheFile  string
	maxEntries int
	timeout    time.Duration
	refresh    time.Duration
	walkers    int

	items    map[string]indexEntry
	built    time.Time
	complete bool
	enabled  bool
	building bool
	estimate int

	progress atomic.Int64

	dirs map[string][]indexEntry

	pendingMu sync.Mutex
	pending   map[string]indexEntry
}

// NewIndex returns an index ready to be enabled and started.
func NewIndex(config IndexConfig) *Index {
	ix := &Index{
		cacheFile:  config.CacheFile,
		maxEntries: config.MaxEntries,
		timeout:    config.Timeout,
		refresh:    config.Refresh,
		walkers:    config.Walkers,
	}
	if ix.maxEntries <= 0 {
		ix.maxEntries = 200000
	}
	if ix.timeout <= 0 {
		ix.timeout = 10 * time.Minute
	}
	if ix.refresh <= 0 {
		ix.refresh = 6 * time.Hour
	}
	if ix.walkers <= 0 {
		ix.walkers = 2
	}
	return ix
}

// indexEntry is one indexed path with the stat the build already paid for.
type indexEntry struct {
	path   string
	ext    string
	size   int64
	mod    int64
	isDir  bool
	hidden bool
}

func (e indexEntry) entry() fsentry.Entry {
	out := fsentry.Entry{
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

func indexEntryFrom(e fsentry.Entry) indexEntry {
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

// Start enables the index, warms it from cache and rebuilds stale snapshots.
func (ix *Index) Start(roots []string) {
	ix.Enable()
	ix.load()
	if ix.Stale() {
		ix.Rebuild(roots)
	}
}

// Enable turns the index on without loading or walking.
func (ix *Index) Enable() {
	ix.mu.Lock()
	ix.enabled = true
	ix.mu.Unlock()
}

// Enabled reports whether the index is in use.
func (ix *Index) Enabled() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.enabled
}

// Stale reports whether the snapshot is empty, partial, or old enough to
// replace with a fresh walk.
func (ix *Index) Stale() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.items) == 0 || !ix.complete || time.Since(ix.built) > ix.refresh
}

// Rebuild walks roots in the background and swaps the result into the snapshot.
// Only one build runs at a time.
func (ix *Index) Rebuild(roots []string) {
	ix.mu.Lock()
	if ix.building || !ix.enabled {
		ix.mu.Unlock()
		return
	}
	ix.building = true
	if ix.estimate <= 0 {
		ix.estimate = len(ix.items)
	}
	ix.progress.Store(0)
	ix.pendingMu.Lock()
	ix.pending = make(map[string]indexEntry, 4096)
	ix.pendingMu.Unlock()
	ix.mu.Unlock()

	go func() {
		complete := ix.walk(roots)
		ix.mu.Lock()
		ix.pendingMu.Lock()
		items := ix.pending
		ix.pending = nil
		ix.pendingMu.Unlock()
		if items == nil {
			items = map[string]indexEntry{}
		}
		for _, entries := range ix.dirs {
			for _, e := range entries {
				items[e.path] = e
			}
		}
		ix.items = items
		ix.built = time.Now()
		ix.complete = complete
		ix.estimate = len(items)
		ix.mu.Unlock()
		ix.save()
		ix.mu.Lock()
		ix.building = false
		ix.progress.Store(0)
		ix.mu.Unlock()
	}()
}

// roots orders the user's home first, then the caller's volume roots. The cap
// therefore cuts system trees before it cuts the folders a person uses.
func (ix *Index) roots(roots []string) (first []string, rest []string) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if st, err := os.Stat(home); err == nil && st.IsDir() {
			first = append(first, filepath.Clean(home))
		}
	}
	for _, root := range roots {
		if !slices.Contains(first, root) {
			rest = append(rest, root)
		}
	}
	return first, rest
}

// walk fills the pending map and reports whether it saw every requested root.
func (ix *Index) walk(roots []string) bool {
	first, rest := ix.roots(roots)
	if len(first) == 0 && len(rest) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ix.timeout)
	defer cancel()
	add := func(e indexEntry) {
		ix.pendingMu.Lock()
		if ix.pending != nil {
			ix.pending[e.path] = e
		}
		ix.pendingMu.Unlock()
	}
	skip := map[string]bool{}
	for _, root := range first {
		skip[root] = true
	}
	ix.walkList(ctx, first, nil, add)
	ix.walkList(ctx, rest, skip, add)
	ix.pendingMu.Lock()
	collected := len(ix.pending)
	ix.pendingMu.Unlock()
	return ctx.Err() == nil && collected < ix.maxEntries
}

func (ix *Index) walkList(ctx context.Context, roots []string, skip map[string]bool, add func(indexEntry)) {
	var (
		wg    sync.WaitGroup
		slots = make(chan struct{}, ix.walkers)
	)
	for _, root := range roots {
		wg.Add(1)
		slots <- struct{}{}
		go func(root string) {
			defer wg.Done()
			defer func() { <-slots }()
			ix.walkRoot(ctx, root, skip, add)
		}(root)
	}
	wg.Wait()
}

func (ix *Index) walkRoot(ctx context.Context, root string, skip map[string]bool, add func(indexEntry)) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if ctx.Err() != nil || ix.progress.Load() >= int64(ix.maxEntries) {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root && d.IsDir() {
			if pruneDir(path) || skip[path] {
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
		ix.progress.Add(1)
		add(e)
		return nil
	})
}

// Note folds one application listing into the index. Listings are fresher than
// a background walk, so they replace the folder's old rows.
func (ix *Index) Note(dir string, entries []fsentry.Entry) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if !ix.enabled || dir == "" {
		return
	}
	if ix.items == nil {
		ix.items = map[string]indexEntry{}
	}
	if ix.dirs == nil {
		ix.dirs = map[string][]indexEntry{}
	}
	for _, old := range ix.dirs[dir] {
		delete(ix.items, old.path)
	}
	kept := make([]indexEntry, 0, len(entries))
	for _, e := range entries {
		if e.Path == "" || e.Name == "" || e.Path == dir {
			continue
		}
		ie := indexEntryFrom(e)
		ix.items[ie.path] = ie
		kept = append(kept, ie)
	}
	ix.dirs[dir] = kept
}

// Query answers from the snapshot when it covers every requested root. The
// boolean is false when the caller should fall back to a live walk.
func (ix *Index) Query(roots []string, query searchquery.Query, hidden bool) ([]fsentry.Entry, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if !ix.enabled || len(ix.items) == 0 || len(roots) == 0 {
		return nil, false
	}
	var (
		hits  []fsentry.Entry
		under int
	)
	for _, e := range ix.items {
		if !underAnyRoot(e.path, roots) {
			continue
		}
		under++
		if e.hidden && !hidden {
			continue
		}
		info := searchquery.MatchInfo{IsDir: e.isDir, Ext: e.ext, Size: e.size}
		if e.mod > 0 {
			info.ModTime = time.Unix(e.mod, 0)
		}
		if !query.Matches(filepath.Base(e.path), e.path, info) {
			continue
		}
		hits = append(hits, e.entry())
		if len(hits) >= DefaultMaxResults {
			break
		}
	}
	if under == 0 {
		return nil, false
	}
	sortHitsByQuery(hits, query)
	return hits, true
}

// Whole reports whether the current snapshot saw the entire requested tree.
func (ix *Index) Whole() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.complete
}

// Flush saves a build in flight as an incomplete snapshot for the next launch.
func (ix *Index) Flush() {
	ix.mu.RLock()
	building := ix.building
	ix.mu.RUnlock()
	if !building {
		return
	}
	ix.mu.Lock()
	ix.pendingMu.Lock()
	pending := ix.pending
	ix.pendingMu.Unlock()
	if len(pending) > 0 {
		if ix.items == nil {
			ix.items = map[string]indexEntry{}
		}
		for path, e := range pending {
			ix.items[path] = e
		}
		ix.built = time.Now()
		ix.complete = false
	}
	ix.mu.Unlock()
	ix.save()
}

// Status reads all fields a progress display needs in one lock.
func (ix *Index) Status() Status {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return Status{
		On:       ix.enabled,
		Total:    len(ix.items),
		Built:    ix.built,
		Complete: ix.complete,
		Building: ix.building,
		Progress: int(ix.progress.Load()),
		Estimate: ix.estimate,
	}
}

func underAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if underRoot(path, root) {
			return true
		}
	}
	return false
}

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
