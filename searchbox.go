package main

import (
	"fmt"
	"log"
	"runtime"
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
	// searchBoxNameW is the 名称 column's width: the folder, size and date
	// columns follow it, so names line up.
	searchBoxNameW float32 = 300
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
	open    bool
	query   string
	scope   searchScope
	results []Entry
	list    ui.ListState
	busy    bool
	started time.Time
	// src is the engine that answered the results on screen (searchSource).
	src searchSource
	// done is when the walk finished; zero while it runs, so the footer can
	// show the elapsed time either way.
	done time.Time
	// cursor is the row the keyboard is on and cursorPath the hit it was on, so
	// a batch that sorts above it does not move the choice. cursorPath stays
	// empty until the user moves the cursor: until then the first row — the
	// first name — stays chosen, whatever streams in above it.
	cursor     int
	cursorPath string
	// walked and walkedScope are the query and scope of the walk in flight or
	// finished: a change to either starts a new one. parsed is that query,
	// ready for the list's hit marking.
	walked      string
	walkedScope searchScope
	parsed      searchquery.Query
	// gen is the walk's generation. Closing the box or typing again bumps it,
	// and the batches of an older walk are dropped.
	gen uint64
	// queue is where the search running off the frame thread hands its results
	// over (frameQueue).
	queue frameQueue
	// seen is the path of every hit already listed, so a hit the index and the
	// walk both found is listed once (addHits).
	seen map[string]bool
}

// searchPanel is the process's one launcher panel. searchPanelApp is the
// window whose folder the 本文件夹 scope means, and whose tabs a hit opens
// in. searchHotkey is whether Ctrl+K is bound for the whole desktop; the
// window's own shortcut stays for a run where that bind failed, and for
// headless tests, which never register it.
var (
	searchPanel    *mygo.Window
	searchPanelApp *App
	searchHotkey   bool
)

// searchPanelSize is the panel's fixed size, in DIPs: wide enough for the
// name, folder, size and date, short of a file-manager window.
const (
	searchPanelW = 760
	searchPanelH = 480
)

// toggleSearchBox is Ctrl+K from anywhere: a second press hides the panel,
// the same way a launcher does. Two deliveries of one key — the global
// bind and the focused window — share this call, so a re-entry does not
// open and immediately close it.
func toggleSearchBox() {
	if searchPanelApp != nil && searchPanelApp.searchBox.open {
		searchPanelApp.searchBoxClose()
		return
	}
	searchHostApp().openSearchBox()
}

// searchHostApp is who a global Ctrl+K searches from: the focused file
// window, else a live one left in the tray, else a new one. It does not
// surface a hidden window — the panel is the thing being summoned.
func searchHostApp() *App {
	if mygo.App.IsReady() {
		for _, w := range mygo.Windows() {
			if w == searchPanel || !w.IsFocused() {
				continue
			}
			if a := appByWindow(w); a != nil {
				return a
			}
		}
	}
	appsMu.Lock()
	var app *App
	for _, a := range apps {
		if a.win != nil && !a.win.IsDestroyed() {
			app = a
			break
		}
	}
	appsMu.Unlock()
	if app != nil {
		return app
	}
	// No file window left: make one to own the search and to open a hit
	// into, and leave it in the tray until a hit is actually chosen.
	app = trayMainApp()
	if app.win != nil {
		app.win.Hide()
	}
	return app
}

func appByWindow(w *mygo.Window) *App {
	appsMu.Lock()
	defer appsMu.Unlock()
	for _, a := range apps {
		if a.win == w {
			return a
		}
	}
	return nil
}

// defaultSearchHotkey is the 快速搜索 chord before the user picks one:
// Command on macOS, Control elsewhere.
const defaultSearchHotkey = "CmdOrCtrl+K"

// boundSearchHotkey is the accelerator currently registered, so a change
// can drop that bind before taking the new one.
var boundSearchHotkey string

// registerSearchHotkey binds the saved 快速搜索 chord for the whole
// desktop. acc is the stored accelerator; empty is the default. A failure
// leaves the window shortcut in place.
func registerSearchHotkey(acc string) {
	if err := applySearchHotkey(acc); err != nil {
		log.Printf("全局搜索快捷键：%v", err)
	}
}

