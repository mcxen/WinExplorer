package main

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	detailsRowH float32 = 30
	listRowH    float32 = 28
	colDateW    float32 = 168
	colTypeW    float32 = 120
	colSizeW    float32 = 96
	rowInset    float32 = 4
)

// filePane is the content area to the right of the sidebar. It fills the
// leftover width and the body row's leftover height, and shows the current
// folder in the active view: details, a compact list, or one of the four
// icon sizes. Only visibleEntries() is rendered.
func (a *App) filePane(c *ui.Context) {
	pane := ui.Column(c).Grow(1).FillHeight().MinWidth(0).
		Background(colorContent())
	pane.Children(func() {
		if a.cur().loadErr != "" {
			a.paneMessage(c, a.cur().loadErr)
			return
		}
		entries := a.visibleEntries()
		switch a.mode {
		case ViewList:
			a.listView(c, entries)
		case ViewSmallIcons, ViewMediumIcons, ViewLargeIcons, ViewExtraLarge:
			a.iconView(c, entries)
		default:
			a.detailsView(c, entries)
		}
	})
	a.bindPaneMenu(c, pane)
}

// detailsView is 详细信息: a sticky header and a virtualized list of 30px
// rows, each inset 4px with a rounded selection, as Explorer's are. ui.Table
// sorts through its own ListState and keeps the choice in a Selection, which
// this app does not; the header calls toggleSort and the rows call the
// selection methods, and the header scrolls sideways with the rows the way
// a table's does.
func (a *App) detailsView(c *ui.Context, entries []Entry) {
	width := a.detailsWidth()
	head := ui.Row(c).Height(detailsRowH).FillWidth().Shrink(0).Clip().
		AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colorDivider())
	head.Children(func() {
		a.headerCell(c, "名称", SortName, 0, true)
		a.headerCell(c, "修改日期", SortDate, colDateW, false)
		a.headerCell(c, "类型", SortType, colTypeW, false)
		a.headerCell(c, "大小", SortSize, colSizeW, false)
	})

	list := ui.List(c, nil, len(entries), func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.detailsRow(c, entries, i)
	}).Grow(1).FillWidth().MinHeight(0).RowWidth(width)
	// List ends its build as it returns, so an empty message has to be one of
	// its own children: a later sibling is parented to the pane instead, and
	// the list covers it.
	list.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	head.ScrollWith(list)
	a.bindPaneMenu(c, list)
}

func (a *App) detailsWidth() float32 {
	return 220 + colDateW + colTypeW + colSizeW + rowInset*2
}

func (a *App) headerCell(c *ui.Context, title string, key SortKey, width float32, flex bool) {
	cell := ui.ButtonBase(c).Height(detailsRowH).Padding(0, 8).Gap(4).
		AlignItems(ui.Center).Margin(0, rowInset).Radius(4).Label(title)
	if flex {
		cell.Grow(1).Shrink(1).MinWidth(160)
	} else {
		cell.Width(width).Shrink(0)
		if key == SortSize {
			cell.Justify(ui.End)
		}
	}
	if cell.Hovered() {
		cell.Background(colorHover())
	}
	cell.Children(func() {
		label := ui.Text(c, title).FontSize(12).TextColor(colorMuted()).
			SingleLine().FontWeight(600)
		if key == SortSize {
			label.TextAlign(ui.End)
		}
		if a.sortKey == key {
			mark := ui.Box(c).Size(10, 10).Shrink(0)
			mark.Draw(func(p *ui.Painter, r ui.Rect) {
				drawSortArrow(p, r, !a.sortAsc)
			})
		}
	})
	if cell.Clicked() {
		a.toggleSort(key)
	}
}

