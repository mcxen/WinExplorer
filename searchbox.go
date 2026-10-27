package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"sunvale/internal/search"
	"sunvale/internal/searchquery"
)

// searchbox.go is the Ctrl+K box: Everything's search, in its own panel —
// the kind of window Raycast and Spotlight use. Ctrl+K (Cmd+K) shows a
// frameless frosted panel above every app, on the current Space, with its
// own query box and a 本文件夹 / 全部磁盘 switch — it starts at every disk,
// which is the point of it — and a live list of hits, streamed by the same
// walk the pane's search box uses (search.go) with Everything's query
// language (searchquery.go), or answered on the keystroke by the search
// index (searchindex.go) when it covers the scope. ↑/↓ walk the hits,
// PageUp/PageDown by a page, Enter opens one (a folder is entered, a file
// opens in the folder it lives in), Ctrl+Enter shows it there instead — the
// tab navigates to the folder with the hit selected — the 位置 button on a
// row does the same with the mouse, and Escape or a click elsewhere hides
// the panel. A headless test has no app run, so the same body is drawn as
// a dialog inside the window under test instead.

const (
	// searchBoxWidth is the palette's width. It is a fixed request, as the
	// other dialogs' widths are: a window narrower than it leaves the panel its
	// insets and no more, and the rows keep their width rather than reflow.
	searchBoxWidth float32 = 960
	// searchBoxListMax is how tall the hits list grows before it scrolls, and
	// searchBoxListMin what it keeps when the window is short.
	searchBoxListMax float32 = 384
	searchBoxListMin float32 = 120
	// searchHitRowH gives each hit a name and a breadcrumb line, the shape
	// Explorer's search results use.
	searchHitRowH float32 = 44
	// searchHitMetaW is the right-hand size/type and date column.
	searchHitMetaW float32 = 150
)

// searchSource is which engine answered the list on screen, which the footer
// says: the index on the keystroke, or the live walk of the disks.
type searchSource int

const (
	// sourceNone is an empty query, or a scope with no roots to search.
	sourceNone searchSource = iota
	// sourceWalk is a live walk: the disks, streamed as they are read.
	sourceWalk
	// sourceIndex is the search index (searchindex.go): instant, as old as
	// the last build.
	sourceIndex
)

// searchBoxState is the palette's own search. One box per window, not per tab:
// it searches the machine, not what a tab happens to show.
type searchBoxState struct {
	open  bool
	query string
	scope searchScope
	// querySelected is the query editor's selection as of the last frame or
	// edit key, so Ctrl+C can leave a selected query to normal text copy.
	querySelected bool
	// copyPath is a path queued by an edit-key callback. The next frame
	// writes it through the current context's clipboard service.
	copyPath string
	// allResults is the raw query's one walk result; results is the filter
	// bar's visible subset. A filter change reuses allResults rather than
	// traversing the disks again.
	filters    searchFilters
	allResults []Entry
	results    []Entry
	rows       []searchDisplayRow
	list       ui.ListState
	busy       bool
	started    time.Time
	// sortBy is the list's order: relevance (the default) or name.
	sortBy searchSort
	// scores, kinds and reasons cache what Score said when a hit arrived, so
	// a streamed batch does not rescore every row or the preview.
	// near is the folder on screen when those scores were computed: hits
	// there lead the default relevance order.
	scores  map[string]int
	kinds   map[string]searchquery.MatchKind
	reasons map[string][]searchquery.Reason
	near    string
	// src is the engine that answered the results on screen (searchSource).
	src searchSource
	// totalHits is everything the raw query found before the filter bar
	// kept its share: the filter bar's x / y count.
	totalHits    int
	filteredHits int
	// done is when the walk finished; zero while it runs, so the footer can
	// show the elapsed time either way.
	done time.Time
	// cursor is the row the keyboard is on and cursorPath the hit it was on, so
	// a batch that sorts above it does not move the choice. cursorPath stays
	// empty until the user moves the cursor: until then the first row — the
	// first name — stays chosen, whatever streams in above it.
	cursor     int
	cursorPath string
	// walked, walkedScope and walkedFilters are the query, scope and filter
	// bar of the walk in flight or finished: a change to any starts a new
	// one. parsed is that query, ready for the list's hit marking.
	walked        string
	walkedScope   searchScope
	walkedFilters searchFilters
	parsed        searchquery.Query
	walkQuery     searchquery.Query
	// gen is the walk's generation. Closing the box or typing again bumps it,
	// and the batches of an older walk are dropped.
	gen uint64
	// queue is where the search running off the frame thread hands its results
	// over (frameQueue).
	queue frameQueue
	// seen is the path of every raw hit already listed, so a hit the index and
	// the walk both found is listed once (addHits).
	seen map[string]bool
}

