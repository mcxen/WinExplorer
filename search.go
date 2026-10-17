package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// search.go is the search behind the box: Everything's experience on
// Explorer's engine — typing a query walks the current folder's subtree in
// the background and streams the hits to the pane as they are found, so
// results appear while the walk is still running instead of after it. The
// query language (searchquery.go) is Everything's semantics the engine can
// honestly answer: case:, file:, folder:, path:, ext:, quoted phrases and
// space-separated AND terms.
const (
	searchMaxResults = 5000
	searchTimeout    = 8 * time.Second
	// searchFlushEvery is how often a running walk reports its batch of
	// new hits to the pane: often enough to feel instant, seldom enough
	// that the repaints cost nothing.
	searchFlushEvery = 150 * time.Millisecond
)

// searchState lives on the tab: the query the last walk was for, its
// results, and whether a walk is in flight. streamed is the walk's
// generation: a new query bumps it, and the worker holding an older
// generation drops everything it still had.
type searchState struct {
	query   string
	results []Entry
	busy    bool
	started time.Time
	// done is when the walk finished; zero while it runs. The status bar
	// shows the elapsed time between started and done, Everything's
	// "找到 N 个项目 (X.XXs)".
	done time.Time
}

// searchEpoch is the tab's walk generation counter, shared with the worker.
// It is not part of searchState because the worker reads it from the tab
// on every batch, not once at start.
type searchEpoch struct {
	gen uint64
}

// updateSearch is called once per frame from view: it starts a walk when
// the query has changed. Results stream in through applySearchBatch; a
// walk for an older query drops its batches, and clearing the box cancels
// the running walk outright.
func (a *App) updateSearch() {
	t := a.cur()
	q := strings.TrimSpace(t.query)
	if specialPath(t.path) {
		return
	}
	if q == t.search.query {
		return
	}
	// A new query invalidates the running walk: the worker's next batch
	// fails the epoch check and exits early.
	t.search.query = q
	t.search.results = nil
	t.search.done = time.Time{}
	if q == "" {
		t.search.busy = false
		return
	}
	t.search.busy = true
	t.search.started = time.Now()
	tab := t
	root := t.path
	hidden := a.showHidden
	query := parseSearchQuery(q)
	deadline := time.Now().Add(searchTimeout)
	go func() {
		streamMatches(root, query, hidden, deadline, func(batch []Entry, more bool) {
			fn := func() {
				if !a.ownsTab(tab) || strings.TrimSpace(tab.query) != q {
					return // a newer query or the tab closed: drop it
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
			// The running app lands on the main thread; headless (tests
			// without App.Run) the batch applies right here, as sizes.go's
			// dirStatUpdateUI does.
			if mygo.App.IsReady() {
				mygo.RunOnMain(fn)
			} else {
				fn()
			}
		})
	}()
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

// streamMatches walks root's subtree and hands every matching entry to
// emit in batches — at least every searchFlushWhile walking, immediately
// at the end. emit's more is false on the final call, so the caller learns
// the walk is over even when the last batch is empty. The same cap and
// deadline rules as before: 5000 hits, 8 seconds, hidden folders skipped
// unless asked.
func streamMatches(root string, query searchQuery, hidden bool, deadline time.Time, emit func(batch []Entry, more bool)) {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var batch []Entry
	lastFlush := time.Now()
	total := 0
	flush := func(more bool) {
		if len(batch) > 0 || !more {
			emit(batch, more)
			batch = batch[:0]
		}
		lastFlush = time.Now()
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || total >= searchMaxResults {
			return filepath.SkipAll
		}
		name := d.Name()
		if path != root && strings.HasPrefix(name, ".") && !hidden {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		isDir := d.IsDir()
		ext := ""
		if !isDir {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
		}
		if query.matches(name, path, ext, isDir) {
			if e := searchEntry(path, d); e.Path != "" {
				batch = append(batch, e)
				total++
				if total >= searchMaxResults {
					flush(false)
					return filepath.SkipAll
				}
			}
		}
		if time.Since(lastFlush) >= searchFlushEvery {
			flush(true)
		}
		return nil
	})
	flush(false)
}

// walkMatches is streamMatches collected at once, for callers that want
// the whole answer in one piece (the tests, the remote search endpoint).
func walkMatches(root, rawQuery string, hidden bool, deadline time.Time) []Entry {
	query := parseSearchQuery(rawQuery)
	var out []Entry
	streamMatches(root, query, hidden, deadline, func(batch []Entry, more bool) {
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

// activeSearchQuery is the parsed query of the current tab's search, for
// the pane's highlighting. The parse is cached per tab: every row of a
// 5000-hit pane asks for it every frame, and the query string changes
// only between keystrokes.
func (a *App) activeSearchQuery() searchQuery {
	t := a.cur()
	q := strings.TrimSpace(t.query)
	if q == "" {
		t.hlCache.query = ""
		t.hlCache.parsed = searchQuery{}
		return searchQuery{}
	}
	if t.hlCache.query != q {
		t.hlCache.query = q
		t.hlCache.parsed = parseSearchQuery(q)
	}
	return t.hlCache.parsed
}

// searchStatusLabel is the status bar's search line, Everything's shape:
// the count, the elapsed seconds while the walk runs or once it finished,
// and the truncated query. An empty string when there is nothing to say.
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
	trunc := truncateRunes(q, 24)
	if t.search.busy {
		return "正在搜索 “" + trunc + "”  已找到 " + itoa(n) + " 个项目" + secs
	}
	return "“" + trunc + "”  找到 " + itoa(n) + " 个项目" + secs
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