// applySearchHotkey registers acc, replacing whatever was bound. An empty
// acc is the default. It reports a bind the desktop refused; the previous
// chord stays registered in that case.
func applySearchHotkey(acc string) error {
	if strings.TrimSpace(acc) == "" {
		acc = defaultSearchHotkey
	}
	if _, _, ok := parseSearchAccel(acc); !ok {
		return fmt.Errorf("无法识别快捷键 %s", acc)
	}
	if acc == boundSearchHotkey && searchHotkey {
		return nil
	}
	if !mygo.App.IsReady() {
		return nil
	}
	prev, prevOK := boundSearchHotkey, searchHotkey
	if prevOK && prev != "" {
		mygo.GlobalShortcut.Unregister(prev)
		searchHotkey = false
	}
	if err := mygo.GlobalShortcut.Register(acc, toggleSearchBox); err != nil {
		if prevOK && prev != "" {
			if err2 := mygo.GlobalShortcut.Register(prev, toggleSearchBox); err2 == nil {
				searchHotkey = true
				boundSearchHotkey = prev
			}
		}
		return err
	}
	searchHotkey = true
	boundSearchHotkey = acc
	return nil
}

// setSearchHotkey stores a 快速搜索 chord and binds it. Empty restores
// the default. A chord the desktop refuses is not stored.
func (a *App) setSearchHotkey(acc string) {
	if err := applySearchHotkey(acc); err != nil {
		a.statusMsg = "快捷键未能绑定：" + err.Error()
		return
	}
	a.applySettings(func(x *appSettings) {
		if acc == "" || acc == defaultSearchHotkey {
			x.SearchHotkey = ""
		} else {
			x.SearchHotkey = acc
		}
	})
	a.statusMsg = "快速搜索快捷键已改为 " + shortcutLabel(acc)
}

// searchShortcutDown reports the window saw the 快速搜索 chord. The
// default accepts both Ctrl+K and Cmd+K, which is what the tests press
// and what CmdOrCtrl means; a saved chord matches only itself.
func (a *App) searchShortcutDown(c *ui.Context) bool {
	acc := ""
	if a.store != nil {
		acc = a.store.settings().SearchHotkey
	}
	if acc == "" {
		return c.Shortcut(ui.Ctrl, ui.KeyK) || c.Shortcut(ui.Cmd, ui.KeyK)
	}
	mods, key, ok := parseSearchAccel(acc)
	return ok && c.Shortcut(mods, key)
}

// openSearchBox raises the palette, at 全部磁盘 with an empty query: the box is
// the machine-wide search, and 本文件夹 is the switch to narrow it. With an
// app running it is the frosted panel; a headless test only marks it open
// and the window under test draws the body.
func (a *App) openSearchBox() {
	b := &a.searchBox
	if b.open && searchPanelApp == a {
		a.focusSearchPanel()
		return
	}
	searchPanelApp = a
	a.searchBoxCancel()
	b.scope = scopeGlobal
	b.list = ui.ListState{}
	b.open = true
	if !mygo.App.IsReady() {
		return
	}
	a.ensureSearchPanel()
	a.placeSearchPanel()
	searchPanel.Show()
	searchPanel.Focus()
}

// searchBoxClose takes the palette down; the next Ctrl+K starts clean.
func (a *App) searchBoxClose() {
	a.searchBox.open = false
	a.searchBoxCancel()
	if searchPanelApp == a && searchPanel != nil && searchPanel.IsVisible() {
		searchPanel.Hide()
	}
}

// showExplorer brings the file window forward for a hit the user chose.
// Summoning the panel itself does not: it floats over whatever they were
// doing.
func (a *App) showExplorer() {
	if a.win == nil {
		return
	}
	if a.win.IsMinimized() {
		a.win.Restore()
	}
	a.win.Show()
	a.win.Focus()
}

func (a *App) focusSearchPanel() {
	if searchPanel == nil {
		return
	}
	searchPanel.Show()
	searchPanel.Focus()
}

// ensureSearchPanel builds the launcher window the first time it is
// summoned. It is a panel: above other apps, off the Dock and the taskbar,
// and on the Space the user is on. VibrancyAcrylic is the frosted glass —
// what is behind the panel, blurred — and where a desktop has no material
// the view paints the window color instead.
func (a *App) ensureSearchPanel() {
	if searchPanel != nil {
		return
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:         "快速搜索",
		Panel:         true,
		Hidden:        true,
		Frameless:     true,
		Transparent:   true,
		Vibrancy:      mygo.VibrancyAcrylic,
		Width:         searchPanelW,
		Height:        searchPanelH,
		DisableResize: true,
		Content:       ui.View(searchPanelView),
	})
	// Escape, Cmd+W and Alt+F4 hide the panel. A real quit must still be
	// able to destroy it, or the quit would be cancelled by this.
	win.OnClose(func(e *mygo.CloseEvent) {
		if appQuitting {
			return
		}
		e.PreventDefault()
		if searchPanelApp != nil {
			searchPanelApp.searchBoxClose()
		}
	})
	win.OnBlur(func() {
		// Creating the window can blur it before it is ever shown. Only a
		// panel the user is looking at hides itself by losing focus.
		if appQuitting || searchPanel == nil || !searchPanel.IsVisible() || searchPanelApp == nil {
			return
		}
		searchPanelApp.searchBoxClose()
	})
	searchPanel = win
}

