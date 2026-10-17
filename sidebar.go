package main

import "github.com/egoist/mygo/ui"

const (
	navMinWidth float32 = 180
	navMaxWidth float32 = 420
	navRowH     float32 = 32
)

// sidebar is the Windows 11 navigation pane: a flat tree — 主文件夹, the
// user folders, then 此电脑 with its volumes under it. The pane is
// a.navWidth wide; a drag handle on its right edge resizes it, clamped to
// 180–420.
func (a *App) sidebar(c *ui.Context) {
	pane := ui.Column(c).Width(a.navWidth).FillHeight().Shrink(0).
		Background(colorNav()).Clip()
	pane.Children(func() {
		ui.Scroll(c).Grow(1).FillWidth().Padding(8, 8, 12, 8).Gap(2).Children(func() {
			a.navHome(c)
			// The plugins' places — the gallery page is the gallery
			// plugin's; switched off, the pane does not list it, the page
			// itself keeping a note.
			for _, it := range a.pluginNavItems() {
				a.navItem(c, it.Key, it.Label, it.Kind, it.Path, a.cur().path == it.Path, it.Run, 0)
			}
			for _, p := range a.pinnedPlaces() {
				a.navPlace(c, p, 0)
			}
			a.navThisPC(c)
			if a.navPCExpanded {
				for _, p := range a.thisPCPlaces() {
					a.navPlace(c, p, 24)
				}
				a.navDisks(c)
				a.navTrash(c)
			}
		})
		a.navResizeHandle(c)
	})
}

// navHome is 主文件夹, the pane's first row. It navigates to the user's
// home directory and is selected when that is the current location.
func (a *App) navHome(c *ui.Context) {
	a.navItem(c, "nav-home", "主文件夹", "home", pathHome, a.cur().path == pathHome, func() {
		a.navigate(pathHome)
	}, 0)
}

// navThisPC is the 此电脑 row: a chevron that expands the volumes, then the
// row itself, which navigates to pathThisPC.
func (a *App) navThisPC(c *ui.Context) {
	row := ui.ButtonBase(c.Key("nav-thispc")).FillWidth().Height(navRowH).
		Radius(4).Padding(0, 6, 0, 8).Gap(6).AlignItems(ui.Center).
		Label("此电脑")
	switch {
	case a.cur().path == pathThisPC:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		chev := ui.ButtonBase(c).Size(22, navRowH).Radius(4).Shrink(0).
			Label("展开此电脑").Tooltip("展开")
		if chev.Hovered() {
			chev.Background(colorHover())
		}
		chev.Children(func() {
			chevronIcon(c, a.navPCExpanded, 12)
		})
		if chev.Clicked() {
			a.navPCExpanded = !a.navPCExpanded
			return
		}
		icon := ui.Box(c).Size(16, 16).Shrink(0)
		icon.Children(func() {
			a.placeIcon(c, "thispc", 16)
		})
		ui.Text(c, "此电脑").FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navigate(pathThisPC)
	}
}

// navTrash is the 回收站 row under the expanded 此电脑.
func (a *App) navTrash(c *ui.Context) {
	a.navItem(c, "nav-trash", "回收站", "trash", pathTrash, a.cur().path == pathTrash, func() {
		a.navigate(pathTrash)
	}, 24)
}

// navDisks is the 磁盘健康 row under the expanded 此电脑.
func (a *App) navDisks(c *ui.Context) {
	a.navItem(c, "nav-disks", "磁盘健康", "drive", pathDisks, a.cur().path == pathDisks, func() {
		a.navigate(pathDisks)
	}, 24)
}

func (a *App) navPlace(c *ui.Context, p Place, indent float32) {
	place := p
	row := a.navItem(c, "nav-"+place.Path, place.Name, place.Kind, place.Path, a.cur().path == place.Path, func() {
		a.navigate(place.Path)
	}, indent)
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("打开").Chosen() {
			a.navigate(place.Path)
		}
		if a.store.isPinned(place.Path) {
			if m.Item("从快速访问取消固定").Chosen() {
				if a.store.unpin(place.Path) {
					a.reload()
				}
			}
		}
		m.Separator()
		if m.Item("属性").Chosen() {
			a.selectOnly(place.Path)
			a.showProperties()
		}
	})
}

// navItem is one navigation row: a 16px place icon, a 12px label, height
// 32, with a rounded selection or hover. pad indents the row's left edge
// for the volumes under 此电脑. Folder rows also accept file drops,
// moving the dragged items into them.
func (a *App) navItem(c *ui.Context, key, label, kind, path string, selected bool, act func(), pad float32) ui.Element {
	row := ui.ButtonBase(c.Key(key)).FillWidth().Height(navRowH).
		Radius(4).Padding(0, 8, 0, 8+pad).Gap(8).AlignItems(ui.Center).
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
	a.bindDropFolder(c, row, path)
	return row
}

// navResizeHandle is the wider grip over the pane's right edge. Dragging
// writes a.navWidth, clamped to 180–420. Element.Dragged reports the
// pointer's movement since the last frame while the grip is pressed (see
// input.md). The pane shows no static divider: the Explorer pane is
// separated from the content by its background alone.
func (a *App) navResizeHandle(c *ui.Context) {
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
