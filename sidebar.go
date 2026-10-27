package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	navMinWidth float32 = 180
	navMaxWidth float32 = 420
	// navContentMin is the least room the file pane keeps beside the
	// navigation pane: components.Split holds the content to it and shrinks
	// the pane below navMinWidth rather than squeeze the listing out.
	navContentMin float32 = 320
	navRowH       float32 = 32
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
	navRowSection
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
	// depth is the tree's logical level. Indent may stop growing on a deep
	// tree, but the guide lines still know which ancestors a row belongs to.
	depth int
	// act replaces navigation, for a plugin's page.
	act func()
	// open and hasKids are the folder's twist: whether it shows the folders
	// under it, and whether it may have any.
	open    bool
	hasKids bool
	// file is a pinned file. A click reveals it in its folder; the row is
	// not a directory a drop can move into.
	file bool
	// pinDrop is the 快速访问 heading: a drop pins paths instead of moving
	// them. The heading is not itself a place.
	pinDrop bool
	// sel marks the one row that shows where the tab is. A folder can be
	// listed twice (pinned and in the tree); only one row lights up.
	sel bool
	// under marks the nearest shown ancestor of a current folder the tree
	// does not show (展开到打开的文件夹 off).
	under bool
	// focus is the keyboard cursor's row. It is separate from sel: moving
	// around the tree must not navigate the tab behind it.
	focus bool
}

// navCurrentRow is the row standing for path: the row the user clicked to
// get there when it still lists path, else the first that does.
// navAncestorRow is the deepest folder row that holds path, -1 if none.
func navAncestorRow(rows []navRow, path string) int {
	best, bestLen := -1, 0
	for i, r := range rows {
		if r.path == "" || specialPath(r.path) || !pathWithin(path, r.path) || len(r.path) <= bestLen {
			continue
		}
		best, bestLen = i, len(r.path)
	}
	return best
}

