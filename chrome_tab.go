package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// Tab widths: a tab is tabMaxW wide while there is room, and shares the
// strip below that down to tabMinW, as the browsers' tabs shrink; past that
// the strip scrolls sideways and keeps the front tab in view.
const (
	tabMaxW = 220
	tabMinW = 96
	// tabChromeW is what the title bar holds besides the tabs: the left
	// padding, the 新建标签页 button and the strip's tail, the window's drag
	// area and the three caption buttons.
	tabChromeW = 8 + 30 + 24 + tabDragMinW + 3*46
	// tabDragMinW keeps a piece of title bar to move the window by however
	// many tabs there are.
	tabDragMinW = 48

	// Explorer and browsers keep a file drag over another tab for a short
	// beat before bringing it forward, leaving time to pass over tabs.
	tabDropHoverDelay = 600 * time.Millisecond
)

// tabStrip is the Windows 11 title bar: the tab strip and the caption
// buttons share one row, and the window is frameless, so this bar is the
// topmost chrome. The bar right of the strip drags the window; a double
// click there maximizes. A double click on the strip's own empty space —
// between the tabs and past the last one — opens a new tab. There is no rule
// under it — the command bar sits on the same mica, as in Explorer.
func (a *App) tabStrip(c *ui.Context) {
	bar := windowBG(c, ui.Row(c.Key("title-bar")).Height(40).FillWidth().Shrink(0).
		AlignItems(ui.Center).Padding(0, 0, 0, 8).
		DragWindow())
	tabW := a.tabWidth(bar.Bounds().W)
	bar.Children(func() {
		// The strip's bar runs along the top edge, above the tabs, where it
		// covers no title.
		strip := ui.ScrollHorizontal(c.Key("tab-strip")).Height(40).Shrink(1).MinWidth(0).
			AlignItems(ui.Center).Gap(2).Padding(0, 24, 0, 0).Label("标签页").
			ScrollbarInsets(0, 0, 35, 0)
		strip.Children(func() {
			// A snapshot: a drag or a close in this frame reshapes a.tabs
			// under the loop, which must still build each tab once.
			for _, t := range slices.Clone(a.tabs) {
				a.tabItem(c, t, tabW)
			}
		})
		if strip.DoubleClicked() {
			a.openTab(a.cur().path)
		}
		// 新建标签页 sits outside the strip, so it stays in reach however far
		// the strip scrolls.
		plus := ui.ButtonBase(c).Size(28, 28).Radius(4).Shrink(0).
			Label("新建标签页").Tooltip("新建标签页 (Ctrl+T)")
		if plus.Hovered() || plus.Pressed() {
			plus.Background(colorHover())
		}
		plus.Children(func() {
			components.Icon(c, lucide.Plus).Size(14, 14).TextColor(colorText())
		})
		if plus.Clicked() {
			a.openTab(a.cur().path)
		}
		a.windowControls(c)
	})
}

// tabWidth is each tab's width for a title bar barW wide (the frame
// before's; zero on the first frame, which takes the full width).
func tabWidth(barW float32, n int) float32 {
	if barW <= 0 || n <= 0 {
		return tabMaxW
	}
	w := (barW-tabChromeW)/float32(n) - 2
	return min(max(w, tabMinW), tabMaxW)
}

func (a *App) tabWidth(barW float32) float32 { return tabWidth(barW, len(a.tabs)) }

