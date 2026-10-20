package main

// files_details.go draws the details view: the sortable column header with
// its resize grips, the fitted column widths, and one row per entry with the
// size, date, type and drive capacity cells. It was split out of files.go,
// which keeps the pane, grouping, selection and menus shared by all views.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

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
	// The next frame fits the columns to the pane this one laid out in.
	if b := list.Bounds(); b.W > 0 {
		t.detailsPaneW = b.W
	}
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

func (a *App) dateColW() float32 { d, _ := a.fittedCols(); return d }
func (a *App) typColW() float32  { _, t := a.fittedCols(); return t }

// Floors the details view squeezes its default columns to, enough for
// "2026/10/20 19:48" and for a short type name.
const (
	colDateFitMin float32 = 112
	colTypeFitMin float32 = 80
)

// fittedCols is the 修改日期 and 类型 widths for the pane the details view
// laid out in last frame. When the pane is narrower than the row (the
// assistant panel or the preview pane open beside it), the default
// columns give way, the date first, so 大小 stays on screen; a width the
// user dragged is theirs and never squeezed.
func (a *App) fittedCols() (date, typ float32) {
	date, typ = colW(a.colWidths.date, colDateW), colW(a.colWidths.typ, colTypeW)
	pane := a.cur().detailsPaneW
	if pane <= 0 {
		return date, typ
	}
	other := typ
	if strings.TrimSpace(a.cur().query) != "" {
		other = a.locColW()
	}
	need := a.nameColW() + date + other + a.sizeColW() + rowInset*2
	return squeezeCols(need-pane, date, typ, a.colWidths.date <= 0, a.colWidths.typ <= 0)
}

// squeezeCols takes over from the date column, down to its floor, then
// from the type column, until over is gone or both are at their floors.
func squeezeCols(over, date, typ float32, dateFree, typFree bool) (float32, float32) {
	if over <= 0 {
		return date, typ
	}
	if dateFree {
		cut := min(over, max(date-colDateFitMin, 0))
		date -= cut
		over -= cut
	}
	if typFree && over > 0 {
		typ -= min(over, max(typ-colTypeFitMin, 0))
	}
	return date, typ
}
func (a *App) locColW() float32 { return colW(a.colWidths.loc, colLocW) }

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
	// A button centers its content; the labels sit where their cells'
	// text starts, as Explorer's do: 大小 at the end, the rest at the start.
	cell.Justify(ui.Start)
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
	aiFxRow(c, row, e.Path, a.detailsRowH(), 4)
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
		// An empty folder's 0 B reads like a file that lost its data;
		// Explorer leaves the cell blank.
		if st.Size == 0 {
			return ""
		}
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
