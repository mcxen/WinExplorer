package main

import "github.com/egoist/mygo/ui"

const (
	navMinWidth float32 = 180
	navMaxWidth float32 = 420
	navRowH     float32 = 32
	// navRowGap is the room a row leaves above and below itself, so the pane
	// reads as a list of rows rather than one block, and navRowPad its left
	// padding without the tree's indent.
	navRowGap float32 = 1
	navRowPad float32 = 8
)

// navRowKind is what a row of the pane is: one of the fixed places and 此电脑's
// own rows, or a folder of the tree.
type navRowKind int

const (
	navRowHome navRowKind = iota
	navRowPlugin
	navRowPlace
	navRowThisPC
	navRowFolder
	navRowDisks
	navRowTrash
)

// navRow is one row gathered before any of them is built. The pane is a
// virtualized list — the tree may hold thousands of rows, and laying them all
// out every frame costs tens of milliseconds (measured: 40 ms for 3000 sibling
// folders) — so only what shows is built, from these.
type navRow struct {
	kind  navRowKind
	key   string
	label string
	// icon is a placeIcon kind, or "folder" for a folder of the tree, which
	// draws the ordinary folder glyph.
	icon string
	path string
	// indent is the row's extra left padding past navRowPad: the tree's depth,
	// and 此电脑's own rows, which sit one step in.
	indent float32
	// act replaces navigation, for a plugin's page.
	act func()
	// open and hasKids are the folder's twist: whether it shows the folders
	// under it, and whether it may have any.
	open    bool
	hasKids bool
}

// sidebar is the Windows 11 navigation pane: 主文件夹, the plugins' pages, the
// pinned places, then 此电脑 as a folder tree — its volumes, and the folders
// under every one of them the user opens. The tree follows the tab: navigating
// the pane opens the current folder's ancestors and scrolls its row into view
// (navtree.go). The pane is a.navWidth wide; a drag handle on its right edge
// resizes it, clamped to 180–420.
func (a *App) sidebar(c *ui.Context) {
	pane := windowBG(c, ui.Column(c).Width(a.navWidth).FillHeight().Shrink(0)).Clip()
	pane.Children(func() {
		// The tab may have moved since the last frame: open what it takes for
		// the tree to show where it is, before the rows are gathered.
		a.navTreeFollow()
		rows := a.navRows()
		ui.List(c, &a.navTree.list, len(rows), func(i int) {
			a.navPaneRow(c, rows[i])
		}).Grow(1).FillWidth().MinHeight(0).Padding(8, 0, 12, 0)
		// The current folder's row has its place in the list by now: show it if
		// the list has it out of sight.
		a.navTreeReveal()
		a.navResizeHandle(c)
	})
}

// navRows gathers the pane's rows: 主文件夹, the plugins' pages, the pinned
// places, then 此电脑 with its volumes and the folders under them the user
// opened. The current location's row is noted for the reveal.
func (a *App) navRows() []navRow {
	rows := []navRow{{kind: navRowHome, key: "nav-home", label: "主文件夹", icon: "home", path: pathHome}}
	for _, it := range a.pluginNavItems() {
		rows = append(rows, navRow{kind: navRowPlugin, key: it.Key, label: it.Label, icon: it.Kind, path: it.Path, act: it.Run})
	}
	for _, p := range a.pinnedPlaces() {
		rows = append(rows, navRow{kind: navRowPlace, key: "nav-" + p.Path, label: p.Name, icon: p.Kind, path: p.Path})
	}
	rows = append(rows, navRow{
		kind: navRowThisPC, key: "nav-thispc", label: "此电脑", icon: "thispc",
		path: pathThisPC, open: a.navPCExpanded,
	})
	if a.navPCExpanded {
		for _, p := range a.thisPCPlaces() {
			rows = a.navTreeRows(rows, p.Name, p.Path, p.Kind, 0)
		}
		if a.pluginEnabled(diskPluginID) {
			rows = append(rows, navRow{
				kind: navRowDisks, key: "nav-disks", label: "磁盘健康", icon: "drive",
				path: pathDisks, indent: navIndent(0),
			})
		}
		rows = append(rows, navRow{
			kind: navRowTrash, key: "nav-trash", label: "回收站", icon: "trash",
			path: pathTrash, indent: navIndent(0),
		})
	}
	a.navTree.row = -1
	for i, r := range rows {
		if r.path != "" && r.path == a.cur().path {
			a.navTree.row = i
			break
		}
	}
	return rows
}

// navTreeRows appends a folder and, when it is open, the folders under it — read
// the first time a folder opens (navChildren) — one row each. depth sets the
// indent and the room the twist takes.
func (a *App) navTreeRows(rows []navRow, name, path, kind string, depth int) []navRow {
	kids, read := a.navTree.kids[path]
	open := a.navTree.open[path]
	rows = append(rows, navRow{
		kind: navRowFolder, key: "navtree-" + path, label: name, icon: kind, path: path,
		indent: navIndent(depth), open: open, hasKids: !read || len(kids) > 0,
	})
	if !open {
		return rows
	}
	for _, kid := range a.navChildren(path) {
		rows = a.navTreeRows(rows, kid.Name, kid.Path, "folder", depth+1)
	}
	return rows
}

// navPaneRow builds one row of the pane: what the list shows, and nothing else.
func (a *App) navPaneRow(c *ui.Context, r navRow) {
	switch r.kind {
	case navRowThisPC:
		a.navThisPCRow(c, r)
	case navRowFolder:
		a.navFolderRow(c, r)
	default:
		a.navPlaceRow(c, r)
	}
}