// placeSearchPanel sits the panel on the display under the pointer, a
// third of the way down the work area, where a launcher is looked for.
func (a *App) placeSearchPanel() {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	w, h := searchPanel.Size()
	if w == 0 || h == 0 {
		w, h = searchPanelW, searchPanelH
	}
	x := d.WorkArea.X + (d.WorkArea.Width-w)/2
	y := d.WorkArea.Y + (d.WorkArea.Height-h)/3
	searchPanel.SetPosition(x, y)
}

// searchPanelView is the panel's page: the same body the dialog draws in
// a test, on the material where the desktop has one.
func searchPanelView(c *ui.Context) {
	a := searchPanelApp
	if a == nil || !a.searchBox.open {
		return
	}
	c.SetTheme(appTheme(c))
	a.searchBoxFrame()
	root := ui.Box(c).Fill().Padding(8, 10)
	if !c.Vibrancy() {
		root.Background(colorWindow())
	}
	root.Children(func() { a.searchBoxBody(c) })
}

// searchBoxCancel drops the box's walk and results, however it closed. The
// generation bump is what stops a walk still running in the background.
func (a *App) searchBoxCancel() {
	b := &a.searchBox
	b.gen++
	b.query = ""
	b.walked = ""
	b.results = nil
	b.seen = nil
	b.busy = false
	b.src = sourceNone
	b.cursor, b.cursorPath = 0, ""
	b.started, b.done = time.Time{}, time.Time{}
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
		return
	}
	b.walked, b.walkedScope = q, b.scope
	b.parsed = searchquery.Parse(q)
	b.results = nil
	b.seen = nil
	b.cursor, b.cursorPath = 0, ""
	b.done = time.Time{}
	b.src = sourceNone
	b.gen++
	gen := b.gen
	if q == "" {
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
	query := searchquery.Parse(q)
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
	if b.cursor < 0 || b.cursor >= len(b.results) {
		return Entry{}, false
	}
	return b.results[b.cursor], true
}

// addHits folds a streamed batch into the list, which stays in Everything's
// order — by name — and keeps the cursor on the hit it was on: rows above it
// move as hits arrive, the choice does not. A hit already listed — the index and
// the walk behind it can find the same file — is folded in once.
func (b *searchBoxState) addHits(batch []Entry) {
	added := false
	for _, e := range batch {
		if e.Path == "" || b.seen[e.Path] {
			continue
		}
		if b.seen == nil {
			b.seen = make(map[string]bool, len(batch))
		}
		b.seen[e.Path] = true
		b.results = append(b.results, e)
		added = true
	}
	if !added {
		return
	}
	sortHitsByName(b.results)
	if i := b.cursorRow(); i != b.cursor {
		b.cursor = i
		b.list.ScrollIntoView(i)
	}
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
		return 0
	}
	for i, e := range b.results {
		if e.Path == b.cursorPath {
			return i
		}
	}
	b.cursorPath = ""
	return 0
}