// tabItem draws the tab at index i: its title and a close button. The
// active tab takes the content surface and rounds its top corners, as the
// Windows 11 active tab does. The tooltip carries the full path the short
// title leaves out. A middle click closes the tab; dragging it along the
// strip moves it.
func (a *App) tabItem(c *ui.Context, t *tabState, w float32) {
	id := t.id
	i := a.tabIndex(id)
	if i < 0 {
		return // closed earlier in this frame
	}
	active := i == a.activeTab
	title := tabTitle(t.path)
	tab := ui.Row(c.Key(fmt.Sprintf("tab-%d", id))).Height(32).Width(w).
		Padding(0, 8, 0, 8).Gap(6).Radius(4, 4, 0, 0).Shrink(0).
		AlignItems(ui.Center).Role(ui.RoleTab).Label(title).
		Tooltip(tabTooltip(t.path))
	if active {
		tab.Background(colorContent())
		if a.tabReveal {
			a.tabReveal = false
			tab.ScrollIntoView()
		}
	} else if tab.Hovered() {
		tab.Background(colorHover())
	}
	tab.HandleInput(func(ev ui.InputEvent) bool { return a.tabPointer(id, ev) })
	closed := false
	tab.Children(func() {
		ui.Text(c, title).FontSize(12).SingleLine().Grow(1).MinWidth(0).
			TextColor(colorText()).MaxLines(1)
		close := ui.ButtonBase(c).Size(20, 20).Radius(4).Shrink(0).
			Label("关闭标签页").Tooltip("关闭标签页 (Ctrl+W)")
		if close.Hovered() || close.Pressed() {
			close.Background(colorHover())
		}
		close.Children(func() {
			components.Icon(c, lucide.X).Size(12, 12).TextColor(colorMuted())
		})
		if close.Clicked() {
			closed = true
		}
	})
	if closed {
		a.closeTabByID(id)
		return
	}
	a.tabDragFrame(tab, id, w)
	if i = a.tabIndex(id); i < 0 {
		return
	}
	if tab.Clicked() {
		a.switchTo(i)
	}
	// Dropping files on a tab moves them into that tab's folder, as
	// Explorer's tabs take drops. Holding the drag over a different tab
	// soon brings it forward so the drop can continue into its listing.
	hovered, dropped := a.bindDropFolder(c, tab, t.path)
	if hovered && !dropped && !active {
		tab.Background(colorHover())
	}
	a.tabDropHoverFrame(c, id, hovered, dropped)
	// The tab's own menu, as Explorer's tab context menu. The items name
	// the tab by id: the menu outlives the frame that built it.
	tab.ContextMenu(func(m *ui.Menu) {
		at := a.tabIndex(id)
		if at < 0 {
			return
		}
		if m.Item("新建标签页").Shortcut(ui.Cmd, ui.KeyT).Chosen() {
			a.openTab(a.cur().path)
		}
		if m.Item("复制标签页").Chosen() {
			a.duplicateTab(at)
		}
		m.Separator()
		if m.Item("关闭标签页").Shortcut(ui.Cmd, ui.KeyW).Disabled(len(a.tabs) <= 1).Chosen() {
			a.closeTab(at)
		}
		if m.Item("关闭其他标签页").Disabled(len(a.tabs) <= 1).Chosen() {
			a.closeOthers(at)
		}
		if m.Item("关闭右侧标签页").Disabled(at >= len(a.tabs)-1).Chosen() {
			a.closeRight(at)
		}
		m.Separator()
		if m.Item("重新打开关闭的标签页").Shortcut(ui.Cmd|ui.Shift, ui.KeyT).Disabled(len(a.closedTabs) == 0).Chosen() {
			a.reopenClosedTab()
		}
	})
}

// tabHoverSwitch reports whether a drag has hovered id long enough to bring
// it forward. Ids outside the tab collection are not switch targets, and a
// drag over the front tab never changes it.
func tabHoverSwitch(prevID, id int, since, now time.Time) bool {
	if id <= 0 || id == prevID || since.IsZero() {
		return false
	}
	return !now.Before(since.Add(tabDropHoverDelay))
}

