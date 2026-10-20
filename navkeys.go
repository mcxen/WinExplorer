package main

import "github.com/egoist/mygo/ui"

// navkeys.go is the navigation pane's keyboard cursor. It moves focus around
// the tree without changing the tab, leaving navigation to Enter or Space as
// Explorer does.

func (a *App) navTreeKeys(c *ui.Context) bool {
	if !a.navTree.focused || a.fieldFocused || a.quickLook || a.propsOpen {
		return false
	}
	if c.Shortcut(0, ui.KeyTab) {
		a.navTree.focused = false
		return true
	}
	// A shortcut can arrive before the next sidebar frame rebuilds its rows
	// after navigation; follow the tab here so the cursor sees the new tree.
	a.navTreeFollow()
	rows := a.navRows()
	i := navTreeCursorIndex(rows, a.navTree.cursor, a.navTree.row)
	if i < 0 {
		return false
	}
	switch {
	case c.Shortcut(0, ui.KeyUp):
		a.navTreeMove(rows, i, -1)
	case c.Shortcut(0, ui.KeyDown):
		a.navTreeMove(rows, i, 1)
	case c.Shortcut(0, ui.KeyHome):
		a.navTreeMoveTo(rows, navTreeSelectable(rows, 0, 1))
	case c.Shortcut(0, ui.KeyEnd):
		a.navTreeMoveTo(rows, navTreeSelectable(rows, len(rows)-1, -1))
	case c.Shortcut(0, ui.KeyRight):
		a.navTreeRight(rows, i)
	case c.Shortcut(0, ui.KeyLeft):
		a.navTreeLeft(rows, i)
	case c.Shortcut(0, ui.KeyEnter), c.Shortcut(0, ui.KeySpace):
		a.navTreeActivate(rows[i])
	default:
		return false
	}
	return true
}

func navTreeCursorIndex(rows []navRow, cursor string, current int) int {
	for i, r := range rows {
		if r.key == cursor {
			return i
		}
	}
	if current >= 0 && current < len(rows) {
		return current
	}
	return navTreeSelectable(rows, 0, 1)
}

func navTreeSelectable(rows []navRow, start, step int) int {
	for i := start; i >= 0 && i < len(rows); i += step {
		if rows[i].kind != navRowSection {
			return i
		}
	}
	return -1
}

func (a *App) navTreeMove(rows []navRow, i, step int) {
	a.navTreeMoveTo(rows, navTreeSelectable(rows, i+step, step))
}

func (a *App) navTreeMoveTo(rows []navRow, i int) {
	if i < 0 || i >= len(rows) {
		return
	}
	a.navTreeFocus(rows[i].key)
	a.navTree.list.ScrollIntoView(i)
}

func (a *App) navTreeRight(rows []navRow, i int) {
	r := rows[i]
	if !r.hasKids {
		return
	}
	if !r.open {
		a.navTreeSetOpen(r, true)
		a.navTreeFocus(r.key)
		return
	}
	for j := i + 1; j < len(rows); j++ {
		if rows[j].depth <= r.depth {
			break
		}
		if rows[j].depth == r.depth+1 && rows[j].kind != navRowSection {
			a.navTreeMoveTo(rows, j)
			return
		}
	}
}

func (a *App) navTreeLeft(rows []navRow, i int) {
	r := rows[i]
	if (r.kind == navRowFolder || r.kind == navRowThisPC) && r.open {
		a.navTreeSetOpen(r, false)
		a.navTreeFocus(r.key)
		return
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].depth < r.depth && rows[j].kind != navRowSection {
			a.navTreeMoveTo(rows, j)
			return
		}
	}
}

func (a *App) navTreeSetOpen(r navRow, open bool) {
	if r.kind == navRowThisPC {
		a.navPCExpanded = open
		return
	}
	if a.navTree.open == nil {
		a.navTree.open = map[string]bool{}
	}
	a.navTree.open[r.path] = open
}

func (a *App) navTreeActivate(r navRow) {
	if r.kind == navRowSection {
		return
	}
	if r.act != nil {
		a.navTreeFocus(r.key)
		r.act()
		return
	}
	if r.kind == navRowFolder {
		a.navFromPane(r.key, r.path, true)
		a.navigate(r.path)
		return
	}
	if r.path != "" {
		a.navFromPane(r.key, r.path, false)
		a.openPinned(r.path, !r.file)
	}
}