// searchFilters is the palette's filter bar. The zero value is 不限 / 全部
// kinds, which adds nothing to the query.
type searchFilters struct {
	kind searchKindFilter
	when searchTimeFilter
	size searchSizeFilter
}

type searchKindFilter int

const (
	kindAny searchKindFilter = iota
	kindFolderFilter
	kindDocumentFilter
	kindImageFilter
	kindMediaFilter
	kindArchiveFilter
	kindCodeFilter
)

var searchKindOptions = []struct {
	filter searchKindFilter
	label  string
	mod    string
}{
	{kindAny, "全部类型", ""},
	{kindFolderFilter, "文件夹", "folder"},
	{kindDocumentFilter, "文档", "document"},
	{kindImageFilter, "图片", "image"},
	{kindMediaFilter, "音视频", "media"},
	{kindArchiveFilter, "压缩包", "archive"},
	{kindCodeFilter, "代码", "code"},
}

type searchTimeFilter int

const (
	timeAnyFilter searchTimeFilter = iota
	timeTodayFilter
	timeWeekFilter
	timeMonthFilter
	timeYearFilter
)

var searchTimeOptions = []struct {
	filter searchTimeFilter
	label  string
	mod    string
}{
	{timeAnyFilter, "不限时间", ""},
	{timeTodayFilter, "今天", "today"},
	{timeWeekFilter, "近 7 天", "7d"},
	{timeMonthFilter, "近 30 天", "30d"},
	{timeYearFilter, "今年", "year"},
}

type searchSizeFilter int

const (
	sizeAnyFilter searchSizeFilter = iota
	sizeSmallFilter
	sizeMediumFilter
	sizeLargeFilter
)

var searchSizeOptions = []struct {
	filter searchSizeFilter
	label  string
	mod    string
}{
	{sizeAnyFilter, "不限大小", ""},
	{sizeSmallFilter, "< 100 KB", "<100kb"},
	{sizeMediumFilter, "100 KB–10 MB", "100kb..10mb"},
	{sizeLargeFilter, "> 10 MB", ">10mb"},
}

type searchSort int

const (
	sortRelevance searchSort = iota
	sortFolder
	sortType
	sortName
)

// searchDisplayRow flattens grouped results for the virtual list: a heading
// is not a hit and the keyboard walks around it.
type searchDisplayRow struct {
	heading bool
	label   string
	count   int
	hit     int
}

// searchQueryWithFilters appends the filter bar's choices to the typed
// query. A raw query of nothing but filters is a legitimate search: it lists
// everything the filters keep.
func searchQueryWithFilters(q string, f searchFilters) string {
	return searchQueryWithFiltersKind(q, f, true)
}

// searchWalkQuery is the same query without the 类型 choice. The walk still
// gathers the type's rejected hits, so the filter bar can say x / y without
// traversing the disk a second time; addHits applies the full query.
func searchWalkQuery(q string, f searchFilters) string {
	return searchQueryWithFiltersKind(q, f, false)
}

func searchQueryWithFiltersKind(q string, f searchFilters, withKind bool) string {
	var mods []string
	if withKind {
		if f.kind == kindFolderFilter {
			mods = append(mods, "folder:")
		} else if f.kind != kindAny {
			for _, opt := range searchKindOptions {
				if opt.filter == f.kind {
					mods = append(mods, "kind:"+opt.mod)
					break
				}
			}
		}
	}
	for _, opt := range searchTimeOptions {
		if opt.filter == f.when && opt.mod != "" {
			mods = append(mods, "dm:"+opt.mod)
			break
		}
	}
	for _, opt := range searchSizeOptions {
		if opt.filter == f.size && opt.mod != "" {
			mods = append(mods, "size:"+opt.mod)
			break
		}
	}
	if len(mods) == 0 {
		return q
	}
	if q == "" {
		return strings.Join(mods, " ")
	}
	return q + " " + strings.Join(mods, " ")
}

