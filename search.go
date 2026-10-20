package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"sunvale/internal/searchquery"
)

// search.go is Everything's experience on Explorer's engine: a query walks the
// tab's folder — or, in the 全局 scope, every mounted volume — in the
// background and streams the hits to the pane as they are found, so results
// appear while the walk is still running instead of after it. The query
// language (searchquery.go) is Everything's semantics this engine can
// honestly answer: case:, file:, folder:, path:, ext:, quoted phrases and
// space-separated AND terms.
const (
	searchMaxResults = 5000
	searchTimeout    = 8 * time.Second
	// searchGlobalTimeout is the 全局 scope's deadline: a whole machine is many
	// times the tree a folder walks. The walk streams what it finds meanwhile,
	// and a new query, an empty box or a closed tab cancels it.
	searchGlobalTimeout = 30 * time.Second
	// searchFlushEvery is how often a running walk reports its batch of new
	// hits to the pane: often enough to feel instant, seldom enough that the
	// repaints cost nothing.
	searchFlushEvery = 150 * time.Millisecond
	// searchWalkers is how many roots the 全局 scope walks at once: one volume
	// does not hold back another's hits, and the volumes are independent trees.
	searchWalkers = 8
)

// searchScope is what a query covers: the tab's folder and its subtree, or
// every mounted volume at once — Everything's 全部本地磁盘.
type searchScope int

const (
	scopeFolder searchScope = iota
	scopeGlobal
)

// searchState lives on the tab: the query and scope the last walk was for, its
// results, whether a walk is in flight, and the times the status bar reports.
type searchState struct {
	query   string
	scope   searchScope
	results []Entry
	busy    bool
	started time.Time
	// done is when the walk finished; zero while it runs. The status bar shows
	// the elapsed time between started and done, Everything's
	// "找到 N 个项目 (X.XXs)".
	done time.Time
	// queue is where the walk running off the frame thread hands its batches
	// over (frameQueue).
	queue frameQueue
}

// frameQueue is how a background search reaches the state it fills in: the
// search's own goroutine writes nothing but the queue, and the frame applies
// what it finds there. Running, the batch lands on the main thread, where the
// frame runs, so the posts are serialized already; headless — the tests'
// ui.Tester, which has no main thread — this queue is what keeps two goroutines
// off one struct.
type frameQueue struct {
	mu  sync.Mutex
	ops []func()
}

// post queues f for the next frame.
func (q *frameQueue) post(f func()) {
	q.mu.Lock()
	q.ops = append(q.ops, f)
	q.mu.Unlock()
}

// take hands the queued ops to the caller, in the order they arrived.
func (q *frameQueue) take() []func() {
	q.mu.Lock()
	defer q.mu.Unlock()
	ops := q.ops
	q.ops = nil
	return ops
}

// updateSearch is called once per frame from view: it starts a walk when the
// query or the scope has changed. Results stream in through the batches below;
// a walk for an older query drops its batches, and clearing the box cancels
// the running walk outright.
func (a *App) updateSearch() {
	t := a.cur()
	// What the walk running off this thread has handed over — the hits it found,
	// and the news that it is through — applies here, where the tab's search
	// state is this frame's alone.
	for _, op := range t.search.queue.take() {
		op()
	}
	q := strings.TrimSpace(t.query)
	if specialPath(t.path) {
		return
	}
	if q == t.search.query && t.queryScope == t.search.scope {
		return
	}
	t.search.query = q
	t.search.scope = t.queryScope
	t.search.results = nil
	t.search.done = time.Time{}
	if q == "" {
		t.search.busy = false
		return
	}
	roots := a.searchRoots(t.queryScope)
	if len(roots) == 0 {
		t.search.busy = false
		return
	}
	t.search.busy = true
	t.search.started = time.Now()
	tab := t
	hidden := a.showHidden
	query := searchquery.Parse(q)
	deadline := time.Now().Add(searchDeadline(t.queryScope))
	go func() {
		streamMatches(roots, query, hidden, deadline, func(batch []Entry, more bool) {
			apply := func() {
				if !a.ownsTab(tab) || strings.TrimSpace(tab.query) != q || tab.queryScope != t.search.scope {
					return // a newer query, another scope, or the tab closed
				}
				if !more {
					tab.search.busy = false
					tab.search.done = time.Now()
				}
				if len(batch) > 0 {
					tab.search.results = append(tab.search.results, batch...)
					if len(tab.search.results) > searchMaxResults {
						tab.search.results = tab.search.results[:searchMaxResults]
					}
				}
				repaint()
			}
			// The running app lands on the main thread, where the frame runs;
			// headless the batch waits for the next frame, which is where the
			// tab's fields are safe to touch.
			if mygo.App.IsReady() {
				mygo.RunOnMain(apply)
				return
			}
			tab.search.queue.post(apply)
		})
	}()
}