// searchBoxMove walks the cursor by d hits, held to the ends of the list, and
// scrolls the hit it lands on into view.
func (a *App) searchBoxMove(d int) {
	b := &a.searchBox
	if len(b.results) == 0 {
		return
	}
	i := min(max(b.cursor+d, 0), len(b.results)-1)
	if i == b.cursor {
		return
	}
	b.cursor, b.cursorPath = i, b.results[i].Path
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
	listH := min(searchBoxListMax, max(searchBoxListMin, h-140))
	ui.Column(c).Gap(0).FillWidth().MinWidth(420).Children(func() {
		row := ui.Row(c).FillWidth().Height(44).Gap(8).AlignItems(ui.Center)
		row.Children(func() {
			components.Icon(c, lucide.Search).Size(16, 16).TextColor(colorMuted())
			in := ui.TextInputBase(c, &b.query).Grow(1).MinWidth(0).Height(28).FontSize(13).
				AutoFocus().
				Placeholder("搜索全部磁盘：多个词是 AND，ext:txt  path:docs  \"完整短语\"  case:")
			a.searchBoxKeys(c, in)
			idx := scopeIndex(b.scope)
			if ui.Segmented(c, &idx, "本文件夹", "全部磁盘").Shrink(0).Changed() {
				b.scope = scopeFromIndex(idx) // the next frame starts the walk
				repaint()
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
		if len(b.results) == 0 {
			a.searchBoxNotice(c, listH)
		} else {
			ui.List(c, &b.list, len(b.results), func(i int) {
				a.searchHitRow(c, i)
			}).FillWidth().Height(listH).MinHeight(0)
		}
		a.searchBoxStatus(c)
	})
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

// searchHitRow is one hit: its icon and name, the folder it lives in, its size,
// its date and the 位置 button. The row under the cursor is highlighted; a click
// moves the cursor to it, a double click opens it, as Enter does, and 位置
// jumps to the folder it lives in with it selected, as Ctrl+Enter does.
func (a *App) searchHitRow(c *ui.Context, i int) {
	b := &a.searchBox
	e := b.results[i]
	row := ui.ButtonBase(c.Key("hit-"+e.Path)).FillWidth().Height(26).
		Padding(0, 4).Gap(8).AlignItems(ui.Center).Radius(4).Label(e.Name)
	switch {
	case i == b.cursor:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		glyph := ui.Box(c).Size(16, 16).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, e, 16)
		})
		searchNameText(c, b.parsed, a.displayName(e), 12).
			SingleLine().Width(searchBoxNameW).Shrink(0)
		ui.Text(c, tailRunes(e.Dir, 42)).FontSize(11).TextColor(colorMuted()).SingleLine().
			Grow(1).MinWidth(0)
		if !e.IsDir {
			ui.Text(c, sizeColumnText(e)).FontSize(11).TextColor(colorMuted()).SingleLine().
				Width(72).Shrink(0).TextAlign(ui.End)
		}
		ui.Text(c, formatModTime(e.ModTime)).FontSize(11).TextColor(colorMuted()).SingleLine().
			Width(84).Shrink(0)
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

// tailRunes is s cut to n runes from the end, with an ellipsis where the cut
// was: a hit's folder reads from the folders nearest it, which is what tells
// two hits apart.
func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// accelFromKey is the accelerator a captured keydown stores. A bare letter
// or digit is refused — it would steal typing — while function keys may
// stand alone. The text is what GlobalShortcut.Register parses.
func accelFromKey(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, ok := accelKeyToken(key)
	if !ok || key == ui.KeyEscape {
		return "", false
	}
	if mods == 0 && (key < ui.KeyF1 || key > ui.KeyF12) {
		return "", false
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{
		{ui.Super, "Super"},
		{ui.Ctrl, "Ctrl"},
		{ui.Alt, "Alt"},
		{ui.Shift, "Shift"},
	} {
		if mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, name), "+"), true
}

// parseSearchAccel splits an accelerator we stored, or the default, into
// the modifiers and key the window shortcut matches. CmdOrCtrl is Command
// on macOS and Control elsewhere.
func parseSearchAccel(acc string) (ui.Modifiers, ui.Key, bool) {
	if strings.TrimSpace(acc) == "" {
		acc = defaultSearchHotkey
	}
	parts := strings.Split(acc, "+")
	if strings.HasSuffix(acc, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return 0, 0, false
	}
	key, ok := accelTokenKey(parts[len(parts)-1])
	if !ok {
		return 0, 0, false
	}
	var mods ui.Modifiers
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "ctrl", "control":
			mods |= ui.Ctrl
		case "alt", "option":
			mods |= ui.Alt
		case "shift":
			mods |= ui.Shift
		case "super", "cmd", "command", "meta", "win":
			mods |= ui.Super
		case "cmdorctrl", "commandorcontrol":
			mods |= ui.Cmd
		default:
			return 0, 0, false
		}
	}
	return mods, key, true
}

// shortcutLabel is the chord the settings row shows: macOS modifier
// symbols, and Ctrl+Shift+K elsewhere.
func shortcutLabel(acc string) string {
	mods, key, ok := parseSearchAccel(acc)
	if !ok {
		if strings.TrimSpace(acc) == "" {
			return "Ctrl+K"
		}
		return acc
	}
	keyText, _ := accelKeyToken(key)
	if len(keyText) == 1 {
		keyText = strings.ToUpper(keyText)
	}
	if runtime.GOOS == "darwin" {
		var b strings.Builder
		if mods&ui.Ctrl != 0 {
			b.WriteString("⌃")
		}
		if mods&ui.Alt != 0 {
			b.WriteString("⌥")
		}
		if mods&ui.Shift != 0 {
			b.WriteString("⇧")
		}
		if mods&ui.Super != 0 {
			b.WriteString("⌘")
		}
		b.WriteString(keyText)
		return b.String()
	}
	var parts []string
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Win")
	}
	return strings.Join(append(parts, keyText), "+")
}

