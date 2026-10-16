package main

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

// tabStrip is the Windows 11 Explorer tab strip: one tab per open tab, the
// active one lit and the rest quiet, each with a close button, then a "+" that
// opens a new tab on the current location. Height is 36; a 1px rule below it
// separates the strip from the command bar, as in Explorer.
func (a *App) tabStrip(c *ui.Context) {
	ui.Column(c).FillWidth().Shrink(0).Background(colorWindow()).Children(func() {
		ui.Row(c).Height(36).FillWidth().AlignItems(ui.End).
			Padding(4, 8, 0, 8).Gap(2).Children(func() {
			for i := range a.tabs {
				a.tabItem(c, i)
			}
			plus := ui.ButtonBase(c).Size(28, 28).Radius(4).Shrink(0).
				Label("新建标签页").Tooltip("新建标签页 (Ctrl+T)")
			if plus.Hovered() || plus.Pressed() {
				plus.Background(colorHover())
			}
			plus.Children(func() {
				ui.Text(c, "+").FontSize(16).TextColor(colorText())
			})
			if plus.Clicked() {
				a.openTab(a.cur().path)
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
	})
}

// tabItem draws the tab at index i: its title and a close button. The active
// tab is brighter and rounded on top, like Windows 11 Explorer.
func (a *App) tabItem(c *ui.Context, i int) {
	active := i == a.activeTab
	title := tabTitle(a.tabs[i].path)
	tab := ui.Row(c).Key("tab-"+string(rune(i))).Height(32).MaxWidth(220).MinWidth(72).
		Padding(0, 8, 0, 8).Gap(6).Radius(6, 6, 0, 0).Shrink(0).
		AlignItems(ui.Center).Role(ui.RoleTab).Label(title)
	if active {
		tab.Background(colorChrome())
	} else if tab.Hovered() {
		tab.Background(colorHover())
	}
	tab.Children(func() {
		ui.Text(c, title).FontSize(12).SingleLine().Grow(1).MinWidth(0).
			TextColor(colorText()).MaxLines(1)
		close := ui.ButtonBase(c).Size(18, 18).Radius(9).Shrink(0).
			Label("关闭标签").Tooltip("关闭标签")
		if close.Hovered() || close.Pressed() {
			close.Background(colorMuted().Alpha(0.25))
		}
		close.Children(func() {
			ui.Text(c, "×").FontSize(13).TextColor(colorMuted()).SingleLine()
		})
		if close.Clicked() {
			a.closeTab(i)
			return
		}
	})
	if tab.Clicked() {
		a.switchTo(i)
	}
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
// 分享 删除 | 排序 查看. Height is 40. Cut, copy, rename and delete stay
// muted until something is selected; paste stays muted while the clipboard
// is empty. The bar calls the ops methods; it does not touch the filesystem.
func (a *App) commandBar(c *ui.Context) {
	hasSel := len(a.cur().selected) > 0
	canPaste := len(a.clipPaths) > 0

	ui.Row(c).Height(40).FillWidth().AlignItems(ui.Center).
		Padding(0, 6).Gap(2).Background(colorChrome()).
		FocusGroup(ui.Horizontal).Children(func() {
		a.commandMenu(c, "newfolder", "新建", "新建", func(m *ui.Menu) {
			if m.Item("文件夹").Chosen() {
				a.doNewFolder()
			}
		})

		a.commandSep(c)

		a.commandButton(c, "cut", "剪切", "剪切", hasSel, a.doCut)
		a.commandButton(c, "copy", "复制", "复制", hasSel, a.doCopy)
		a.commandButton(c, "paste", "粘贴", "粘贴", canPaste, a.doPaste)
		a.commandButton(c, "rename", "重命名", "重命名", hasSel, a.doRenameStart)
		a.commandButton(c, "share", "分享", "分享", true, func() {
			a.statusMsg = "分享在此版本中不可用"
		})
		a.commandButton(c, "delete", "删除", "删除", hasSel, a.doDelete)

		a.commandSep(c)

		a.commandMenu(c, "sort", "排序", "排序", a.sortMenu)
		a.commandMenu(c, "view", "查看", "查看", a.viewMenu)

		a.commandSep(c)

		a.commandButton(c, "preview", "预览窗格", "预览窗格 (Alt+P)", true, a.togglePreview)
	})
	ui.Box(c).Height(1).FillWidth().Background(colorDivider())
}

// commandSep is the 1px hairline between command groups, about 16 tall.
func (a *App) commandSep(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Shrink(0).Margin(0, 4).
		Background(colorDivider())
}

// commandButton is an icon plus a 12px label. Disabled actions are muted
// and do not call act. MyGo also drops clicks and paints a disabled element
// at half opacity.
func (a *App) commandButton(c *ui.Context, icon, label, tip string, enabled bool, act func()) {
	btn := ui.ButtonBase(c).Height(32).Padding(4, 8).Gap(6).Radius(4).
		Shrink(0).Label(label).Tooltip(tip).Disabled(!enabled)
	if enabled && (btn.Hovered() || btn.Pressed()) {
		btn.Background(colorHover())
	}
	fg := colorText()
	if !enabled {
		fg = colorMuted()
		btn.Opacity(0.45)
	}
	btn.TextColor(fg).Children(func() {
		a.uiIcon(c, icon, 16)
		ui.Text(c, label).FontSize(12).SingleLine()
	})
	if enabled && btn.Clicked() {
		act()
	}
}

// commandMenu is a command button that opens a system menu. The menu is
// attached with Element.Menu so the button keeps the command-bar look
// instead of MenuButton's themed face and extra chevron.
func (a *App) commandMenu(c *ui.Context, icon, label, tip string, build func(m *ui.Menu)) {
	btn := ui.ButtonBase(c).Height(32).Padding(4, 8).Gap(6).Radius(4).
		Shrink(0).Label(label).Tooltip(tip)
	if btn.Hovered() || btn.Pressed() {
		btn.Background(colorHover())
	}
	btn.TextColor(colorText()).Children(func() {
		a.uiIcon(c, icon, 16)
		ui.Text(c, label).FontSize(12).SingleLine()
	})
	btn.Menu(build)
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
	m.Separator()
	if m.Item("显示隐藏的项目").Checked(a.showHidden).Chosen() {
		a.toggleHidden()
	}
}
