package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

const (
	colDateW float32 = 168
	colTypeW float32 = 120
	colSizeW float32 = 96
	colLocW  float32 = 200
	rowInset float32 = 4
)

// detailsRowH is 30, or 22 with 查看>紧凑视图 on, as Explorer's Compact
// view tightens the list.
func (a *App) detailsRowH() float32 {
	if a.compact {
		return 22
	}
	return 30
}

func (a *App) listRowH() float32 {
	if a.compact {
		return 22
	}
	return 28
}

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
		if a.cur().path == pathHome {
			a.homeView(c)
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

// groupRow is one render row: either a group header band, or an entry.
type groupRow struct {
	header string
	count  int
	e      Entry
}

// groupedRows interleaves group header rows with the entries, when
// 分组依据 is on; otherwise it is the entries as they are. Groups follow
// the current sort within them, like Explorer's.
func (a *App) groupedRows(entries []Entry) []groupRow {
	if a.grouping == groupNone || specialPath(a.cur().path) {
		rows := make([]groupRow, len(entries))
		for i, e := range entries {
			rows[i] = groupRow{e: e}
		}
		return rows
	}
	now := time.Now()
	label := func(e Entry) (string, int) {
		switch a.grouping {
		case SortName:
			r := []rune(strings.ToUpper(e.Name))
			if len(r) == 0 {
				return "#", 0
			}
			if r[0] < 'A' || r[0] > 'Z' {
				return "#", 0
			}
			return string(r[0]), int(r[0])
		case SortDate:
			d := now.Sub(e.ModTime)
			switch {
			case sameDay(now, e.ModTime):
				return "今天", 0
			case sameDay(now.AddDate(0, 0, -1), e.ModTime):
				return "昨天", 1
			case d < 7*24*time.Hour:
				return "本周", 2
			case d < 14*24*time.Hour:
				return "上周", 3
			default:
				return "更早", 4
			}
		case SortType:
			return typeLabel(e), 0
		case SortSize:
			switch {
			case e.IsDir:
				return "文件夹", 0
			case e.Size == 0:
				return "空", 1
			case e.Size < 100*1024:
				return "小于 100 KB", 2
			case e.Size < 1024*1024:
				return "小于 1 MB", 3
			case e.Size < 128*1024*1024:
				return "中", 4
			case e.Size < 1024*1024*1024:
				return "大", 5
			default:
				return "巨大", 6
			}
		}
		return "", 0
	}
	// interleave: a header, then the run of entries sharing its label.
	// The entries are already sorted, so first-seen label order is the
	// group order — natural for dates and sizes, alphabetical for names
	// and types.
	out := make([]groupRow, 0, len(entries)+8)
	idx := 0
	for idx < len(entries) {
		l, _ := label(entries[idx])
		end := idx
		for end < len(entries) {
			le, _ := label(entries[end])
			if le != l {
				break
			}
			end++
		}
		out = append(out, groupRow{header: l, count: end - idx})
		for _, e := range entries[idx:end] {
			out = append(out, groupRow{e: e})
		}
		idx = end
	}
	return out
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// groupHeaderRow is a 分组 band: the label and its item count.
func (a *App) groupHeaderRow(c *ui.Context, label string, count int) {
	row := ui.Row(c).FillWidth().Height(30).Shrink(0).Padding(0, 12, 0, 12).
		AlignItems(ui.Center).Gap(6)
	row.Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(colorText()).SingleLine()
		ui.Text(c, fmt.Sprintf("(%d)", count)).FontSize(12).TextColor(colorMuted()).SingleLine()
	})
}