func accelKeyToken(key ui.Key) (string, bool) {
	switch {
	case key >= ui.KeyA && key <= ui.KeyZ:
		return string(rune('A' + key - ui.KeyA)), true
	case key >= ui.Key0 && key <= ui.Key9:
		return string(rune('0' + key - ui.Key0)), true
	case key >= ui.KeyF1 && key <= ui.KeyF12:
		return fmt.Sprintf("F%d", int(key-ui.KeyF1)+1), true
	}
	switch key {
	case ui.KeySpace:
		return "Space", true
	case ui.KeyTab:
		return "Tab", true
	case ui.KeyEnter:
		return "Enter", true
	case ui.KeyBackspace:
		return "Backspace", true
	case ui.KeyDelete:
		return "Delete", true
	case ui.KeyInsert:
		return "Insert", true
	case ui.KeyHome:
		return "Home", true
	case ui.KeyEnd:
		return "End", true
	case ui.KeyPageUp:
		return "PageUp", true
	case ui.KeyPageDown:
		return "PageDown", true
	case ui.KeyUp:
		return "Up", true
	case ui.KeyDown:
		return "Down", true
	case ui.KeyLeft:
		return "Left", true
	case ui.KeyRight:
		return "Right", true
	case ui.KeyMinus:
		return "-", true
	case ui.KeyEqual:
		return "=", true
	case ui.KeyComma:
		return ",", true
	case ui.KeyPeriod:
		return ".", true
	case ui.KeySlash:
		return "/", true
	case ui.KeySemicolon:
		return ";", true
	case ui.KeyQuote:
		return "'", true
	case ui.KeyBracketLeft:
		return "[", true
	case ui.KeyBracketRight:
		return "]", true
	case ui.KeyBackslash:
		return "\\", true
	case ui.KeyBackquote:
		return "`", true
	default:
		return "", false
	}
}

func accelTokenKey(token string) (ui.Key, bool) {
	switch strings.ToLower(token) {
	case "space":
		return ui.KeySpace, true
	case "tab":
		return ui.KeyTab, true
	case "enter", "return":
		return ui.KeyEnter, true
	case "backspace":
		return ui.KeyBackspace, true
	case "delete":
		return ui.KeyDelete, true
	case "insert":
		return ui.KeyInsert, true
	case "home":
		return ui.KeyHome, true
	case "end":
		return ui.KeyEnd, true
	case "pageup":
		return ui.KeyPageUp, true
	case "pagedown":
		return ui.KeyPageDown, true
	case "up":
		return ui.KeyUp, true
	case "down":
		return ui.KeyDown, true
	case "left":
		return ui.KeyLeft, true
	case "right":
		return ui.KeyRight, true
	case "-":
		return ui.KeyMinus, true
	case "=":
		return ui.KeyEqual, true
	case ",", "comma":
		return ui.KeyComma, true
	case ".", "period":
		return ui.KeyPeriod, true
	case "/", "slash":
		return ui.KeySlash, true
	case ";", "semicolon":
		return ui.KeySemicolon, true
	case "'", "quote":
		return ui.KeyQuote, true
	case "[":
		return ui.KeyBracketLeft, true
	case "]":
		return ui.KeyBracketRight, true
	case "\\", "backslash":
		return ui.KeyBackslash, true
	case "`":
		return ui.KeyBackquote, true
	}
	if len(token) == 1 {
		r := []rune(strings.ToUpper(token))[0]
		switch {
		case r >= 'A' && r <= 'Z':
			return ui.KeyA + ui.Key(r-'A'), true
		case r >= '0' && r <= '9':
			return ui.Key0 + ui.Key(r-'0'), true
		}
	}
	if len(token) >= 2 && (token[0] == 'F' || token[0] == 'f') {
		n := 0
		for _, r := range token[1:] {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		if n >= 1 && n <= 12 {
			return ui.KeyF1 + ui.Key(n-1), true
		}
	}
	return 0, false
}
