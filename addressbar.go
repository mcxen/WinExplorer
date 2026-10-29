package main

// addressbar.go is the address bar: the breadcrumbs, with a menu of
// subfolders on each separator and the leading crumbs folded into a "…"
// menu when they do not fit, and the path editor that replaces them, with
// its completion dropdown. pathinput.go turns the text into places.
//
// The editor's focus is the app's to give: AutoFocus only takes in the
// frame an element is born, so an editor left on screen after it lost the
// focus could never be given it back by F4 or a click on the crumbs, and
// typing went nowhere. The editor is now taken down the moment the focus
// leaves it, and every way in asks for the focus explicitly (focusReq).

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// addrState is the window's address bar state. Only the front tab's bar
// is on screen, so one per window suffices; switching tabs ends an edit.
type addrState struct {
	// focusReq asks the editor to take the focus and select its text in
	// the next frame it is built.
	focusReq bool
	// caretEnd asks the editor to put the caret at the end, after a
	// completion was accepted.
	caretEnd bool
	// hadFocus is whether the editor held the focus in the last frame;
	// losing it ends the edit.
	hadFocus bool
	// errMsg is the inline error of the last Enter, until the text changes.
	errMsg string
	// seen is the text the suggestions were made for; initial is the text
	// the edit started with, so the dropdown opens only once the user types.
	seen, initial string
	open          bool
	idx           int
	sugs          []pathSuggestion
	pending       bool
	// caretAtEnd is whether the caret stood at the end of the text in the
	// last frame, so → accepts a completion only there.
	caretAtEnd bool
	// barW is the crumb area's width in the last frame, for the overflow.
	barW float32
}

// addressField is the rounded bar: crumbs and refresh, or the editor.
func (a *App) addressField(c *ui.Context) {
	editing := a.cur().editingPath
	field := ui.Row(c.Key("address-field")).Grow(1).Height(32).MinWidth(0).Radius(fieldRadius()).
		AlignItems(ui.Center).Background(colorAddress()).Clip()
	switch {
	case editing && a.addr.errMsg != "":
		field.Border(1, colorCloseHover())
	case editing:
		field.Border(1, colorAccent())
	default:
		field.Border(1, colorDivider())
	}
	var in ui.Element
	field.Children(func() {
		if editing {
			in = a.pathEditor(c)
			return
		}
		a.breadcrumbBar(c)
		a.refreshButton(c)
	})
	if !editing {
		// Read again: a click on the crumbs may have just opened the
		// editor, and its focus request must survive to the next frame.
		if !a.cur().editingPath {
			a.addr = addrState{barW: a.addr.barW}
		}
		return
	}
	panel := a.pathDropdown(c, field)
	// The focus left the editor and its dropdown — a click in the list, the
	// sidebar, another window: the edit ends, as Explorer's does, rather
	// than leaving an editor that no key reaches.
	focused := in.FocusWithin() || (panel != (ui.Element{}) && panel.FocusWithin())
	if a.addr.hadFocus && !focused {
		a.cancelPathEdit()
		return
	}
	a.addr.hadFocus = focused
}

// pathDisplay is the text the editor starts with for the folder on show.
func pathDisplay(path string) string {
	switch path {
	case pathThisPC:
		return "此电脑"
	case pathHome:
		return "主文件夹"
	case pathTrash:
		return "回收站"
	case pathGallery:
		return "画廊"
	case pathDisks:
		return "磁盘健康"
	}
	return path
}

// beginPathEdit opens the editor on the folder's path, all of it
// selected, so typing replaces it. From an open editor it selects again.
func (a *App) beginPathEdit() {
	t := a.cur()
	if !t.editingPath {
		t.pathDraft = pathDisplay(t.path)
		a.addr = addrState{barW: a.addr.barW, initial: t.pathDraft, seen: t.pathDraft}
	}
	t.editingPath = true
	a.addr.focusReq = true
}

