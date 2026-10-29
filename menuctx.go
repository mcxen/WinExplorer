package main

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// menuctx.go builds the right-click menus, shown as the system's native
// context menu, and the command-bar menu buttons, drawn as the in-window
// flyout of menuflyout.go, from one imperative builder
// (Item(...).Chosen()). xMenu mirrors ui.Menu's API:
// the build function runs once to collect the rows, and again, with the
// chosen row's place, where that row's Chosen reports true.

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

// contextMenu gives el a right-click menu shown as the system's own
// context menu (GTK, Win32, NSMenu), as ui.ContextMenu shows it: the list
// row under the pointer is chosen first, and Shift+F10 / the menu key open
// it. build's rows are collected as for the flyout (tidyMenu drops stray
// separators, an empty submenu shows disabled) and handed to ui.Menu; the
// run after a choice collects them again and runs the chosen row.
// Native menus carry no icons: ui.Menu has none, so Icon is ignored here.
func (a *App) contextMenu(c *ui.Context, el ui.Element, build func(m *xMenu)) {
	el.ContextMenu(func(m *ui.Menu) {
		nativeMenu(m, collectXMenu(build))
	})
}

// nativeMenu adds es to m; a radio row shows as a checked item, the only
// mark ui.Menu has.
func nativeMenu(m *ui.Menu, es []*menuEntry) {
	for _, e := range es {
		switch {
		case e.sep:
			m.Separator()
		case e.sub != nil:
			sub := e.sub
			m.Submenu(e.label, func(n *ui.Menu) { nativeMenu(n, sub) }).Disabled(e.disabled)
		default:
			it := m.Item(e.label).Disabled(e.disabled)
			if e.toggle || e.radio {
				it.Checked(e.checked)
			}
			if e.hasKey {
				it.Shortcut(e.mods, e.key)
			}
			if it.Chosen() && e.run != nil {
				e.run()
			}
		}
	}
}

// menuButton opens build's rows as a flyout below btn on a click.
func (a *App) menuButton(c *ui.Context, btn ui.Element, alignEnd bool, build func(m *xMenu)) {
	id := "mbtn:" + strconv.FormatUint(btn.ID(), 36)
	a.attachCommandMenu(c, btn, id, alignEnd, func() []*menuEntry { return collectXMenu(build) })
}
