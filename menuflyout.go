package main

import (
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// menuflyout.go draws the command bar's menus (新建, 排序, 查看, 查看更多)
// in the window under 经典 as WinUI 3's MenuFlyout does in File Explorer:
// a rounded panel with a soft shadow, every row with a 16px glyph column,
// marks in a column of their own, shortcuts right-aligned in the secondary
// text color, submenus beside their row. v2 keeps the system menus
// (ui.Menu). Both are built from one []*menuEntry, so the verbs, order and
// shortcuts cannot drift apart (docs/specs/16-commands-layout.md).

// Menu flyout metrics, in DIPs (docs/UI.md「菜单」).
const (
	flyRadius     = 8   // panel corners
	flyPad        = 4   // panel padding: the row highlight's inset
	flyMinWidth   = 200 // panel's least width
	flyRowH       = 32  // a row
	flyRowRadius  = 4   // the row highlight's corners
	flyRowPadX    = 12  // label side from the highlight's edges
	flyIcon       = 16  // glyph column
	flyIconGap    = 12  // glyph to label
	flyMarkW      = 16  // check / radio column, only in menus that have marks
	flyMarkGap    = 8   // mark to glyph
	flyAccelGap   = 28  // least gap between a label and its shortcut
	flySepMargin  = 4   // above and below the 1px separator
	flyFont       = 12  // label
	flyAccelFont  = 11  // shortcut
	flyChevron    = 12  // submenu chevron
	flyAnchorGap  = 4   // panel from its button
	flySubOverlap = 4   // a submenu laps its parent by the panel padding
)

// menuEntry is one row of a command menu: a verb, a separator (sep) or a
// submenu (sub). radio rows show a dot when checked, toggle rows a check.
type menuEntry struct {
	label    string
	icon     func() *ui.SVG
	mods     ui.Modifiers
	key      ui.Key
	hasKey   bool
	toggle   bool
	radio    bool
	checked  bool
	disabled bool
	sep      bool
	sub      []*menuEntry
	run      func()
}

// menuList builds a []*menuEntry fluently.
type menuList struct{ es []*menuEntry }

func (l *menuList) item(label string, icon func() *ui.SVG, run func()) *menuEntry {
	e := &menuEntry{label: label, icon: icon, run: run}
	l.es = append(l.es, e)
	return e
}

func (l *menuList) sep() { l.es = append(l.es, &menuEntry{sep: true}) }

func (l *menuList) submenu(label string, icon func() *ui.SVG, sub []*menuEntry) *menuEntry {
	e := &menuEntry{label: label, icon: icon, sub: sub}
	l.es = append(l.es, e)
	return e
}

func (e *menuEntry) shortcut(mods ui.Modifiers, key ui.Key) *menuEntry {
	e.mods, e.key, e.hasKey = mods, key, true
	return e
}
func (e *menuEntry) off(d bool) *menuEntry     { e.disabled = d; return e }
func (e *menuEntry) check(on bool) *menuEntry  { e.toggle, e.checked = true, on; return e }
func (e *menuEntry) choice(on bool) *menuEntry { e.radio, e.checked = true, on; return e }

// menuLabels lists es's labels, "-" for a separator, as Tester.Menu does.
func menuLabels(es []*menuEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.label
		if e.sep {
			out[i] = "-"
		}
	}
	return out
}

// nativeMenu puts es into a system menu (v2).
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
			if e.hasKey {
				it.Shortcut(e.mods, e.key)
			}
			if e.toggle || e.radio {
				it.Checked(e.checked)
			}
			if it.Chosen() && e.run != nil {
				e.run()
			}
		}
	}
}

// flyState is the open flyout: its button's id and the open submenu's
// label.
type flyState struct {
	open, sub string
}