func (a *App) cancelPathEdit() {
	a.cur().editingPath = false
	a.cur().pathDraft = ""
	a.addr = addrState{barW: a.addr.barW}
}

// commitPathEdit goes where text leads. Nowhere keeps the editor open with
// the error under it, the text as typed, as Explorer's message box does
// without the box.
func (a *App) commitPathEdit(text string) {
	target, msg := resolvePathInput(text, a.cur().path)
	if msg != "" {
		a.addr.errMsg = msg
		a.addr.open = false
		a.addr.focusReq = true
		a.addr.caretEnd = true
		return
	}
	a.cancelPathEdit()
	a.navigate(target.dir)
	if target.file != "" {
		// A file opens as its folder with that file selected and raised
		// into view, as Explorer's address bar does.
		a.selectOnly(target.file)
		a.cur().reveal = target.file
	}
}

// pathEditor is the one-line editor. Enter goes, Escape closes the
// dropdown and then the editor, ↑/↓ move in the dropdown, Tab and → at
// the end accept a completion. The field consumes those keys so the
// window does not see them.
func (a *App) pathEditor(c *ui.Context) ui.Element {
	t := a.cur()
	in := ui.TextInputBase(c.Key("path-edit"), &t.pathDraft).
		Grow(1).MinWidth(0).Height(28).FontSize(12).Padding(0, 8).
		Placeholder("输入路径").Label("地址")
	if a.addr.focusReq {
		a.addr.focusReq = false
		in.Focus()
		n := utf8.RuneCountInString(t.pathDraft)
		if a.addr.caretEnd {
			in.SetTextSelection(n, n)
		} else {
			in.SetTextSelection(0, n)
		}
		a.addr.caretEnd = false
		a.addr.hadFocus = true
	} else if a.addr.caretEnd {
		a.addr.caretEnd = false
		n := utf8.RuneCountInString(t.pathDraft)
		in.SetTextSelection(n, n)
	}
	if t.pathDraft != a.addr.seen {
		a.addr.seen = t.pathDraft
		a.addr.errMsg = ""
		a.addr.open = true
		a.addr.idx = -1
	}
	if a.addr.open {
		a.addr.sugs, a.addr.pending = a.pathSuggestions(t.pathDraft)
		if a.addr.idx >= len(a.addr.sugs) {
			a.addr.idx = len(a.addr.sugs) - 1
		}
	}
	s, e := in.TextSelection()
	n := utf8.RuneCountInString(t.pathDraft)
	a.addr.caretAtEnd = s == e && e == n
	in.OnTextKey(func(k ui.TextKey) bool { return a.pathEditorKey(k) })
	a.refreshButton(c)
	return in
}

// pathEditorKey handles a key in the editor between frames.
func (a *App) pathEditorKey(k ui.TextKey) bool {
	if k.Mods&^ui.Shift != 0 {
		return false
	}
	st := &a.addr
	sugs := st.sugs
	if !st.open {
		sugs = nil
	}
	switch k.Key {
	case ui.KeyEnter:
		if st.idx >= 0 && st.idx < len(sugs) {
			a.commitPathEdit(sugs[st.idx].path)
		} else {
			a.commitPathEdit(k.Text())
		}
		return true
	case ui.KeyEscape:
		if st.open && (len(sugs) > 0 || st.pending) {
			st.open = false
			return true
		}
		a.cancelPathEdit()
		return true
	case ui.KeyDown, ui.KeyUp:
		if !st.open {
			st.open, st.idx = true, -1
			st.sugs, st.pending = a.pathSuggestions(k.Text())
			sugs = st.sugs
		}
		if len(sugs) == 0 {
			return true
		}
		if k.Key == ui.KeyDown {
			st.idx = (st.idx + 1) % len(sugs)
		} else if st.idx <= 0 {
			st.idx = len(sugs) - 1
		} else {
			st.idx--
		}
		return true
	case ui.KeyTab, ui.KeyRight:
		if len(sugs) == 0 || (k.Key == ui.KeyRight && !st.caretAtEnd) || k.Mods != 0 {
			return k.Key == ui.KeyTab // Tab never leaves the bar mid-edit
		}
		i := max(st.idx, 0)
		k.SetText(sugs[i].insert)
		st.seen = "" // recompute for the accepted text
		st.caretEnd = true
		return true
	}
	return false
}

