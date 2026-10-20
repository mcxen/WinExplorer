package main

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

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
	searchBoxWidth float32 = 760
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
	// scores and kinds cache what Score said when a hit arrived, so a
	// streamed batch does not rescore every row.
	scores map[string]int
	kinds  map[string]searchquery.MatchKind
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
	// What the search running off this thread has handed over — the hits it
	// found, and the news that it is through — applies here, where the box's
	// fields are this frame's alone.
	for _, op := range b.queue.take() {
		op()
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
		streamMatches(roots, query, hidden, deadline, func(batch []Entry, more bool) {
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
		score, kind, _ := b.parsed.Score(e.Name, e.Path, e.IsDir, e.ModTime, now, home)
		b.results = append(b.results, e)
		b.scores[e.Path], b.kinds[e.Path] = score, kind
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
		scores[e.Path], _, _ = q.Score(e.Name, e.Path, e.IsDir, e.ModTime, now, home)
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
		a.navigate(e.Path)
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
func (a *App) searchBoxModal(c *ui.Context) {
	if searchPanel != nil {
		return
	}
	b := &a.searchBox
	if !b.open {
		return
	}
	floatDialog(c, &b.open, "快速搜索", searchBoxWidth, true, func() {
		a.searchBoxBody(c)
	})
	if !b.open {
		a.searchBoxCancel()
	}
}

// searchBoxBody is the palette: the query row with its scope switch, the
// streamed hits, and the footer with the count, the elapsed time and the keys.
func (a *App) searchBoxBody(c *ui.Context) {
	b := &a.searchBox
	_, h := c.Size()
	listH := min(searchBoxListMax, max(searchBoxListMin, h-188))
	ui.Column(c).Gap(0).FillWidth().MinWidth(420).Children(func() {
		row := ui.Row(c).FillWidth().Height(44).Gap(8).AlignItems(ui.Center)
		row.Children(func() {
			components.Icon(c, lucide.Search).Size(16, 16).TextColor(colorMuted())
			in := ui.TextInputBase(c, &b.query).Grow(1).MinWidth(0).Height(28).FontSize(13).
				AutoFocus().
				Placeholder("搜索全部磁盘：多个词是 AND，ext:txt  path:docs  \"完整短语\"  case:")
			a.searchBoxKeys(c, in)
			sortIdx := int(b.sortBy)
			idx := scopeIndex(b.scope)
			if ui.Segmented(c, &sortIdx, "相关度", "按文件夹", "按类型", "名称").Shrink(0).Changed() {
				b.sortBy = searchSort(sortIdx)
				b.sortHits()
				b.cursor = b.cursorRow()
				b.list.ScrollIntoView(b.cursor)
				repaint()
			}
			if ui.Segmented(c, &idx, "本文件夹", "全部磁盘").Shrink(0).Changed() {
				b.scope = scopeFromIndex(idx) // the next frame starts the walk
				repaint()
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
		a.searchFilterBar(c)
		if len(b.results) == 0 {
			a.searchBoxNotice(c, listH)
		} else {
			ui.List(c, &b.list, len(b.rows), func(i int) {
				a.searchDisplayRowAt(c, i)
			}).FillWidth().Height(listH).MinHeight(0)
		}
		a.searchBoxStatus(c)
	})
}

func (a *App) searchDisplayRowAt(c *ui.Context, i int) {
	b := &a.searchBox
	if i < 0 || i >= len(b.rows) {
		return
	}
	row := b.rows[i]
	if row.heading {
		ui.Row(c).FillWidth().Height(24).Padding(0, 6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(11).FontWeight(600).TextColor(colorMuted()).SingleLine()
		})
		return
	}
	a.searchHitRow(c, row.hit)
}

// searchFilterBar is the row under the query: each menu narrows the same
// walk by appending a query modifier, and the count on the right is what the
// filters kept out of the hits that came back. Changing a filter starts a new
// walk only when one is not already answering; the filter is part of the
// walk key, so the disk is traversed at most once per distinct query.
func (a *App) searchFilterBar(c *ui.Context) {
	b := &a.searchBox
	ui.Row(c).FillWidth().Height(30).Gap(8).AlignItems(ui.Center).Children(func() {
		searchKindChip(c, &b.filters.kind)
		searchTimeChip(c, &b.filters.when)
		searchSizeChip(c, &b.filters.size)
		searchPlaceChip(c, &b.scope)
	})
	ui.Row(c).FillWidth().Height(18).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		ui.Text(c, filterResultCount(b)).FontSize(11).TextColor(colorMuted()).
			SingleLine().Shrink(0).TextAlign(ui.End)
	})
}

// searchPlaceChip is the 位置 filter. It is the existing scope switch by
// another name: 当前文件夹 walks the tab's folder, 主文件夹 the home subtree
// and 全部磁盘 every mounted volume.
func searchPlaceChip(c *ui.Context, scope *searchScope) {
	value := "全部磁盘"
	if *scope == scopeFolder {
		value = "当前文件夹"
	}
	chip := searchFilterChip(c, "位置", value)
	changed := false
	chip.Menu(func(m *ui.Menu) {
		options := []struct {
			scope searchScope
			label string
		}{{scopeFolder, "当前文件夹"}, {scopeGlobal, "全部磁盘"}}
		for _, opt := range options {
			value := opt.scope
			if m.Item(opt.label).Checked(value == *scope).Chosen() {
				*scope = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchFilterChip(c *ui.Context, title, value string) ui.Element {
	chip := ui.ButtonBase(c.Key("search-filter-"+title)).Height(22).Padding(0, 8).Gap(4).
		Radius(11).Border(1, colorFieldBorder()).Shrink(0).AlignItems(ui.Center).
		Label(title + "：" + value)
	chip.Background(func() ui.Color {
		if chip.Hovered() || chip.Pressed() {
			return colorHover()
		}
		return ui.Color{}
	}())
	chip.Children(func() {
		ui.Text(c, title+"：").FontSize(11).TextColor(colorMuted()).SingleLine()
		ui.Text(c, value).FontSize(11).TextColor(colorText()).SingleLine()
	})
	return chip
}

func searchKindOptionsLabel(kind searchKindFilter) string {
	for _, opt := range searchKindOptions {
		if opt.filter == kind {
			return opt.label
		}
	}
	return searchKindOptions[0].label
}

func searchTimeOptionsLabel(when searchTimeFilter) string {
	for _, opt := range searchTimeOptions {
		if opt.filter == when {
			return opt.label
		}
	}
	return searchTimeOptions[0].label
}

func searchSizeOptionsLabel(size searchSizeFilter) string {
	for _, opt := range searchSizeOptions {
		if opt.filter == size {
			return opt.label
		}
	}
	return searchSizeOptions[0].label
}

func searchKindChip(c *ui.Context, current *searchKindFilter) {
	chip := searchFilterChip(c, "类型", searchKindOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchKindOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchTimeChip(c *ui.Context, current *searchTimeFilter) {
	chip := searchFilterChip(c, "时间", searchTimeOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchTimeOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchSizeChip(c *ui.Context, current *searchSizeFilter) {
	chip := searchFilterChip(c, "大小", searchSizeOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchSizeOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

// filterResultCount is the filter bar's right-hand count. It names kept hits
// when a filter is active; without one, the footer already has the count.
func filterResultCount(b *searchBoxState) string {
	if b.filters == (searchFilters{}) {
		return ""
	}
	return itoa(b.filteredHits) + " / " + itoa(b.totalHits) + " 个结果"
}

// searchBoxNotice is the list's place while there is nothing in it: whether the
// walk is running, found nothing, or has not been asked yet.
func (a *App) searchBoxNotice(c *ui.Context, height float32) {
	b := &a.searchBox
	text := "输入即搜索全部磁盘"
	switch {
	case strings.TrimSpace(b.query) == "":
	case b.busy:
		text = "正在搜索…"
	default:
		text = "没有找到匹配的项目"
	}
	ui.Row(c).FillWidth().Height(height).AlignItems(ui.Center).Justify(ui.Center).
		Children(func() {
			ui.Text(c, text).FontSize(12).TextColor(colorMuted())
		})
}

// searchHitRow is one hit: its icon, complete name and parent breadcrumb, then
// its type/size and date. The row under the cursor is highlighted; a click moves
// the cursor to it, a double click opens it, as Enter does, and 位置 jumps to
// the folder it lives in with it selected, as Ctrl+Enter does.
func (a *App) searchHitRow(c *ui.Context, i int) {
	b := &a.searchBox
	e := b.results[i]
	kind := b.kinds[e.Path]
	label := e.Name
	if why := kind.Label(); why != "" {
		label += " " + why
	}
	row := ui.ButtonBase(c.Key("hit-"+e.Path)).FillWidth().Height(searchHitRowH).
		MinHeight(searchHitRowH).Padding(0, 6).Gap(8).AlignItems(ui.Center).
		Radius(4).Label(label)
	row.Tooltip(e.Path)
	switch {
	case i == b.cursor:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		glyph := ui.Box(c).Size(24, 24).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, e, 24)
		})
		main := ui.Column(c).Grow(1).MinWidth(0).Height(searchHitRowH).Justify(ui.Center)
		main.Children(func() {
			name := ui.Row(c).MinWidth(0).Height(16).AlignItems(ui.Center).Gap(6)
			name.Children(func() {
				ui.RichText(c, searchNameSpans(b.parsed, e.Name, e.IsDir, 12)...).
					SingleLine().MinWidth(0)
				if label := kind.Label(); label != "" {
					searchReasonBadge(c, kind)
				}
			})
			a.searchPathRow(c, e)
		})
		meta := ui.Column(c).Width(searchHitMetaW).Shrink(0).Height(searchHitRowH).Justify(ui.Center)
		meta.Children(func() {
			kind := typeLabel(e)
			if e.IsDir {
				kind = "文件夹"
			} else if size := sizeColumnText(e); size != "" {
				kind = size + " · " + kind
			}
			ui.Text(c, kind).FontSize(11).TextColor(colorMuted()).SingleLine().
				Width(searchHitMetaW).TextAlign(ui.End)
			ui.Text(c, formatModTime(e.ModTime)).FontSize(11).TextColor(colorMuted()).SingleLine().
				Width(searchHitMetaW).TextAlign(ui.End)
		})
		// 位置 is the mouse's Ctrl+Enter: the hit's folder, with the hit in it.
		// The button takes its own clicks (the innermost element does), so it
		// neither moves the cursor nor opens the hit.
		loc := ui.ButtonBase(c.Key("loc-"+e.Path)).Size(18, 18).Shrink(0).Padding(0).
			Radius(4).Label("打开所在文件夹")
		if loc.Hovered() {
			loc.Background(colorHover())
		}
		loc.Children(func() {
			components.Icon(c, lucide.FolderOpen).Size(13, 13).TextColor(colorMuted())
		})
		if loc.Clicked() {
			a.revealSearchHit(e)
		}
	})
	if row.Clicked() {
		b.cursor, b.cursorPath = i, e.Path
	}
	if row.DoubleClicked() {
		a.openSearchHit(e)
	}
}

// searchPathRow is a hit's parent path as clickable breadcrumbs. A segment
// takes the mouse to its folder without choosing the row: the innermost
// button gets the click, as the 位置 button does. The elided middle has no
// destination of its own and shows the hidden folders on hover instead.
func (a *App) searchPathRow(c *ui.Context, e Entry) {
	segs := searchPathSegments(e.Dir, homeForSearch(), 5)
	q := a.searchBox.parsed
	row := ui.Row(c).MinWidth(0).Height(16).AlignItems(ui.Center).Gap(2).ClipX()
	row.Children(func() {
		for i, seg := range segs {
			if i > 0 {
				ui.Text(c, "›").FontSize(11).TextColor(colorMuted()).Shrink(0).
					Padding(0, 2)
			}
			if seg.Elided {
				text := ui.RichText(c, pathRunSpans(q, seg.Label, 11, colorMuted())...).Shrink(0)
				hidden := elidedPath(e.Dir, segs)
				text.Tooltip("省略的路径：" + hidden)
				continue
			}
			btn := ui.ButtonBase(c.Key("seg-"+e.Path+"|"+seg.Path)).
				Height(16).Padding(0, 2).Radius(3).Shrink(0)
			if btn.Hovered() {
				btn.Background(colorHover())
			}
			btn.Children(func() {
				color := colorMuted()
				if btn.Hovered() {
					color = colorAccent()
				}
				ui.RichText(c, pathRunSpans(q, seg.Label, 11, color)...).SingleLine()
			})
			if btn.Clicked() {
				a.searchBoxClose()
				a.showExplorer()
				a.navigate(seg.Path)
			}
		}
	})
}

// searchReasonBadge says why a hit is in the list: its name matched, or
// only its path did. It is a label, not a control.
func searchReasonBadge(c *ui.Context, kind searchquery.MatchKind) {
	label := kind.Label()
	if label == "" {
		return
	}
	ui.Row(c).Height(16).Padding(0, 6).Radius(8).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).
		Background(colorHover()).Children(func() {
		color := colorMuted()
		if kind == searchquery.MatchName {
			color = colorAccent()
		}
		ui.Text(c, label).FontSize(10).TextColor(color).SingleLine()
	})
}

// elidedPath names the folders the ellipsis stands for, or the full parent
// path when the segments cannot say exactly which ones they hid.
func elidedPath(dir string, segs []pathSeg) string {
	for i, seg := range segs {
		if !seg.Elided {
			continue
		}
		if i == 0 || i+1 >= len(segs) {
			return dir
		}
		rel, ok := searchPathBelow(segs[i+1].Path, segs[i-1].Path)
		if !ok {
			return dir
		}
		names := splitPath(rel)
		if len(names) > 1 {
			names = names[:len(names)-1]
		}
		if len(names) > 0 {
			return strings.Join(names, " › ")
		}
	}
	return dir
}

// searchBoxStatus is the palette's footer, Everything's line: which engine
// answered, how many hits it has and how long it took, the index's own state
// with the 重建索引 button beside it, and the keys.
func (a *App) searchBoxStatus(c *ui.Context) {
	b := &a.searchBox
	keys := "↑↓ 选择   Enter 打开   Ctrl+Enter 定位   Esc 关闭"
	text := "输入即搜索"
	if idx := searchIndexSummary(); idx != "" {
		text += " · " + idx
	}
	if strings.TrimSpace(b.query) != "" {
		secs := ""
		if !b.started.IsZero() {
			to := b.started
			if !b.done.IsZero() {
				to = b.done
			}
			secs = " (" + formatElapsed(to.Sub(b.started)) + ")"
		}
		where := searchIndexSourceNote(b.src)
		if b.busy {
			text = where + " · 正在搜索…  已找到 " + itoa(len(b.results)) + " 个项目" + secs
		} else {
			text = where + " · 找到 " + itoa(len(b.results)) + " 个项目" + searchCapNote(len(b.results)) + secs
		}
	}
	ui.Row(c).FillWidth().Height(24).Gap(8).AlignItems(ui.Center).Children(func() {
		if b.busy {
			components.Spinner(c, components.SpinnerRing)
		}
		ui.Text(c, text).FontSize(11).TextColor(colorMuted()).SingleLine().Grow(1).MinWidth(0)
		if searchIndexOn() {
			btn := ui.ButtonBase(c.Key("search-index-rebuild")).Height(18).Shrink(0).
				Padding(0, 6).Gap(4).Radius(4).AlignItems(ui.Center).Label("重建索引")
			if btn.Hovered() {
				btn.Background(colorHover())
			}
			btn.Children(func() {
				components.Icon(c, lucide.RefreshCw).Size(12, 12).TextColor(colorMuted())
				ui.Text(c, "重建索引").FontSize(11).TextColor(colorMuted()).SingleLine()
			})
			if btn.Clicked() {
				searchIndexRebuild()
				repaint()
			}
		}
		ui.Text(c, keys).FontSize(11).TextColor(colorMuted()).SingleLine().Shrink(0)
	})
}

// searchIndexSourceNote is what the footer says about the engine that answered
// the hits on screen: the live walk, the index, or — for a snapshot that is not
// the whole machine, or is being rebuilt right now — the index with the walk
// running on top of it.
func searchIndexSourceNote(src searchSource) string {
	if src != sourceIndex {
		return "实时遍历"
	}
	st := searchIndexState()
	switch {
	case !st.complete:
		return "索引 + 实时遍历（未完整）"
	case st.building:
		return "索引命中（正在重建）"
	}
	return "索引命中"
}

// formatElapsed is a search's elapsed time: seconds and milliseconds for a
// walk, microseconds for an index hit, which is over before a millisecond is
// up — Everything's own timing, at our scale.
func formatElapsed(d time.Duration) string {
	if d < time.Millisecond {
		if d < time.Microsecond {
			d = time.Microsecond
		}
		return d.Truncate(time.Microsecond).String()
	}
	return d.Truncate(time.Millisecond).String()
}

// searchBoxKeys wires the palette's keyboard. The query box keeps the keys it
// needs to edit, so the ones that walk and open the hits are consumed here:
// Enter opens the hit under the cursor, Ctrl/Cmd+Enter shows it in its folder,
// Escape closes the box.
func (a *App) searchBoxKeys(c *ui.Context, in ui.Element) {
	b := &a.searchBox
	mods := c.Modifiers()
	in.OnTextKey(func(k ui.TextKey) bool {
		switch k.Key {
		case ui.KeyUp:
			a.searchBoxMove(-1)
			return true
		case ui.KeyDown:
			a.searchBoxMove(1)
			return true
		case ui.KeyPageUp:
			a.searchBoxMove(-10)
			return true
		case ui.KeyPageDown:
			a.searchBoxMove(10)
			return true
		case ui.KeyEnter:
			e, ok := b.hit()
			if !ok {
				return true
			}
			if mods&(ui.Ctrl|ui.Cmd) != 0 {
				a.revealSearchHit(e)
			} else {
				a.openSearchHit(e)
			}
			return true
		case ui.KeyEscape:
			a.searchBoxClose()
			return true
		}
		return false
	})
}

// scopeIndex is the scope switch's slot for a scope, and scopeFromIndex its
// inverse: 本文件夹 first, 全部磁盘 second.
func scopeIndex(s searchScope) int {
	if s == scopeGlobal {
		return 1
	}
	return 0
}

func scopeFromIndex(i int) searchScope {
	if i == 1 {
		return scopeGlobal
	}
	return scopeFolder
}