// searchDeadline is how long a scope's walk may run.
func searchDeadline(scope searchScope) time.Duration {
	if scope == scopeGlobal {
		return searchGlobalTimeout
	}
	return searchTimeout
}

// searchRoots is where a scope's walk starts: the tab's folder, or every
// mounted volume for the 全局 scope, the way Everything searches all local
// disks at once. A volume that is not a directory, or a repeat of one already
// listed, is dropped.
func (a *App) searchRoots(scope searchScope) []string {
	if scope != scopeGlobal {
		path := a.cur().path
		if path == "" || specialPath(path) {
			return nil
		}
		return []string{path}
	}
	return globalSearchRoots()
}

// globalSearchRoots are the machine's volumes as walk roots — the 全局 scope's
// roots, and the roots the index's own build walks (searchindex.go).
func globalSearchRoots() []string {
	var roots []string
	seen := map[string]bool{}
	for _, v := range sysVolumes() {
		if v.Path == "" || seen[v.Path] {
			continue
		}
		if st, err := os.Stat(v.Path); err != nil || !st.IsDir() {
			continue
		}
		seen[v.Path] = true
		roots = append(roots, v.Path)
	}
	return roots
}

// ownsTab reports whether tab still belongs to the app's tab strip.
func (a *App) ownsTab(tab *tabState) bool {
	for _, t := range a.tabs {
		if t == tab {
			return true
		}
	}
	return false
}

// activeSearchQuery is the parsed query of the current tab's search, for the
// pane's hit marking. The parse is cached per tab: every row of a 5000-hit pane
// asks for it every frame, and the query string changes only between
// keystrokes.
func (a *App) activeSearchQuery() searchquery.Query {
	t := a.cur()
	q := strings.TrimSpace(t.query)
	if q == "" {
		t.hlCache.query, t.hlCache.parsed = "", searchquery.Query{}
		return searchquery.Query{}
	}
	if t.hlCache.query != q {
		t.hlCache.query, t.hlCache.parsed = q, searchquery.Parse(q)
	}
	return t.hlCache.parsed
}

// streamMatches walks every root and hands the entries that match query to
// emit in batches: at least every searchFlushEvery while the walk runs, and
// always once at the end with more false, so the caller learns the walk is
// over even when the last batch is empty. The roots walk at once, up to
// searchWalkers at a time, so the hits of a fast volume are not held back by a
// slow one — the whole-machine feel of Everything, on a walk instead of an
// index. The rules are the ones a subtree walk had: searchMaxResults hits, the
// deadline, hidden entries skipped unless asked, unreadable folders skipped
// rather than fatal.
func streamMatches(roots []string, query searchquery.Query, hidden bool, deadline time.Time, emit func(batch []Entry, more bool)) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	found := make(chan Entry, 256)
	var (
		wg      sync.WaitGroup
		walkers = make(chan struct{}, searchWalkers)
		total   atomic.Int64
	)
	for _, root := range roots {
		wg.Add(1)
		walkers <- struct{}{} // at most searchWalkers roots walk at once
		go func(root string) {
			defer wg.Done()
			defer func() { <-walkers }()
			walkRoot(ctx, root, query, hidden, found, &total)
		}(root)
	}
	go func() {
		wg.Wait()
		close(found)
	}()

	// One goroutine emits, in order: the walkers only hand entries over.
	var batch []Entry
	ticker := time.NewTicker(searchFlushEvery)
	defer ticker.Stop()
	for {
		select {
		case e, ok := <-found:
			if !ok {
				emit(batch, false)
				return
			}
			batch = append(batch, e)
		case <-ticker.C:
			if len(batch) > 0 {
				emit(batch, true)
				batch = batch[:0]
			}
		}
	}
}