// detailsView is 详细信息: a sticky header and a virtualized list of 30px
// rows, each inset 4px with a rounded selection, as Explorer's are. ui.Table
// sorts through its own ListState and keeps the choice in a Selection, which
// this app does not; the header calls toggleSort and the rows call the
// selection methods, and the header scrolls sideways with the rows the way
// a table's does.
func (a *App) detailsView(c *ui.Context, entries []Entry) {
	width := a.detailsWidth()
	head := ui.Row(c).Height(a.detailsRowH()).FillWidth().Shrink(0).Clip().
		AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colorDivider())
	searching := strings.TrimSpace(a.cur().query) != ""
	head.Children(func() {
		a.headerCell(c, "名称", SortName, 0, true)
		a.headerCell(c, "修改日期", SortDate, colDateW, false)
		if searching {
			a.headerCell(c, "位置", SortType, colLocW, false)
		} else {
			a.headerCell(c, "类型", SortType, colTypeW, false)
		}
		a.headerCell(c, "大小", SortSize, colSizeW, false)
	})

	rows := a.groupedRows(entries)
	list := ui.List(c, nil, len(rows), func(i int) {
		if i < 0 || i >= len(rows) {
			return
		}
		if rows[i].header != "" {
			a.groupHeaderRow(c, rows[i].header, rows[i].count)
			return
		}
		a.detailsRow(c, rows[i].e, searching)
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
	extra := colTypeW
	if strings.TrimSpace(a.cur().query) != "" {
		extra = colLocW
	}
	return 220 + colDateW + extra + colSizeW + rowInset*2
}

func (a *App) headerCell(c *ui.Context, title string, key SortKey, width float32, flex bool) {
	cell := ui.ButtonBase(c).Height(a.detailsRowH()).Padding(0, 8).Gap(4).
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
			components.Icon(c, chevronUpDown(a.sortAsc)).Size(12, 12).TextColor(colorMuted())
		}
	})
	if cell.Clicked() {
		a.toggleSort(key)
	}
}

func (a *App) detailsRow(c *ui.Context, e Entry, searching bool) {
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(a.detailsRowH()).
		AlignItems(ui.Center).Margin(0, rowInset).MinWidth(a.detailsWidth()).
		Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	row.Children(func() {
		name := ui.Row(c).Grow(1).Shrink(1).MinWidth(160).Height(a.detailsRowH()).
			Padding(0, 8).Gap(8).AlignItems(ui.Center)
		name.Children(func() {
			a.nameCell(c, e, 16, 12)
		})
		a.detailText(c, formatModTime(e.ModTime), colDateW, false)
		if searching {
			a.detailText(c, searchLocation(e), colLocW, false)
		} else {
			a.detailText(c, typeLabel(e), colTypeW, false)
		}
		if e.VolTotal > 0 {
			a.detailText(c, driveCapacity(e), colSizeW, false)
		} else {
			size := ""
			if !e.IsDir {
				size = formatBytes(e.Size)
			}
			a.detailText(c, size, colSizeW, true)
		}
	})
	a.bindItemInput(c, row, e)
	a.attachRowMenu(row, e)
}

// searchLocation is the 位置 column: the folder the hit was found in,
// relative to the search root when it is under it.
func searchLocation(e Entry) string {
	if e.Dir == "" {
		return ""
	}
	return filepath.Base(e.Dir)
}

// driveCapacity is the 此电脑 page's drive text: free of total.
func driveCapacity(e Entry) string {
	if e.VolTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("可用 %s / 共 %s", formatBytes(e.VolFree), formatBytes(e.VolTotal))
}

func (a *App) detailText(c *ui.Context, text string, width float32, end bool) {
	cell := ui.Row(c).Width(width).Shrink(0).Height(a.detailsRowH()).
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
	rows := a.groupedRows(entries)
	list := ui.List(c, nil, len(rows), func(i int) {
		if i < 0 || i >= len(rows) {
			return
		}
		if rows[i].header != "" {
			a.groupHeaderRow(c, rows[i].header, rows[i].count)
			return
		}
		a.listRow(c, rows[i].e)
	}).Grow(1).FillWidth().MinHeight(0)
	list.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, list)
}

func (a *App) listRow(c *ui.Context, e Entry) {
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(a.listRowH()).
		Padding(0, 8).Gap(8).AlignItems(ui.Center).Margin(0, rowInset).
		Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	row.Children(func() {
		a.nameCell(c, e, 16, 12)
	})
	a.bindItemInput(c, row, e)
	a.attachRowMenu(row, e)
}