// pathDropdown shows the completions, or the inline error, under the
// field. It keeps the focus in the editor while pressed (KeepFocus), so a
// click on a row does not end the edit before it lands.
func (a *App) pathDropdown(c *ui.Context, field ui.Element) ui.Element {
	st := &a.addr
	showSugs := st.open && (len(st.sugs) > 0 || st.pending)
	if st.errMsg == "" && !showSugs {
		return ui.Element{}
	}
	var panel ui.Element
	ui.Overlay(c, func() {
		w := field.Bounds().W
		if w <= 0 {
			w = 400
		}
		panel = ui.Column(c.Key("path-dropdown")).Width(w).MaxHeight(360).
			AttachTo(field, ui.AnchorBottomStart, ui.AnchorTopStart).Margin(4, 0, 0, 0).
			Padding(4).Radius(8).Background(colorAddress()).Border(1, colorDivider()).
			Shadow(0, 8, 16, 0, ui.RGBA(0, 0, 0, 0.14)).KeepFocus().Label("地址建议")
		panel.Children(func() {
			if st.errMsg != "" {
				ui.Row(c).FillWidth().Height(32).Padding(0, 8).Gap(8).AlignItems(ui.Center).Children(func() {
					components.Icon(c, lucide.CircleAlert).Size(14, 14).TextColor(colorCloseHover())
					ui.Text(c, st.errMsg).FontSize(12).TextColor(colorText()).SingleLine()
				})
				return
			}
			ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
				if len(st.sugs) == 0 {
					ui.Row(c).FillWidth().Height(28).Padding(0, 8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "正在读取…").FontSize(12).TextColor(colorMuted())
					})
				}
				for i, s := range st.sugs {
					a.pathSuggestionRow(c, i, s)
				}
			})
		})
	})
	return panel
}

func (a *App) pathSuggestionRow(c *ui.Context, i int, s pathSuggestion) {
	row := ui.Row(c.Key("sug:"+s.path)).FillWidth().Height(28).Padding(0, 8).Gap(8).
		Radius(4).AlignItems(ui.Center).Shrink(0).Label(s.label)
	switch {
	case i == a.addr.idx:
		row.Background(colorSelected())
		row.ScrollIntoView()
	case row.Pressed():
		row.Background(colorPressed())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		icon := lucide.Folder
		if s.place {
			icon = lucide.Clock
		}
		components.Icon(c, icon).Size(14, 14).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, s.label).FontSize(12).TextColor(colorText()).SingleLine().Shrink(0)
		if s.detail != "" {
			ui.Text(c, s.detail).FontSize(11).TextColor(colorMuted()).SingleLine().Shrink(1).MinWidth(0)
		}
	})
	if row.Clicked() {
		a.commitPathEdit(s.path)
	}
}

// colorPressed is the fill of a plain control while the pointer holds it
// down, a step darker than its hover.
func colorPressed() ui.Color { return colorText().Alpha(0.1) }

// Breadcrumb widths, for the overflow: a crumb is its label plus padding,
// a separator its fixed box.
const (
	crumbPadX    = 6
	crumbSepW    = 16
	crumbFont    = 12
	crumbMoreW   = 28
	crumbMaxName = 220
)

// crumbsThatFit returns from which crumb on the bar shows them, given the
// crumb widths and the room: the last always shows; leading ones fold
// into the "…" menu until the rest fit.
func crumbsThatFit(widths []float32, room float32) int {
	if room <= 0 || len(widths) == 0 {
		return 0
	}
	total := float32(0)
	for i, w := range widths {
		total += w
		if i > 0 {
			total += crumbSepW
		}
	}
	first := 0
	for total > room && first < len(widths)-1 {
		if first == 0 {
			total += crumbMoreW + crumbSepW // the "…" appears
		}
		total -= widths[first] + crumbSepW
		first++
	}
	return first
}

