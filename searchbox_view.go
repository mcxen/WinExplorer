package main

// searchbox_view.go draws the quick-search palette body: filters, hit rows,
// status footer, and key handling. Split from searchbox.go (LAY2).

import (
	"strings"
	"sunvale/internal/searchquery"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

func (a *App) searchBoxModal(c *ui.Context) {
	if searchPanel != nil {
		return
	}
	b := &a.searchBox
	if !b.open {
		return
	}
	floatDialog(c, &b.open, "快速搜索", searchBoxWidth, true, func() {
		a.searchBoxBody(c)
	})
	if !b.open {
		a.searchBoxCancel()
	}
}

// searchBoxBody is the palette: the query row with its scope switch, the
// streamed hits, and the footer with the count, the elapsed time and the keys.
func (a *App) searchBoxBody(c *ui.Context) {
	b := &a.searchBox
	w, h := c.Size()
	listH := min(searchBoxListMax, max(searchBoxListMin, h-188))
	ui.Column(c).Gap(0).FillWidth().MinWidth(420).Children(func() {
		row := ui.Row(c).FillWidth().Height(44).Gap(8).AlignItems(ui.Center)
		row.Children(func() {
			components.Icon(c, lucide.Search).Size(16, 16).TextColor(colorMuted())
			in := ui.TextInputBase(c, &b.query).Grow(1).MinWidth(0).Height(28).FontSize(13).
				AutoFocus().
				Placeholder("搜索全部磁盘：多个词是 AND，ext:txt  path:docs  \"完整短语\"  case:")
			a.searchBoxKeys(c, in)
			sortIdx := int(b.sortBy)
			idx := scopeIndex(b.scope)
			if ui.Segmented(c, &sortIdx, "相关度", "按文件夹", "按类型", "名称").Shrink(0).Changed() {
				b.sortBy = searchSort(sortIdx)
				b.sortHits()
				b.cursor = b.cursorRow()
				b.list.ScrollIntoView(b.cursor)
				repaint()
			}
			if ui.Segmented(c, &idx, "本文件夹", "全部磁盘").Shrink(0).Changed() {
				b.scope = scopeFromIndex(idx) // the next frame starts the walk
				repaint()
			}
		})
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
		a.searchFilterBar(c)
		bodyH := listH + 24
		ui.Row(c).FillWidth().Height(bodyH).Children(func() {
			list := ui.Column(c).Grow(1).MinWidth(0).Height(bodyH)
			list.Children(func() {
				if len(b.results) == 0 {
					a.searchBoxNotice(c, listH)
				} else {
					ui.List(c, &b.list, len(b.rows), func(i int) {
						a.searchDisplayRowAt(c, i)
					}).FillWidth().Height(listH).MinHeight(0)
				}
				a.searchBoxStatus(c)
			})
			// The preview needs room for its path and actions without making
			// the result list reflow; narrow windows keep the list full width.
			if w >= 900 {
				ui.Box(c).Width(1).Height(bodyH).Background(colorDivider())
				if e, ok := b.hit(); ok {
					a.searchPreview(c, e)
				} else {
					searchPreviewEmpty(c, bodyH)
				}
			}
		})
	})
}

func (a *App) searchDisplayRowAt(c *ui.Context, i int) {
	b := &a.searchBox
	if i < 0 || i >= len(b.rows) {
		return
	}
	row := b.rows[i]
	if row.heading {
		ui.Row(c).FillWidth().Height(24).Padding(0, 6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(11).FontWeight(600).TextColor(colorMuted()).SingleLine()
		})
		return
	}
	a.searchHitRow(c, row.hit)
}

// searchFilterBar is the row under the query: each menu narrows the same
// walk by appending a query modifier, and the count on the right is what the
// filters kept out of the hits that came back. Changing a filter starts a new
// walk only when one is not already answering; the filter is part of the
// walk key, so the disk is traversed at most once per distinct query.
func (a *App) searchFilterBar(c *ui.Context) {
	b := &a.searchBox
	ui.Row(c).FillWidth().Height(30).Gap(8).AlignItems(ui.Center).Children(func() {
		searchKindChip(c, &b.filters.kind)
		searchTimeChip(c, &b.filters.when)
		searchSizeChip(c, &b.filters.size)
		searchPlaceChip(c, &b.scope)
	})
	ui.Row(c).FillWidth().Height(18).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		ui.Text(c, filterResultCount(b)).FontSize(11).TextColor(colorMuted()).
			SingleLine().Shrink(0).TextAlign(ui.End)
	})
}

