package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// menuctx.go draws the right-click menus and the menu buttons that build
// their items imperatively (Item(...).Chosen()) as the in-window flyout of
// menuflyout.go, under both visual styles. xMenu mirrors ui.Menu's API:
// the build function runs once to collect the rows, and again, with the
// chosen row's place, where that row's Chosen reports true.
//
// A right-click still goes through ui.ContextMenu, so the list row under
// the pointer is chosen and Shift+F10 / the menu key keep working; the
// collecting run adds nothing to the system menu, which then never shows.

type xRun struct {
	collect bool
	seq     int
	choice  int
	label   string
	pick    func(seq int, label string)
}

// xMenu is a menu being built for the flyout.
type xMenu struct {
	r  *xRun
	es []*menuEntry
}

// xItem is one row of an xMenu.
type xItem struct {
	r        *xRun
	seq      int
	label    string
	disabled bool
	sub      bool
	e        *menuEntry
}

func (m *xMenu) add(label string) *xItem {
	r := m.r
	r.seq++
	it := &xItem{r: r, seq: r.seq, label: label}
	if r.collect {
		e := &menuEntry{label: label}
		seq := r.seq
		e.run = func() { r.pick(seq, label) }
		m.es = append(m.es, e)
		it.e = e
	}
	return it
}

// Item adds a row showing label.
func (m *xMenu) Item(label string) *xItem { return m.add(label) }

// Separator adds a line between groups of rows.
func (m *xMenu) Separator() {
	if m.r.collect {
		m.es = append(m.es, &menuEntry{sep: true})
	}
}

// Submenu adds a row that opens build's rows beside it.
func (m *xMenu) Submenu(label string, build func(n *xMenu)) *xItem {
	it := m.add(label)
	it.sub = true
	n := &xMenu{r: m.r}
	build(n)
	if it.e != nil {
		it.e.run = nil
		it.e.sub = tidyMenu(n.es)
		if it.e.sub == nil {
			it.e.sub = []*menuEntry{}
			it.e.disabled = true
		}
	}
	return it
}

func (it *xItem) Disabled(d bool) *xItem {
	it.disabled = d
	if it.e != nil {
		it.e.disabled = d
	}
	return it
}

// Checked shows a check mark: only for rows that toggle a setting.
func (it *xItem) Checked(on bool) *xItem {
	if it.e != nil && !it.sub {
		it.e.check(on)
	}
	return it
}

// Radio shows a dot on the chosen row of a one-of-many group.
func (it *xItem) Radio(on bool) *xItem {
	if it.e != nil && !it.sub {
		it.e.choice(on)
	}
	return it
}

func (it *xItem) Shortcut(mods ui.Modifiers, key ui.Key) *xItem {
	if it.e != nil {
		it.e.shortcut(mods, key)
	}
	return it
}

// Icon puts a glyph in the row's icon column; tint colors it (zero: text).
func (it *xItem) Icon(icon func() *ui.SVG, tint ...ui.Color) *xItem {
	if it.e != nil {
		it.e.icon = icon
		if len(tint) > 0 {
			it.e.tint, it.e.tinted = tint[0], true
		}
	}
	return it
}

// Chosen reports whether the row was chosen, in the run after the click.
func (it *xItem) Chosen() bool {
	r := it.r
	if r.collect || it.disabled || it.sub || r.choice != it.seq || r.label != it.label {
		return false
	}
	return true
}

// collectXMenu runs build to collect its rows; a row's run runs build
// again with that row chosen.
func collectXMenu(build func(m *xMenu)) []*menuEntry {
	r := &xRun{collect: true}
	r.pick = func(seq int, label string) {
		build(&xMenu{r: &xRun{choice: seq, label: label}})
	}
	m := &xMenu{r: r}
	build(m)
	return tidyMenu(m.es)
}

// tidyMenu drops leading, trailing and doubled separators.
func tidyMenu(es []*menuEntry) []*menuEntry {
	var out []*menuEntry
	for _, e := range es {
		if e.sep && (len(out) == 0 || out[len(out)-1].sep) {
			continue
		}
		out = append(out, e)
	}
	for len(out) > 0 && out[len(out)-1].sep {
		out = out[:len(out)-1]
	}
	return out
}

// ctxMenuState is the open right-click flyout: the element it opened on
// and where, in window DIPs.
type ctxMenuState struct {
	id   string
	x, y float32
}

// contextMenu gives el a right-click menu drawn as a flyout at the
// pointer (below el for the keyboard). Near the window's edges the flyout
// flips to the other side of the point, as AttachTo places it.
func (a *App) contextMenu(c *ui.Context, el ui.Element, build func(m *xMenu)) {
	id := "ctx:" + strconv.FormatUint(el.ID(), 36)
	el.ContextMenu(func(*ui.Menu) {
		b := el.Bounds()
		x, y := b.X, b.Y+b.H
		if px, py, over := el.PointerPosition(); over {
			x, y = b.X+px, b.Y+py
		}
		a.ctx = ctxMenuState{id: id, x: x, y: y}
		a.fly = flyState{open: id, idx: -1, parentIdx: -1}
	})
	if a.fly.open != id || a.ctx.id != id {
		return
	}
	es := collectXMenu(build)
	if len(es) == 0 {
		a.fly = flyState{}
		return
	}
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("fly-back:" + id)).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if back.Clicked() || back.RightClicked() {
			a.fly = flyState{}
		}
		pt := ui.Box(c.Key("fly-pt:" + id)).Absolute().Left(a.ctx.x).Top(a.ctx.y).Width(0).Height(0)
		a.flyLevels(c, id, pt, ui.AnchorBottomStart, ui.AnchorTopStart, 0, es)
	})
}

// menuButton opens build's rows as a flyout below btn on a click.
func (a *App) menuButton(c *ui.Context, btn ui.Element, alignEnd bool, build func(m *xMenu)) {
	id := "mbtn:" + strconv.FormatUint(btn.ID(), 36)
	a.attachCommandMenu(c, btn, id, alignEnd, func() []*menuEntry { return collectXMenu(build) })
}
