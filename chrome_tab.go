package main

import (
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// tabStrip is the Windows 11 title bar: the tab strip and the caption
// buttons share one row, and the window is frameless, so this bar is the
// topmost chrome. The row drags the window; a double click on it maximizes.
// There is no rule under it — the command bar sits on the same mica, as in
// Explorer.
func (a *App) tabStrip(c *ui.Context) {
	bar := ui.Row(c).Height(40).FillWidth().Shrink(0).
		AlignItems(ui.Center).Padding(0, 0, 0, 8).Gap(2).
		Background(colorWindow()).DragWindow()
	bar.Children(func() {
		for i := range a.tabs {
			a.tabItem(c, i)
		}
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

// tabItem draws the tab at index i: its title and a close button. The
// active tab takes the content surface and rounds its top corners, as the
// Windows 11 active tab does.
func (a *App) tabItem(c *ui.Context, i int) {
	active := i == a.activeTab
	title := tabTitle(a.tabs[i].path)
	tab := ui.Row(c).Key("tab-"+string(rune(i))).Height(32).MaxWidth(220).MinWidth(72).
		Padding(0, 8, 0, 8).Gap(6).Radius(4, 4, 0, 0).Shrink(0).
		AlignItems(ui.Center).Role(ui.RoleTab).Label(title)
	if active {
		tab.Background(colorContent())
	} else if tab.Hovered() {
		tab.Background(colorHover())
	}
	tab.Children(func() {
		ui.Text(c, title).FontSize(12).SingleLine().Grow(1).MinWidth(0).
			TextColor(colorText()).MaxLines(1)
		close := ui.ButtonBase(c).Size(20, 20).Radius(4).Shrink(0).
			Label("关闭标签").Tooltip("关闭标签")
		if close.Hovered() || close.Pressed() {
			close.Background(colorHover())
		}
		close.Children(func() {
			components.Icon(c, lucide.X).Size(12, 12).TextColor(colorMuted())
		})
		if close.Clicked() {
			a.closeTab(i)
			return
		}
	})
	if tab.Clicked() {
		a.switchTo(i)
	}
	// Dropping files on a tab moves them into that tab's folder, as
	// Explorer's tabs take drops.
	a.bindDropFolder(c, tab, a.tabs[i].path)
	// The tab's own menu, as Explorer's tab context menu.
	tab.ContextMenu(func(m *ui.Menu) {
		if m.Item("复制标签页").Chosen() {
			a.openTab(a.tabs[i].path)
		}
		m.Separator()
		if m.Item("关闭标签页").Disabled(len(a.tabs) <= 1).Chosen() {
			a.closeTab(i)
		}
		if m.Item("关闭其他标签页").Disabled(len(a.tabs) <= 1).Chosen() {
			a.closeOthers(i)
		}
		if m.Item("关闭右侧标签页").Disabled(i >= len(a.tabs)-1).Chosen() {
			a.closeRight(i)
		}
	})
}

// windowControls ends the title bar: the empty strip that drags, then the
// three caption buttons Windows 11 puts at the right end — minimize,
// maximize, close — each a 46×40 flat target. Close hovers red.
func (a *App) windowControls(c *ui.Context) {
	ui.Box(c).Grow(1).Height(1).DragWindow()
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

// tabTitle is a tab's base name, or 此电脑 on This PC.
func tabTitle(path string) string {
	if path == pathThisPC {
		return "此电脑"
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

	ui.Row(c).Height(44).FillWidth().AlignItems(ui.Center).
		Padding(0, 6).Gap(2).Background(colorChrome()).
		FocusGroup(ui.Horizontal).Children(func() {
		a.commandMenu(c, lucide.FolderPlus, "新建", "新建", func(m *ui.Menu) {
			if m.Item("文件夹").Chosen() {
				a.doNewFolder()
			}
		})

		a.commandSep(c)

		a.commandIconButton(c, lucide.Scissors, "剪切", hasSel, a.doCut)
		a.commandIconButton(c, lucide.Copy, "复制", hasSel, a.doCopy)
		a.commandIconButton(c, lucide.ClipboardPaste, "粘贴", canPaste, a.doPaste)
		a.commandIconButton(c, lucide.Pencil, "重命名", hasSel, a.doRenameStart)
		a.commandIconButton(c, lucide.Share2, "分享", true, func() {
			a.statusMsg = "分享在此版本中不可用"
		})
		a.commandIconButton(c, lucide.Trash, "删除", hasSel, a.doDelete)

		a.commandSep(c)

		a.commandMenu(c, lucide.ArrowDownWideNarrow, "排序", "排序", a.sortMenu)
		a.commandMenu(c, lucide.LayoutGrid, "查看", "查看", a.viewMenu)

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

// commandMenu is a command button that opens a system menu: a Lucide icon,
// a label and a chevron. The menu is attached with Element.Menu so the button
// keeps the command-bar look instead of MenuButton's themed face.
func (a *App) commandMenu(c *ui.Context, icon func() *ui.SVG, label, tip string, build func(m *ui.Menu)) {
	btn := ui.ButtonBase(c).Height(32).Padding(4, 8).Gap(4).Radius(4).
		Shrink(0).Label(label).Tooltip(tip)
	if btn.Hovered() || btn.Pressed() {
		btn.Background(colorHover())
	}
	btn.TextColor(colorText()).Children(func() {
		components.Icon(c, icon).Size(16, 16)
		ui.Text(c, label).FontSize(12).SingleLine()
		chevronIcon(c, true, 10)
	})
	btn.Menu(build)
}

// moreButton is the 查看更多 overflow at the command bar's right end.
func (a *App) moreButton(c *ui.Context) {
	btn := components.IconButton(c, lucide.Ellipsis(), "查看更多", components.IconButtonPlain)
	btn.Menu(func(m *ui.Menu) {
		if m.Item("预览窗格").Checked(a.previewOpen).Chosen() {
			a.togglePreview()
		}
		m.Separator()
		// The remote plugin being off keeps both items out of the menu:
		// the settings page's 插件 switch is what brings them back.
		if a.pluginEnabled("remote") {
			if m.Item("远程访问").Checked(a.remoteConf().Enabled).Chosen() {
				a.remoteToggle()
			}
			if m.Item("复制远程地址").Chosen() {
				mygo.Clipboard.WriteText(a.remoteLANURL() + "\n账号 " + a.remoteConf().Username + "\n密码 " + a.remotePlainPassword())
				a.statusMsg = "已复制远程地址"
			}
		}
		m.Separator()
		if m.Item("设置").Chosen() {
			a.settingsOpen = true
		}
	})
}

func (a *App) sortMenu(m *ui.Menu) {
	keys := []struct {
		label string
		key   SortKey
	}{
		{"名称", SortName},
		{"修改日期", SortDate},
		{"类型", SortType},
		{"大小", SortSize},
	}
	for _, k := range keys {
		if m.Item(k.label).Checked(a.sortKey == k.key).Chosen() {
			a.toggleSort(k.key)
		}
	}
	m.Separator()
	if m.Item("递增").Checked(a.sortAsc).Chosen() {
		if !a.sortAsc {
			a.toggleSort(a.sortKey)
		}
	}
	if m.Item("递减").Checked(!a.sortAsc).Chosen() {
		if a.sortAsc {
			a.toggleSort(a.sortKey)
		}
	}
}

func (a *App) viewMenu(m *ui.Menu) {
	modes := []struct {
		label string
		mode  ViewMode
	}{
		{"超大图标", ViewExtraLarge},
		{"大图标", ViewLargeIcons},
		{"中等图标", ViewMediumIcons},
		{"小图标", ViewSmallIcons},
		{"列表", ViewList},
		{"详细信息", ViewDetails},
	}
	for _, mode := range modes {
		if m.Item(mode.label).Checked(a.mode == mode.mode).Chosen() {
			a.setMode(mode.mode)
		}
	}
	if m.Item("紧凑视图").Checked(a.compact).Chosen() {
		a.compact = !a.compact
	}
	m.Separator()
	if m.Item("放大图标").Disabled(!a.canZoomIn()).Chosen() {
		a.zoomIn()
	}
	if m.Item("缩小图标").Disabled(!a.canZoomOut()).Chosen() {
		a.zoomOut()
	}
	m.Submenu("显示", func(n *ui.Menu) {
		if n.Item("文件扩展名").Checked(a.showExt).Chosen() {
			a.showExt = !a.showExt
		}
		if n.Item("隐藏的项目").Checked(a.showHidden).Chosen() {
			a.toggleHidden()
		}
		if n.Item("项目复选框").Checked(a.itemCheckboxes).Chosen() {
			a.itemCheckboxes = !a.itemCheckboxes
		}
	})
	m.Submenu("分组依据", func(n *ui.Menu) {
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
			if n.Item(g.label).Checked(a.grouping == g.key).Chosen() {
				a.grouping = g.key
			}
		}
	})
}