// searchPlaceChip is the 位置 filter. It is the existing scope switch by
// another name: 当前文件夹 walks the tab's folder, 主文件夹 the home subtree
// and 全部磁盘 every mounted volume.
func searchPlaceChip(c *ui.Context, scope *searchScope) {
	value := "全部磁盘"
	if *scope == scopeFolder {
		value = "当前文件夹"
	}
	chip := searchFilterChip(c, "位置", value)
	changed := false
	chip.Menu(func(m *ui.Menu) {
		options := []struct {
			scope searchScope
			label string
		}{{scopeFolder, "当前文件夹"}, {scopeGlobal, "全部磁盘"}}
		for _, opt := range options {
			value := opt.scope
			if m.Item(opt.label).Checked(value == *scope).Chosen() {
				*scope = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchFilterChip(c *ui.Context, title, value string) ui.Element {
	chip := ui.ButtonBase(c.Key("search-filter-"+title)).Height(22).Padding(0, 8).Gap(4).
		Radius(11).Border(1, colorFieldBorder()).Shrink(0).AlignItems(ui.Center).
		Label(title + "：" + value)
	chip.Background(func() ui.Color {
		if chip.Hovered() || chip.Pressed() {
			return colorHover()
		}
		return ui.Color{}
	}())
	chip.Children(func() {
		ui.Text(c, title+"：").FontSize(11).TextColor(colorMuted()).SingleLine()
		ui.Text(c, value).FontSize(11).TextColor(colorText()).SingleLine()
	})
	return chip
}

func searchKindOptionsLabel(kind searchKindFilter) string {
	for _, opt := range searchKindOptions {
		if opt.filter == kind {
			return opt.label
		}
	}
	return searchKindOptions[0].label
}

func searchTimeOptionsLabel(when searchTimeFilter) string {
	for _, opt := range searchTimeOptions {
		if opt.filter == when {
			return opt.label
		}
	}
	return searchTimeOptions[0].label
}

func searchSizeOptionsLabel(size searchSizeFilter) string {
	for _, opt := range searchSizeOptions {
		if opt.filter == size {
			return opt.label
		}
	}
	return searchSizeOptions[0].label
}

func searchKindChip(c *ui.Context, current *searchKindFilter) {
	chip := searchFilterChip(c, "类型", searchKindOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchKindOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchTimeChip(c *ui.Context, current *searchTimeFilter) {
	chip := searchFilterChip(c, "时间", searchTimeOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchTimeOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

func searchSizeChip(c *ui.Context, current *searchSizeFilter) {
	chip := searchFilterChip(c, "大小", searchSizeOptionsLabel(*current))
	changed := false
	chip.Menu(func(m *ui.Menu) {
		for _, opt := range searchSizeOptions {
			value := opt.filter
			if m.Item(opt.label).Checked(value == *current).Chosen() {
				*current = value
				changed = true
			}
		}
	})
	if changed {
		repaint()
	}
}

// filterResultCount is the filter bar's right-hand count. It names kept hits
// when a filter is active; without one, the footer already has the count.
func filterResultCount(b *searchBoxState) string {
	if b.filters == (searchFilters{}) {
		return ""
	}
	return itoa(b.filteredHits) + " / " + itoa(b.totalHits) + " 个结果"
}

// searchBoxNotice is the list's place while there is nothing in it: whether the
// walk is running, found nothing, or has not been asked yet.
func (a *App) searchBoxNotice(c *ui.Context, height float32) {
	b := &a.searchBox
	text := "输入即搜索全部磁盘"
	switch {
	case strings.TrimSpace(b.query) == "":
	case b.busy:
		text = "正在搜索…"
	default:
		text = "没有找到匹配的项目"
	}
	ui.Row(c).FillWidth().Height(height).AlignItems(ui.Center).Justify(ui.Center).
		Children(func() {
			ui.Text(c, text).FontSize(12).TextColor(colorMuted())
		})
}

// searchHitRow is one hit: its icon, complete name and parent breadcrumb, then
// its type/size and date. The row under the cursor is highlighted; a click moves
// the cursor to it, a double click opens it, as Enter does, and 位置 jumps to
// the folder it lives in with it selected, as Ctrl+Enter does.
func (a *App) searchHitRow(c *ui.Context, i int) {
	b := &a.searchBox
	e := b.results[i]
	kind := b.kinds[e.Path]
	label := e.Name
	if why := kind.Label(); why != "" {
		label += " " + why
	}
	row := ui.ButtonBase(c.Key("hit-"+e.Path)).FillWidth().Height(searchHitRowH).
		MinHeight(searchHitRowH).Padding(0, 6).Gap(8).AlignItems(ui.Center).
		Radius(4).Label(label)
	row.Tooltip(e.Path)
	switch {
	case i == b.cursor:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		glyph := ui.Box(c).Size(24, 24).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, e, 24)
		})
		main := ui.Column(c).Grow(1).MinWidth(0).Height(searchHitRowH).Justify(ui.Center)
		main.Children(func() {
			name := ui.Row(c).MinWidth(0).Height(16).AlignItems(ui.Center).Gap(6)
			name.Children(func() {
				ui.RichText(c, searchNameSpans(b.parsed, e.Name, e.IsDir, 12)...).
					SingleLine().MinWidth(0)
				if label := kind.Label(); label != "" {
					searchReasonBadge(c, kind)
				}
			})
			a.searchPathRow(c, e)
		})
		meta := ui.Column(c).Width(searchHitMetaW).Shrink(0).Height(searchHitRowH).Justify(ui.Center)
		meta.Children(func() {
			kind := typeLabel(e)
			if e.IsDir {
				kind = "文件夹"
			} else if size := sizeColumnText(e); size != "" {
				kind = size + " · " + kind
			}
			ui.Text(c, kind).FontSize(11).TextColor(colorMuted()).SingleLine().
				Width(searchHitMetaW).TextAlign(ui.End)
			ui.Text(c, formatModTime(e.ModTime)).FontSize(11).TextColor(colorMuted()).SingleLine().
				Width(searchHitMetaW).TextAlign(ui.End)
		})
		// 位置 is the mouse's Ctrl+Enter: the hit's folder, with the hit in it.
		// The button takes its own clicks (the innermost element does), so it
		// neither moves the cursor nor opens the hit.
		loc := ui.ButtonBase(c.Key("loc-"+e.Path)).Size(18, 18).Shrink(0).Padding(0).
			Radius(4).Label("打开所在文件夹")
		if loc.Hovered() {
			loc.Background(colorHover())
		}
		loc.Children(func() {
			components.Icon(c, lucide.FolderOpen).Size(13, 13).TextColor(colorMuted())
		})
		if loc.Clicked() {
			a.revealSearchHit(e)
		}
	})
	if row.Clicked() {
		b.cursor, b.cursorPath = i, e.Path
	}
	if row.DoubleClicked() {
		a.openSearchHit(e)
	}
}

// searchPathRow is a hit's parent path as clickable breadcrumbs. A segment
// takes the mouse to its folder without choosing the row: the innermost
// button gets the click, as the 位置 button does. The elided middle has no
// destination of its own and shows the hidden folders on hover instead.
func (a *App) searchPathRow(c *ui.Context, e Entry) {
	segs := searchPathSegments(e.Dir, homeForSearch(), 5)
	q := a.searchBox.parsed
	row := ui.Row(c).MinWidth(0).Height(16).AlignItems(ui.Center).Gap(2).ClipX()
	row.Children(func() {
		for i, seg := range segs {
			if i > 0 {
				ui.Text(c, "›").FontSize(11).TextColor(colorMuted()).Shrink(0).
					Padding(0, 2)
			}
			if seg.Elided {
				text := ui.RichText(c, pathRunSpans(q, seg.Label, 11, colorMuted())...).Shrink(0)
				hidden := elidedPath(e.Dir, segs)
				text.Tooltip("省略的路径：" + hidden)
				continue
			}
			btn := ui.ButtonBase(c.Key("seg-"+e.Path+"|"+seg.Path)).
				Height(16).Padding(0, 2).Radius(3).Shrink(0)
			if btn.Hovered() {
				btn.Background(colorHover())
			}
			btn.Children(func() {
				color := colorMuted()
				if btn.Hovered() {
					color = colorAccent()
				}
				ui.RichText(c, pathRunSpans(q, seg.Label, 11, color)...).SingleLine()
			})
			if btn.Clicked() {
				a.searchBoxClose()
				a.showExplorer()
				a.navigate(seg.Path)
			}
		}
	})
}

// searchReasonBadge says why a hit is in the list: its name matched, or
// only its path did. It is a label, not a control.
func searchReasonBadge(c *ui.Context, kind searchquery.MatchKind) {
	label := kind.Label()
	if label == "" {
		return
	}
	ui.Row(c).Height(16).Padding(0, 6).Radius(8).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).
		Background(colorHover()).Children(func() {
		color := colorMuted()
		if kind == searchquery.MatchName {
			color = colorAccent()
		}
		ui.Text(c, label).FontSize(10).TextColor(color).SingleLine()
	})
}

// elidedPath names the folders the ellipsis stands for, or the full parent
// path when the segments cannot say exactly which ones they hid.
func elidedPath(dir string, segs []pathSeg) string {
	for i, seg := range segs {
		if !seg.Elided {
			continue
		}
		if i == 0 || i+1 >= len(segs) {
			return dir
		}
		rel, ok := searchPathBelow(segs[i+1].Path, segs[i-1].Path)
		if !ok {
			return dir
		}
		names := splitPath(rel)
		if len(names) > 1 {
			names = names[:len(names)-1]
		}
		if len(names) > 0 {
			return strings.Join(names, " › ")
		}
	}
	return dir
}

// searchBoxStatus is the palette's footer, Everything's line: which engine
// answered, how many hits it has and how long it took, the index's own state
// with the 重建索引 button beside it, and the keys.
func (a *App) searchBoxStatus(c *ui.Context) {
	b := &a.searchBox
	keys := "↑↓ 选择   Enter 打开   Ctrl+Enter 定位   Ctrl+C 复制路径   Esc 关闭"
	text := "输入即搜索"
	if idx := searchIndexSummary(); idx != "" {
		if searchIndexState().building {
			text = idx
		} else {
			text += " · " + idx
		}
	}
	if strings.TrimSpace(b.query) != "" {
		secs := ""
		if !b.started.IsZero() {
			to := b.started
			if !b.done.IsZero() {
				to = b.done
			}
			secs = " (" + formatElapsed(to.Sub(b.started)) + ")"
		}
		where := searchIndexSourceNote(b.src)
		if b.busy {
			text = where + " · 正在搜索…  已找到 " + itoa(len(b.results)) + " 个项目" + secs
		} else {
			text = where + " · 找到 " + itoa(len(b.results)) + " 个项目" + searchCapNote(len(b.results)) + secs
		}
	}
	ui.Row(c).FillWidth().Height(24).Gap(8).AlignItems(ui.Center).Children(func() {
		if b.busy {
			components.Spinner(c, components.SpinnerRing)
		}
		ui.Text(c, text).FontSize(11).TextColor(colorMuted()).SingleLine().Grow(1).MinWidth(0)
		if st := searchIndexState(); st.building {
			searchIndexProgress(c, st)
		}
		if searchIndexOn() {
			btn := ui.ButtonBase(c.Key("search-index-rebuild")).Height(18).Shrink(0).
				Padding(0, 6).Gap(4).Radius(4).AlignItems(ui.Center).Label("重建索引")
			if btn.Hovered() {
				btn.Background(colorHover())
			}
			btn.Children(func() {
				components.Icon(c, lucide.RefreshCw).Size(12, 12).TextColor(colorMuted())
				ui.Text(c, "重建索引").FontSize(11).TextColor(colorMuted()).SingleLine()
			})
			if btn.Clicked() {
				searchIndexRebuild()
				repaint()
			}
		}
		ui.Text(c, keys).FontSize(11).TextColor(colorMuted()).SingleLine().Shrink(0)
	})
}

// searchIndexProgress is the build's compact meter. Onevra's Progress already
// owns the indeterminate sweep and leaves it still under ReduceMotion, so the
// status line only supplies this feature's fixed 80x4 footprint and estimate.
func searchIndexProgress(c *ui.Context, st searchIndexStatus) {
	value := -1.0
	percent := ""
	if st.estimate > 0 {
		value = float64(min(st.progress, st.estimate)) / float64(st.estimate)
		percent = itoa(int(value*100)) + "%"
	}
	ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Width(80).Height(4).Shrink(0).Children(func() {
			components.Progress(c, value).FillWidth().Height(4)
		})
		if percent != "" {
			ui.Text(c, percent).FontSize(10).TextColor(colorMuted()).SingleLine()
		}
	})
}

// searchIndexSourceNote is what the footer says about the engine that answered
// the hits on screen: the live walk, the index, or — for a snapshot that is not
// the whole machine, or is being rebuilt right now — the index with the walk
// running on top of it.
func searchIndexSourceNote(src searchSource) string {
	if src != sourceIndex {
		return "实时遍历"
	}
	st := searchIndexState()
	switch {
	case !st.complete:
		return "索引 + 实时遍历（未完整）"
	case st.building:
		return "索引命中（正在重建）"
	}
	return "索引命中"
}

// formatElapsed is a search's elapsed time: seconds and milliseconds for a
// walk, microseconds for an index hit, which is over before a millisecond is
// up — Everything's own timing, at our scale.
func formatElapsed(d time.Duration) string {
	if d < time.Millisecond {
		if d < time.Microsecond {
			d = time.Microsecond
		}
		return d.Truncate(time.Microsecond).String()
	}
	return d.Truncate(time.Millisecond).String()
}

// searchBoxKeys wires the palette's keyboard. The query box keeps the keys it
// needs to edit, so the ones that walk and open the hits are consumed here:
// Enter opens the hit under the cursor, Ctrl/Cmd+Enter shows it in its folder,
// Escape closes the box.
func (a *App) searchBoxKeys(c *ui.Context, in ui.Element) {
	b := &a.searchBox
	if b.copyPath != "" {
		path := b.copyPath
		b.copyPath = ""
		if copyPathTextWith(path, c.WriteClipboard) {
			a.statusMsg = "已复制路径"
		}
	}
	start, end := in.TextSelection()
	b.querySelected = start != end
	in.OnTextKey(func(k ui.TextKey) bool {
		switch k.Key {
		case ui.KeyA:
			if k.Mods&(ui.Ctrl|ui.Cmd) != 0 {
				b.querySelected = true
			}
			return false
		case ui.KeyEnd, ui.KeyHome, ui.KeyLeft, ui.KeyRight:
			b.querySelected = false
			return false
		case ui.KeyUp:
			a.searchBoxMove(-1)
			return true
		case ui.KeyDown:
			a.searchBoxMove(1)
			return true
		case ui.KeyPageUp:
			a.searchBoxMove(-10)
			return true
		case ui.KeyPageDown:
			a.searchBoxMove(10)
			return true
		case ui.KeyC:
			if k.Mods&(ui.Ctrl|ui.Cmd) == 0 {
				return false
			}
			// Let the query editor copy its own selection. With no selected
			// text Ctrl+C copies the hit under the cursor instead.
			if b.querySelected {
				return false
			}
			if e, ok := b.hit(); ok && e.Path != "" && e.Path != pathThisPC {
				b.copyPath = e.Path
			}
			return true
		case ui.KeyEnter:
			e, ok := b.hit()
			if !ok {
				return true
			}
			if k.Mods&(ui.Ctrl|ui.Cmd) != 0 {
				a.revealSearchHit(e)
			} else {
				a.openSearchHit(e)
			}
			return true
		case ui.KeyEscape:
			a.searchBoxClose()
			return true
		}
		return false
	})
}

// scopeIndex is the scope switch's slot for a scope, and scopeFromIndex its
// inverse: 本文件夹 first, 全部磁盘 second.
func scopeIndex(s searchScope) int {
	if s == scopeGlobal {
		return 1
	}
	return 0
}

func scopeFromIndex(i int) searchScope {
	if i == 1 {
		return scopeGlobal
	}
	return scopeFolder
}