// tabDropHoverFrame keeps one delayed tab switch across frames. Only the
// hovered tab advances its timer; another unhovered tab clears the pending
// state only when it is the tab being left, so tabs before the hovered one
// cannot restart the timer.
func (a *App) tabDropHoverFrame(c *ui.Context, id int, hovered, dropped bool) {
	if dropped || !hovered {
		if a.tabDropHover.id == id {
			a.tabDropHover = struct {
				id    int
				since time.Time
			}{}
		}
		return
	}
	i := a.tabIndex(id)
	if i < 0 {
		return
	}
	now := c.Now()
	if a.activeTab == i {
		if a.tabDropHover.id == id {
			a.tabDropHover = struct {
				id    int
				since time.Time
			}{}
		}
		return
	}
	if a.tabDropHover.id != id {
		a.tabDropHover.id, a.tabDropHover.since = id, now
	}
	if tabHoverSwitch(a.tabs[a.activeTab].id, id, a.tabDropHover.since, now) {
		a.tabDropHover = struct {
			id    int
			since time.Time
		}{}
		a.switchTo(i)
		// A timer-triggered switch has no pointer event to invalidate the
		// frame after the state changes.
		repaint()
		return
	}
	// The timer is the only reason to build another frame while the drag
	// remains over this tab.
	c.After(tabDropHoverDelay - now.Sub(a.tabDropHover.since))
}

// tabPointer is a tab's raw pointer input: the middle button closes it on
// release, as in every tabbed browser and Explorer. The other buttons go on
// to the tab's click and drag handling.
func (a *App) tabPointer(id int, ev ui.InputEvent) bool {
	if ev.Button != 2 {
		return false
	}
	switch ev.Kind {
	case ui.InputPointerDown:
		return true
	case ui.InputPointerUp:
		a.closeTabByID(id)
		repaint()
		return true
	}
	return false
}

// tabDragFrame moves a pressed tab along the strip as the pointer drags
// it: once the pointer has gone half a tab past the tab's place, the tab
// swaps with its neighbour that way.
func (a *App) tabDragFrame(tab ui.Element, id int, w float32) {
	// Asked every frame, so the tab claims its drags before the strip, which
	// scrolls by dragging, would take them.
	// Dragged's ok is the press itself, which Pressed is not: Pressed goes
	// false as the pointer leaves the tab, as it does mid-drag.
	dx, _, held := tab.Dragged()
	if !held {
		if a.tabDrag.id == id {
			a.tabDrag.id, a.tabDrag.acc = 0, 0
		}
		return
	}
	if a.tabDrag.id != id {
		a.tabDrag.id, a.tabDrag.acc = id, 0
	}
	a.tabDrag.acc += dx
	step := w + 2
	for {
		i := a.tabIndex(id)
		switch {
		case a.tabDrag.acc > step/2 && i+1 < len(a.tabs):
			a.moveTab(i, i+1)
			a.tabDrag.acc -= step
		case a.tabDrag.acc < -step/2 && i > 0:
			a.moveTab(i, i-1)
			a.tabDrag.acc += step
		default:
			return
		}
	}
}

// tabTooltip is the full place a tab shows, for its tooltip: the path, or
// the page's name for the pages that are no folder.
func tabTooltip(path string) string {
	if specialPath(path) {
		return tabTitle(path)
	}
	return path
}

// windowControls ends the title bar: the empty strip that drags, then the
// three caption buttons Windows 11 puts at the right end — minimize,
// maximize, close — each a 46×40 flat target. Close hovers red.
func (a *App) windowControls(c *ui.Context) {
	// The bar itself drags; this filler only claims the room, and keeps a
	// piece of it however many tabs the strip holds.
	ui.Box(c).Grow(1).MinWidth(tabDragMinW).Height(40).DragWindow()
	a.winControl(c, "最小化", drawWinMin, false, func() {
		if a.win != nil {
			a.win.Minimize()
		}
	})
	a.winControl(c, "最大化", drawWinMax, false, func() {
		if a.win != nil {
			a.win.ToggleMaximize()
		}
	})
	a.winControl(c, "关闭", drawWinClose, true, func() {
		if a.win != nil {
			a.win.Close()
		}
	})
}