// searchBoxFrame is called once per frame from view: it starts a search when
// the query or the scope changed and applies what comes back — the index's
// snapshot on the keystroke when it covers the scope (searchindex.go), else the
// same live walk the pane's search box runs, on the box's own roots.
func (a *App) searchBoxFrame() {
	b := &a.searchBox
	// The folder on screen leads the relevance order. Set it before the
	// queue drains, so a batch that lands this frame is scored against it.
	near := ""
	if p := a.cur().path; p != "" && !specialPath(p) {
		near = p
	}
	prevNear := b.near
	b.near = near
	// What the search running off this thread has handed over — the hits it
	// found, and the news that it is through — applies here, where the box's
	// fields are this frame's alone.
	for _, op := range b.queue.take() {
		op()
	}
	if b.open && prevNear != b.near && b.walked != "" && len(b.allResults) > 0 {
		b.refilter()
		b.cursor = b.cursorRow()
	}
	if !b.open {
		return
	}
	q := strings.TrimSpace(b.query)
	if q == b.walked && b.scope == b.walkedScope {
		if b.filters != b.walkedFilters {
			b.walkedFilters = b.filters
			rawQuery := searchQueryWithFilters(q, b.filters)
			b.parsed = searchquery.Parse(rawQuery)
			b.refilter()
			b.cursor = b.cursorRow()
			b.list.ScrollIntoView(b.cursor)
			repaint()
		}
		return
	}
	b.walked, b.walkedScope, b.walkedFilters = q, b.scope, b.filters
	rawQuery := searchQueryWithFilters(q, b.filters)
	b.parsed = searchquery.Parse(rawQuery)
	b.walkQuery = searchquery.Parse(strings.TrimSpace(q))
	b.allResults = nil
	b.results = nil
	b.rows = nil
	b.seen = nil
	b.scores = nil
	b.kinds = nil
	b.reasons = nil
	b.totalHits = 0
	b.filteredHits = 0
	b.cursor, b.cursorPath = 0, ""
	b.done = time.Time{}
	b.src = sourceNone
	b.gen++
	gen := b.gen
	if rawQuery == "" {
		b.busy = false
		return
	}
	roots := a.searchRoots(b.scope)
	if len(roots) == 0 {
		b.busy = false
		return
	}
	b.busy = true
	b.src = sourceWalk
	b.started = time.Now()
	hidden := a.showHidden
	query := b.walkQuery
	deadline := time.Now().Add(searchDeadline(b.scope))
	// land hands a closure to the frame: the main thread while the app runs,
	// and the next frame's own thread in a headless test (frameQueue).
	land := func(f func()) {
		if mygo.App.IsReady() {
			mygo.RunOnMain(f)
			return
		}
		b.queue.post(f)
	}
	// live is whether the box still wants this search's results: closed, or a
	// newer query or scope took over.
	live := func() bool {
		return a.searchBox.open && a.searchBox.gen == gen
	}
	go func() {
		// The index answers on the keystroke when it covers the scope. It
		// cannot when it is off or still empty, or when the scope is a folder
		// no walk ever reached — then the disks are walked, as before.
		if hits, ok := searchIndexQuery(roots, query, hidden); ok {
			// A snapshot short of the whole machine — the cap, or a quit in
			// the middle of a build — answers at once and leaves the walk to
			// fill in the rest: instant hits, then completeness.
			whole := searchIndexWhole()
			land(func() {
				if !live() {
					return
				}
				b.src = sourceIndex
				b.addHits(hits)
				if whole {
					b.busy = false
					b.done = time.Now()
				}
				repaint()
			})
			if whole {
				return
			}
		}
		search.Stream(roots, query, search.Options{
			Hidden: hidden, Deadline: deadline, Max: searchMaxResults,
		}, func(batch []Entry, more bool) {
			land(func() {
				if !live() {
					return // closed, or a newer query took over
				}
				if !more {
					b.busy = false
					b.done = time.Now()
				}
				if len(batch) > 0 {
					b.addHits(batch)
					repaint()
				}
			})
		})
	}()
}

