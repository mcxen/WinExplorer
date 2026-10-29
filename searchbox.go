package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

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
	// cancel stops the walk of generation gen; a new query or closing the
	// box calls it, so the old walkers exit instead of running to the deadline.
	cancel context.CancelFunc
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
