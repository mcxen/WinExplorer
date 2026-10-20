package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/searchquery"
)

// searchbox_test.go drives the Ctrl+K palette in a laid-out window: the
// shortcut, the streamed hits of its list, the cursor keys and the ways out.

// waitSearchBox runs frames until the box's search finishes, bounded so a wedged
// search fails the test instead of hanging it. The frames are the point: the
// box applies what the search hands over (frameQueue) in a frame, so a test that
// wants the hits must pump them.
func waitSearchBox(t *testing.T, tt *ui.Tester, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if !a.searchBox.busy {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the search box's walk never finished")
}

// TestCtrlKSearchBox raises the palette with Ctrl+K, searches the tab's folder
// — the whole machine is far too big to walk in a test, so it takes the folder
// scope the switch offers — and opens a hit.
func TestCtrlKSearchBox(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	// Nested hits keep the palette's labels unique: the pane shows the
	// folders but not the files below them.
	for _, sub := range []string{"alpha-dir", "beta-dir", "gamma-dir"} {
		subdir := filepath.Join(dir, sub, "inner-"+sub)
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "nested-"+sub+".txt"), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !a.searchBox.open {
		t.Fatal("Ctrl+K did not raise the search box")
	}
	// The box is the machine-wide one: it starts at 全部磁盘.
	if a.searchBox.scope != scopeGlobal {
		t.Fatalf("the box opened at scope %v, want 全部磁盘", a.searchBox.scope)
	}
	a.searchBox.scope = scopeFolder
	tt.Type("nested")
	tt.Frame()
	waitSearchBox(t, tt, a)
	if len(a.searchBox.results) != 3 {
		t.Fatalf("the box found %d hits, want the folder's three", len(a.searchBox.results))
	}
	if !tt.HasText("nested-") {
		t.Fatalf("the list does not show its hits; the window shows %v", tt.Texts())
	}
	// The hits are laid out as rows: two of them stack at the same left edge,
	// 44 DIP apart — the list inside the dialog has a real height.
	firstRow, okFirst := tt.Find(a.searchBox.results[0].Name + " 文件名")
	secondRow, okSecond := tt.Find(a.searchBox.results[1].Name + " 文件名")
	if !okFirst || !okSecond {
		t.Fatalf("the hits are not in the window: %v / %v", firstRow, secondRow)
	}
	if firstRow.X != secondRow.X {
		t.Fatalf("the rows are not in one column: %v / %v", firstRow, secondRow)
	}
	if d := secondRow.Y - firstRow.Y; d < 40 || d > 50 {
		t.Fatalf("the rows are %v apart, want a 44 DIP row", d)
	}
	for _, hit := range a.searchBox.results {
		if _, ok := tt.Find(hit.Name); !ok {
			t.Fatalf("the palette does not show the complete name %q: %v", hit.Name, tt.Texts())
		}
	}
	// The row's tooltip is its full path, not the breadcrumb it can shorten.
	row, ok := tt.Find(a.searchBox.results[0].Name)
	if !ok {
		t.Fatal("the first palette row vanished")
	}
	tt.Move(2, 2)
	tt.Advance(time.Second)
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Advance(time.Second)
	if !tt.HasText(a.searchBox.results[0].Path) {
		t.Fatalf("the row does not show its full path as a tooltip: %v", tt.Texts())
	}

	// The cursor walks the hits and holds at the ends. The keys stay with the
	// box: the pane's own selection is untouched.
	tt.Key(0, ui.KeyUp)
	if a.searchBox.cursor != 0 {
		t.Fatalf("↑ from the first hit moved the cursor to %d", a.searchBox.cursor)
	}
	tt.Key(0, ui.KeyDown)
	if a.searchBox.cursor != 1 {
		t.Fatalf("↓ moved the cursor to %d, want 1", a.searchBox.cursor)
	}
	if n := len(a.cur().selected); n != 0 {
		t.Fatalf("the box's ↑/↓ moved the pane's selection to %d items", n)
	}
	hit, ok := a.searchBox.hit()
	if !ok {
		t.Fatal("no hit under the cursor")
	}

	// Ctrl+Enter shows the hit in the pane: the tab selects it, and the box
	// takes itself down.
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("the box stayed open after Ctrl+Enter")
	}
	navIs(t, a, "Ctrl+Enter on a hit", hit.Name)
	if a.cur().path != hit.Dir {
		t.Fatalf("the tab is in %q, want the hit's folder %q", a.cur().path, hit.Dir)
	}

	// Escape closes the box, and the next Ctrl+K starts from an empty one.
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	if !a.searchBox.open {
		t.Fatal("Ctrl+K did not raise the box a second time")
	}
	tt.Type("f0")
	tt.Frame()
	if a.searchBox.query != "f0" {
		t.Fatalf("query = %q, want what was typed", a.searchBox.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("Escape did not close the box")
	}
	if a.searchBox.query != "" || len(a.searchBox.results) != 0 {
		t.Fatalf("the closed box kept query %q and %d hits", a.searchBox.query, len(a.searchBox.results))
	}
}

// TestSearchDetailsShowRelativeLocation: the pane's 位置 column keeps the
// folders below the search root, not just the folder holding the hit.
func TestSearchDetailsShowRelativeLocation(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	nested := filepath.Join(dir, "reports", "2026", "q4")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "needle.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.mode = ViewDetails
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyE)
	tt.Type("needle")
	tt.Frame()
	waitSearch(t, a)
	tt.Frame()
	if !tt.HasText("reports › 2026 › q4") {
		t.Fatalf("the 位置 column does not keep the relative path: %v", tt.Texts())
	}
}

