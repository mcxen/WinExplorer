package search

import (
	"context"
	"log/slog"
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
	// Throttle is the pause after every 256 folders of a refresh that has an
	// old snapshot to fall back on; a cold build runs flat out.
	Throttle   time.Duration
	PreferHome bool
	// Watch keeps fsnotify watches on recently listed folders so their rows
	// follow creates and deletes between refreshes.
	Watch bool
}

// Status is one atomic view of the index for progress and fallback decisions.
type Status struct {
	On        bool
	Total     int
	Built     time.Time
	Complete  bool
	Building  bool
	Progress  int
	Estimate  int
	Bytes     int64         // index file size on disk
	Took      time.Duration // last build's wall time
	Rescanned int           // folders the last build read from disk
}

// Index is the persistent filename index (docs/specs/23-search-index.md): an
// immutable on-disk snapshot read in place, plus a small overlay of folders
// the application listed since. A rebuild writes a new file and swaps it in,
// so a long walk never blocks a lookup.
type Index struct {
	mu sync.RWMutex

	cacheFile  string
	maxEntries int
	timeout    time.Duration
	refresh    time.Duration
	throttle   time.Duration
	preferHome bool

	snap     *snapshot
	bytes    int64
	enabled  bool
	building bool
	estimate int
	took     time.Duration
	rescans  int
	cancel   context.CancelFunc
	done     chan struct{}
	// dirty holds folders the running refresh found changed: the snapshot's
	// rows for them are stale until the swap.
	dirty []string

	progress atomic.Int64

	// dirs holds the rows each noted listing added, newer than the snapshot.
	// It is an LRU bounded by maxNotedDirs folders and maxNotedRows rows.
	dirs      map[string][]indexEntry
	dirOrder  []string
	notedRows int

	watch *watcher
}

// NewIndex returns an index ready to be enabled and started.
func NewIndex(config IndexConfig) *Index {
	ix := &Index{
		cacheFile:  config.CacheFile,
		maxEntries: config.MaxEntries,
		timeout:    config.Timeout,
		refresh:    config.Refresh,
		throttle:   config.Throttle,
		preferHome: config.PreferHome,
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
	if config.Watch {
		ix.watch = newWatcher(ix)
	}
	return ix
}

const (
	maxNotedDirs = 256
	maxDirty     = 64
)

func (ix *Index) maxNotedRows() int { return max(ix.maxEntries/4, 1) }

// indexEntry is one noted row with the stat its listing already paid for.
type indexEntry struct {
	path   string
	size   int64
	mod    int64
	isDir  bool
	hidden bool
}

func (e indexEntry) entry() fsentry.Entry {
	return makeEntry(e.path, filepath.Base(e.path), e.isDir, e.size, e.mod)
}

func makeEntry(path, name string, isDir bool, size, mod int64) fsentry.Entry {
	out := fsentry.Entry{Name: name, Path: path, Dir: filepath.Dir(path), IsDir: isDir, Size: size}
	if mod > 0 {
		out.ModTime = time.Unix(mod, 0)
	}
	if !isDir {
		out.Ext = extOf(name)
	}
	return out
}

func extOf(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

func indexEntryFrom(e fsentry.Entry) indexEntry {
	out := indexEntry{path: e.Path, size: e.Size, isDir: e.IsDir, hidden: strings.HasPrefix(e.Name, ".")}
	if !e.ModTime.IsZero() {
		out.mod = e.ModTime.Unix()
	}
	return out
}

// Start enables the index, maps the last snapshot and refreshes a stale one.
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

// Disable stops a build, unmaps the snapshot and drops the overlay. The file
// stays for the next Start.
func (ix *Index) Disable() {
	ix.stopBuild(5 * time.Second)
	ix.mu.Lock()
	ix.enabled = false
	ix.snap.close()
	ix.snap = nil
	ix.dirs, ix.dirOrder, ix.notedRows = nil, nil, 0
	ix.mu.Unlock()
	if ix.watch != nil {
		ix.watch.reset()
	}
}

// Enabled reports whether the index is in use.
func (ix *Index) Enabled() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.enabled
}

// Stale reports whether the snapshot is missing, partial, or older than the
// refresh interval.
func (ix *Index) Stale() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	s := ix.snap
	return s == nil || !s.complete || time.Since(time.Unix(s.built, 0)) > ix.refresh
}