// hit is the entry under the cursor, if the list has one.
func (b *searchBoxState) hit() (Entry, bool) {
	if b.cursor < 0 || b.cursor >= len(b.rows) {
		return Entry{}, false
	}
	i := b.rows[b.cursor].hit
	if b.rows[b.cursor].heading || i < 0 || i >= len(b.results) {
		return Entry{}, false
	}
	return b.results[i], true
}

// addHits folds a raw batch into the walk's results. The filter bar then
// derives its visible rows from those results, so changing a filter never
// needs to walk the disks again. A hit already listed — the index and the walk
// behind it can find the same file — is folded in once.
func (b *searchBoxState) addHits(batch []Entry) {
	added := false
	if b.seen == nil {
		b.seen = make(map[string]bool, len(batch))
	}
	for _, e := range batch {
		if e.Path == "" || b.seen[e.Path] {
			continue
		}
		b.seen[e.Path] = true
		b.allResults = append(b.allResults, e)
		added = true
	}
	if !added {
		return
	}
	b.refilter()
	if i := b.cursorRow(); i != b.cursor {
		b.cursor = i
		b.list.ScrollIntoView(i)
	}
}

// refilter rebuilds the visible list from allResults for the filter bar's
// current choices. It is the one place that applies the full query, so the
// index, the walk and a filter change cannot drift apart.
func (b *searchBoxState) refilter() {
	now := time.Now()
	home := homeForSearch()
	b.results = nil
	b.scores = make(map[string]int, len(b.allResults))
	b.kinds = make(map[string]searchquery.MatchKind, len(b.allResults))
	b.reasons = make(map[string][]searchquery.Reason, len(b.allResults))
	for _, e := range b.allResults {
		info := searchquery.MatchInfo{
			IsDir:   e.IsDir,
			Ext:     e.Ext,
			Size:    e.Size,
			ModTime: e.ModTime,
		}
		if !b.parsed.Matches(e.Name, e.Path, info) {
			continue
		}
		score, kind, reasons := b.parsed.Score(e.Name, e.Path, e.IsDir, e.ModTime, now, home, b.near)
		b.results = append(b.results, e)
		b.scores[e.Path], b.kinds[e.Path] = score, kind
		b.reasons[e.Path] = reasons
	}
	b.totalHits = len(b.allResults)
	b.filteredHits = len(b.results)
	b.sortHits()
}

// rebuildDisplayRows turns the sorted results into list rows. Ungrouped modes
// have one row per hit; folder/type modes put a heading before each run.
func (b *searchBoxState) rebuildDisplayRows() {
	b.rows = b.rows[:0]
	if b.sortBy != sortFolder && b.sortBy != sortType {
		for i := range b.results {
			b.rows = append(b.rows, searchDisplayRow{hit: i})
		}
		return
	}
	for i := 0; i < len(b.results); {
		j := i + 1
		for j < len(b.results) && b.groupKey(b.results[i]) == b.groupKey(b.results[j]) {
			j++
		}
		b.rows = append(b.rows, searchDisplayRow{
			heading: true,
			label:   b.groupLabel(b.results[i]) + " · " + itoa(j-i),
			count:   j - i,
		})
		for ; i < j; i++ {
			b.rows = append(b.rows, searchDisplayRow{hit: i})
		}
	}
}

func (b *searchBoxState) groupKey(e Entry) string {
	if b.sortBy == sortType {
		return typeLabel(e)
	}
	return strings.ToLower(e.Dir)
}

func (b *searchBoxState) groupLabel(e Entry) string {
	if b.sortBy == sortType {
		return typeLabel(e)
	}
	return searchPathText(searchPathSegments(e.Dir, homeForSearch(), 4))
}

// sortHits puts the hits in the order the switch asks for. Relevance keeps
// the name and then the path as ties, so the order is deterministic as
// batches stream in; folder and type modes add heading rows, while name is
// the Everything order.
func (b *searchBoxState) sortHits() {
	switch b.sortBy {
	case sortName:
		sortHitsByName(b.results)
	case sortFolder:
		slices.SortStableFunc(b.results, func(x, y Entry) int {
			if c := strings.Compare(strings.ToLower(x.Dir), strings.ToLower(y.Dir)); c != 0 {
				return c
			}
			return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		})
	case sortType:
		slices.SortStableFunc(b.results, func(x, y Entry) int {
			if c := strings.Compare(typeLabel(x), typeLabel(y)); c != 0 {
				return c
			}
			return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		})
	default:
		sortHitsByScore(b.results, b.scores)
	}
	b.rebuildDisplayRows()
}

