package main

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"sunvale/internal/search"
	"sunvale/internal/searchquery"
)

// search.go connects the reusable search engine to a tab's frame-owned state.
// A query walks the tab's folder — or, in the 全局 scope, every mounted volume —
// in the background and streams hits to the pane as they are found.
const (
	searchMaxResults = search.DefaultMaxResults
	searchTimeout    = 8 * time.Second
	// searchGlobalTimeout is the 全局 scope's deadline: a whole machine is many
	// times the tree a folder walks. The walk streams what it finds meanwhile,
	// and a new query, an empty box or a closed tab cancels it.
	searchGlobalTimeout = 30 * time.Second
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
	// cancel stops the walk in flight; a new query, an empty box or a closed
	// tab calls it so an abandoned walk does not run on to its deadline.
	cancel context.CancelFunc
}

// stop cancels the walk in flight, if any.
func (s *searchState) stop() {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// frameQueue is how a background search reaches the state it fills in: the
// search's own goroutine writes nothing but the queue, and the frame applies
// what it finds there. Running, the batch lands on the main thread; headless,
// this queue keeps two goroutines off one struct.
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
	t.search.stop()
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
	scope := t.queryScope
	hidden := a.showHidden
	query := searchquery.Parse(q)
	deadline := time.Now().Add(searchDeadline(t.queryScope))
	ctx, cancel := context.WithCancel(context.Background())
	t.search.cancel = cancel
	go func() {
		defer cancel()
		search.Stream(roots, query, search.Options{
			Hidden: hidden, Deadline: deadline, Max: searchMaxResults, Ctx: ctx,
		}, func(batch []Entry, more bool) {
			apply := func() {
				if !a.ownsTab(tab) || strings.TrimSpace(tab.query) != q || tab.search.scope != scope || tab.search.query != q {
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

// walkMatches adapts the reusable one-root walk to the remote endpoint and
// feature tests that already speak in the application's Entry alias.
func walkMatches(root, rawQuery string, hidden bool, deadline time.Time) []Entry {
	return search.Walk(root, rawQuery, hidden, deadline)
}

// searchPruneDir remains a compatibility bridge for the index code that is
// still in this package; the next refactor step moves that code as well.
func searchPruneDir(path string) bool { return search.PruneDir(path) }

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