// walkRoot walks one root, sending every matching entry to found. The cap is
// shared with the other roots and checked against the shared total, so a walk
// that reached it stops everywhere; the deadline comes in through ctx.
func walkRoot(ctx context.Context, root string, query searchquery.Query, hidden bool, found chan<- Entry, total *atomic.Int64) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // the root itself is unreadable: this walk is over
			}
			return nil // unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || total.Load() >= searchMaxResults {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root {
			if d.IsDir() && searchPruneDir(path) {
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") && !hidden {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		isDir := d.IsDir()
		ext := ""
		if !isDir {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		}
		if !query.Matches(name, path, ext, isDir) {
			return nil
		}
		e := searchEntry(path, d)
		if e.Path == "" {
			return nil
		}
		if total.Add(1) > searchMaxResults {
			return filepath.SkipAll
		}
		select {
		case found <- e:
		case <-ctx.Done():
			return filepath.SkipAll
		}
		return nil
	})
}

// searchPruneDir reports whether a walk skips this directory: the platform's
// virtual filesystems and the mount points the volume roots cover themselves
// (sysSearchPrune). Walking /proc on Linux, or the volumes under /Volumes on
// macOS a second time, is either endless or plain duplication.
func searchPruneDir(path string) bool {
	for _, p := range sysSearchPrune() {
		if path == p {
			return true
		}
	}
	return false
}

// walkMatches is streamMatches collected at once, for callers that want the
// whole answer in one piece: the remote search endpoint and the tests.
func walkMatches(root, rawQuery string, hidden bool, deadline time.Time) []Entry {
	query := searchquery.Parse(rawQuery)
	var out []Entry
	streamMatches([]string{root}, query, hidden, deadline, func(batch []Entry, more bool) {
		out = append(out, batch...)
	})
	return out
}

// searchEntry builds an Entry for a hit, resolving symlinked dirs.
func searchEntry(path string, d os.DirEntry) Entry {
	isDir := d.IsDir()
	if !isDir && d.Type()&os.ModeSymlink != 0 {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			isDir = true
		}
	}
	e := Entry{
		Name:  d.Name(),
		Path:  path,
		IsDir: isDir,
		Dir:   filepath.Dir(path),
	}
	if info, err := d.Info(); err == nil {
		e.Size = info.Size()
		e.ModTime = info.ModTime()
	}
	if !e.IsDir {
		e.Ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Name), "."))
	}
	return e
}

// searchStatusLabel is the status bar's search line, Everything's shape: the
// count, the elapsed seconds while the walk runs or once it finished, and the
// truncated query. An empty string when there is nothing to say.
func (a *App) searchStatusLabel() string {
	t := a.cur()
	q := strings.TrimSpace(t.query)
	if q == "" || specialPath(t.path) {
		return ""
	}
	n := len(t.search.results)
	if n >= searchMaxResults {
		n = searchMaxResults
	}
	elapsed := t.search.started
	if !t.search.done.IsZero() {
		elapsed = t.search.done
	}
	secs := ""
	if !elapsed.IsZero() {
		secs = " (" + elapsed.Sub(t.search.started).Truncate(time.Millisecond).String() + ")"
	}
	where := "本文件夹"
	if t.search.scope == scopeGlobal {
		where = "全部磁盘"
	}
	trunc := truncateRunes(q, 24)
	if t.search.busy {
		return "正在搜索 " + where + " “" + trunc + "”  已找到 " + itoa(n) + " 个项目" + secs
	}
	return where + " “" + trunc + "”  找到 " + itoa(n) + " 个项目" + searchCapNote(n) + secs
}

// searchCapNote is what a count says once the walk stopped at searchMaxResults:
// the list is not everything there is, and saying so is the honest thing.
func searchCapNote(n int) string {
	if n >= searchMaxResults {
		return "（已达 " + itoa(searchMaxResults) + " 条上限）"
	}
	return ""
}

// itoa is strconv.Itoa without the import's fanfare for two call sites.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
