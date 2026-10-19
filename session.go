package main

import (
	"os"
)

// session.go is 恢复上次会话: the tabs and windows of the run that ended,
// put back on the next launch. The store keeps the record (statestore.go's
// sessionState); this file captures it from the live windows and builds
// windows from it again.

// windowGeom is a window's saved box, carried on the App until its window
// is made: newAppWindow reads it for the WindowOptions. A zero geom keeps
// the default size and centering, as every window but a restored one does.
type windowGeom struct {
	x, y, w, h int
}

// captureSession records every open window — its box and its tabs — into
// the store. Called as the app quits, after each window's close refreshed
// its own record; this pass is the final word for the windows still open,
// merging them into the saved session rather than rebuilding it, so a
// window closed earlier keeps the record its close left. One save for all.
func captureSession(store *persistedState) {
	// 清除会话记录 in this run keeps the session cleared: the quit leaves
	// nothing to restore.
	if store.sessionClearedNow() {
		return
	}
	appsMu.Lock()
	all := append([]*App(nil), apps...)
	appsMu.Unlock()
	s := store.session()
	if s == nil {
		s = &sessionState{}
	}
	active := -1
	for _, app := range all {
		ws, ok := app.windowSession()
		if !ok {
			continue
		}
		slot := app.putWindowSession(s, ws)
		if app.win != nil && app.win.IsFocused() {
			active = slot
		}
	}
	if active < 0 && len(s.Windows) > 0 {
		active = 0
	}
	s.Active = max(active, 0)
	store.setSession(s)
}

// windowSession is the app's own record: its tabs from its state, with
// Active as the live tab's place among them, and its box from the window.
// ok is false for a window with nothing to restore.
func (a *App) windowSession() (windowSession, bool) {
	ws := windowSession{}
	for _, t := range a.tabs {
		ws.Tabs = append(ws.Tabs, t.path)
	}
	if len(ws.Tabs) == 0 {
		return ws, false
	}
	ws.Active = min(max(a.activeTab, 0), len(ws.Tabs)-1)
	if w := a.win; w != nil {
		if b := w.Bounds(); b.Width > 0 && b.Height > 0 {
			ws.X, ws.Y, ws.W, ws.H = b.X, b.Y, b.Width, b.Height
		}
	}
	return ws, true
}

// saveWindowSession refreshes this window's own record in the stored
// session, as its close runs. See storeWindowSession for the merge.
func (a *App) saveWindowSession() {
	if a.store.sessionClearedNow() {
		return
	}
	ws, ok := a.windowSession()
	if !ok {
		return
	}
	a.storeWindowSession(ws)
}

// putWindowSession writes ws into s at the slot the app owns, appending
// and claiming a new slot when it has none, and returns the slot used. The
// slot rule lives here once: the close-time update and the quit-time
// capture both go through it, so they cannot disagree about which record a
// window owns.
func (a *App) putWindowSession(s *sessionState, ws windowSession) int {
	if a.sessionSlot >= 0 && a.sessionSlot < len(s.Windows) {
		s.Windows[a.sessionSlot] = ws
		return a.sessionSlot
	}
	a.sessionSlot = len(s.Windows)
	s.Windows = append(s.Windows, ws)
	return a.sessionSlot
}

// storeWindowSession writes one window's record into the stored session,
// keeping the other windows' records as they are. A closed window leaves
// its record behind, so the next launch still restores what the user had —
// Explorer's reopen-the-same-tabs behavior.
func (a *App) storeWindowSession(ws windowSession) {
	s := a.store.session()
	if s == nil {
		s = &sessionState{}
	}
	a.putWindowSession(s, ws)
	a.store.setSession(s)
}

// restoreTabPath is the folder a restored tab opens: the saved one when it
// is still there — a special page always is — and 主文件夹 otherwise.
func restoreTabPath(saved string) string {
	if specialPath(saved) {
		return saved
	}
	if info, err := os.Stat(saved); err == nil && info.IsDir() {
		return saved
	}
	return pathHome
}

// newSessionApp builds the App for one saved window: its tabs restored in
// order with the active one in front, the window's box queued for
// newAppWindow to use, and the record's slot kept for the close-time update.
func newSessionApp(store *persistedState, ws windowSession, slot int) *App {
	a := newAppWithStore(store)
	a.sessionSlot = slot
	if len(ws.Tabs) == 0 {
		return a
	}
	tabs := make([]*tabState, 0, len(ws.Tabs))
	for _, p := range ws.Tabs {
		tabs = append(tabs, newTab(restoreTabPath(p)))
	}
	a.tabs = tabs
	a.activeTab = min(max(ws.Active, 0), len(tabs)-1)
	if ws.W > 0 && ws.H > 0 {
		a.geom = &windowGeom{x: ws.X, y: ws.Y, w: ws.W, h: ws.H}
	}
	a.reload()
	return a
}