// breadcrumbBar shows the crumbs, separated by chevron buttons whose menu
// lists the subfolders of the crumb before them, as Explorer's. Each crumb
// navigates; a click on the bar past the crumbs, or on the edit button,
// opens the editor. Right-click copies the path.
func (a *App) breadcrumbBar(c *ui.Context) {
	bar := ui.Row(c.Key("crumb-bar")).Grow(1).Height(30).MinWidth(0).Gap(0).
		Padding(0, 4).AlignItems(ui.Center).Label("地址栏")
	if bar.Clicked() {
		a.beginPathEdit()
	}
	cur := a.cur().path
	a.contextMenu(c, bar, func(m *xMenu) {
		if m.Item("复制地址").Chosen() {
			c.WriteClipboard(pathDisplay(cur))
		}
		if m.Item("编辑地址").Chosen() {
			a.beginPathEdit()
		}
	})
	if b := bar.Bounds(); b.W > 0 {
		a.addr.barW = b.W - 8 - 24 // padding and the edit button
	}
	crumbs := a.breadcrumbs()
	widths := make([]float32, len(crumbs))
	for i, cr := range crumbs {
		w, _ := c.MeasureText(0, ui.Span{Text: cr.Name, Size: crumbFont})
		widths[i] = min(w, crumbMaxName) + 2*crumbPadX
	}
	first := crumbsThatFit(widths, a.addr.barW)
	bar.Children(func() {
		row := ui.Row(c).Grow(1).MinWidth(0).Height(30).AlignItems(ui.Center).Clip()
		row.Children(func() {
			if first > 0 {
				a.crumbOverflow(c, crumbs[:first])
			}
			for i := first; i < len(crumbs); i++ {
				if i > 0 && i != first { // the "…" brings its own
					a.crumbSeparator(c, crumbs[i-1])
				}
				a.crumbButton(c, crumbs[i], i == len(crumbs)-1)
			}
			// The rest of the row is the bar's: a click there edits.
			ui.Box(c).Grow(1).MinWidth(8).Height(30).
				Label("地址栏空白").OnClick(a.beginPathEdit)
		})
		a.addressRecentButton(c)
	})
}

// addressRecentLimit is how many visits the address-bar chevron lists.
const addressRecentLimit = 15

// addressRecentButton is the chevron at the end of the crumb bar: a dense
// menu of recently visited folders (title · path). Picking one navigates.
// Editing the address stays on blank-click / Ctrl+L / F4 / the bar menu.
func (a *App) addressRecentButton(c *ui.Context) {
	btn := ui.ButtonBase(c).Size(24, 24).Radius(4).Shrink(0).
		Label("最近访问").Tooltip("最近访问")
	switch {
	case btn.Pressed():
		btn.Background(colorPressed())
	case btn.Hovered():
		btn.Background(colorHover())
	}
	btn.Children(func() {
		components.Icon(c, lucide.ChevronDown).Size(12, 12).TextColor(colorMuted())
	})
	a.menuButton(c, btn, false, func(m *xMenu) {
		for _, it := range a.addressRecentItems() {
			if m.Item(it.label).Icon(lucide.Folder, colorFolder()).Chosen() {
				a.navigate(it.path)
			}
		}
	})
}

type addressRecentItem struct{ label, path string }

// addressRecentItems builds the chevron menu rows from persisted visits,
// newest first, skipping the folder already on show.
func (a *App) addressRecentItems() []addressRecentItem {
	if a.store == nil {
		return nil
	}
	cur := a.cur().path
	known := knownFolderNames()
	var out []addressRecentItem
	for _, dir := range a.store.visitList(maxVisited) {
		if dir == cur || !dirExists(dir) {
			continue
		}
		title := filepath.Base(dir)
		if n, ok := known[dir]; ok {
			title = n
		}
		label := title
		disp := pathDisplay(dir)
		if disp != title {
			label = title + " · " + disp
		}
		out = append(out, addressRecentItem{label: label, path: dir})
		if len(out) >= addressRecentLimit {
			break
		}
	}
	return out
}

