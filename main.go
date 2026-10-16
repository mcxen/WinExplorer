package main

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// apps is every open window's App, so closing a window can stop its
// preview's audio and drop its state.
var (
	appsMu sync.Mutex
	apps   []*App
)

func registerApp(a *App) {
	appsMu.Lock()
	defer appsMu.Unlock()
	apps = append(apps, a)
}

// unregisterApp retires a closed window's app: audio stops, tickers end,
// and the tab state becomes garbage.
func unregisterApp(a *App) {
	appsMu.Lock()
	for i, x := range apps {
		if x == a {
			apps = append(apps[:i], apps[i+1:]...)
			break
		}
	}
	appsMu.Unlock()
	if st := a.preview.Load(); st != nil && st.media.player != nil {
		st.media.player.stop()
	}
	a.quickLook = false
}

func main() {
	app := newApp()
	registerApp(app)
	mygo.App.WhenReady(func() {
		win := newAppWindow(app)
		win.OnClosed(func() { unregisterApp(app) })
		if app.remoteConf().Enabled {
			go app.remoteStart()
		}
	})
	// remoteStop waits for the requests in flight, so it runs once the
	// windows are gone and the event loop is about to stop, not per window.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { app.remoteStop() })
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// newAppWindow opens the window an App drives. Ctrl+N opens another one
// over its own App, so each window has its own tabs, undo and clipboard.
func newAppWindow(app *App) *mygo.Window {
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:     "晴谷",
		Width:     1100,
		Height:    720,
		MinWidth:  640,
		MinHeight: 420,
		Frameless: true,
		Content:   ui.View(app.view),
	})
	app.win = win
	return win
}

// openNewWindow is Ctrl+N: a new window on the same place, as Explorer's.
func (a *App) openNewWindow(path string) {
	app := newAppWithStore(a.store)
	if !specialPath(path) {
		app.navigate(path)
	}
	registerApp(app)
	win := newAppWindow(app)
	win.OnClosed(func() { unregisterApp(app) })
}

func newApp() *App { return newAppWithStore(loadState()) }

// newAppWithStore builds one window's worth of app state over the shared
// persisted store: pins, recents and the trash log stay one collection
// across every open window.
func newAppWithStore(store *persistedState) *App {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/"
	}
	a := &App{
		// Explorer opens on the 主文件夹 (Home) page.
		tabs:        []*tabState{newTab(pathHome)},
		activeTab:   0,
		sortKey:     SortName,
		sortAsc:     true,
		mode:        ViewDetails,
		iconZoom:    1,
		showHidden:  false,
		showExt:     true,
		grouping:    groupNone,
		navWidth:    248,
		homePath:    home,
		previewOpen: false,
		previewW:    320,
		store:       store,
	}
	if exts, err := loadShellExtensions(); err != nil {
		a.statusMsg = "扩展配置无效：" + err.Error()
	} else {
		a.shellExts = exts
	}
	// The app and open-with registries are process-wide; scan once no
	// matter how many windows open.
	if openWith.Load() == nil {
		go scanOpenWithApps()
	}
	startGalleryScan()
	a.reload()
	return a
}

// view stacks the Explorer chrome and a body row that takes the rest of
// the window. Each band fills the width; the body and the file pane take
// the remaining height.
func (a *App) view(c *ui.Context) {
	a.handleKeys(c)
	a.updateSearch()
	ui.Column(c).Fill().Background(colorWindow()).Children(func() {
		a.tabStrip(c)
		a.commandBar(c)
		a.navRow(c)
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c)
			a.filePane(c)
			if a.previewOpen {
				a.previewPane(c)
			}
		})
		a.statusBar(c)
	})
	a.quickLookModal(c)
	a.propertiesModal(c)
}