// attachCommandMenu opens build's menu from btn: in the window under 经典,
// the system's under v2. alignEnd lines the panel's right edge up with
// the button's, for the bar's last button.
func (a *App) attachCommandMenu(c *ui.Context, btn ui.Element, id string, alignEnd bool, build func() []*menuEntry) {
	if visualV2 {
		btn.Menu(func(m *ui.Menu) { nativeMenu(m, build()) })
		return
	}
	if btn.Clicked() {
		if a.fly.open == id {
			a.fly = flyState{}
		} else {
			a.fly = flyState{open: id}
		}
	}
	if a.fly.open != id {
		return
	}
	es := build()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("fly-back:" + id)).Absolute().Left(0).Top(0).Right(0).Bottom(0)
		if back.Clicked() {
			a.fly = flyState{}
		}
		at, self := ui.AnchorBottomStart, ui.AnchorTopStart
		if alignEnd {
			at, self = ui.AnchorBottomEnd, ui.AnchorTopEnd
		}
		var subRow ui.Element
		var subEs []*menuEntry
		panel := a.flyPanel(c.Key("fly:"+id), es, func(e *menuEntry, row ui.Element) {
			subRow, subEs = row, e.sub
		}).AttachTo(btn, at, self).Margin(flyAnchorGap, 0, 0, 0)
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			a.fly = flyState{}
		}
		if subEs != nil && a.fly.sub != "" {
			a.flyPanel(c.Key("fly:"+id+">"+a.fly.sub), subEs, nil).
				AttachTo(subRow, ui.AnchorTopEnd, ui.AnchorTopStart).
				Top(-flyPad).Left(flySubOverlap - flyPad)
		}
	})
}

// flyPanel draws one level of a flyout. openSub reports the row of the
// open submenu, so the caller can attach its panel there.
func (a *App) flyPanel(c *ui.Context, es []*menuEntry, openSub func(e *menuEntry, row ui.Element)) ui.Element {
	marks, icons, subs := false, false, false
	for _, e := range es {
		marks = marks || e.toggle || e.radio
		icons = icons || e.icon != nil
		subs = subs || e.sub != nil
	}
	panel := ui.Column(c).MinWidth(flyMinWidth).Padding(flyPad).Radius(flyRadius).
		Background(colorFlyout()).Border(1, colorFlyoutBorder()).
		Shadow(0, 8, 16, 0, colorFlyoutShadow()).Shadow(0, 0, 2, 0, colorFlyoutShadow().Alpha(0.5)).
		FocusGroup(ui.Vertical).Role(ui.RoleMenu)
	panel.Children(func() {
		for i, e := range es {
			if e.sep {
				ui.Box(c.Key("sep"+itoa(i))).FillWidth().Height(1).Margin(flySepMargin, 0).
					Background(colorFlyoutBorder()).Shrink(0)
				continue
			}
			a.flyRow(c.Key("row"+itoa(i)+e.label), e, marks, icons, subs, openSub)
		}
	})
	return panel
}