// TestSearchBoxScopeSwitchIsTheWalkKey pins the switch: the same query over
// another scope starts a new walk, and the two scopes are the ones searchRoots
// knows.
func TestSearchBoxScopeSwitchIsTheWalkKey(t *testing.T) {
	if scopeIndex(scopeFolder) == scopeIndex(scopeGlobal) {
		t.Fatal("both scopes share a switch slot")
	}
	if scopeFromIndex(scopeIndex(scopeFolder)) != scopeFolder ||
		scopeFromIndex(scopeIndex(scopeGlobal)) != scopeGlobal {
		t.Fatal("the switch does not round-trip a scope")
	}
	root := t.TempDir()
	a := newTestApp(t, root)
	a.reload()
	a.searchBox.open = true
	a.searchBox.query = "alpha"
	a.searchBox.scope = scopeFolder
	a.searchBoxFrame()
	if a.searchBox.walkedScope != scopeFolder {
		t.Fatalf("the walk recorded scope %v", a.searchBox.walkedScope)
	}
	// Switching the scope restarts the walk for the same query.
	a.searchBox.scope = scopeGlobal
	a.searchBoxFrame()
	if a.searchBox.walkedScope != scopeGlobal || a.searchBox.walked != "alpha" {
		t.Fatalf("scope change did not restart the walk: %q in %v", a.searchBox.walked, a.searchBox.walkedScope)
	}
	a.searchBoxClose() // the whole-machine walk stops with the box
}

// TestSearchBoxSortSwitch: the palette starts by relevance and the 名称
// switch restores Everything's name order without restarting the walk.
func TestSearchBoxSortSwitch(t *testing.T) {
	now := time.Now()
	b := &searchBoxState{parsed: searchquery.Parse("report")}
	b.addHits([]Entry{
		{Name: "final-report.txt", Path: "/work/final-report.txt", ModTime: now},
		{Name: "report.docx", Path: "/work/report.docx", ModTime: now},
		{Name: "report-final.docx", Path: "/work/report-final.docx", ModTime: now},
		{Name: "report.md", Path: "/work/report.md", ModTime: now},
	})
	got := hitNames(b.results)
	want := []string{"report.docx", "report.md", "report-final.docx", "final-report.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("relevance order = %v, want %v", got, want)
		}
	}
	b.sortBy = sortName
	b.sortHits()
	got = hitNames(b.results)
	want = []string{"final-report.txt", "report-final.docx", "report.docx", "report.md"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("name order = %v, want %v", got, want)
		}
	}
	b.sortBy = sortRelevance
	b.sortHits()
	if got := hitNames(b.results); got[0] != "report.docx" {
		t.Fatalf("switching back to relevance = %v", got)
	}
}