func (ix *Index) load() {
	if ix.cacheFile == "" {
		return
	}
	s, err := openSnapshot(ix.cacheFile)
	if err != nil {
		return
	}
	ix.mu.Lock()
	ix.snap.close()
	ix.snap = s
	ix.bytes = int64(len(s.data))
	ix.estimate = s.n - s.roots
	ix.mu.Unlock()
}

// Rebuild refreshes the snapshot in the background: unchanged folders reuse
// their old rows. Only one build runs at a time.
func (ix *Index) Rebuild(roots []string) {
	ix.RebuildWith(func() []string { return roots })
}

// RebuildWith is Rebuild with the roots gathered on the build goroutine, for
// callers on the UI thread: the build is marked running at once, and listing
// the volumes (which stats disks) happens off the frame.
func (ix *Index) RebuildWith(roots func() []string) {
	ix.mu.Lock()
	if ix.building || !ix.enabled {
		ix.mu.Unlock()
		return
	}
	ix.building = true
	ix.dirty = nil
	old := ix.snap
	if old != nil {
		ix.estimate = old.n - old.roots
	}
	ix.progress.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), ix.timeout)
	ix.cancel = cancel
	done := make(chan struct{})
	ix.done = done
	ix.mu.Unlock()

	go func() {
		defer close(done)
		defer cancel()
		ix.runBuild(ctx, roots(), old)
	}()
}

func (ix *Index) runBuild(ctx context.Context, roots []string, old *snapshot) {
	start := time.Now()
	first, rest := ix.roots(roots)
	b := &builder{ctx: ctx, old: old, max: ix.maxEntries, progress: &ix.progress}
	if old != nil {
		b.throttle = ix.throttle
		b.onDirty = func(dir string) {
			ix.mu.Lock()
			if len(ix.dirty) < maxDirty {
				ix.dirty = append(ix.dirty, dir)
			}
			ix.mu.Unlock()
		}
	}
	tree := b.build(append(first, rest...))
	cancelled := ctx.Err() != nil
	finish := func() {
		ix.mu.Lock()
		ix.building, ix.cancel, ix.dirty = false, nil, nil
		ix.progress.Store(0)
		ix.mu.Unlock()
	}
	// A refresh stopped part way keeps the older, complete snapshot; a first
	// build keeps what it has, marked partial.
	if (cancelled && old != nil) || len(tree) == 0 || ix.cacheFile == "" {
		finish()
		return
	}
	data := encode(tree, time.Now(), !b.stats.cut)
	tree = nil
	err := writeIndex(ix.cacheFile, data)
	data = nil
	var s *snapshot
	if err == nil {
		s, err = openSnapshot(ix.cacheFile)
	}
	if err != nil {
		slog.Warn("search index: write failed", "err", err)
		finish()
		return
	}
	took := time.Since(start)
	slog.Info("search index built", "entries", s.n, "folders", b.stats.dirs,
		"reused", b.stats.reused, "rescanned", b.stats.rescanned,
		"bytes", len(s.data), "complete", s.complete, "took", took.Round(time.Millisecond))
	ix.mu.Lock()
	if ix.enabled {
		ix.snap.close()
		ix.snap = s
		ix.bytes = int64(len(s.data))
		ix.estimate = s.n - s.roots
		ix.took = took
		ix.rescans = b.stats.rescanned
	} else {
		s.close()
	}
	ix.mu.Unlock()
	finish()
}

// stopBuild cancels a build in flight and waits up to wait for it.
func (ix *Index) stopBuild(wait time.Duration) {
	ix.mu.Lock()
	cancel, done := ix.cancel, ix.done
	ix.mu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// roots orders the user's home first, then the caller's volume roots. The cap
// therefore cuts system trees before it cuts the folders a person uses.
func (ix *Index) roots(roots []string) (first []string, rest []string) {
	if ix.preferHome {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			if st, err := os.Stat(home); err == nil && st.IsDir() {
				first = append(first, filepath.Clean(home))
			}
		}
	}
	for _, root := range roots {
		if !slices.Contains(first, root) {
			rest = append(rest, root)
		}
	}
	return first, rest
}