func (a *App) flyRow(c *ui.Context, e *menuEntry, marks, icons, subs bool, openSub func(*menuEntry, ui.Element)) {
	row := ui.ButtonBase(c).FillWidth().Height(flyRowH).Padding(0, flyRowPadX).
		Radius(flyRowRadius).AlignItems(ui.Center).Shrink(0).Label(e.label).Disabled(e.disabled)
	isOpen := e.sub != nil && openSub != nil && a.fly.sub == e.label
	switch {
	case e.disabled:
	case row.Pressed():
		row.Background(colorFlyoutPressed())
	case row.Hovered() || isOpen:
		row.Background(colorFlyoutHover())
	}
	fg := colorText()
	if e.disabled {
		fg = colorText().Alpha(0.36)
	}
	row.TextColor(fg).Children(func() {
		if marks {
			ui.Row(c.Key("mark")).Width(flyMarkW).Height(flyIcon).Shrink(0).Margin(0, flyMarkGap, 0, 0).
				Center().Children(func() {
				switch {
				case e.checked && e.radio:
					ui.Box(c.Key("dot")).Width(6).Height(6).Radius(3).Background(fg)
				case e.checked:
					components.Icon(c.Key("chk"), lucide.Check).Size(14, 14).TextColor(fg)
				}
			})
		}
		if icons {
			ui.Row(c.Key("ico")).Width(flyIcon).Height(flyIcon).Shrink(0).Margin(0, flyIconGap, 0, 0).Children(func() {
				if e.icon != nil {
					components.Icon(c.Key("g"), e.icon).Size(flyIcon, flyIcon).TextColor(fg)
				}
			})
		}
		ui.Text(c.Key("l"), e.label).FontSize(flyFont).TextColor(fg).SingleLine().Grow(1).Shrink(0)
		switch {
		case e.hasKey:
			ui.Text(c.Key("k"), shortcutText(e.mods, e.key)).FontSize(flyAccelFont).
				TextColor(colorMuted().Alpha(map[bool]float32{false: 1, true: 0.6}[e.disabled])).
				SingleLine().Shrink(0).Margin(0, 0, 0, flyAccelGap)
		case e.sub != nil:
			ui.Box(c.Key("gap")).Width(flyAccelGap).Shrink(0)
			components.Icon(c.Key("chev"), lucide.ChevronRight).Size(flyChevron, flyChevron).TextColor(colorMuted())
		case subs:
			ui.Box(c.Key("gap")).Width(flyAccelGap + flyChevron).Shrink(0)
		}
	})
	if openSub == nil {
		if row.Clicked() && !e.disabled {
			a.fly = flyState{}
			if e.run != nil {
				e.run()
			}
		}
		return
	}
	if e.sub != nil {
		if (row.Hovered() || row.Clicked()) && !e.disabled {
			a.fly.sub = e.label
		}
		if a.fly.sub == e.label {
			openSub(e, row)
		}
		return
	}
	if row.Hovered() {
		a.fly.sub = ""
	}
	if row.Clicked() && !e.disabled {
		a.fly = flyState{}
		if e.run != nil {
			e.run()
		}
	}
}

// shortcutText spells a shortcut as Windows' menus do (Ctrl+Shift+=), or
// with the Mac's glyphs there.
func shortcutText(mods ui.Modifiers, key ui.Key) string {
	mac := runtime.GOOS == "darwin"
	s := ""
	add := func(win, glyph string) {
		if mac {
			s += glyph
		} else {
			s += win + "+"
		}
	}
	if mods&ui.Cmd != 0 {
		add("Ctrl", "⌘")
	}
	if mods&ui.Alt != 0 {
		add("Alt", "⌥")
	}
	if mods&ui.Shift != 0 {
		add("Shift", "⇧")
	}
	return s + keyLabel(key)
}

func keyLabel(k ui.Key) string {
	switch k {
	case ui.KeyEnter:
		return "Enter"
	case ui.KeyComma:
		return ","
	case ui.KeyEqual:
		return "="
	case ui.KeyMinus:
		return "-"
	}
	if k >= ui.KeyA && k <= ui.KeyZ {
		return string(rune('A' + (k - ui.KeyA)))
	}
	if k >= ui.KeyF1 && k <= ui.KeyF12 {
		return "F" + itoa(int(k-ui.KeyF1)+1)
	}
	return "?"
}

// Flyout surfaces, after WinUI's AcrylicInAppFillColorDefault solid
// fallback and its control fills.
func colorFlyout() ui.Color { return pick("#F9F9F9", "#2C2C2C") }
func colorFlyoutBorder() ui.Color {
	if darkMode() {
		return ui.RGBA(255, 255, 255, 0.08)
	}
	return ui.RGBA(0, 0, 0, 0.08)
}
func colorFlyoutShadow() ui.Color {
	if darkMode() {
		return ui.RGBA(0, 0, 0, 0.36)
	}
	return ui.RGBA(0, 0, 0, 0.14)
}
func colorFlyoutHover() ui.Color   { return colorText().Alpha(0.06) }
func colorFlyoutPressed() ui.Color { return colorText().Alpha(0.04) }