// handleKeys maps Explorer shortcuts. A focused text field consumes its
// own editing keys before Context.Shortcut sees them, so Escape and Enter
// stay with the rename field and the path editor.
func (a *App) handleKeys(c *ui.Context) {
	// The row a keyboard move went to scrolls into view in the frame that
	// moved it: clear the request first, so a wheel scroll right after a
	// move keeps the user's place.
	a.cur().reveal = ""
	if a.cur().renaming != "" || a.cur().editingPath {
		return
	}
	// 空格 is the quick preview (Finder's Quick Look); ←/→ still seek a
	// loaded audio preview.
	if a.quickLook {
		if c.Shortcut(0, ui.KeySpace) || c.Shortcut(0, ui.KeyEscape) {
			a.quickLook = false
			return
		}
	}
	if st := a.preview.Load(); st != nil && st.kind == previewAudio && st.media.player != nil {
		pl := st.media.player
		switch {
		case c.Shortcut(0, ui.KeyLeft):
			pl.player.Seek(pl.player.Position() - 5*time.Second)
			return
		case c.Shortcut(0, ui.KeyRight):
			pl.player.Seek(pl.player.Position() + 5*time.Second)
			return
		}
	}
	switch {
	case c.Shortcut(0, ui.KeyBackspace):
		a.up()
	case c.Shortcut(ui.Alt, ui.KeyLeft), c.Shortcut(ui.Cmd, ui.KeyBracketLeft):
		a.back()
	case c.Shortcut(ui.Alt, ui.KeyRight), c.Shortcut(ui.Cmd, ui.KeyBracketRight):
		a.forward()
	case c.Shortcut(ui.Alt, ui.KeyUp):
		a.up()
	case c.Shortcut(0, ui.KeyEnter):
		a.doOpen()
	case c.Shortcut(0, ui.KeyDelete), c.Shortcut(ui.Cmd, ui.KeyBackspace), c.Shortcut(ui.Ctrl, ui.KeyD):
		a.doDelete()
	case c.Shortcut(0, ui.KeyF2):
		a.doRenameStart()
	case c.Shortcut(0, ui.KeySpace):
		if a.quickLook {
			a.quickLook = false
		} else {
			a.openQuickLook()
		}
	case c.Shortcut(0, ui.KeyF5):
		a.doRefresh()
	case c.Shortcut(ui.Cmd, ui.KeyC), c.Shortcut(ui.Ctrl, ui.KeyC):
		a.doCopy()
	case c.Shortcut(ui.Cmd, ui.KeyZ), c.Shortcut(ui.Ctrl, ui.KeyZ):
		a.doUndo()
	case c.Shortcut(ui.Shift, ui.KeyDelete):
		a.doPermanentDelete()
	case c.Shortcut(ui.Alt, ui.KeyEnter):
		if sel := a.selectedEntries(); len(sel) == 1 {
			a.showProperties()
		}
	case c.Shortcut(ui.Cmd, ui.KeyE), c.Shortcut(ui.Ctrl, ui.KeyE),
		c.Shortcut(ui.Ctrl, ui.KeyF), c.Shortcut(ui.Cmd, ui.KeyF), c.Shortcut(0, ui.KeyF3):
		a.focusSearch = true
	case c.Shortcut(ui.Alt, ui.KeyD), c.Shortcut(ui.Ctrl, ui.KeyL), c.Shortcut(0, ui.KeyF4):
		a.beginPathEdit()
	case c.Shortcut(0, ui.KeyF11):
		if a.win != nil {
			a.win.ToggleMaximize()
		}
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyC), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyC):
		a.doCopyAsPath()
	case c.Shortcut(ui.Cmd, ui.KeyX), c.Shortcut(ui.Ctrl, ui.KeyX):
		a.doCut()
	case c.Shortcut(ui.Cmd, ui.KeyV), c.Shortcut(ui.Ctrl, ui.KeyV):
		a.doPaste()
	case c.Shortcut(ui.Cmd, ui.KeyA), c.Shortcut(ui.Ctrl, ui.KeyA):
		a.selectAll()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyEqual), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyEqual):
		a.zoomIn()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyMinus), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyMinus):
		a.zoomOut()
	case c.Shortcut(ui.Alt, ui.KeyP):
		a.togglePreview()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyN):
		a.doNewFolder()
	case c.Shortcut(ui.Cmd, ui.KeyT), c.Shortcut(ui.Ctrl, ui.KeyT):
		a.openTab(a.cur().path)
	case c.Shortcut(ui.Cmd, ui.KeyW), c.Shortcut(ui.Ctrl, ui.KeyW):
		a.closeTab(a.activeTab)
	case c.Shortcut(ui.Cmd, ui.KeyBracketRight), c.Shortcut(ui.Ctrl, ui.KeyTab):
		a.switchTo((a.activeTab + 1) % len(a.tabs))
	case c.Shortcut(ui.Cmd, ui.KeyBracketLeft), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyTab):
		a.switchTo((a.activeTab + len(a.tabs) - 1) % len(a.tabs))
	}
	// Ctrl/Cmd+1..8 goes to that tab, +9 to the last, as Explorer's tab
	// shortcuts.
	// Ctrl+Shift+1..6: view styles, as Explorer's.
	styles := []struct {
		key  ui.Key
		mode ViewMode
	}{
		{ui.Key1, ViewExtraLarge},
		{ui.Key2, ViewLargeIcons},
		{ui.Key3, ViewMediumIcons},
		{ui.Key4, ViewSmallIcons},
		{ui.Key5, ViewList},
		{ui.Key6, ViewDetails},
	}
	for _, st := range styles {
		if c.Shortcut(ui.Ctrl|ui.Shift, st.key) || c.Shortcut(ui.Cmd|ui.Shift, st.key) {
			a.setMode(st.mode)
			return
		}
	}
	digits := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4,
		ui.Key5, ui.Key6, ui.Key7, ui.Key8}
	for i, key := range digits {
		if i >= len(a.tabs) {
			break
		}
		if c.Shortcut(ui.Ctrl, key) || c.Shortcut(ui.Cmd, key) {
			a.switchTo(i)
			return
		}
	}
	if c.Shortcut(ui.Ctrl, ui.Key9) || c.Shortcut(ui.Cmd, ui.Key9) {
		a.switchTo(len(a.tabs) - 1)
	}
	// The pane's keyboard navigation: the arrows step the selection, Home
	// and End go to its ends, Page Up and Page Down by a screenful, and
	// Shift extends from the anchor as Shift+click does. A modal owns the
	// keys while it is up, and the search field keeps them while it has
	// the focus.
	if a.quickLook || a.propsOpen || a.fieldFocused {
		return
	}
	switch {
	case c.Shortcut(0, ui.KeyUp):
		a.moveSelection(-1, 0, false)
	case c.Shortcut(0, ui.KeyDown):
		a.moveSelection(1, 0, false)
	case c.Shortcut(0, ui.KeyLeft):
		a.moveSelection(0, -1, false)
	case c.Shortcut(0, ui.KeyRight):
		a.moveSelection(0, 1, false)
	case c.Shortcut(0, ui.KeyHome):
		a.moveSelectionEnd(false, false)
	case c.Shortcut(0, ui.KeyEnd):
		a.moveSelectionEnd(true, false)
	case c.Shortcut(0, ui.KeyPageUp):
		a.moveSelectionPage(-1, false)
	case c.Shortcut(0, ui.KeyPageDown):
		a.moveSelectionPage(1, false)
	case c.Shortcut(ui.Shift, ui.KeyUp):
		a.moveSelection(-1, 0, true)
	case c.Shortcut(ui.Shift, ui.KeyDown):
		a.moveSelection(1, 0, true)
	case c.Shortcut(ui.Shift, ui.KeyLeft):
		a.moveSelection(0, -1, true)
	case c.Shortcut(ui.Shift, ui.KeyRight):
		a.moveSelection(0, 1, true)
	case c.Shortcut(ui.Shift, ui.KeyHome):
		a.moveSelectionEnd(false, true)
	case c.Shortcut(ui.Shift, ui.KeyEnd):
		a.moveSelectionEnd(true, true)
	case c.Shortcut(ui.Shift, ui.KeyPageUp):
		a.moveSelectionPage(-1, true)
	case c.Shortcut(ui.Shift, ui.KeyPageDown):
		a.moveSelectionPage(1, true)
	}
}