// Note folds one application listing into the overlay. Listings are fresher
// than the snapshot, so they replace the folder's snapshot rows in queries.
func (ix *Index) Note(dir string, entries []fsentry.Entry) {
	ix.mu.Lock()
	if !ix.enabled || dir == "" {
		ix.mu.Unlock()
		return
	}
	if ix.dirs == nil {
		ix.dirs = map[string][]indexEntry{}
	}
	if _, ok := ix.dirs[dir]; ok {
		ix.forgetDir(dir)
	}
	limit := ix.maxNotedRows()
	kept := make([]indexEntry, 0, min(len(entries), limit))
	for _, e := range entries {
		if e.Path == "" || e.Name == "" || e.Path == dir {
			continue
		}
		if len(kept) >= limit {
			break // one huge folder may not crowd out every other listing
		}
		kept = append(kept, indexEntryFrom(e))
	}
	ix.dirs[dir] = kept
	ix.dirOrder = append(ix.dirOrder, dir)
	ix.notedRows += len(kept)
	for len(ix.dirOrder) > 1 && (len(ix.dirOrder) > maxNotedDirs || ix.notedRows > limit) {
		ix.forgetDir(ix.dirOrder[0])
	}
	ix.mu.Unlock()
	if ix.watch != nil {
		ix.watch.add(dir)
	}
}

// forgetDir drops one noted listing from the LRU. The caller holds ix.mu.
func (ix *Index) forgetDir(dir string) {
	ix.notedRows -= len(ix.dirs[dir])
	delete(ix.dirs, dir)
	if i := slices.Index(ix.dirOrder, dir); i >= 0 {
		ix.dirOrder = slices.Delete(ix.dirOrder, i, i+1)
	}
}

// covers reports whether the snapshot or overlay knows root. The caller holds
// ix.mu.
func (ix *Index) covers(root string) bool {
	if s := ix.snap; s != nil {
		if id, ok := s.lookup(root); ok && s.isDir(id) {
			return true
		}
		for r := 0; r < s.roots; r++ {
			if underRoot(string(s.nameBytes(uint32(r))), root) {
				return true
			}
		}
	}
	for dir := range ix.dirs {
		if underRoot(dir, root) {
			return true
		}
	}
	return false
}

// Query answers from the snapshot and overlay when they cover a requested
// root. The boolean is false when the caller should fall back to a live walk.
func (ix *Index) Query(roots []string, query searchquery.Query, hidden bool) ([]fsentry.Entry, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if !ix.enabled || len(roots) == 0 {
		return nil, false
	}
	covered := false
	for _, r := range roots {
		if ix.covers(r) {
			covered = true
			break
		}
	}
	if !covered {
		return nil, false
	}
	var hits []fsentry.Entry
	if s := ix.snap; s != nil {
		hits = ix.querySnap(s, roots, query, hidden)
	}
	for dir, rows := range ix.dirs {
		if len(hits) >= DefaultMaxResults {
			break
		}
		if !underAnyRoot(dir, roots) && !slices.ContainsFunc(roots, func(r string) bool { return underRoot(r, dir) }) {
			continue
		}
		for _, e := range rows {
			if (e.hidden && !hidden) || !underAnyRoot(e.path, roots) {
				continue
			}
			if matchRow(query, filepath.Base(e.path), e.path, e.isDir, e.size, e.mod) {
				hits = append(hits, e.entry())
			}
		}
	}
	if len(hits) > DefaultMaxResults {
		hits = hits[:DefaultMaxResults]
	}
	sortHitsByQuery(hits, query)
	return hits, true
}

func matchRow(query searchquery.Query, name, path string, isDir bool, size, mod int64) bool {
	info := searchquery.MatchInfo{IsDir: isDir, Size: size}
	if !isDir {
		info.Ext = extOf(name)
	}
	if mod > 0 {
		info.ModTime = time.Unix(mod, 0)
	}
	return query.Matches(name, path, info)
}

// candidates intersects the trigram postings of every needle of three bytes
// or more. ok is false when no needle is long enough: the caller scans.
func candidates(s *snapshot, needles []string) (ids []uint32, ok bool) {
	var lists [][]uint32
	var tris []uint32
	for _, nd := range needles {
		tris = trigramsOf(nd, tris)
		for _, t := range tris {
			p := s.postings(t)
			if len(p) == 0 {
				return nil, true
			}
			lists = append(lists, p)
		}
	}
	if len(lists) == 0 {
		return nil, false
	}
	slices.SortFunc(lists, func(a, b []uint32) int { return len(a) - len(b) })
	ids = lists[0]
	for _, l := range lists[1:] {
		ids = intersect(ids, l)
		if len(ids) == 0 {
			break
		}
	}
	return ids, true
}