func (a *App) winControl(c *ui.Context, label string, glyph func(*ui.Painter, ui.Rect, ui.Color), danger bool, act func()) {
	btn := ui.ButtonBase(c).Width(46).Height(40).Radius(0).Shrink(0).
		Label(label).Tooltip(label)
	red := danger && (btn.Hovered() || btn.Pressed())
	switch {
	case red:
		btn.Background(colorCloseHover())
	case btn.Hovered() || btn.Pressed():
		btn.Background(colorHover())
	}
	btn.Children(func() {
		box := ui.Box(c).Size(12, 12).Shrink(0)
		box.Draw(func(p *ui.Painter, r ui.Rect) {
			ink := colorText()
			if red {
				ink = ui.Hex("#FFFFFF")
			}
			glyph(p, r, ink)
		})
	})
	if btn.Clicked() {
		act()
	}
}

func drawWinMin(p *ui.Painter, r ui.Rect, ink ui.Color) {
	var l ui.Path
	l.MoveTo(r.X+r.W*0.15, r.Y+r.H*0.5).LineTo(r.X+r.W*0.85, r.Y+r.H*0.5)
	p.StrokePath(&l, 1, ink)
}

func drawWinMax(p *ui.Painter, r ui.Rect, ink ui.Color) {
	sq := ui.Rect{X: r.X + r.W*0.15, Y: r.Y + r.H*0.15, W: r.W * 0.7, H: r.H * 0.7}
	p.Stroke(sq, ink, 1, 1)
}

func drawWinClose(p *ui.Painter, r ui.Rect, ink ui.Color) {
	k := r.W * 0.28
	var x ui.Path
	x.MoveTo(r.X+k, r.Y+k).LineTo(r.X+r.W-k, r.Y+r.H-k)
	x.MoveTo(r.X+r.W-k, r.Y+k).LineTo(r.X+k, r.Y+r.H-k)
	p.StrokePath(&x, 1, ink)
}

// tabTitle is the name a tab shows: the same words the address bar and the
// sidebar use (主文件夹, 画廊, 下载) rather than a place's internal id or
// the English folder name on disk; any other folder shows its base name.
func tabTitle(path string) string {
	switch path {
	case pathThisPC:
		return "此电脑"
	case pathHome:
		return "主文件夹"
	case pathGallery:
		return "画廊"
	case pathTrash:
		return "回收站"
	case pathDisks:
		return "磁盘健康"
	}
	if n, ok := knownFolderNames()[filepath.Clean(path)]; ok {
		return n
	}
	if filepath.Clean(path) == "/" {
		return sysRootName()
	}
	name := filepath.Base(path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "此电脑"
	}
	return name
}

// commandBar is the Windows 11 command bar: 新建 | 剪切 复制 粘贴 重命名
// 分享 删除 | 排序 查看, then 查看更多 at the right end. The middle group is
// icons only; 新建, 排序 and 查看 carry labels and chevrons. Cut, copy,
// rename and delete stay muted until something is selected; paste stays
// muted while the clipboard is empty. The bar calls the ops methods; it
// does not touch the filesystem.
func (a *App) commandBar(c *ui.Context) {
	hasSel := len(a.cur().selected) > 0
	canPaste := a.canPaste()

	windowBG(c, ui.Row(c).Height(44).FillWidth().AlignItems(ui.Center).
		Padding(0, 6).Gap(2)).
		FocusGroup(ui.Horizontal).Children(func() {
		a.commandMenu(c, lucide.FolderPlus, "新建", "新建", a.newMenu)

		a.commandSep(c)

		a.commandIconButton(c, lucide.Scissors, "剪切", hasSel, a.doCut)
		a.commandIconButton(c, lucide.Copy, "复制", hasSel, a.doCopy)
		a.commandIconButton(c, lucide.ClipboardPaste, "粘贴", canPaste, a.doPaste)
		a.commandIconButton(c, lucide.Pencil, "重命名", hasSel, a.doRenameStart)
		a.commandIconButton(c, lucide.Share2, "分享", hasSel, a.shareDialogOpen)
		a.commandIconButton(c, lucide.Trash, "删除", hasSel, a.doDelete)

		a.commandSep(c)

		a.commandMenu(c, lucide.ArrowDownWideNarrow, "排序", "排序", a.sortMenu)
		a.commandMenu(c, lucide.LayoutGrid, "查看", "查看", a.viewMenu)
		a.commandIconButton(c, lucide.ListChecks, "任务 (Ctrl+Shift+J)", true, a.toggleTasksView)
		a.commandIconButton(c, lucide.Bot, "助手", true, a.toggleAssistant)

		a.moreButton(c)
	})
}