// TestSearchBoxSortSegmentIsOnScreen: the relevance/folder/type/name switch is
// in the query row and changing it reorders the visible hits.
func TestSearchBoxSortSegmentIsOnScreen(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	sortLabels := append(tt.Texts()[:0:0], tt.Texts()...)
	if !tt.HasText("相关度") || !tt.HasText("按文件夹") || !tt.HasText("按类型") || !tt.HasText("名称") {
		t.Fatalf("the sort switch is missing: %v", tt.Texts())
	}
	// The pane's 名称 column has the same label earlier in the walk: click the
	// segment after 相关度, which is the palette's switch.
	first, ok := tt.Find("相关度")
	if !ok {
		t.Fatalf("the relevance segment is not built: %v", sortLabels)
	}
	// Use the type segment's rectangle to reach the name segment: the pane's
	// own 名称 column can also be present in the window.
	typ, okType := tt.Find("按类型")
	if !okType {
		t.Fatalf("the type segment is not built: %v", tt.Texts())
	}
	// The labels are text boxes inside padded segments: stepping past the
	// type label's padding lands in the center of the next segment's name.
	tt.ClickAt(typ.X+typ.W+24, first.Y+first.H/2)
	tt.Frame()
	if a.searchBox.sortBy != sortName {
		t.Fatalf("sort = %v, want name after clicking 名称", a.searchBox.sortBy)
	}
}

func TestSearchBoxGroupRows(t *testing.T) {
	now := time.Now()
	folderResults := []Entry{
		{Name: "b.md", Path: "/work/reports/b.md", Dir: "/work/reports", Ext: "md", ModTime: now},
		{Name: "a.txt", Path: "/work/reports/a.txt", Dir: "/work/reports", Ext: "txt", ModTime: now},
		{Name: "e.txt", Path: "/work/archive/e.txt", Dir: "/work/archive", Ext: "txt", ModTime: now},
		{Name: "c.png", Path: "/work/archive/c.png", Dir: "/work/archive", Ext: "png", ModTime: now},
		{Name: "d.pdf", Path: "/work/archive/d.pdf", Dir: "/work/archive", Ext: "pdf", ModTime: now},
	}
	b := &searchBoxState{sortBy: sortFolder, results: folderResults}
	b.sortHits()
	groups := groupedRows(b.rows)
	if len(groups) != 2 {
		t.Fatalf("folder headings = %d, want 2: %+v", len(groups), groups)
	}
	if groups[0].count != 3 || groups[1].count != 2 {
		t.Fatalf("folder group counts = %+v, want archive=3 reports=2", groups)
	}
	for _, g := range groups {
		if !strings.HasSuffix(g.label, " · "+itoa(g.count)) {
			t.Fatalf("folder heading %q does not show %d items", g.label, g.count)
		}
	}

	typeResults := []Entry{
		{Name: "b.md", Path: "/work/b.md", Ext: "md", ModTime: now},
		{Name: "a.txt", Path: "/work/a.txt", Ext: "txt", ModTime: now},
		{Name: "c.png", Path: "/work/c.png", Ext: "png", ModTime: now},
		{Name: "folder", Path: "/work/folder", IsDir: true, ModTime: now},
	}
	b = &searchBoxState{sortBy: sortType, results: typeResults}
	b.sortHits()
	groups = groupedRows(b.rows)
	got := make(map[string]int, len(groups))
	for _, g := range groups {
		got[g.label] = g.count
	}
	want := map[string]int{"文本文档 · 2": 2, "PNG 文件 · 1": 1, "文件夹 · 1": 1}
	if len(groups) != len(want) {
		t.Fatalf("type headings = %+v, want one per type %v", groups, want)
	}
	for label, count := range want {
		if got[label] != count {
			t.Fatalf("type group %q = %d items, want %d: %v", label, got[label], count, got)
		}
	}
}