// iconView lays tiles in a grid. Extra large, large and medium put the
// label under the icon; small icons put it beside, in a shorter tile.
func (a *App) iconView(c *ui.Context, entries []Entry) {
	box := iconBoxSize(a.mode)
	minW, height := iconTileSize(a.mode, box)
	if a.grouping != groupNone && !specialPath(a.cur().path) {
		scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(8)
		scroll.Children(func() {
			rows := a.groupedRows(entries)
			var group []Entry
			emit := func() {
				if len(group) == 0 {
					return
				}
				grid := ui.GridView(c, nil, len(group), minW, height, func(i int) {
					if i >= 0 && i < len(group) {
						a.iconTile(c, group[i], box)
					}
				}).FillWidth().Shrink(0)
				grid.Children(func() {})
				group = group[:0]
			}
			for _, r := range rows {
				if r.header != "" {
					emit()
					a.groupHeaderRow(c, r.header, r.count)
					continue
				}
				group = append(group, r.e)
			}
			emit()
			if len(rows) == 0 {
				a.paneMessage(c, "此文件夹为空")
			}
		})
		a.bindPaneMenu(c, scroll)
		return
	}
	grid := ui.GridView(c, nil, len(entries), minW, height, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.iconTile(c, entries[i], box)
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
func (a *App) iconTile(c *ui.Context, e Entry, box float32) {
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
			ui.Text(c, a.displayName(e)).FontSize(12).TextColor(colorText()).
				TextAlign(ui.Center).MaxLines(2).FillWidth()
		})
	})
	a.bindItemInput(c, tile, e)
	a.attachRowMenu(tile, e)
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
	ui.Text(c, a.displayName(e)).FontSize(font).TextColor(colorText()).
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
func (a *App) bindItemInput(c *ui.Context, el ui.Element, e Entry) {
	if el.Clicked() {
		a.clickEntry(e.Path, el.ClickModifiers())
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

func (a *App) clickEntry(path string, mods ui.Modifiers) {
	if path == "" {
		return
	}
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
		if a.cur().path == pathTrash {
			a.trashPaneMenu(m)
			return
		}
		special := specialPath(a.cur().path)
		m.Submenu("新建", func(n *ui.Menu) {
			if n.Item("文件夹").Disabled(special).Chosen() {
				a.doNewFolder()
			}
			if n.Item("文本文档").Disabled(special).Chosen() {
				a.doNewTextFile()
			}
		})
		if m.Item("粘贴").Disabled(len(a.clipPaths) == 0 || special).Chosen() {
			a.doPaste()
		}
		m.Separator()
		if m.Item("在终端中打开").Disabled(special).Chosen() {
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
func (a *App) itemMenu(m *ui.Menu, e Entry) {
	if e.Path != "" && !a.isSelected(e.Path) {
		a.selectOnly(e.Path)
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
	onSpecial := specialPath(a.cur().path)

	if m.Item("打开").Disabled(!anySel).Chosen() {
		a.doOpen()
	}
	if oneDir && !onSpecial {
		if a.store.isPinned(sel[0].Path) {
			if m.Item("从快速访问取消固定").Chosen() {
				if a.store.unpin(sel[0].Path) {
					a.reload()
				}
			}
		} else if m.Item("固定到快速访问").Chosen() {
			if a.store.pin(sel[0].Path) {
				a.reload()
			}
		}
	}
	m.Separator()
	if m.Item("剪切").Disabled(!anySel).Chosen() {
		a.doCut()
	}
	if m.Item("复制").Disabled(!anySel).Chosen() {
		a.doCopy()
	}
	if m.Item("粘贴").Disabled(len(a.clipPaths) == 0 || onSpecial).Chosen() {
		a.doPaste()
	}
	if m.Item("复制为路径").Disabled(!anySel || onSpecial).Chosen() {
		a.doCopyAsPath()
	}
	if m.Item("重命名").Disabled(!one || onSpecial).Chosen() {
		a.doRenameStart()
	}
	m.Separator()
	if m.Item("压缩为 ZIP 文件").Disabled(!anySel || onSpecial).Chosen() {
		a.doCompress()
	}
	if m.Item("全部解压").Disabled(!allArchives || onSpecial).Chosen() {
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

// attachRowMenu gives a row the menu its page wants: the trash page's
// 还原/永久删除, every other page's item menu.
func (a *App) attachRowMenu(el ui.Element, e Entry) {
	el.ContextMenu(func(m *ui.Menu) {
		if a.cur().path == pathTrash {
			a.trashMenu(m, e)
			return
		}
		a.itemMenu(m, e)
	})
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