func (a *App) detailsRow(c *ui.Context, entries []Entry, i int) {
	e := entries[i]
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(detailsRowH).
		AlignItems(ui.Center).Margin(0, rowInset).MinWidth(a.detailsWidth()).
		Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	row.Children(func() {
		name := ui.Row(c).Grow(1).Shrink(1).MinWidth(160).Height(detailsRowH).
			Padding(0, 8).Gap(8).AlignItems(ui.Center)
		name.Children(func() {
			a.nameCell(c, e, 16, 12)
		})
		a.detailText(c, formatModTime(e.ModTime), colDateW, false)
		a.detailText(c, typeLabel(e), colTypeW, false)
		size := ""
		if !e.IsDir {
			size = formatBytes(e.Size)
		}
		a.detailText(c, size, colSizeW, true)
	})
	a.bindItemInput(c, row, entries, i)
	row.ContextMenu(func(m *ui.Menu) {
		a.itemMenu(m, entries, i)
	})
}

func (a *App) detailText(c *ui.Context, text string, width float32, end bool) {
	cell := ui.Row(c).Width(width).Shrink(0).Height(detailsRowH).
		Padding(0, 8).AlignItems(ui.Center)
	if end {
		cell.Justify(ui.End)
	}
	cell.Children(func() {
		label := ui.Text(c, text).FontSize(12).TextColor(colorText()).SingleLine()
		if end {
			label.TextAlign(ui.End)
		}
	})
}

// listView is one column of compact icon-and-name rows, 28px each.
func (a *App) listView(c *ui.Context, entries []Entry) {
	list := ui.List(c, nil, len(entries), func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.listRow(c, entries, i)
	}).Grow(1).FillWidth().MinHeight(0)
	list.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, list)
}

func (a *App) listRow(c *ui.Context, entries []Entry, i int) {
	e := entries[i]
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(listRowH).
		Padding(0, 8).Gap(8).AlignItems(ui.Center).Margin(0, rowInset).
		Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	row.Children(func() {
		a.nameCell(c, e, 16, 12)
	})
	a.bindItemInput(c, row, entries, i)
	row.ContextMenu(func(m *ui.Menu) {
		a.itemMenu(m, entries, i)
	})
}

// iconView lays tiles in a grid. Extra large, large and medium put the
// label under the icon; small icons put it beside, in a shorter tile.
func (a *App) iconView(c *ui.Context, entries []Entry) {
	box := iconBoxSize(a.mode)
	minW, height := iconTileSize(a.mode, box)
	grid := ui.GridView(c, nil, len(entries), minW, height, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.iconTile(c, entries, i, box)
	}).Grow(1).FillWidth().MinHeight(0).Padding(8)
	grid.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, grid)
}

func iconBoxSize(mode ViewMode) float32 {
	switch mode {
	case ViewExtraLarge:
		return 96
	case ViewLargeIcons:
		return 72
	case ViewMediumIcons:
		return 48
	default:
		return 32
	}
}

func iconTileSize(mode ViewMode, box float32) (minW, height float32) {
	if mode == ViewSmallIcons {
		return 180, 44
	}
	return box + 40, box + 52
}