func TestSearchBoxMoveSkipsGroupHeadings(t *testing.T) {
	now := time.Now()
	a := &App{searchBox: searchBoxState{
		sortBy: sortFolder,
		results: []Entry{
			{Name: "a.txt", Path: "/work/reports/a.txt", Dir: "/work/reports", Ext: "txt", ModTime: now},
			{Name: "b.txt", Path: "/work/reports/b.txt", Dir: "/work/reports", Ext: "txt", ModTime: now},
			{Name: "c.txt", Path: "/work/archive/c.txt", Dir: "/work/archive", Ext: "txt", ModTime: now},
		},
	}}
	a.searchBox.sortHits()
	a.searchBox.cursor = 0 // The list starts on the first heading.

	for _, step := range []struct {
		move int
		row  int
		name string
	}{
		{1, 1, "c.txt"},
		{1, 3, "a.txt"},
		{1, 4, "b.txt"},
		{-1, 3, "a.txt"},
		{-1, 1, "c.txt"},
	} {
		a.searchBoxMove(step.move)
		if a.searchBox.cursor != step.row {
			t.Fatalf("move %+v landed on display row %d, want %d", step, a.searchBox.cursor, step.row)
		}
		hit, ok := a.searchBox.hit()
		if !ok || hit.Name != step.name {
			t.Fatalf("move %+v selected %+v, want %q", step, hit, step.name)
		}
	}
}

// BenchmarkSearchBoxGroupSwitch10000 is the frame budget for changing the
// grouping of a full result set: sorting and rebuilding the display rows must
// stay below 16ms even before the next paint.
func BenchmarkSearchBoxGroupSwitch10000(b *testing.B) {
	now := time.Now()
	results := make([]Entry, 10000)
	for i := range results {
		ext := []string{"txt", "md", "png", "pdf"}[i%4]
		dir := filepath.Join("/work", "folder-"+itoa(i%100))
		results[i] = Entry{
			Name:    "result-" + itoa(i) + "." + ext,
			Path:    filepath.Join(dir, "result-"+itoa(i)+"."+ext),
			Dir:     dir,
			Ext:     ext,
			ModTime: now.Add(-time.Duration(i) * time.Second),
		}
	}

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		state := &searchBoxState{sortBy: sortFolder, results: slices.Clone(results)}
		state.sortHits()
	}
	b.StopTimer()
	avg := time.Since(start) / time.Duration(b.N)
	if avg >= 16*time.Millisecond {
		b.Fatalf("group switch averaged %v, want < 16ms", avg)
	}
}

type searchRowGroup struct {
	label string
	count int
}

func groupedRows(rows []searchDisplayRow) []searchRowGroup {
	var out []searchRowGroup
	for _, row := range rows {
		if row.heading {
			out = append(out, searchRowGroup{label: row.label, count: row.count})
		}
	}
	return out
}

// TestSearchPathSegmentClick: a breadcrumb under a hit is a mouse shortcut to
// that folder: the panel closes, the tab navigates there, and the row's cursor
// does not move first.
func TestSearchPathSegmentClick(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	nested := filepath.Join(dir, "reports", "2026", "q4")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "needle.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("needle")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if len(a.searchBox.results) != 1 {
		t.Fatalf("hits = %v, want needle.txt", namesOf(a.searchBox.results))
	}
	before := a.searchBox.cursor
	link := a.searchBox.results[0]
	segs := searchPathSegments(link.Dir, homeForSearch(), 5)
	if len(segs) < 2 {
		t.Fatalf("segments = %+v, want a breadcrumb", segs)
	}
	// Click the second-to-last segment, which is 2026: its label is unique
	// in the palette's path line.
	want := segs[len(segs)-2]
	r, ok := tt.Find(want.Label)
	if !ok {
		t.Fatalf("the %q segment is not built: %v", want.Label, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("the palette stayed open after a breadcrumb click")
	}
	if a.cur().path != want.Path {
		t.Fatalf("the tab is in %q, want the segment %q", a.cur().path, want.Path)
	}
	if a.searchBox.cursor != before {
		t.Fatalf("the breadcrumb moved the palette cursor to %d", a.searchBox.cursor)
	}
}

