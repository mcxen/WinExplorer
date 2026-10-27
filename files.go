package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

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
	// Clip so a details row wider than the pane cannot paint under the
	// assistant beside it; the AI glow lives on the row's right edge
	// inside this box (aifx.go), left of that pane.
	pane := ui.Column(c).Grow(1).FillHeight().MinWidth(0).
		Background(colorContent()).Clip()
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

// nameCell is the icon plus the name, or the rename field when this entry
// is the one being renamed. With 查看>显示>项目复选框 on, a check box
// leads them, as Explorer's item check boxes do.
func (a *App) nameCell(c *ui.Context, e Entry, icon, font float32) {
	if a.itemCheckboxes {
		a.checkboxCell(c, e)
	}
	glyph := ui.Box(c).Size(icon, icon).Shrink(0).Center()
	glyph.Children(func() {
		a.fileIcon(c, e, icon)
	})
	if a.cur().renaming == e.Name {
		a.renameField(c, font)
		return
	}
	// A live search shows complete names: hiding the extension would make
	// report.docx, report.md and report.txt indistinguishable. It also marks
	// the runs matched, Everything-style; path-only terms mark nothing.
	name := a.displayName(e)
	if strings.TrimSpace(a.cur().query) != "" {
		name = e.Name
	}
	label := searchNameText(c, a.activeSearchQuery(), name, font).
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
		a.navTree.focused = false
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
	if el.Pressed() || el.Clicked() {
		a.navTree.focused = false
	}
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
			if n.Item("文件夹").Shortcut(ui.Cmd|ui.Shift, ui.KeyN).Disabled(special).Chosen() {
				a.doNewFolder()
			}
			if n.Item("文本文档").Disabled(special).Chosen() {
				a.doNewTextFile()
			}
		})
		if m.Item("粘贴").Shortcut(ui.Cmd, ui.KeyV).Disabled(!a.canPaste() || special).Chosen() {
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
		if m.Item("刷新").Shortcut(0, ui.KeyF5).Chosen() {
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
		if !entryIsArchive(e) {
			allArchives = false
			break
		}
	}
	oneDir := one && sel[0].IsDir
	onSpecial := specialPath(a.cur().path)

	if m.Item("打开").Shortcut(0, ui.KeyEnter).Disabled(!anySel).Chosen() {
		a.doOpen()
	}
	// Finder's 显示包内容: walk into a macOS package without launching it.
	if one && isMacOSBundle(sel[0]) && !onSpecial {
		if m.Item("显示包内容").Chosen() {
			a.navigate(sel[0].Path)
		}
	}
	if one && !sel[0].IsDir && !onSpecial {
		m.Submenu("打开方式", func(n *ui.Menu) {
			openWithMenu(a, n, sel[0])
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
	if m.Item("剪切").Shortcut(ui.Cmd, ui.KeyX).Disabled(!anySel).Chosen() {
		a.doCut()
	}
	if m.Item("复制").Shortcut(ui.Cmd, ui.KeyC).Disabled(!anySel).Chosen() {
		a.doCopy()
	}
	if m.Item("粘贴").Shortcut(ui.Cmd, ui.KeyV).Disabled(!a.canPaste() || onSpecial).Chosen() {
		a.doPaste()
	}
	if m.Item("复制为路径").Shortcut(ui.Cmd|ui.Shift, ui.KeyC).Disabled(!anySel || onSpecial).Chosen() {
		a.doCopyAsPath()
	}
	// 单选内联重命名；多选为批量重命名对话框——菜单项都叫“重命名”。
	if m.Item("重命名").Shortcut(0, ui.KeyF2).Disabled(!anySel || onSpecial).Chosen() {
		a.doRenameStart()
	}
	// 删除 sits with the edit verbs, as Explorer's and Files' menus keep
	// it, not after the plugins' items (docs/specs/16).
	if m.Item("删除").Shortcut(0, ui.KeyDelete).Disabled(!anySel).Chosen() {
		a.doDelete()
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
	m.Separator()
	if m.Item("属性").Shortcut(ui.Alt, ui.KeyEnter).Disabled(!one).Chosen() {
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

// typeLabel is 文件夹 for a directory, "应用程序" (etc.) for a macOS
// package, otherwise "PNG 文件", "文本文档", or "文件" from the extension.
func typeLabel(e Entry) string {
	if e.IsDir {
		if label := bundleTypeLabel(e.Name); label != "" {
			return label
		}
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

// tileNameRunes is about how many characters a tile's two label lines hold
// at the icon size box.
func tileNameRunes(box float32) int {
	switch {
	case box >= 96:
		return 44
	case box >= 48:
		return 32
	}
	return 26
}

// tileName is a tile's label: a name past max runes keeps its start and
// its end (the extension, the counter, the date at the tail) around a
// middle ellipsis, and a zero-width space after each _ - . lets the
// two lines break between the name's parts instead of mid-word, as
// "IMG_20261012_18 / 3344.jpg" did.
func tileName(name string, max int) string {
	r := []rune(name)
	if max > 3 && len(r) > max {
		head := (max - 1) * 3 / 5
		tail := max - 1 - head
		r = append(append(append([]rune{}, r[:head]...), '…'), r[len(r)-tail:]...)
	}
	var b strings.Builder
	for i, ch := range r {
		b.WriteRune(ch)
		if (ch == '_' || ch == '-' || ch == '.') && i > 0 && i < len(r)-1 {
			b.WriteRune('\u200b')
		}
	}
	return b.String()
}
