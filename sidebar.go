package main

import "github.com/egoist/mygo/ui"

const (
	navMinWidth float32 = 180
	navMaxWidth float32 = 420
	navRowH     float32 = 32
)

// sidebar is the Windows 11 navigation pane: 快速访问, then 此电脑 and its
// volumes. The pane is a.navWidth wide; its right edge is a 1px divider
// and a drag handle that resizes it, clamped to 180–420.
func (a *App) sidebar(c *ui.Context) {
	pane := ui.Column(c).Width(a.navWidth).FillHeight().Shrink(0).
		Background(colorNav()).Clip()
	pane.Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().Padding(8, 8, 12, 8).Gap(2).Children(func() {
			a.navSectionLabel(c, "快速访问")
			for _, p := range a.quickPlaces() {
				a.navPlace(c, p)
			}
			a.navSectionGap(c)
			a.navThisPC(c)
			for _, p := range a.thisPCPlaces() {
				a.navPlace(c, p)
			}
		})
		a.navResizeHandle(c)
	})
}

func (a *App) navSectionLabel(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(12).TextColor(colorMuted()).
		Padding(8, 8, 2, 8).SingleLine()
}

func (a *App) navSectionGap(c *ui.Context) {
	ui.Box(c).Height(10).FillWidth()
}

// navThisPC is the 此电脑 header. It navigates to pathThisPC and is selected
// when that is the current location.
func (a *App) navThisPC(c *ui.Context) {
	a.navItem(c, "nav-thispc", "此电脑", "thispc", a.cur().path == pathThisPC, func() {
		a.navigate(pathThisPC)
	})
}

func (a *App) navPlace(c *ui.Context, p Place) {
	place := p
	a.navItem(c, "nav-"+place.Path, place.Name, place.Kind, a.cur().path == place.Path, func() {
		a.navigate(place.Path)
	})
}

// navItem is one navigation row: a 16px place icon, a 12px label, height
// 32, with a rounded selection or hover.
func (a *App) navItem(c *ui.Context, key, label, kind string, selected bool, act func()) {
	row := ui.ButtonBase(c.Key(key)).FillWidth().Height(navRowH).
		Radius(4).Padding(0, 8).Gap(8).AlignItems(ui.Center).
		Label(label).Selected(selected)
	switch {
	case selected:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		icon := ui.Box(c).Size(16, 16).Shrink(0)
		icon.Children(func() {
			a.placeIcon(c, kind, 16)
		})
		ui.Text(c, label).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		act()
	}
}

// navResizeHandle is the 1px divider on the pane's right edge and the
// wider grip over it. Dragging writes a.navWidth, clamped to 180–420.
// Element.Dragged reports the pointer's movement since the last frame
// while the grip is pressed (see input.md).
func (a *App) navResizeHandle(c *ui.Context) {
	ui.Box(c).Absolute().Top(0).Bottom(0).Right(0).Width(1).
		Background(colorDivider())

	grip := ui.Box(c).Absolute().Top(0).Bottom(0).Right(-3).Width(7).
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := grip.Dragged(); ok && dx != 0 {
		a.navWidth = clampNavWidth(a.navWidth + dx)
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		if !grip.Hovered() && !grip.Dragging() {
			return
		}
		p.Fill(ui.Rect{X: r.X + (r.W-1)/2, Y: r.Y, W: 1, H: r.H}, colorAccent(), 0)
	})
}

func clampNavWidth(w float32) float32 {
	if w < navMinWidth {
		return navMinWidth
	}
	if w > navMaxWidth {
		return navMaxWidth
	}
	return w
}