func (a *App) selectOnly(path string) {
	t := a.cur()
	t.selected = map[string]bool{path: true}
	t.anchor = path
	t.cursor = path
	a.requestPreview()
}

func (a *App) toggleSelect(path string) {
	t := a.cur()
	if len(t.selected) == 0 {
		t.selected = map[string]bool{}
	}
	if t.selected[path] {
		delete(t.selected, path)
		if t.anchor == path {
			t.anchor = ""
		}
		a.requestPreview()
		return
	}
	t.selected[path] = true
	t.anchor = path
	t.cursor = path
	a.requestPreview()
}

// selectRange selects every visible entry from the anchor through path,
// replacing the previous selection. With no anchor it selects only path.
func (a *App) selectRange(path string) {
	t := a.cur()
	vis := a.visibleEntries()
	if t.anchor == "" {
		a.selectOnly(path)
		return
	}
	from, to := -1, -1
	for i, e := range vis {
		if e.Path == t.anchor {
			from = i
		}
		if e.Path == path {
			to = i
		}
	}
	if from < 0 || to < 0 {
		a.selectOnly(path)
		return
	}
	if from > to {
		from, to = to, from
	}
	next := make(map[string]bool, to-from+1)
	for _, e := range vis[from : to+1] {
		next[e.Path] = true
	}
	t.selected = next
	t.cursor = path
	a.requestPreview()
}

func (a *App) selectAll() {
	t := a.cur()
	vis := a.visibleEntries()
	next := make(map[string]bool, len(vis))
	for _, e := range vis {
		next[e.Path] = true
	}
	t.selected = next
	if t.anchor == "" && len(vis) > 0 {
		t.anchor = vis[0].Path
	}
	a.requestPreview()
}

func (a *App) clearSelection() {
	t := a.cur()
	t.selected = map[string]bool{}
	t.anchor = ""
	t.cursor = ""
	a.requestPreview()
}

// togglePreview flips the preview pane on or off, refreshing its contents
// for the current selection. Closing the pane stops a playing preview,
// as Explorer's preview pane does.
func (a *App) togglePreview() {
	a.previewOpen = !a.previewOpen
	if a.previewOpen {
		a.requestPreview()
		return
	}
	if st := a.preview.Load(); st != nil && st.media.player != nil {
		st.media.player.stop()
	}
}

func (a *App) isSelected(path string) bool {
	return a.cur().selected[path]
}

// selectedEntries returns the selected visible entries, in view order.
func (a *App) selectedEntries() []Entry {
	t := a.cur()
	if len(t.selected) == 0 {
		return nil
	}
	var out []Entry
	for _, e := range a.visibleEntries() {
		if t.selected[e.Path] {
			out = append(out, e)
		}
	}
	return out
}
