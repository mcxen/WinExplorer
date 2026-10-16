package main

import "github.com/egoist/mygo/ui"

// keynav.go is Explorer's pane keyboard navigation: the arrow keys step the
// selection through the current view, Home and End go to its ends, Page Up
// and Page Down by the rows in view, and Shift does all of it extending the
// selection from the anchor, as Shift+click does. A move never wraps and
// never leaves the pane, so the ends of the listing hold. The view showing
// the moved-to item raises it into view, from tabState.reveal.

// navGridMode reports whether the page on show lays its items in a grid the
// arrows step in two dimensions: the icon views, and the 画廊 page, which
// grids its photos whatever 查看 mode it was left in.
func (a *App) navGridMode() bool {
	return isIconMode(a.mode) || a.cur().path == pathGallery
}

// navCursor is the index in vis the keyboard moves from: the item the last
// click or arrow key landed on, else the anchor, else the first selected
// item, or -1 with nothing selected.
func (a *App) navCursor(vis []Entry) int {
	t := a.cur()
	for i, e := range vis {
		if e.Path == t.cursor {
			return i
		}
	}
	for i, e := range vis {
		if e.Path == t.anchor {
			return i
		}
	}
	for i, e := range vis {
		if t.selected[e.Path] {
			return i
		}
	}
	return -1
}

// navPage is how many items Page Up and Page Down move: the rows the view
// shows, times the columns an icon view lays out.
func (a *App) navPage() int {
	if a.navGridMode() {
		t := a.cur()
		return max(t.gridRows, 1) * max(t.gridCols, 1)
	}
	first, last := a.rowList().Visible()
	if last < first {
		return 1
	}
	return last - first + 1
}

// rowList is the ListState of the current row-based view.
func (a *App) rowList() *ui.ListState {
	if a.mode == ViewList {
		return &a.cur().list
	}
	return &a.cur().details
}

// moveSelection steps the selection by dy rows and dx items, as the arrow
// keys do: the icon views step across by dx and by a whole row by dy, the
// row-based views by dy alone, as Explorer's ←/→ leave the details and list
// views alone. extend keeps the anchor and takes the range from it.
func (a *App) moveSelection(dy, dx int, extend bool) {
	step := dy
	if a.navGridMode() {
		step = dy*max(a.cur().gridCols, 1) + dx
	}
	a.stepSelection(step, extend)
}

// moveSelectionPage moves the selection by a screenful of the current view.
func (a *App) moveSelectionPage(dir int, extend bool) {
	a.stepSelection(dir*a.navPage(), extend)
}

// moveSelectionEnd moves the selection to the first item, or to the last
// with last set, as Home and End do.
func (a *App) moveSelectionEnd(last, extend bool) {
	vis := a.visibleEntries()
	if len(vis) == 0 {
		return
	}
	i := 0
	if last {
		i = len(vis) - 1
	}
	a.selectStep(vis, i, extend)
}

// stepSelection moves the selection step items along the listing. With
// nothing selected a move into the listing takes its first item going down
// and its last going up, as Explorer's arrows do.
func (a *App) stepSelection(step int, extend bool) {
	vis := a.visibleEntries()
	if len(vis) == 0 || step == 0 {
		return
	}
	cur := a.navCursor(vis)
	next := cur + step
	if cur < 0 {
		if step > 0 {
			next = 0
		} else {
			next = len(vis) - 1
		}
	}
	a.selectStep(vis, next, extend)
}

// selectStep selects vis[i], clamped to the listing, and asks the view to
// show it. With extend the range runs from the anchor, as Shift+click's.
func (a *App) selectStep(vis []Entry, i int, extend bool) {
	i = min(max(i, 0), len(vis)-1)
	path := vis[i].Path
	if extend {
		a.selectRange(path)
	} else {
		a.selectOnly(path)
	}
	a.cur().reveal = path
}