func navCurrentRow(rows []navRow, path, clicked string) int {
	first := -1
	for i, r := range rows {
		if r.path == "" || r.path != path {
			continue
		}
		if clicked != "" && r.key == clicked {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// sidebar is the navigation pane: 主文件夹, the plugins' pages, 快速访问
// (pinned files and folders), 最近位置, then 此电脑 as a folder tree — its
// volumes, and the folders under every one of them the user opens. Folder
// rows can be dragged, and a drop on the 快速访问 heading pins instead of
// moving. The tree follows the tab: navigating the
// pane opens the current folder's ancestors and scrolls its row into view
// (navtree.go). components.Split (main.go's bodyRow) gives the pane its width
// and the divider on its right edge, which draws the hairline and resizes the
// pane when dragged, so the pane itself only fills the room it is given.
func (a *App) sidebar(c *ui.Context) {
	pane := windowBG(c, ui.Column(c).FillHeight().FillWidth().Shrink(0)).Clip()
	if visualV2 {
		// macOS 26: the sidebar floats on the window as a glass pane.
		pane.Margin(0, 2, v2Gutter, v2Gutter)
		macFloat(c, pane, a.flipLowEnd)
	}
	a.navTree.hover = pane.Hovered()
	pane.Children(func() {
		// The tab may have moved since the last frame: open what it takes for
		// the tree to show where it is, before the rows are gathered.
		a.navTreeFollow()
		rows := a.navRows()
		a.navTree.list.Key = func(i int) any { return rows[i].key }
		ui.List(c, &a.navTree.list, len(rows), func(i int) {
			a.navPaneRow(c, rows[i])
		}).Grow(1).FillWidth().MinHeight(0).Padding(sidebarListPad(), sidebarSidePad(), 12, sidebarSidePad())
		// The current folder's row has its place in the list by now: show it if
		// the list has it out of sight.
		a.navTreeReveal()
	})
}

// recentPlacesLimit is how many 最近位置 the sidebar shows: enough to get
// back somewhere from this session, few enough not to push 此电脑 down.
const recentPlacesLimit = 8

// dirExistsTTL is how long a 最近位置 existence check is trusted. The pane
// asks every frame, and an os.Stat on the UI thread per row per frame is a
// stall on a network share whose server went away; a stale "yes" costs one
// failed listing, a stale "no" one frame of a missing row.
const dirExistsTTL = 30 * time.Second

var (
	dirExistsMu   sync.Mutex
	dirExistsSeen = map[string]dirExistsEntry{}
)

type dirExistsEntry struct {
	ok bool
	at time.Time
}

// dirExists reports whether dir is a folder, caching the answer for
// dirExistsTTL.
func dirExists(dir string) bool {
	dirExistsMu.Lock()
	if e, ok := dirExistsSeen[dir]; ok && time.Since(e.at) < dirExistsTTL {
		dirExistsMu.Unlock()
		return e.ok
	}
	dirExistsMu.Unlock()
	info, err := os.Stat(dir)
	ok := err == nil && info.IsDir()
	dirExistsMu.Lock()
	dirExistsSeen[dir] = dirExistsEntry{ok: ok, at: time.Now()}
	dirExistsMu.Unlock()
	return ok
}

// recentPlaceRows are the sidebar's 最近位置 rows. The list holds still
// while the window is open: a folder joins it, at the end, once the user
// has left it, and then keeps its place — current or not — until 刷新 or
// a restart sorts it newest first again. A click on a recent row therefore
// leaves every row above and below where it was, rather than taking the row
// away and pushing the tree up under the pointer. A pinned place has its
// own row and is left out; a folder the tree shows when it would join is
// not added. A folder that no longer exists leaves the list, and the store.
func (a *App) recentPlaceRows() []navRow {
	pinned := map[string]bool{}
	for _, p := range a.pinnedPlaces() {
		pinned[p.Path] = true
	}
	cur := a.cur().path
	t := &a.navTree
	if t.recentIn == nil {
		t.recentIn = map[string]bool{}
	}
	var shown map[string]bool
	for _, dir := range a.store.visitList(maxVisited) {
		if len(t.recent) >= recentPlacesLimit {
			break
		}
		if t.recentIn[dir] || dir == cur || pinned[dir] {
			continue
		}
		if shown == nil {
			shown = map[string]bool{}
			a.navTreeShown(shown)
		}
		if shown[dir] {
			continue
		}
		t.recent = append(t.recent, dir)
		t.recentIn[dir] = true
	}
	var rows []navRow
	kept := t.recent[:0]
	for _, dir := range t.recent {
		if !dirExists(dir) {
			a.store.forgetVisit(dir)
			delete(t.recentIn, dir)
			continue
		}
		kept = append(kept, dir)
		if pinned[dir] {
			continue
		}
		rows = append(rows, navRow{
			kind: navRowPlace, key: "nav-recent-" + dir,
			label: filepath.Base(dir), icon: "folder", path: dir,
		})
	}
	t.recent = kept
	return rows
}

// navRecentReset lets 最近位置 sort itself newest first again.
func (a *App) navRecentReset() {
	a.navTree.recent = nil
	a.navTree.recentIn = nil
}

// navTreeShown adds the folders the tree draws to shown: the volumes' tree
// rows, the folders they show and, only when expanded, their children. A
// collapsed folder's children are not drawn, so they belong in 最近位置.
func (a *App) navTreeShown(shown map[string]bool) {
	if !a.navPCExpanded {
		return
	}
	for _, p := range a.thisPCPlaces() {
		a.navTreeShownAt(shown, p.Path)
	}
}

// navTreeShownAt walks one tree branch: the folder is drawn, and its
// children only when it is open.
func (a *App) navTreeShownAt(shown map[string]bool, path string) {
	shown[path] = true
	if !a.navTree.open[path] {
		return
	}
	for _, kid := range a.navChildren(path) {
		a.navTreeShownAt(shown, kid.Path)
	}
}

// navRows gathers the pane's rows: 主文件夹, the plugins' pages, the 快速访问
// heading and the pinned places under it, 最近位置 when there are any, then
// 此电脑 with its volumes and the folders under them the user opened. The
// heading stays when nothing is pinned, so a drop still has a target. The
// current location's row is noted for the reveal.
func (a *App) navRows() []navRow {
	rows := []navRow{{kind: navRowHome, key: "nav-home", label: "主文件夹", icon: "home", path: pathHome}}
	for _, it := range a.pluginNavItems() {
		rows = append(rows, navRow{kind: navRowPlugin, key: it.Key, label: it.Label, icon: it.Kind, path: it.Path, act: it.Run})
	}
	// The 快速访问 heading stays when collapsed — and when nothing is
	// pinned — so a drop still has a target; its pins hide with it.
	rows = append(rows, navRow{
		kind: navRowSection, key: "nav-quick-access", label: "快速访问", pinDrop: true,
		hasKids: true, open: a.navQuickExpanded,
	})
	if a.navQuickExpanded {
		for _, p := range a.pinnedPlaces() {
			rows = append(rows, navRow{
				kind: navRowPlace, key: "nav-" + p.Path, label: p.Name, icon: p.Kind, path: p.Path,
				file: p.Kind == "file",
			})
		}
	}
	if recent := a.recentPlaceRows(); len(recent) > 0 {
		rows = append(rows, navRow{
			kind: navRowSection, key: "nav-recent-head", label: "最近位置",
			hasKids: true, open: a.navRecentExpanded,
		})
		if a.navRecentExpanded {
			rows = append(rows, recent...)
		}
	}
	rows = append(rows, navRow{
		kind: navRowThisPC, key: "nav-thispc", label: "此电脑", icon: "thispc",
		path: pathThisPC, depth: -1, open: a.navPCExpanded, hasKids: true,
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
	a.navTree.row = navCurrentRow(rows, a.cur().path, a.navTree.clicked)
	a.navTree.activeDepth = -1
	if a.navTree.row >= 0 {
		rows[a.navTree.row].sel = true
		a.navTree.activeDepth = rows[a.navTree.row].depth
	} else if a.treeNoExpand() {
		// The current folder is inside a closed branch: its nearest shown
		// ancestor gets a faint mark instead.
		if i := navAncestorRow(rows, a.cur().path); i >= 0 {
			rows[i].under = true
		}
	}
	for i := range rows {
		rows[i].focus = a.navTree.focused && rows[i].key == a.navTree.cursor
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
		indent: navIndent(depth), depth: depth, open: open, hasKids: !read || len(kids) > 0,
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
	case navRowSection:
		a.navSectionRow(c, r)
	default:
		a.navPlaceRow(c, r)
	}
}

// navSectionRow is a heading: 快速访问 or 最近位置. It does not navigate.
// Its twist collapses the section, hiding its rows while the heading stays;
// 快速访问 takes a drop and pins it, collapsed or not. A folder row under
// the heading still moves a drop into that folder; only the heading pins.
func (a *App) navSectionRow(c *ui.Context, r navRow) {
	top := navRowGap
	if visualV2 {
		top = 10 // Finder-style section breathing room
	}
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowHt()).Margin(top, navRowPad).
		Radius(rowR()).Padding(0, 8, 0, 2).Gap(4).AlignItems(ui.Center).Justify(ui.Start).Label(r.label)
	a.paintNavRow(row, r)
	row.Children(func() {
		chev := a.navTwist(c, r, "折叠 "+r.label)
		if chev.Clicked() {
			a.navSetSectionOpen(r, !r.open)
			return
		}
		ui.Text(c, r.label).FontSize(11).FontWeight(600).TextColor(colorMuted()).
			SingleLine()
	})
	if row.Clicked() {
		a.navTreeFocus(r.key)
	}
	if r.pinDrop {
		a.bindPinDrop(row)
	}
}

// navSetSectionOpen opens or closes a section heading's rows.
func (a *App) navSetSectionOpen(r navRow, open bool) {
	switch r.key {
	case "nav-quick-access":
		a.navQuickExpanded = open
	case "nav-recent-head":
		a.navRecentExpanded = open
	}
}

// navPlaceRow is a row that goes somewhere: 主文件夹, a plugin's page, a pinned
// place, 磁盘健康, 回收站. A plugin's page runs its own action, the rest navigate;
// files dropped on the row move there, as anywhere else in the app.
func (a *App) navPlaceRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowHt()).Margin(navRowGap, navRowPad).
		Radius(rowR()).Padding(0, 4, 0, r.indent).Gap(8).AlignItems(ui.Center).
		Label(r.label).Selected(r.sel)
	a.paintNavRow(row, r)
	row.Children(func() {
		a.navRowGlyph(c, r)
		ui.Text(c, r.label).FontSize(bodyFont()).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navTreeFocus(r.key)
		if r.act != nil {
			r.act()
			return
		}
		// A place row is already in sight: the tree is left as it is, and
		// this row stays the one that lights up.
		a.navFromPane(r.key, r.path, false)
		a.openPinned(r.path, !r.file)
	}
	// A pinned file is not a directory, so a drop on it must not try to
	// move other files into it. A folder row still accepts that drop.
	if !r.file {
		a.bindDropFolder(c, row, r.path)
	}
	if navCanDrag(r) {
		a.bindDragSource(row, r.path)
	}
	if r.kind == navRowPlace {
		a.navFolderMenu(row, r.label, r.path, r.file)
	}
}

// navCanDrag reports whether the row names a real file or folder. Pages
// such as 主文件夹, 此电脑 and a plugin are not dragged out of the pane.
func navCanDrag(r navRow) bool {
	return r.path != "" && !specialPath(r.path)
}

// navThisPCRow is 此电脑: a twist that shows the volumes and the folders under
// them, and the row itself, which navigates to the This PC page.
func (a *App) navThisPCRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowHt()).Margin(navRowGap, navRowPad).
		Radius(rowR()).Padding(0, 2).Gap(4).AlignItems(ui.Center).
		Label(r.label).Selected(a.cur().path == pathThisPC)
	a.paintNavRow(row, r)
	row.Children(func() {
		chev := a.navTwist(c, r, "展开此电脑")
		if chev.Clicked() {
			a.navPCExpanded = !a.navPCExpanded
			return
		}
		a.navRowGlyph(c, r)
		ui.Text(c, r.label).FontSize(bodyFont()).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navFromPane(r.key, r.path, false)
		a.navigate(pathThisPC)
	}
}

// navFolderRow is one folder of the tree: its twist, its icon and name, which
// navigate, its context menu; files dropped on it move into it.
func (a *App) navFolderRow(c *ui.Context, r navRow) {
	row := ui.ButtonBase(c.Key(r.key)).FillWidth().Height(navRowHt()).Margin(navRowGap, navRowPad).
		Radius(rowR()).Padding(0, 2).Gap(4).AlignItems(ui.Center).
		Label(r.label).Selected(r.sel)
	a.paintNavRow(row, r)
	row.Children(func() {
		if gutter := r.indent - navIndent(0); gutter > 0 {
			navIndentGuides(c, gutter, r.depth, a.navTree.hover, a.navTree.activeDepth)
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
		ui.Text(c, r.label).FontSize(bodyFont()).TextColor(colorText()).
			SingleLine().Grow(1).MinWidth(0)
	})
	if row.Clicked() {
		a.navFromPane(r.key, r.path, true)
		a.navigate(r.path)
	}
	a.bindDropFolder(c, row, r.path)
	if navCanDrag(r) {
		a.bindDragSource(row, r.path)
	}
	a.navFolderMenu(row, r.label, r.path, false)
}

// navTwist is the chevron that opens and closes a folder, with the label the
// tests and the accessibility tree read it by.
func (a *App) navTwist(c *ui.Context, r navRow, label string) ui.Element {
	chev := ui.ButtonBase(c).Size(18, navRowHt()).Radius(rowR()).Shrink(0).
		Label(label).Tooltip("展开")
	if chev.Hovered() {
		chev.Background(colorHover())
	}
	chev.Children(func() {
		chevronIconAnimated(c, "navtwist-"+r.path, r.open, 12)
	})
	if chev.Clicked() {
		a.navTreeFocus(r.key)
	}
	return chev
}

// navIndentGuides draws the tree's VS Code-style indent guides in the gutter
// before a folder label. The line of each ancestor is muted along the active
// path; the whole set fades with the pane's hover state.
func navIndentGuides(c *ui.Context, width float32, depth int, hovered bool, active int) {
	gutter := ui.Box(c).Width(width).Height(navRowHt()).Shrink(0)
	// The indent stops at navTreeMaxIndent, so a deep branch must not keep
	// painting guides into the folder label beside the compressed gutter.
	lines := min(depth, int(width/navTreeStep))
	colors := make([]ui.Color, lines)
	for i := range colors {
		target := colorDivider().Alpha(0)
		if hovered {
			target = colorDivider()
			if i < active {
				target = colorMuted()
			}
		}
		colors[i] = gutter.AnimateColor("guide-"+itoa(i), target,
			ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseOut})
	}
	gutter.Draw(func(p *ui.Painter, r ui.Rect) {
		for i, ink := range colors {
			x := r.X + 7 + float32(i)*navTreeStep
			p.Fill(ui.Rect{X: x, Y: r.Y, W: 1, H: r.H}, ink, 0)
		}
	})
}

// navTwistSlot is the room a twist would take, for a folder with none.
func navTwistSlot(c *ui.Context) {
	ui.Box(c).Size(18, navRowHt()).Shrink(0)
}

// navRowGlyph is a row's 16px icon: a place or volume glyph by kind, and the
// folder glyph for a folder of the tree.
func (a *App) navRowGlyph(c *ui.Context, r navRow) {
	glyph := ui.Box(c).Size(16, 16).Shrink(0).Center()
	glyph.Children(func() {
		if r.file {
			a.fileIcon(c, Entry{Name: r.label, Path: r.path}, 16)
			return
		}
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
	case r.sel && visualV2:
		v2Chosen(row.Radius(v2RowRadius))
	case r.sel:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
		if visualV2 {
			row.Radius(v2RowRadius)
		}
	case r.under:
		row.Background(colorSelected().Alpha(0.35))
	}
	if r.focus && a.navTree.focused {
		// Keep the toolkit's focus on the row the logical cursor names. Its
		// Enter/Space activation is then the same one-shot action as a click;
		// without this, a previously clicked twist would keep the key.
		row.Focus()
		row.FocusRing(false).DrawOver(func(p *ui.Painter, rect ui.Rect) {
			r := float32(4)
			if visualV2 {
				r = v2RowRadius
			}
			p.FocusRing(rect, [4]float32{r, r, r, r})
		})
	}
}

// navFolderMenu is the context menu a folder row of the pane carries, pinned
// places and tree folders alike: open, unpin, a terminal, the plugins' folder
// actions, and the properties dialog.
func (a *App) navFolderMenu(row ui.Element, name, path string, file bool) {
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("打开").Chosen() {
			a.openPinned(path, !file)
		}
		if a.store.isPinned(path) {
			if m.Item("从快速访问取消固定").Chosen() {
				if a.store.unpin(path) {
					a.reload()
				}
			}
		}
		// A pinned file has no folder to open a terminal in, and the
		// plugins' folder actions do not apply to it.
		if !file {
			m.Separator()
			if m.Item("在终端中打开").Chosen() {
				a.openInTerminal(path)
			}
			sel := []Entry{{Name: name, Path: path, IsDir: true}}
			for _, it := range a.pluginMenuItems(sel) {
				if m.Item(it.Label).Disabled(it.Disabled).Checked(it.Checked).Chosen() {
					it.Run()
				}
			}
		}
		m.Separator()
		if m.Item("属性").Chosen() {
			a.selectOnly(path)
			a.showProperties()
		}
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
