package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/internal/netfs"

	"sunvale/internal/searchquery"
)

const (
	colDateW     float32 = 168
	colTypeW     float32 = 120
	colSizeW     float32 = 96
	colDriveW    float32 = 260
	colLocW      float32 = 200
	colNameFlexW float32 = 220 // the 名称 column's share of the row width
	colNameMin   float32 = 160 // the 名称 column's flex floor
	rowInset     float32 = 4
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
	// macOS routes Cmd+C/X/V through the Edit menu, which reaches the view
	// as an edit command rather than a key press; every row and tile takes
	// those (bindItemInput), so they work while the listing has the focus.
	// The pane itself stays out of it: a click focuses the row it hit.
	pane.Children(func() {
		// The band hit-tests the boxes the rows laid out in the frame before;
		// clear the map so a view or folder change drops the boxes it no
		// longer shows, then the rows below fill it afresh.
		t := a.cur()
		if t.itemBounds == nil {
			t.itemBounds = map[string]ui.Rect{}
		} else if !t.marquee.on {
			clear(t.itemBounds)
		}
		if a.cur().loadErr != "" {
			a.paneMessage(c, a.cur().loadErr)
			return
		}
		if a.cur().path == pathHome {
			a.homeView(c)
			return
		}
		if a.cur().path == pathGallery {
			a.galleryView(c)
			// The gallery's tiles select through the band as any view's do.
			a.marqueeOverlay(c)
			return
		}
		if a.cur().path == pathDisks {
			a.disksView(c)
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
		// The rubber band draws above the rows, on a layer of its own.
		a.marqueeOverlay(c)
	})
	a.bindPaneMenu(c, pane)
	// The 重复文件查找 plugin's dialog renders over the window while open.
	a.dupsModal(c)
	// The 可视化查看占用 plugin's treemap dialog renders the same way.
	a.usageModal(c)
	// The 清理 .DS_Store plugin's dialog renders the same way.
	a.dsstoreModal(c)
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

// groupRowKey is a list row's identity: the entry's path, or the group
// band's label. A refresh that inserts or drops rows keeps the scroll
// anchored to those identities instead of to a row number that moved.
func groupRowKey(rows []groupRow) func(int) any {
	return func(i int) any {
		if i < 0 || i >= len(rows) {
			return i
		}
		if rows[i].header != "" {
			return "group:" + rows[i].header
		}
		if rows[i].e.Path != "" {
			return rows[i].e.Path
		}
		return i
	}
}

// scrollToReveal raises the pending row into view and drops the request,
// so the next frame — a wheel scroll, a refresh — keeps the place the user
// is at. A request whose row is not in this listing is dropped too: it
// belonged to a folder that is no longer on screen.
func (a *App) scrollToReveal(rows []groupRow, scroll func(int)) {
	t := a.cur()
	if t.reveal == "" {
		return
	}
	if i := rowIndexOf(rows, t.reveal); i >= 0 {
		scroll(i)
	}
	t.reveal = ""
}

// rowIndexOf is the render row showing the entry at path, or -1: 分组依据
// inserts header rows, so an entry sits below its listing index.
func rowIndexOf(rows []groupRow, path string) int {
	if path == "" {
		return -1
	}
	for i, r := range rows {
		if r.e.Path == path {
			return i
		}
	}
	return -1
}

// entryIndexOf is the index of the entry at path among entries, or -1.
func entryIndexOf(entries []Entry, path string) int {
	if path == "" {
		return -1
	}
	for i, e := range entries {
		if e.Path == path {
			return i
		}
	}
	return -1
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
	// The header's cells carry the same geometry as the rows': the 4 DIP inset
	// is the row's, not each cell's. Per-cell insets made every header column
	// 8 DIP narrower than the one under it, so the headers — and the column
	// grips at their right edges — drifted further left with each column.
	head := ui.Row(c).Height(a.detailsRowH()).FillWidth().Shrink(0).Clip().
		AlignItems(ui.Center).Margin(0, rowInset).MinWidth(width).
		BorderWidth(0, 0, 1, 0).BorderColor(colorDivider())
	searching := strings.TrimSpace(a.cur().query) != ""
	head.Children(func() {
		a.headerCell(c, "名称", SortName, a.nameColW(), a.colWidths.name <= 0, &a.colWidths.name)
		a.headerCell(c, "修改日期", SortDate, a.dateColW(), false, &a.colWidths.date)
		if searching {
			a.headerCell(c, "位置", SortType, a.locColW(), false, &a.colWidths.loc)
		} else {
			a.headerCell(c, "类型", SortType, a.typColW(), false, &a.colWidths.typ)
		}
		// The 此电脑 page's 大小 slot is the drive capacity column, which
		// keeps its fixed width — no grip there.
		sizeSlot := &a.colWidths.size
		if a.cur().path == pathThisPC {
			sizeSlot = nil
		}
		a.headerCell(c, "大小", SortSize, a.sizeColW(), false, sizeSlot)
	})

	rows := a.groupedRows(entries)
	t := a.cur()
	// The row's identity is its path, and the scroll offset stays put when
	// rows above it come or go: a delete or a paste refreshes the listing
	// without throwing the pane back to the top.
	t.details.KeepOffset = true
	t.details.Key = groupRowKey(rows)
	a.scrollToReveal(rows, t.details.ScrollIntoView)
	list := ui.List(c, &t.details, len(rows), func(i int) {
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
	extra := a.typColW()
	if strings.TrimSpace(a.cur().query) != "" {
		extra = a.locColW()
	}
	return a.nameColW() + a.dateColW() + extra + a.sizeColW() + rowInset*2
}

// nameColW is the 名称 column's width: the user's once dragged,
// otherwise the flex default's footprint in the row width math.
func (a *App) nameColW() float32 {
	return colW(a.colWidths.name, colNameFlexW)
}

func (a *App) dateColW() float32 { return colW(a.colWidths.date, colDateW) }
func (a *App) typColW() float32  { return colW(a.colWidths.typ, colTypeW) }
func (a *App) locColW() float32  { return colW(a.colWidths.loc, colLocW) }

// sizeColW is the 大小 column's width. The 此电脑 page's drive rows carry
// "可用 X，共 Y", which the 96px file-size column cannot hold, so drives get
// the wider column Explorer gives 总大小 and 可用空间.
func (a *App) sizeColW() float32 {
	if a.cur().path == pathThisPC {
		return colDriveW
	}
	return colW(a.colWidths.size, colSizeW)
}

// colW is the user's width for a column, or its default when the column
// was never dragged.
func colW(w, def float32) float32 {
	if w > 0 {
		return w
	}
	return def
}

// saveColWidths persists the details view's column widths through the
// settings store, as the settings dialog's toggles do.
func (a *App) saveColWidths() {
	w := a.colWidths
	a.applySettings(func(s *appSettings) {
		s.ColName, s.ColDate, s.ColType, s.ColLoc, s.ColSize =
			w.name, w.date, w.typ, w.loc, w.size
	})
}

// headerCell is one details column's header: the label with its sort
// chevron, and — when slot is set — a drag handle on its right edge that
// rewrites the column's width. flex cells grow with the pane; the rest
// keep width.
func (a *App) headerCell(c *ui.Context, title string, key SortKey, width float32, flex bool, slot *float32) {
	cell := ui.ButtonBase(c).Height(a.detailsRowH()).Padding(0, 8).Gap(4).
		AlignItems(ui.Center).Radius(4).Label(title)
	if flex {
		cell.Grow(1).Shrink(1).MinWidth(colNameMin)
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
	if slot != nil {
		a.colGrip(c, slot, width, colMinFor(key))
	}
}

// colMinFor is the narrowest a column may be dragged to, its text and
// header still readable.
func colMinFor(key SortKey) float32 {
	if key == SortName {
		return colNameMin
	}
	return 64
}

// colGrip is a column's resize handle over the header cell's right edge:
// dragging rewrites the column's width slot between its floor and a sane
// ceiling, a double-click restores the default, and a drag's end persists
// the widths. Dragged reports the pointer's movement since the last frame
// while the grip is pressed, as the nav pane's handle does.
func (a *App) colGrip(c *ui.Context, slot *float32, def, min float32) {
	grip := ui.Box(c).Absolute().Right(-2).Top(0).Bottom(0).Width(5).
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := grip.Dragged(); ok && dx != 0 {
		w := *slot
		if w <= 0 {
			w = def
		}
		*slot = clampColW(w+dx, min, maxColW)
	}
	if _, _, ok := grip.DragEnded(); ok {
		a.saveColWidths()
	}
	if grip.DoubleClicked() {
		*slot = 0
		a.saveColWidths()
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		if !grip.Hovered() && !grip.Dragging() {
			return
		}
		p.Fill(ui.Rect{X: r.X + (r.W-1)/2, Y: r.Y + 4, W: 1, H: r.H - 8}, colorAccent(), 0)
	})
}

// maxColW caps a dragged column, so no single column swallows the row.
const maxColW float32 = 720

// clampColW keeps w within min and max.
func clampColW(w, min, max float32) float32 {
	if w < min {
		return min
	}
	if w > max {
		return max
	}
	return w
}

func (a *App) detailsRow(c *ui.Context, e Entry, searching bool) {
	// Justify Start: a button centers its content, and once the name column has
	// a width of its own — dragged by the user — the slack would be split
	// either side of the cells, shifting every data column right of its header.
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(a.detailsRowH()).
		AlignItems(ui.Center).Justify(ui.Start).Margin(0, rowInset).
		MinWidth(a.detailsWidth()).Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	aiFxRow(c, row, aiFx.look(e.Path), a.detailsRowH(), 4)
	row.Children(func() {
		name := ui.Row(c).Height(a.detailsRowH()).Padding(0, 8).Gap(8).
			AlignItems(ui.Center)
		if a.colWidths.name > 0 {
			// The user fixed the 名称 column's width; the rest of the row
			// stays blank past the columns, as Explorer's does.
			name.Width(a.colWidths.name).Shrink(0)
		} else {
			name.Grow(1).Shrink(1).MinWidth(colNameMin)
		}
		name.Children(func() {
			a.nameCell(c, e, 16, 12)
		})
		a.detailText(c, formatModTime(e.ModTime), a.dateColW(), false)
		if searching {
			a.detailText(c, searchLocation(e), a.locColW(), false)
		} else {
			a.detailText(c, typeLabel(e), a.typColW(), false)
		}
		if e.VolTotal > 0 {
			a.driveCapCell(c, e)
		} else {
			a.detailText(c, sizeColumnText(e), a.sizeColW(), true)
		}
	})
	a.bindItemInput(c, row, e)
	a.attachRowMenu(row, e)
}

// sizeColumnText is the 大小 column's cell: a file's own bytes, and a
// folder's tally from the background engine (sizes.go) — 0 B for a cached
// empty folder, "…" while its walk is queued or running, and blank until
// one is known.
func sizeColumnText(e Entry) string {
	if !e.IsDir {
		return formatBytes(e.Size)
	}
	if st, ok := lookupDirStat(e.Path); ok {
		return formatBytes(st.Size)
	}
	if dirStatInProgress(e.Path) {
		return "…"
	}
	return ""
}

// searchLocation is the 位置 column: the folder the hit was found in,
// relative to the search root when it is under it.
func searchLocation(e Entry) string {
	if e.Dir == "" {
		return ""
	}
	return filepath.Base(e.Dir)
}

// driveCapCell is the 此电脑 page's drive usage cell: a slim capacity bar
// then the free/total text, as Explorer's drive tiles show. The tooltip
// carries the percentage.
func (a *App) driveCapCell(c *ui.Context, e Entry) {
	cell := ui.Row(c).Width(a.sizeColW()).Shrink(0).Height(a.detailsRowH()).
		Padding(0, 8).Gap(8).AlignItems(ui.Center)
	cell.Tooltip(driveCapacity(e) + "（" + drivePercent(e) + " 可用）")
	cell.Children(func() {
		bar := ui.Box(c).Width(64).Height(8).Shrink(0)
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			drawCapacityBar(p, r, e.VolFree, e.VolTotal)
		})
		ui.Text(c, driveCapacity(e)).FontSize(12).TextColor(colorText()).SingleLine()
	})
}

// driveCapacity is the 此电脑 page's drive text: free of total, as
// Explorer's 可用空间 / 总大小 columns read.
func driveCapacity(e Entry) string {
	if e.VolTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("可用 %s，共 %s", formatBytes(e.VolFree), formatBytes(e.VolTotal))
}

// drivePercent is the free share of a volume, for the usage tooltip.
func drivePercent(e Entry) string {
	if e.VolTotal <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f%%", float64(e.VolFree)/float64(e.VolTotal)*100)
}

// drawCapacityBar paints a rounded usage bar: a muted track with an accent
// fill sized to the used fraction, the way Explorer fills its drive bars.
func drawCapacityBar(p *ui.Painter, r ui.Rect, free, total int64) {
	rad := r.H / 2
	p.Fill(r, colorDivider(), rad)
	if total <= 0 {
		return
	}
	used := float64(total-free) / float64(total)
	if used <= 0.002 {
		return
	}
	if used > 1 {
		used = 1
	}
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(used), H: r.H}, colorAccent(), rad)
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
	t := a.cur()
	t.list.KeepOffset = true
	t.list.Key = groupRowKey(rows)
	a.scrollToReveal(rows, t.list.ScrollIntoView)
	list := ui.List(c, &t.list, len(rows), func(i int) {
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
	aiFxRow(c, row, aiFx.look(e.Path), a.listRowH(), 4)
	row.Children(func() {
		a.nameCell(c, e, 16, 12)
	})
	a.bindItemInput(c, row, e)
	a.attachRowMenu(row, e)
}

// iconView lays tiles in a grid. Extra large, large and medium put the
// label under the icon; small icons put it beside, in a shorter tile.
func (a *App) iconView(c *ui.Context, entries []Entry) {
	// The folder's mount decides each tile's eagerness, once for the view:
	// probing it per tile would be thousands of lookups a frame.
	a.cur().networkDir = netfs.IsNetworkPath(a.cur().path)
	// Whether the first tiles warm up is the folder's answer and the
	// setting's, read once for the view rather than per tile.
	a.cur().networkWarm = a.cur().networkDir && a.store.settings().networkWarmup()
	box := a.iconBoxSize(a.mode)
	minW, height := a.iconTileSize(a.mode, box)
	if a.grouping != groupNone && !specialPath(a.cur().path) {
		scroll := ui.Scroll(c).Grow(1).FillWidth().Padding(iconGridPad)
		scroll.Children(func() {
			rows := a.groupedRows(entries)
			var group []Entry
			warm := 0 // tiles decoded so far across the groups
			emit := func() {
				if len(group) == 0 {
					return
				}
				// The folder's first tileWarmup tiles decode even when groups
				// split the listing: each group's tiles count on from the
				// last, rather than warming up again from zero.
				base := warm
				warm += len(group)
				grid := ui.GridView(c, nil, len(group), minW, height, func(i int) {
					if i >= 0 && i < len(group) {
						a.iconTile(c, group[i], box, base+i)
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
		// The per-group grids share the scroll's width, so its box says how
		// many tiles a row takes.
		a.measureGrid(c, scroll, minW, height, iconGridPad)
		a.bindPaneMenu(c, scroll)
		// The grouped icon view scrolls as one column of grids, which has no
		// row index to reveal. Drop the request so a later switch of view does
		// not jump to it.
		a.cur().reveal = ""
		return
	}
	t := a.cur()
	t.grid.Key = func(i int) any {
		if i < 0 || i >= len(entries) || entries[i].Path == "" {
			return i
		}
		return entries[i].Path
	}
	if i := entryIndexOf(entries, t.reveal); i >= 0 {
		t.grid.ScrollIntoView(i)
		t.reveal = ""
	} else if t.reveal != "" {
		t.reveal = ""
	}
	grid := ui.GridView(c, &t.grid, len(entries), minW, height, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		a.iconTile(c, entries[i], box, i)
	}).Grow(1).FillWidth().MinHeight(0).Padding(iconGridPad)
	grid.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, grid)
	a.measureGrid(c, grid, minW, height, iconGridPad)
}

// iconGridPad is the padding the icon views leave around their grid, which
// the columns and the grid's measurement both take off its width.
const iconGridPad float32 = 8

// measureGrid records the icon view's shape as the frame before laid it
// out: how many tiles a row takes, and how many rows the pane shows, for
// the arrows' row-sized steps and the Page keys. box is the element the
// tiles are laid in — the grid, or the scroll holding the grouped view's
// per-group grids — and pad the padding between its edge and them.
func (a *App) measureGrid(c *ui.Context, box ui.Element, minW, height, pad float32) {
	b := box.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	t := a.cur()
	t.gridCols = gridCols(c, b.W, minW, pad)
	t.gridRows = max(int((b.H-pad*2)/height), 1)
}

// gridCols is how many items a grid lays in a row of width w, as GridView
// counts them: what fits at least minW wide and gap apart, inside pad on
// either side. A keyboard step down lands on the item under this one only
// where the count agrees with the grid's own.
func gridCols(c *ui.Context, w, minW, pad float32) int {
	gap := c.Theme().Space(2)
	return max(int((w-pad*2+gap)/(minW+gap)), 1)
}

// iconBaseSize is a view's icon box before 查看>放大/缩小: the four sizes
// Explorer's icon views use.
func iconBaseSize(mode ViewMode) float32 {
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

// isIconMode reports whether mode lays out a grid of tiles, the views
// 放大/缩小 resizes.
func isIconMode(mode ViewMode) bool {
	switch mode {
	case ViewSmallIcons, ViewMediumIcons, ViewLargeIcons, ViewExtraLarge:
		return true
	}
	return false
}

// iconBoxSize is a view's icon box with the 放大/缩小 factor applied, so
// 超大图标 reaches as far as 4× its 96 px.
func (a *App) iconBoxSize(mode ViewMode) float32 {
	base := iconBaseSize(mode)
	if !isIconMode(mode) {
		return base
	}
	return base * a.iconZoom
}

func (a *App) iconTileSize(mode ViewMode, box float32) (minW, height float32) {
	if mode == ViewSmallIcons {
		return box + 148, max(44, box+12)
	}
	h := box + 52
	if a.cur().path == pathThisPC {
		h += 12 // room for the drive usage bar under the name
	}
	return box + 40, h
}

// tileFont is the tile label's size, which grows a little with the tile so
// a 384 px icon does not sit over a 12 pt name.
func tileFont(box float32) float32 {
	f := 12 + (box-96)/32
	return min(max(f, 12), 18)
}

// tileWarmup is how many tiles of a network folder decode their pictures
// before anyone asks: the first rows fill in as on a local folder, and the
// rest wait for the pointer or the selection. The cap is what keeps opening
// a share from downloading the whole folder's media.
const tileWarmup = 12

// iconTile is one grid cell. ButtonBase is a row, so a small icon sits
// beside its name. Larger sizes put a column in the button: the icon, then
// the name under it. i is the tile's place in the listing, which decides
// whether a network tile warms up its picture or waits to be looked at.
func (a *App) iconTile(c *ui.Context, e Entry, box float32, i int) {
	tile := ui.ButtonBase(c.Key(e.Path)).Fill().Radius(4).Label(e.Name)
	if e.VolTotal > 0 {
		tile.Tooltip(driveCapacity(e) + "（" + drivePercent(e) + " 可用）")
	}
	if a.mode == ViewSmallIcons {
		tile.Padding(4, 6).Gap(8).AlignItems(ui.Center).Justify(ui.Start)
	}
	a.paintSelect(tile, e.Path)
	look := aiFx.look(e.Path)
	aiFxRow(c, tile, look, 0, 4)
	// A network tile decodes its picture for the first rows of the folder and
	// for a tile under the pointer or in the selection; the rest keep the
	// drawn glyph until someone looks, so opening a share does not download
	// the whole folder's media at once.
	eager := !a.cur().networkDir || (a.cur().networkWarm && i < tileWarmup) || tile.Hovered() || a.isSelected(e.Path)
	tile.Children(func() {
		if a.mode == ViewSmallIcons {
			a.nameCell(c, e, box, 12)
			return
		}
		font := tileFont(box)
		body := ui.Column(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(6, 4)
		body.Children(func() {
			glyph := ui.Box(c).Size(box, box).Shrink(0)
			glyph.Children(func() {
				a.tileGlyph(c, e, box, eager)
			})
			if a.cur().renaming == e.Name {
				a.renameField(c, font)
				return
			}
			label := searchNameText(c, a.activeSearchQuery(), a.displayName(e), font).
				TextAlign(ui.Center).MaxLines(2).FillWidth()
			if look.flash > 0 {
				label.TextColor(flashColor(colorText(), look))
			}
			if e.VolTotal > 0 {
				bar := ui.Box(c).Width(box).Height(6).Shrink(0)
				bar.Draw(func(p *ui.Painter, r ui.Rect) {
					drawCapacityBar(p, r, e.VolFree, e.VolTotal)
				})
			}
		})
	})
	a.bindItemInput(c, tile, e)
	a.attachRowMenu(tile, e)
	// The grouped icon views lay every tile out inside one plain Scroll, so
	// the tile the keyboard moved to is built even off screen and raises
	// itself: the ungrouped grid scrolls to its row through GridState.
	if a.grouping != groupNone && !specialPath(a.cur().path) && a.cur().reveal == e.Path {
		tile.ScrollIntoView()
	}
}

// tileGlyph is a tile's icon area: a picture or video shows its own frame,
// a folder holding media keeps its folder shape with the pictures inside
// it, and everything else is the drawn file or folder glyph. The drawn
// glyphs stay under thumbMinBox, where 16 px of picture says less than the
// file-type colour. eager false leaves the picture undecoded — the network
// tile's rule — and the drawn glyph stands in until the tile is looked at.
func (a *App) tileGlyph(c *ui.Context, e Entry, box float32, eager bool) {
	if box >= thumbMinBox && eager {
		if e.IsDir {
			if a.folderCoverEligible(e) {
				if bm := folderCoverThumb(e.Path, box); bm != nil {
					a.folderCoverTile(c, box, bm)
					return
				}
			}
		} else if bm := mediaThumb(e.Path, box); bm != nil {
			a.mediaTile(c, e, box, bm)
			return
		}
	}
	a.fileIcon(c, e, box)
}

// mediaTile is a picture's tile: the picture fills it, and a video's gets
// the play badge over it.
func (a *App) mediaTile(c *ui.Context, e Entry, box float32, bm *ui.Bitmap) {
	frame := ui.Box(c).Size(box, box).Shrink(0).Radius(4).Clip()
	frame.Children(func() {
		ui.Image(c, bm).Fit(ui.Cover).Size(box, box)
		if isVideoExt(lowerExt(e.Path)) {
			a.videoBadge(c, box)
		}
	})
}

// folderCoverTile is a media folder's tile. The folder is still a folder —
// its tab, its edges, its colour — and the pictures of its media fill the
// front panel inside it, which is how Explorer draws a folder that holds
// pictures: the folder's silhouette is what tells it apart from a file
// among the tiles.
func (a *App) folderCoverTile(c *ui.Context, box float32, bm *ui.Bitmap) {
	glyph := ui.Box(c).Size(box, box).Shrink(0)
	glyph.Children(func() {
		shell := ui.Box(c).Absolute().Left(0).Top(0).Size(box, box).Shrink(0)
		shell.Draw(func(p *ui.Painter, r ui.Rect) {
			drawFolder(p, r, colorFolder())
		})
		panel := folderCoverPanel(box)
		cover := ui.Box(c).Absolute().Left(panel.X).Top(panel.Y).
			Size(panel.W, panel.H).Shrink(0).
			Radius(folderCoverPanelRadius(box)).Clip()
		cover.Children(func() {
			ui.Image(c, bm).Fit(ui.Cover).Size(panel.W, panel.H)
		})
	})
}

// videoBadge marks a video's thumbnail with the round play badge Explorer
// paints over one.
func (a *App) videoBadge(c *ui.Context, box float32) {
	size := min(max(box*0.2, 20), 40)
	ui.Box(c).Absolute().Left(6).Bottom(6).Size(size, size).Shrink(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			d := min(r.W, r.H)
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: d, H: d}, ui.RGBA(0, 0, 0, 0.55), d/2)
			var tri ui.Path
			tri.MoveTo(r.X+d*0.36, r.Y+d*0.28)
			tri.LineTo(r.X+d*0.74, r.Y+d*0.50)
			tri.LineTo(r.X+d*0.36, r.Y+d*0.72)
			tri.Close()
			p.FillPath(&tri, ui.RGB(255, 255, 255))
		})
}

// folderCoverEligible reports whether a folder tile may show a cover of
// its contents: only real folders may, and never a volume root, whose
// contents are not a folder's.
func (a *App) folderCoverEligible(e Entry) bool {
	if e.Path == "" || specialPath(e.Path) || isVolumeRoot(e.Path) {
		return false
	}
	return a.cur().path != pathThisPC
}

// isVolumeRoot reports whether path is a mount point — "/" or
// "/Volumes/…" — rather than a folder inside one.
func isVolumeRoot(path string) bool {
	clean := filepath.Clean(path)
	if clean == "/" || clean == "." {
		return true
	}
	if vol := filepath.VolumeName(clean); vol != "" && filepath.Clean(vol) == clean {
		return true
	}
	return filepath.Dir(clean) == clean
}

// nameCell is the icon plus the name, or the rename field when this entry
// is the one being renamed. With 查看>显示>项目复选框 on, a check box
// leads them, as Explorer's item check boxes do.
func (a *App) nameCell(c *ui.Context, e Entry, icon, font float32) {
	if a.itemCheckboxes {
		a.checkboxCell(c, e)
	}
	glyph := ui.Box(c).Size(icon, icon).Shrink(0)
	glyph.Children(func() {
		a.fileIcon(c, e, icon)
	})
	if a.cur().renaming == e.Name {
		a.renameField(c, font)
		return
	}
	// A live search marks the runs it matched, Everything-style. A query whose
	// terms all sit in the path (path:docs) marks nothing in a name, and falls
	// through to the plain text.
	label := searchNameText(c, a.activeSearchQuery(), a.displayName(e), font).
		SingleLine().Grow(1).MinWidth(0)
	if l := aiFx.look(e.Path); l.flash > 0 {
		label.TextColor(flashColor(colorText(), l))
	}
}

// searchNameText is a name with the query's hits marked, Everything's yellow
// runs, or the plain name when the query marks nothing in it. The caller lays
// the element out — a row grows it, a tile centers it.
func searchNameText(c *ui.Context, q searchquery.Query, name string, font float32) ui.Element {
	if spans := q.Highlight(name); markedHit(spans) {
		return ui.RichText(c, searchHitSpans(spans, font)...)
	}
	return ui.Text(c, name).FontSize(font).TextColor(colorText())
}

// markedHit reports whether the highlight found anything to mark.
func markedHit(spans []searchquery.Span) bool {
	for _, s := range spans {
		if s.Hit {
			return true
		}
	}
	return false
}

// searchHitSpans converts the highlight runs into RichText spans: hit runs
// carry the hit background, plain runs the ordinary text color. The hit runs
// keep the text color: on the light theme the amber is pale enough to carry
// dark text, and on the dark theme it is dark enough for light text.
func searchHitSpans(spans []searchquery.Span, font float32) []ui.Span {
	out := make([]ui.Span, 0, len(spans))
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		sp := ui.Span{Text: s.Text, Size: font, Color: colorText()}
		if s.Hit {
			sp.Background = colorHit()
		}
		out = append(out, sp)
	}
	return out
}

// renameField edits a.cur().renameBuf. components.Text binds the buffer
// directly — every keystroke is in it — and Enter commits through Submitted.
// Escape is not the editor's: the window's shortcut handler cancels the edit
// (handleKeys), as it did when the field consumed the key itself.
func (a *App) renameField(c *ui.Context, font float32) {
	text := components.Text(c, a.cur().renameBuf, components.TextOptions{Editable: &a.cur().renameBuf})
	text.Label("重命名").AutoFocus().
		Height(font+10).FontSize(font).Padding(0, 4).Radius(2).
		Background(colorAddress()).Border(1, colorAccent())
	if text.Submitted() {
		a.commitRename()
	}
}

// checkboxCell is the item check box: a small square before the name,
// checked when the item is selected. Clicking it toggles that item alone,
// leaving the selection anchor where it was.
func (a *App) checkboxCell(c *ui.Context, e Entry) {
	box := ui.ButtonBase(c.Key("check-"+e.Path)).Size(16, 16).Shrink(0).
		Radius(3).Label("选择 " + e.Name)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		selected := a.isSelected(e.Path)
		ink := colorMuted()
		if selected {
			ink = colorAccent()
		} else if box.Hovered() {
			ink = colorText()
		}
		p.Stroke(r, ink, 3, 1)
		if selected {
			var mark ui.Path
			mark.MoveTo(r.X+r.W*0.22, r.Y+r.H*0.55)
			mark.LineTo(r.X+r.W*0.42, r.Y+r.H*0.75)
			mark.LineTo(r.X+r.W*0.78, r.Y+r.H*0.28)
			p.StrokePath(&mark, 1.8, colorAccent())
		}
	})
	if box.Clicked() {
		t := a.cur()
		if len(t.selected) == 0 {
			t.selected = map[string]bool{}
		}
		if t.selected[e.Path] {
			delete(t.selected, e.Path)
		} else {
			t.selected[e.Path] = true
		}
		a.requestPreview()
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
//
// The row also takes the Edit menu's commands: on macOS Cmd+C/X/V reach the
// focused element as "copy"/"cut"/"paste" rather than as key presses, and
// the row a click focused is where they arrive. Without this the pane's
// shortcuts never see them.
func (a *App) bindItemInput(c *ui.Context, el ui.Element, e Entry) {
	t := a.cur()
	if t.itemBounds == nil {
		t.itemBounds = map[string]ui.Rect{}
	}
	t.itemBounds[e.Path] = el.Bounds()
	el.HandleInput(a.paneCommand)
	if el.Clicked() {
		a.clickEntry(e.Path, el.ClickModifiers())
	}
	if el.DoubleClicked() {
		a.selectOnly(e.Path)
		a.doOpen()
	}
	if a.cur().path != pathThisPC && e.Path != "" && e.Path != pathThisPC {
		a.bindDragSource(el, e.Path)
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

// paneHandlesCommand reports whether name is one of the Edit menu's
// commands the file pane acts on. The names are the platform's, as the Edit
// menu declares them: "pasteAndMatchStyle" arrives when a menu item asks
// for plain text, which for files is the same paste.
func paneHandlesCommand(name string) bool {
	switch name {
	case "copy", "cut", "paste", "pasteAndMatchStyle", "selectAll", "undo", "delete":
		return true
	}
	return false
}

// paneCommand handles the Edit menu's commands — what macOS sends for
// Cmd+C/X/V/A/Z and Delete instead of a key press — while the file pane has
// the keyboard focus. A focused text field intercepts them first, so typing
// in the search box or a rename field still copies its own text.
func (a *App) paneCommand(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputCommand || !paneHandlesCommand(ev.Text) {
		return false
	}
	switch ev.Text {
	case "copy":
		a.doCopy()
	case "cut":
		a.doCut()
	case "paste", "pasteAndMatchStyle":
		a.doPaste()
	case "selectAll":
		a.selectAll()
	case "undo":
		a.doUndo()
	case "delete":
		a.doDelete()
	}
	return true
}

// bindPaneMenu is the menu of the pane background: New (folder / text
// document), paste, open in Terminal, refresh, refresh of the folder's
// size statistics, hidden items and the folder's properties. A row's own
// menu is the innermost one, so it takes the click when the pointer is on
// a row. The pane also accepts files dropped from other applications,
// copying them into the current folder.
func (a *App) bindPaneMenu(c *ui.Context, el ui.Element) {
	if a.cur().path != pathThisPC && a.cur().path != pathDisks {
		if files := el.DroppedFiles(); len(files) > 0 {
			a.doDropInto(dropModifiers(c.Modifiers()), a.cur().path, files)
		}
		a.bindNativeDrop(el, func(paths []string) {
			a.doDropInto(dropModifiers(c.Modifiers()), a.cur().path, paths)
		})
	}
	// A drag on the empty space the rows and tiles leave is a rubber band
	// selecting what it crosses; the rows' own drags never reach here.
	a.bindMarquee(c, el)
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
		if m.Item("粘贴").Disabled(!a.canPaste() || special).Chosen() {
			a.doPaste()
		}
		m.Separator()
		if m.Item("在终端中打开").Disabled(special).Chosen() {
			a.openInTerminal(a.cur().path)
		}
		// The plugins' pane-background items — dups' scan, usage's treemap.
		for _, it := range a.pluginMenuItems(nil) {
			if m.Item(it.Label).Disabled(it.Disabled).Checked(it.Checked).Chosen() {
				it.Run()
			}
		}
		m.Separator()
		if m.Item("刷新").Chosen() {
			a.doRefresh()
		}
		// 刷新统计 drops the folder's cached sizes and walks its subtree
		// again: only the size column moves — no reload, no new thumbnails.
		if m.Item("刷新统计").Disabled(special).Chosen() {
			refreshDirStats(a.cur().path)
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
// Refresh stats (folders), Delete, Properties.
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
	if one && !sel[0].IsDir && !onSpecial {
		m.Submenu("打开方式", func(n *ui.Menu) {
			sysOpenWithMenu(a, n, sel[0])
		})
	}
	if one && !onSpecial {
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
	if m.Item("粘贴").Disabled(!a.canPaste() || onSpecial).Chosen() {
		a.doPaste()
	}
	if m.Item("复制为路径").Disabled(!anySel || onSpecial).Chosen() {
		a.doCopyAsPath()
	}
	// 单选内联重命名；多选为批量重命名对话框——菜单项都叫“重命名”。
	if m.Item("重命名").Disabled(!anySel || onSpecial).Chosen() {
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
	// 刷新统计 rewalks the one selected folder's subtree for the size
	// column, as the pane menu's item does for the current folder.
	if m.Item("刷新统计").Disabled(!oneDir || onSpecial).Chosen() {
		refreshDirStats(sel[0].Path)
	}
	// The plugins' row-menu items — usage's treemap of the selected folder.
	for _, it := range a.pluginMenuItems(sel) {
		if m.Item(it.Label).Disabled(it.Disabled).Checked(it.Checked).Chosen() {
			it.Run()
		}
	}
	a.extMenuItems(m)
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