func intersect(a, b []uint32) []uint32 {
	out := a[:0]
	j := 0
	for _, v := range a {
		for j < len(b) && b[j] < v {
			j++
		}
		if j < len(b) && b[j] == v {
			out = append(out, v)
		}
	}
	return out
}

func (ix *Index) querySnap(s *snapshot, roots []string, query searchquery.Query, hidden bool) []fsentry.Entry {
	needles := query.NameNeedles()
	ids, ok := candidates(s, needles)
	paths := map[uint32]string{}
	var pathOf func(id uint32) string
	pathOf = func(id uint32) string {
		if p, ok := paths[id]; ok {
			return p
		}
		var p string
		if par := s.parent(id); par == noParent {
			p = string(s.nameBytes(id))
		} else {
			p = joinPath(pathOf(par), string(s.nameBytes(id)))
		}
		paths[id] = p
		return p
	}
	var hits []fsentry.Entry
	check := func(id uint32) bool {
		f := s.flags(id)
		if f&flagHidden != 0 && !hidden {
			return true
		}
		raw := s.nameBytes(id)
		for _, nd := range needles {
			if !containsFold(raw, nd) {
				return true
			}
		}
		dir := pathOf(s.parent(id))
		if _, noted := ix.dirs[dir]; noted {
			return true // the overlay's fresher listing answers for it
		}
		name := string(raw)
		path := joinPath(dir, name)
		if !underAnyRoot(path, roots) {
			return true
		}
		isDir := f&flagDir != 0
		size, mod := s.size(id), s.mod(id)
		if !matchRow(query, name, path, isDir, size, mod) {
			return true
		}
		hits = append(hits, makeEntry(path, name, isDir, size, mod))
		return len(hits) < DefaultMaxResults
	}
	if ok {
		for _, id := range ids {
			if !check(id) {
				break
			}
		}
		return hits
	}
	for id := uint32(s.roots); id < uint32(s.n); id++ {
		if !check(id) {
			break
		}
	}
	return hits
}

// containsFold is a cheap prefilter: an ASCII case-folded substring test that
// defers to strings.ToLower when either side is not ASCII.
func containsFold(name []byte, lowNeedle string) bool {
	for _, c := range name {
		if c >= 0x80 {
			return strings.Contains(strings.ToLower(string(name)), lowNeedle)
		}
	}
	n := len(lowNeedle)
	for i := 0; i+n <= len(name); i++ {
		j := 0
		for ; j < n; j++ {
			c := name[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lowNeedle[j] {
				break
			}
		}
		if j == n {
			return true
		}
	}
	return false
}

// Whole reports whether the snapshot saw the entire requested tree and no
// refresh has yet found a folder changed under it.
func (ix *Index) Whole() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.snap != nil && ix.snap.complete && len(ix.dirty) == 0
}

// Complete reports whether the snapshot finished its walk.
func (ix *Index) Complete() bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.snap != nil && ix.snap.complete
}

// Dirty lists the folders under roots a running refresh found changed; the
// caller walks them live to top up the snapshot's answer.
func (ix *Index) Dirty(roots []string) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []string
	for _, d := range ix.dirty {
		if underAnyRoot(d, roots) {
			out = append(out, d)
		}
	}
	return out
}

// Flush stops a build in flight before quit. A first build still lands on
// disk, marked partial, so the next launch starts from it.
func (ix *Index) Flush() {
	ix.stopBuild(3 * time.Second)
	if ix.watch != nil {
		ix.watch.reset()
	}
}

// Status reads all fields a progress display needs in one lock.
func (ix *Index) Status() Status {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	st := Status{
		On: ix.enabled, Building: ix.building, Progress: int(ix.progress.Load()),
		Estimate: ix.estimate, Bytes: ix.bytes, Took: ix.took, Rescanned: ix.rescans,
		Total: ix.notedRows,
	}
	if s := ix.snap; s != nil {
		st.Total += s.n - s.roots
		st.Built = time.Unix(s.built, 0)
		st.Complete = s.complete
	}
	return st
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