// iconTile is one grid cell. ButtonBase is a row, so a small icon sits
// beside its name. Larger sizes put a column in the button: the icon, then
// the name under it.
func (a *App) iconTile(c *ui.Context, entries []Entry, i int, box float32) {
	e := entries[i]
	tile := ui.ButtonBase(c.Key(e.Path)).Fill().Radius(4).Label(e.Name)
	if a.mode == ViewSmallIcons {
		tile.Padding(4, 6).Gap(8).AlignItems(ui.Center).Justify(ui.Start)
	}
	a.paintSelect(tile, e.Path)
	tile.Children(func() {
		if a.mode == ViewSmallIcons {
			a.nameCell(c, e, box, 12)
			return
		}
		body := ui.Column(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(6, 4)
		body.Children(func() {
			glyph := ui.Box(c).Size(box, box).Shrink(0)
			glyph.Children(func() {
				a.fileIcon(c, e, box)
			})
			if a.cur().renaming == e.Name {
				a.renameField(c, 12)
				return
			}
			ui.Text(c, e.Name).FontSize(12).TextColor(colorText()).
				TextAlign(ui.Center).MaxLines(2).FillWidth()
		})
	})
	a.bindItemInput(c, tile, entries, i)
	tile.ContextMenu(func(m *ui.Menu) {
		a.itemMenu(m, entries, i)
	})
}

// nameCell is the icon plus the name, or the rename field when this entry
// is the one being renamed.
func (a *App) nameCell(c *ui.Context, e Entry, icon, font float32) {
	glyph := ui.Box(c).Size(icon, icon).Shrink(0)
	glyph.Children(func() {
		a.fileIcon(c, e, icon)
	})
	if a.cur().renaming == e.Name {
		a.renameField(c, font)
		return
	}
	ui.Text(c, e.Name).FontSize(font).TextColor(colorText()).
		SingleLine().Grow(1).MinWidth(0)
}

// renameField edits a.cur().renameBuf. Enter commits, Escape cancels, and both
// are consumed so the window shortcut handler leaves them to the field.
func (a *App) renameField(c *ui.Context, font float32) {
	in := ui.TextInputBase(c.Key("rename"), &a.cur().renameBuf).
		Grow(1).MinWidth(40).Height(font+10).FontSize(font).
		Padding(0, 4).Radius(2).AutoFocus().
		Background(colorAddress()).Border(1, colorAccent())
	in.OnTextKey(func(k ui.TextKey) bool {
		switch k.Key {
		case ui.KeyEnter:
			a.cur().renameBuf = k.Text()
			a.commitRename()
			return true
		case ui.KeyEscape:
			a.cancelRename()
			return true
		}
		return false
	})
	if in.Submitted() {
		a.commitRename()
	}
}

func (a *App) paintSelect(el ui.Element, path string) {
	switch {
	case a.isSelected(path):
		el.Background(colorSelected())
	case el.Hovered():
		el.Background(colorHover())
	}
}

// bindItemInput applies Explorer selection on the row the pointer hit.
// Double-click and Enter both open: Enter is the window shortcut in
// handleKeys, a double-click selects the row first and then doOpen.
// Every row is also a drag source (the selection when the row is in it,
// else the row alone), and folder rows accept drops that move files into
// them.
func (a *App) bindItemInput(c *ui.Context, el ui.Element, entries []Entry, i int) {
	if i < 0 || i >= len(entries) {
		return
	}
	e := entries[i]
	if el.Clicked() {
		a.clickEntry(entries, i, el.ClickModifiers())
	}
	if el.DoubleClicked() {
		a.selectOnly(e.Path)
		a.doOpen()
	}
	if a.cur().path != pathThisPC && e.Path != "" && e.Path != pathThisPC {
		el.Drag(a.dragValueFor(e.Path))
		if e.IsDir {
			a.bindDropFolder(c, el, e.Path)
		}
	}
}

func (a *App) clickEntry(entries []Entry, i int, mods ui.Modifiers) {
	if i < 0 || i >= len(entries) {
		return
	}
	path := entries[i].Path
	switch {
	case mods&ui.Shift != 0:
		a.selectRange(path)
	case mods&ui.Cmd != 0 || mods&ui.Ctrl != 0:
		a.toggleSelect(path)
	default:
		a.selectOnly(path)
	}
}

// bindPaneMenu is the menu of the pane background: New (folder / text
// document), paste, open in Terminal, refresh, hidden items and the
// folder's properties. A row's own menu is the innermost one, so it takes
// the click when the pointer is on a row. The pane also accepts files
// dragged from other applications, moving them into the current folder.
func (a *App) bindPaneMenu(c *ui.Context, el ui.Element) {
	if a.cur().path != pathThisPC {
		if files := el.DroppedFiles(); len(files) > 0 {
			a.doDropInto(c.Modifiers(), a.cur().path, files)
		}
	}
	el.ContextMenu(func(m *ui.Menu) {
		m.Submenu("新建", func(n *ui.Menu) {
			if n.Item("文件夹").Disabled(a.cur().path == pathThisPC).Chosen() {
				a.doNewFolder()
			}
			if n.Item("文本文档").Disabled(a.cur().path == pathThisPC).Chosen() {
				a.doNewTextFile()
			}
		})
		if m.Item("粘贴").Disabled(len(a.clipPaths) == 0 || a.cur().path == pathThisPC).Chosen() {
			a.doPaste()
		}
		m.Separator()
		if m.Item("在终端中打开").Disabled(a.cur().path == pathThisPC).Chosen() {
			a.openInTerminal(a.cur().path)
		}
		m.Separator()
		if m.Item("刷新").Chosen() {
			a.doRefresh()
		}
		if m.Item("显示隐藏的项目").Checked(a.showHidden).Chosen() {
			a.toggleHidden()
		}
		m.Separator()
		if m.Item("属性").Chosen() {
			a.showProperties()
		}
	})
}

// itemMenu is the row menu. A right-click on a row that was not selected
// selects it first, so the ops methods act on the row the menu opened on.
// The items follow Windows 11's: Open, Cut/Copy/Paste, Copy as path,
// Rename, Compress to ZIP, Extract all, Open in Terminal (folders),
// Delete, Properties.
func (a *App) itemMenu(m *ui.Menu, entries []Entry, i int) {
	if i >= 0 && i < len(entries) && !a.isSelected(entries[i].Path) {
		a.selectOnly(entries[i].Path)
	}
	sel := a.selectedEntries()
	one := len(sel) == 1
	anySel := len(sel) > 0
	allArchives := anySel
	for _, e := range sel {
		if e.IsDir || !isArchive(e.Name) {
			allArchives = false
			break
		}
	}
	oneDir := one && sel[0].IsDir
	inThisPC := a.cur().path == pathThisPC

	if m.Item("打开").Disabled(!anySel).Chosen() {
		a.doOpen()
	}
	m.Separator()
	if m.Item("剪切").Disabled(!anySel).Chosen() {
		a.doCut()
	}
	if m.Item("复制").Disabled(!anySel).Chosen() {
		a.doCopy()
	}
	if m.Item("粘贴").Disabled(len(a.clipPaths) == 0 || inThisPC).Chosen() {
		a.doPaste()
	}
	if m.Item("复制为路径").Disabled(!anySel || inThisPC).Chosen() {
		a.doCopyAsPath()
	}
	if m.Item("重命名").Disabled(!one || inThisPC).Chosen() {
		a.doRenameStart()
	}
	m.Separator()
	if m.Item("压缩为 ZIP 文件").Disabled(!anySel || inThisPC).Chosen() {
		a.doCompress()
	}
	if m.Item("全部解压").Disabled(!allArchives || inThisPC).Chosen() {
		a.doExtract()
	}
	m.Separator()
	if m.Item("在终端中打开").Disabled(!oneDir).Chosen() {
		a.openInTerminal(sel[0].Path)
	}
	if m.Item("删除").Disabled(!anySel).Chosen() {
		a.doDelete()
	}
	m.Separator()
	if m.Item("属性").Disabled(!one).Chosen() {
		a.showProperties()
	}
}

func (a *App) paneMessage(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(13).TextColor(colorMuted()).Padding(24, 16)
}

func formatModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006/1/2 15:04")
}

// typeLabel is 文件夹 for a directory, otherwise "PNG 文件", "文本文档", or
// "文件" from the extension.
func typeLabel(e Entry) string {
	if e.IsDir {
		return "文件夹"
	}
	switch e.Ext {
	case "":
		return "文件"
	case "txt", "md", "log", "csv", "rtf":
		return "文本文档"
	default:
		return strings.ToUpper(e.Ext) + " 文件"
	}
}

// drawSortArrow is a small chevron. descending points down (递减).
func drawSortArrow(p *ui.Painter, r ui.Rect, descending bool) {
	s := r.W
	if r.H < s {
		s = r.H
	}
	x := r.X + (r.W-s)/2
	y := r.Y + (r.H-s)/2
	var path ui.Path
	if descending {
		path.MoveTo(x+s*0.1, y+s*0.3).LineTo(x+s*0.5, y+s*0.7).LineTo(x+s*0.9, y+s*0.3)
	} else {
		path.MoveTo(x+s*0.1, y+s*0.7).LineTo(x+s*0.5, y+s*0.3).LineTo(x+s*0.9, y+s*0.7)
	}
	p.StrokePath(&path, 1.4, colorMuted())
}
