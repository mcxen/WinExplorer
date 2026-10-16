package main

import (
	"log"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	app := newApp()
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "文件资源管理器",
			Width:     1100,
			Height:    720,
			MinWidth:  640,
			MinHeight: 420,
			Frameless: true,
			Content:   ui.View(app.view),
		})
		app.win = win
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func newApp() *App {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/"
	}
	a := &App{
		tabs:        []*tabState{newTab(home)},
		activeTab:   0,
		sortKey:     SortName,
		sortAsc:     true,
		mode:        ViewDetails,
		showHidden:  false,
		navWidth:    248,
		homePath:    home,
		previewOpen: false,
		previewW:    320,
	}
	a.reload()
	return a
}

// view stacks the Explorer chrome and a body row that takes the rest of
// the window. Each band fills the width; the body and the file pane take
// the remaining height.
func (a *App) view(c *ui.Context) {
	a.handleKeys(c)
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
}

// handleKeys maps Explorer shortcuts. A focused text field consumes its
// own editing keys before Context.Shortcut sees them, so Escape and Enter
// stay with the rename field and the path editor.
func (a *App) handleKeys(c *ui.Context) {
	if a.cur().renaming != "" || a.cur().editingPath {
		return
	}
	// Media playback shortcuts, as Explorer's preview pane: Space toggles
	// the audio preview, arrows seek. Only when an audio preview is loaded.
	if a.previewOpen {
		if st := a.preview.Load(); st != nil && st.kind == previewAudio && st.media.player != nil {
			pl := st.media.player
			switch {
			case c.Shortcut(0, ui.KeySpace):
				if pl.player.Playing() {
					pl.player.Pause()
				} else {
					pl.player.Play()
				}
				pl.startTicker()
				return
			case c.Shortcut(0, ui.KeyLeft):
				pl.player.Seek(pl.player.Position() - 5*time.Second)
				return
			case c.Shortcut(0, ui.KeyRight):
				pl.player.Seek(pl.player.Position() + 5*time.Second)
				return
			}
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
	case c.Shortcut(0, ui.KeyDelete), c.Shortcut(ui.Cmd, ui.KeyBackspace):
		a.doDelete()
	case c.Shortcut(0, ui.KeyF2):
		a.doRenameStart()
	case c.Shortcut(0, ui.KeyF5):
		a.doRefresh()
	case c.Shortcut(ui.Cmd, ui.KeyC), c.Shortcut(ui.Ctrl, ui.KeyC):
		a.doCopy()
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyC), c.Shortcut(ui.Ctrl|ui.Shift, ui.KeyC):
		a.doCopyAsPath()
	case c.Shortcut(ui.Cmd, ui.KeyX), c.Shortcut(ui.Ctrl, ui.KeyX):
		a.doCut()
	case c.Shortcut(ui.Cmd, ui.KeyV), c.Shortcut(ui.Ctrl, ui.KeyV):
		a.doPaste()
	case c.Shortcut(ui.Cmd, ui.KeyA), c.Shortcut(ui.Ctrl, ui.KeyA):
		a.selectAll()
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
}

func (a *App) selectOnly(path string) {
	t := a.cur()
	t.selected = map[string]bool{path: true}
	t.anchor = path
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