// TestSearchPathElidedSegmentHasNoDestination: the middle ellipsis is not a
// button, and its tooltip names the hidden folders.
func TestSearchPathElidedSegmentHasNoDestination(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "alice")
	dir := filepath.Join(home, "Library", "Caches", "com", "app", "very", "deep", "nested", "folder", "structure")
	segs := searchPathSegments(dir, home, 5)
	elided := -1
	for i := range segs {
		if segs[i].Elided {
			elided = i
		}
	}
	if elided < 0 {
		t.Fatalf("segments = %+v, want one elided segment", segs)
	}
	want := "Caches › com › app › very › deep › nested"
	if got := elidedPath(dir, segs); got != want {
		t.Fatalf("elided tooltip = %q, want %q", got, want)
	}
}

// TestSearchReasonBadge: a hit matched by its name says 文件名, and a hit
// that only its path satisfies says 路径. A plain-name hit never grows the
// path label merely because the same letters appear in its folder.
func TestSearchReasonBadge(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	if err := os.MkdirAll(filepath.Join(dir, "reports", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reports", "alpha", "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder

	tt.Type("notes")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if got := reasonLabelOf(t, a, "notes.txt"); got != "文件名" {
		t.Fatalf("name hit label = %q, want 文件名", got)
	}
	if !tt.HasText("文件名") || tt.HasText("路径") {
		t.Fatalf("the window's labels = %v, want only 文件名", tt.Texts())
	}

	// The query starts over: notes is in the file name, so the old query
	// would win the name match before path:alpha could be examined.
	a.searchBoxClose()
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("path:alpha")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if got := reasonLabelOf(t, a, "notes.txt"); got != "路径" {
		t.Fatalf("path hit label = %q, want 路径", got)
	}
	if !tt.HasText("路径") || tt.HasText("文件名") {
		t.Fatalf("the window's labels = %v, want only 路径", tt.Texts())
	}
}

func reasonLabelOf(t *testing.T, a *App, name string) string {
	t.Helper()
	for _, e := range a.searchBox.results {
		if e.Name == name {
			return a.searchBox.kinds[e.Path].Label()
		}
	}
	t.Fatalf("the palette does not list %q: %v", name, namesOf(a.searchBox.results))
	return ""
}

// TestSearchFilterCount: 类型 narrows the visible hits without walking the
// disk again, and the filter bar's count says x / y for the kept and total
// matches. Switching back to 全部类型 restores the rest.
func TestSearchFilterCount(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	writeFiles(t, dir, map[string]string{
		"alpha.txt": "x",
		"alpha.png": "x",
		"alpha.zip": "x",
	})
	a.reload()
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("alpha")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if len(a.searchBox.results) != 3 {
		t.Fatalf("hits = %v, want all three", namesOf(a.searchBox.results))
	}
	started := a.searchBox.started
	gen := a.searchBox.gen
	if err := tt.Click("类型：全部类型"); err != nil {
		t.Fatalf("类型 filter did not open: %v", err)
	}
	tt.Frame()
	if menu := tt.Menu(); !slices.Contains(menu, "文档") {
		t.Fatalf("类型 menu = %v, want 文档", menu)
	}
	if err := tt.ChooseMenuItem("文档"); err != nil {
		t.Fatalf("类型 menu did not choose 文档: %v", err)
	}
	waitSearchBox(t, tt, a)
	tt.Frame()
	if got := namesOf(a.searchBox.results); len(got) != 1 || got[0] != "alpha.txt" {
		t.Fatalf("文档 filter = %v, want alpha.txt", got)
	}
	if a.searchBox.gen != gen {
		t.Fatal("a filter change started a second walk")
	}
	if a.searchBox.started != started {
		t.Fatal("a filter change restarted the walk clock")
	}
	if !tt.HasText("1 / 3 个结果") {
		t.Fatalf("the count is missing: %v", tt.Texts())
	}
	if err := tt.Click("类型：文档"); err != nil {
		t.Fatalf("类型 filter did not reopen: %v", err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("全部类型"); err != nil {
		t.Fatalf("类型 menu did not restore all: %v", err)
	}
	waitSearchBox(t, tt, a)
	tt.Frame()
	if len(a.searchBox.results) != 3 {
		t.Fatalf("removing the filter left %v", namesOf(a.searchBox.results))
	}
}

// TestSearchFilterModifiers: the bar and the query language are one thing:
// the same dm:, size: and kind: modifiers answer a typed query.
func TestSearchFilterModifiers(t *testing.T) {
	got := searchQueryWithFilters("report", searchFilters{
		kind: kindDocumentFilter,
		when: timeWeekFilter,
		size: sizeLargeFilter,
	})
	if !strings.Contains(got, "kind:document") || !strings.Contains(got, "dm:7d") ||
		!strings.Contains(got, "size:>10mb") {
		t.Fatalf("query with filters = %q", got)
	}
	if got := searchQueryWithFilters("", searchFilters{kind: kindFolderFilter}); got != "folder:" {
		t.Fatalf("folder filter alone = %q", got)
	}
	walk := searchWalkQuery("report", searchFilters{kind: kindDocumentFilter})
	if strings.Contains(walk, "kind:") || !strings.Contains(walk, "report") {
		t.Fatalf("the walk query kept the type filter: %q", walk)
	}
}

// TestSearchBoxCursorStaysOnItsHit: a batch that sorts above the cursor moves
// the rows but not the choice, and a cursor the user never moved stays on the
// first row as hits arrive above it.
func TestSearchBoxCursorStaysOnItsHit(t *testing.T) {
	b := &searchBoxState{}
	b.addHits([]Entry{{Name: "bravo", Path: "/b"}, {Name: "delta", Path: "/d"}})
	if b.cursor != 0 || hitNames(b.results)[0] != "bravo" {
		t.Fatalf("the list starts at %v with the cursor on %d", hitNames(b.results), b.cursor)
	}
	// Nothing chose a row yet: the first row stays chosen as hits sort in.
	b.addHits([]Entry{{Name: "alpha", Path: "/a"}})
	if b.cursor != 0 {
		t.Fatalf("an unchosen cursor left the first row: row %d of %v", b.cursor, hitNames(b.results))
	}
	// The user walks down to delta; the next batch keeps the cursor on it.
	b.cursor, b.cursorPath = 2, "/d"
	b.addHits([]Entry{{Name: "aaa", Path: "/aaa"}})
	if b.results[b.cursor].Path != "/d" {
		t.Fatalf("the cursor moved off its hit: row %d of %v", b.cursor, hitNames(b.results))
	}
	// A hit the list no longer holds leaves the cursor at the top.
	b.cursorPath = "/gone"
	if row := b.cursorRow(); row != 0 || b.cursorPath != "" {
		t.Fatalf("a lost hit left the cursor at %d with path %q", row, b.cursorPath)
	}
}

// hitNames lists hits by name, the palette's own order.
func hitNames(hits []Entry) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return out
}

// TestSearchCapNote: a list that stopped at the walker's cap says so.
func TestSearchCapNote(t *testing.T) {
	if note := searchCapNote(3); note != "" {
		t.Fatalf("a short list carries the note %q", note)
	}
	if note := searchCapNote(searchMaxResults); !strings.Contains(note, itoa(searchMaxResults)) {
		t.Fatalf("the cap note is %q, want the cap in it", note)
	}
}

// TestSearchHotkeyChord pins what the settings recorder stores and what
// the row then shows: a bare letter is refused, a chord round-trips, and
// the default reads as Command on macOS and Control elsewhere.
func TestSearchHotkeyChord(t *testing.T) {
	if _, ok := accelFromKey(0, ui.KeyK); ok {
		t.Fatal("a bare letter was accepted as a shortcut")
	}
	acc, ok := accelFromKey(ui.Ctrl|ui.Shift, ui.KeyK)
	if !ok || acc != "Ctrl+Shift+K" {
		t.Fatalf("chord = %q, ok %v", acc, ok)
	}
	mods, key, ok := parseSearchAccel(acc)
	if !ok || mods != ui.Ctrl|ui.Shift || key != ui.KeyK {
		t.Fatalf("parsed %q as mods %v key %v", acc, mods, key)
	}
	f, ok := accelFromKey(0, ui.KeyF5)
	if !ok || f != "F5" {
		t.Fatalf("function key = %q, ok %v", f, ok)
	}
	label := shortcutLabel("")
	if runtime.GOOS == "darwin" {
		if label != "⌘K" {
			t.Fatalf("default label = %q, want ⌘K", label)
		}
	} else if label != "Ctrl+K" {
		t.Fatalf("default label = %q, want Ctrl+K", label)
	}
}