// navPlaceRow is a row that goes somewhere: 主文件夹, a plugin's page, a pinned
// place, 磁盘健康, 回收站. A plugin's page runs its own action, the rest navigate;
// files dropped on the row move there, as anywhere else in the app.
func (a *App) navPlaceRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowH).Margin(navRowGap, navRowPad).
		Radius(4).Padding(0, 4, 0, r.indent).Gap(8).AlignItems(ui.Center).
		Label(r.label).Selected(a.cur().path == r.path)
	a.paintNavRow(row, r)
	row.Children(func() {
		a.navRowGlyph(c, r)
		ui.Text(c, r.label).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		if r.act != nil {
			r.act()
			return
		}
		a.navigate(r.path)
	}
	a.bindDropFolder(c, row, r.path)
	if r.kind == navRowPlace {
		a.navFolderMenu(row, r.label, r.path)
	}
}

// navThisPCRow is 此电脑: a twist that shows the volumes and the folders under
// them, and the row itself, which navigates to the This PC page.
func (a *App) navThisPCRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowH).Margin(navRowGap, navRowPad).
		Radius(4).Padding(0, 2).Gap(4).AlignItems(ui.Center).
		Label(r.label).Selected(a.cur().path == pathThisPC)
	a.paintNavRow(row, r)
	row.Children(func() {
		chev := a.navTwist(c, r, "展开此电脑")
		if chev.Clicked() {
			a.navPCExpanded = !a.navPCExpanded
			return
		}
		a.navRowGlyph(c, r)
		ui.Text(c, r.label).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navigate(pathThisPC)
	}
}

// navFolderRow is one folder of the tree: its twist, its icon and name, which
// navigate, its context menu; files dropped on it move into it.
func (a *App) navFolderRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowH).Margin(navRowGap, navRowPad).
		Radius(4).Padding(0, 2).Gap(4).AlignItems(ui.Center).
		Label(r.label).Selected(a.cur().path == r.path)
	a.paintNavRow(row, r)
	row.Children(func() {
		if gutter := r.indent - navIndent(0); gutter > 0 {
			ui.Box(c).Width(gutter).Height(navRowH).Shrink(0)
		}
		if r.hasKids {
			chev := a.navTwist(c, r, "展开 "+r.label)
			if chev.Clicked() {
				if a.navTree.open == nil {
					a.navTree.open = map[string]bool{}
				}
				a.navTree.open[r.path] = !a.navTree.open[r.path]
				return
			}
		} else {
			navTwistSlot(c)
		}
		a.navRowGlyph(c, r)
		ui.Text(c, r.label).FontSize(12).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navigate(r.path)
	}
	a.bindDropFolder(c, row, r.path)
	a.navFolderMenu(row, r.label, r.path)
}

// navTwist is the chevron that opens and closes a folder, with the label the
// tests and the accessibility tree read it by.
func (a *App) navTwist(c *ui.Context, r navRow, label string) ui.Element {
	chev := ui.ButtonBase(c).Size(18, navRowH).Radius(4).Shrink(0).
		Label(label).Tooltip("展开")
	if chev.Hovered() {
		chev.Background(colorHover())
	}
	chev.Children(func() {
		chevronIcon(c, r.open, 12)
	})
	return chev
}

// navTwistSlot is the room a twist would take, for a folder with none.
func navTwistSlot(c *ui.Context) {
	ui.Box(c).Size(18, navRowH).Shrink(0)
}

// navRowGlyph is a row's 16px icon: a place or volume glyph by kind, and the
// folder glyph for a folder of the tree.
func (a *App) navRowGlyph(c *ui.Context, r navRow) {
	glyph := ui.Box(c).Size(16, 16).Shrink(0)
	glyph.Children(func() {
		if r.icon == "folder" {
			a.fileIcon(c, Entry{Name: r.label, Path: r.path, IsDir: true}, 16)
			return
		}
		a.placeIcon(c, r.icon, 16)
	})
}

// paintNavRow is the row's selected or hovered background, as everywhere the
// app lists places.
func (a *App) paintNavRow(row ui.Element, r navRow) {
	switch {
	case a.cur().path == r.path:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
}

// navFolderMenu is the context menu a folder row of the pane carries, pinned
// places and tree folders alike: open, unpin, a terminal, the plugins' folder
// actions, and the properties dialog.
func (a *App) navFolderMenu(row ui.Element, name, path string) {
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("打开").Chosen() {
			a.navigate(path)
		}
		if a.store.isPinned(path) {
			if m.Item("从快速访问取消固定").Chosen() {
				if a.store.unpin(path) {
					a.reload()
				}
			}
		}
		m.Separator()
		if m.Item("在终端中打开").Chosen() {
			a.openInTerminal(path)
		}
		// The plugins' folder actions — usage's treemap of this folder —
		// the same collectors the pane's menus read.
		sel := []Entry{{Name: name, Path: path, IsDir: true}}
		for _, it := range a.pluginMenuItems(sel) {
			if m.Item(it.Label).Disabled(it.Disabled).Checked(it.Checked).Chosen() {
				it.Run()
			}
		}
		m.Separator()
		if m.Item("属性").Chosen() {
			a.selectOnly(path)
			a.showProperties()
		}
	})
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