// commandSep is the 1px hairline between command groups, about 16 tall.
func (a *App) commandSep(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Shrink(0).Margin(0, 4).
		Background(colorDivider())
}

// commandIconButton is a 32×32 icon-only command built on Onevra's
// IconButton: the Lucide glyph, its tooltip and focus handling come from the
// component, and MyGo grays out and drops input on the disabled face.
func (a *App) commandIconButton(c *ui.Context, icon func() *ui.SVG, tip string, enabled bool, act func()) {
	btn := components.IconButton(c, icon(), tip, components.IconButtonPlain).Disabled(!enabled)
	if btn.Clicked() {
		act()
	}
}

// commandMenu is a command button that opens a menu: a Lucide icon, a
// label and a chevron. Under 经典 the menu is a Fluent flyout drawn in the
// window (menuflyout.go); under v2 the system's.
func (a *App) commandMenu(c *ui.Context, icon func() *ui.SVG, label, tip string, build func() []*menuEntry) {
	btn := ui.ButtonBase(c).Height(32).Padding(4, 8).Gap(4).Radius(4).
		Shrink(0).Label(label).Tooltip(tip)
	if btn.Hovered() || btn.Pressed() || a.fly.open == label {
		btn.Background(colorHover())
	}
	btn.TextColor(colorText()).Children(func() {
		components.Icon(c, icon).Size(16, 16)
		ui.Text(c, label).FontSize(12).SingleLine()
		chevronIcon(c, true, 10)
	})
	a.attachCommandMenu(c, btn, label, false, build)
}

// moreButton is the 查看更多 overflow at the command bar's right end; its
// flyout lines up with the button's right edge, as Explorer's does.
func (a *App) moreButton(c *ui.Context) {
	btn := components.IconButton(c, lucide.Ellipsis(), "查看更多", components.IconButtonPlain)
	a.attachCommandMenu(c, btn, "查看更多", true, a.moreMenu)
}

func (a *App) newMenu() []*menuEntry {
	var l menuList
	l.item("文件夹", lucide.FolderPlus, a.doNewFolder).shortcut(ui.Cmd|ui.Shift, ui.KeyN)
	return l.es
}

// moreMenu builds the command bar's 查看更多 menu. Layout mirrors Windows 11
// Explorer's 「…」: undo; the selection verbs; a 插件 submenu for extension
// entries; then properties and settings, every row with a glyph
// (docs/specs/16-commands-layout.md). The panes (预览窗格, 助手) live under
// 查看 > 显示 and the bar's own 助手 button, so they are not repeated here.
func (a *App) moreMenu() []*menuEntry {
	var l menuList
	l.item("撤销", lucide.Undo2, a.doUndo).shortcut(ui.Cmd, ui.KeyZ).off(!a.canUndo())
	l.sep()
	l.item("全选", lucide.CheckCheck, a.selectAll).shortcut(ui.Cmd, ui.KeyA)
	l.item("全部取消", lucide.SquareDashed, a.clearSelection).off(len(a.cur().selected) == 0)
	l.item("反向选择", lucide.ArrowLeftRight, a.invertSelection)
	if items := a.pluginMoreItems(); len(items) > 0 {
		l.sep()
		// Plugin overflow items nest under 插件 so a few plugins cannot
		// flatten the top-level flyout. A plugin being off keeps its items
		// out: the settings page's 插件 switch is what brings them back.
		var sub menuList
		for _, it := range items {
			e := sub.item(it.Label, nil, it.Run).off(it.Disabled)
			if it.Checked {
				e.check(true)
			}
		}
		l.submenu("插件", lucide.Puzzle, sub.es)
	}
	l.sep()
	l.item("属性", lucide.Info, a.showProperties).shortcut(ui.Alt, ui.KeyEnter).off(len(a.selectedEntries()) != 1)
	l.item("设置", lucide.Settings, func() { a.settingsOpen = true }).shortcut(ui.Cmd, ui.KeyComma)
	return l.es
}

