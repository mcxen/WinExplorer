package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// searchbox_panel.go owns the launcher window: its process-wide state,
// global shortcut registration and the panel that hosts searchbox.go's body.

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