// sortHitsByScore puts higher scores first, with name and path breaking ties.
// The lookup is by path so a hit's score survives both orderings.
func sortHitsByScore(hits []Entry, scores map[string]int) {
	slices.SortStableFunc(hits, func(x, y Entry) int {
		if c := scores[y.Path] - scores[x.Path]; c != 0 {
			return c
		}
		if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
			return c
		}
		return strings.Compare(x.Path, y.Path)
	})
}

// sortHitsByQuery ranks a batch whose scores have not been cached yet. The
// index uses it when it answers in one frame; the palette uses its cached
// scores so streamed batches cost one score per new hit.
func sortHitsByQuery(hits []Entry, q searchquery.Query) {
	now, home := time.Now(), homeForSearch()
	scores := make(map[string]int, len(hits))
	for _, e := range hits {
		scores[e.Path], _, _ = q.Score(e.Name, e.Path, e.IsDir, e.ModTime, now, home, "")
	}
	sortHitsByScore(hits, scores)
}

// sortHitsByName puts hits in Everything's order: by name, case-insensitively,
// with the path breaking ties, so two hits of the same name have one order
// whatever order they arrived in.
func sortHitsByName(hits []Entry) {
	slices.SortStableFunc(hits, func(x, y Entry) int {
		if c := strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)); c != 0 {
			return c
		}
		return strings.Compare(x.Path, y.Path)
	})
}

// cursorRow is the row the cursor belongs on: the hit it was on, or the first
// row while the user has not chosen one. A hit that is no longer listed — a
// newer walk — leaves the cursor at the top.
func (b *searchBoxState) cursorRow() int {
	if b.cursorPath == "" {
		for i, row := range b.rows {
			if !row.heading {
				return i
			}
		}
		return 0
	}
	for i, row := range b.rows {
		if row.heading || row.hit < 0 || row.hit >= len(b.results) {
			continue
		}
		if b.results[row.hit].Path == b.cursorPath {
			return i
		}
	}
	b.cursorPath = ""
	for i, row := range b.rows {
		if !row.heading {
			return i
		}
	}
	return 0
}

// searchBoxMove walks the cursor by d hits, held to the ends of the list, and
// scrolls the hit it lands on into view.
func (a *App) searchBoxMove(d int) {
	b := &a.searchBox
	if len(b.rows) == 0 {
		return
	}
	i := max(b.cursor, 0)
	step := 1
	if d < 0 {
		step = -1
	}
	steps := d
	if steps < 0 {
		steps = -steps
	}
	for n := 0; n < steps; n++ {
		next := i
		for {
			next += step
			if next < 0 || next >= len(b.rows) {
				break
			}
			if !b.rows[next].heading {
				i = next
				break
			}
		}
		if next < 0 || next >= len(b.rows) {
			break
		}
	}
	if i == b.cursor {
		return
	}
	b.cursor = i
	b.cursorPath = b.results[b.rows[i].hit].Path
	b.list.ScrollIntoView(i)
	repaint()
}

// openSearchHit opens a hit as Enter does in the listing: a folder is entered,
// a file opens in the folder it lives in — the tab navigates there and selects
// it first, so the pane shows what the box found.
func (a *App) openSearchHit(e Entry) {
	if e.Path == "" {
		return
	}
	a.searchBoxClose()
	a.showExplorer()
	if e.IsDir {
		a.openDirEntry(e)
		return
	}
	a.revealSearchHit(e)
	a.doOpen()
}

// revealSearchHit shows a hit in the pane: the tab navigates to the folder it
// lives in and selects it, Ctrl+Enter's 在文件夹中显示.
func (a *App) revealSearchHit(e Entry) {
	if e.Path == "" {
		return
	}
	dir := e.Dir
	a.searchBoxClose()
	a.showExplorer()
	if dir != "" {
		a.navigate(dir)
	}
	a.selectOnly(e.Path)
	a.cur().reveal = e.Path
}

// searchBoxModal draws the palette while it is open, from view with the
// window's other overlays. The running app draws it in the panel instead,
// so this is the headless path: a test lays the same body out inside the
// window it drives. A close from outside the body — a click on the
// backdrop, DialogBase's own Escape — cancels the walk here.