func (a *App) crumbButton(c *ui.Context, crumb Crumb, last bool) {
	path := crumb.Path
	b := ui.ButtonBase(c.Key("crumb:"+path)).Height(24).Radius(4).
		Padding(0, crumbPadX).Shrink(0).MaxWidth(crumbMaxName + 2*crumbPadX).
		AlignItems(ui.Center).Label(crumb.Name).Tooltip(pathDisplay(path))
	switch {
	case b.Pressed():
		b.Background(colorPressed())
	case b.Hovered():
		b.Background(colorHover())
	}
	b.Children(func() {
		t := ui.Text(c, crumb.Name).FontSize(crumbFont).SingleLine().TextColor(colorText())
		if last {
			t.FontWeight(600)
		}
	})
	if b.Clicked() {
		a.navigate(path)
	}
	a.contextMenu(c, b, func(m *xMenu) {
		if m.Item("复制路径").Chosen() {
			c.WriteClipboard(pathDisplay(path))
		}
		if m.Item("在新标签页中打开").Chosen() {
			a.openTab(path)
		}
	})
	// Dropping files on a crumb moves them to that level.
	a.bindDropFolder(c, b, path)
}

// crumbSeparator is the chevron between two crumbs; its menu lists the
// folders in parent, the folder on the path marked, as Explorer's.
func (a *App) crumbSeparator(c *ui.Context, parent Crumb) {
	sep := ui.ButtonBase(c.Key("sep:"+parent.Path)).Size(crumbSepW, 24).Radius(4).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Label(parent.Name + " 中的文件夹")
	switch {
	case sep.Pressed():
		sep.Background(colorPressed())
	case sep.Hovered():
		sep.Background(colorHover())
	}
	sep.Children(func() {
		chevronIcon(c, false, 12)
	})
	a.menuButton(c, sep, false, func(m *xMenu) {
		for _, p := range a.childPlaces(parent.Path) {
			if m.Item(p.Name).Icon(lucide.Folder, colorFolder()).Chosen() {
				a.navigate(p.Path)
			}
		}
	})
}

// crumbOverflow is the "…" that holds the crumbs the bar has no room for,
// nearest first.
func (a *App) crumbOverflow(c *ui.Context, hidden []Crumb) {
	more := ui.ButtonBase(c.Key("crumb-more")).Size(crumbMoreW, 24).Radius(4).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Label("更多位置").Tooltip("更多位置")
	switch {
	case more.Pressed():
		more.Background(colorPressed())
	case more.Hovered():
		more.Background(colorHover())
	}
	more.Children(func() {
		components.Icon(c, lucide.Ellipsis).Size(14, 14).TextColor(colorText())
	})
	a.menuButton(c, more, false, func(m *xMenu) {
		for i := len(hidden) - 1; i >= 0; i-- {
			if m.Item(hidden[i].Name).Icon(lucide.Folder, colorFolder()).Chosen() {
				a.navigate(hidden[i].Path)
			}
		}
	})
	a.crumbSeparator(c, hidden[len(hidden)-1])
}

// childPlaces lists what a separator's menu offers under dir: the drives
// under 此电脑, else dir's subfolders with their display names.
func (a *App) childPlaces(dir string) []Place {
	if dir == pathThisPC {
		return a.thisPCPlaces()
	}
	if strings.HasPrefix(dir, "::{") {
		return nil
	}
	known := knownFolderNames()
	var out []Place
	names := folderNames.now(dir).visibleNames(a.showHidden)
	for _, n := range names {
		p := filepath.Join(dir, n)
		name := n
		if k, ok := known[p]; ok {
			name = k
		}
		out = append(out, Place{Name: name, Path: p})
		if len(out) >= 300 {
			break
		}
	}
	return out
}