func (a *App) sortMenu() []*menuEntry {
	var l menuList
	keys := []struct {
		label string
		icon  func() *ui.SVG
		key   SortKey
	}{
		{"名称", lucide.CaseSensitive, SortName},
		{"修改日期", lucide.Calendar, SortDate},
		{"类型", lucide.FileType, SortType},
		{"大小", lucide.Ruler, SortSize},
	}
	for _, k := range keys {
		key := k.key
		l.item(k.label, k.icon, func() { a.toggleSort(key) }).choice(a.sortKey == key)
	}
	l.sep()
	l.item("递增", lucide.ArrowUpNarrowWide, func() {
		if !a.sortAsc {
			a.toggleSort(a.sortKey)
		}
	}).choice(a.sortAsc)
	l.item("递减", lucide.ArrowDownNarrowWide, func() {
		if a.sortAsc {
			a.toggleSort(a.sortKey)
		}
	}).choice(!a.sortAsc)
	return l.es
}

func (a *App) viewMenu() []*menuEntry {
	var l menuList
	modes := []struct {
		label string
		icon  func() *ui.SVG
		mode  ViewMode
	}{
		{"超大图标", lucide.Grid2x2, ViewExtraLarge},
		{"大图标", lucide.LayoutGrid, ViewLargeIcons},
		{"中等图标", lucide.Grid3x3, ViewMediumIcons},
		{"小图标", lucide.LayoutList, ViewSmallIcons},
		{"列表", lucide.List, ViewList},
		{"详细信息", lucide.TableProperties, ViewDetails},
	}
	for _, m := range modes {
		mode := m.mode
		l.item(m.label, m.icon, func() { a.setMode(mode) }).choice(a.mode == mode)
	}
	l.sep()
	l.item("紧凑视图", lucide.Rows3, func() { a.compact = !a.compact }).check(a.compact)
	l.sep()
	l.item("放大图标", lucide.ZoomIn, a.zoomIn).shortcut(ui.Cmd|ui.Shift, ui.KeyEqual).off(!a.canZoomIn())
	l.item("缩小图标", lucide.ZoomOut, a.zoomOut).shortcut(ui.Cmd|ui.Shift, ui.KeyMinus).off(!a.canZoomOut())
	l.sep()
	var show menuList
	// The panes first, as Explorer's 查看 > 显示 lists 预览窗格.
	show.item("预览窗格", nil, a.togglePreview).shortcut(ui.Alt, ui.KeyP).check(a.previewOpen)
	show.item("助手", nil, a.toggleAssistant).check(a.assistantOpen)
	show.sep()
	show.item("文件扩展名", nil, func() { a.showExt = !a.showExt }).check(a.showExt)
	show.item("隐藏的项目", nil, a.toggleHidden).check(a.showHidden)
	show.item("项目复选框", nil, func() { a.itemCheckboxes = !a.itemCheckboxes }).check(a.itemCheckboxes)
	l.submenu("显示", lucide.Eye, show.es)
	var group menuList
	groups := []struct {
		label string
		key   SortKey
	}{
		{"无", groupNone},
		{"名称", SortName},
		{"修改日期", SortDate},
		{"类型", SortType},
		{"大小", SortSize},
	}
	for _, g := range groups {
		key := g.key
		group.item(g.label, nil, func() { a.grouping = key }).choice(a.grouping == key)
	}
	l.submenu("分组依据", lucide.Group, group.es)
	return l.es
}
