package main

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// quicklook_bar.go is the quick preview's title bar, laid out the way
// QuickLook for Windows, macOS Quick Look and the Windows 11 Photos viewer
// agree on (docs/specs/16-commands-layout.md): stepping and the file's name
// on the left; on the right the one frequent action as a split button —
// 打开 with the system default, its chevron the 打开方式 list — then the
// 更多 overflow for what is occasional, and 关闭 last at the far edge.

const quickLookBarH float32 = 40

// quickLookBar renders the bar for the file st shows.
func (a *App) quickLookBar(c *ui.Context, st *previewState) {
	e := a.singleSelected()
	path := e.Path
	if st != nil && st.path != "" {
		path = st.path
	}
	isImage := st != nil && st.kind == previewImage
	prevOK, nextOK, label := a.quickLookStepState(path, isImage)
	ui.Row(c).FillWidth().Height(quickLookBarH).Gap(4).AlignItems(ui.Center).Children(func() {
		a.quickLookIconButton(c, lucide.ChevronLeft, "上一个 (←)", prevOK, func() { a.quickLookMove(-1, isImage) })
		a.quickLookIconButton(c, lucide.ChevronRight, "下一个 (→)", nextOK, func() { a.quickLookMove(1, isImage) })
		ui.Box(c).Size(16, 16).Shrink(0).Margin(0, 4, 0, 4).Children(func() {
			a.fileIcon(c, Entry{Name: filepath.Base(path), Path: path, Ext: lowerExt(path)}, 16)
		})
		ui.Text(c, label).FontSize(13).TextColor(colorText()).Grow(1).MinWidth(0).MaxLines(1)
		a.quickLookOpenSplit(c, Entry{Name: filepath.Base(path), Path: path, Ext: lowerExt(path)})
		a.quickLookMore(c, path)
		a.quickLookIconButton(c, lucide.X, "关闭 (Esc)", true, func() { a.quickLook = false })
	})
}

// quickLookStepState reports whether ← / → have somewhere to go and the
// title: the name, and the position among its neighbours for a picture.
func (a *App) quickLookStepState(path string, isImage bool) (prev, next bool, label string) {
	label = filepath.Base(path)
	if a.quickLookFile != "" {
		return false, false, label
	}
	if isImage {
		imgs := a.sameDirImages(path)
		i := imageIndexOf(imgs, path)
		if i >= 0 && len(imgs) > 0 {
			label = fmt.Sprintf("%s（%d / %d）", label, i+1, len(imgs))
		}
		return i > 0, i >= 0 && i < len(imgs)-1, label
	}
	vis := a.visibleEntries()
	i := a.navCursor(vis)
	return i > 0, i >= 0 && i < len(vis)-1, label
}

func (a *App) quickLookMove(dir int, isImage bool) {
	if isImage {
		a.quickLookStepImage(dir)
	} else {
		a.quickLookStep(dir)
	}
}

// quickLookIconButton is a 32×32 subtle button, the command-bar face.
func (a *App) quickLookIconButton(c *ui.Context, icon func() *ui.SVG, tip string, enabled bool, act func()) {
	if components.IconButton(c, icon(), tip, components.IconButtonPlain).Disabled(!enabled).Clicked() {
		act()
	}
}

// quickLookOpenSplit is WinUI's SplitButton: the accent face opens with
// the system default; the chevron half lists the 打开方式 apps.
func (a *App) quickLookOpenSplit(c *ui.Context, e Entry) {
	ui.Row(c).Height(32).Shrink(0).Radius(4).Clip().Background(colorAccent()).Children(func() {
		main := ui.ButtonBase(c).Height(32).Padding(0, 12).Label("打开").Tooltip("用默认应用打开 (Enter)")
		if main.Hovered() || main.Pressed() {
			main.Background(c.Theme().AccentHover)
		}
		main.Children(func() {
			ui.Text(c, "打开").FontSize(12).TextColor(colorBtnText()).SingleLine()
		})
		if main.Clicked() {
			if err := openWithOS(e.Path); err != nil {
				a.statusMsg = "无法打开：" + err.Error()
			}
			a.quickLook = false
		}
		ui.Box(c).Width(1).Height(32).Shrink(0).Background(colorBtnText().Alpha(0.3))
		drop := ui.ButtonBase(c).Size(28, 32).AlignItems(ui.Center).Justify(ui.Center).
			Label("打开方式").Tooltip("打开方式")
		if drop.Hovered() || drop.Pressed() {
			drop.Background(c.Theme().AccentHover)
		}
		drop.Children(func() {
			components.Icon(c, lucide.ChevronDown).Size(14, 14).TextColor(colorBtnText())
		})
		drop.Menu(func(m *ui.Menu) { openWithMenu(a, m, e) })
	})
}

// quickLookMore is the 更多 overflow: locate, copy, share, properties.
// Items that act on the selection are grayed when the preview shows a file
// that is not the selection (an assistant media card).
func (a *App) quickLookMore(c *ui.Context, path string) {
	onSel := a.quickLookFile == ""
	btn := components.IconButton(c, lucide.Ellipsis(), "更多", components.IconButtonPlain)
	btn.Menu(func(m *ui.Menu) { a.quickLookMoreMenu(m, path, onSel) })
}

func (a *App) quickLookMoreMenu(m *ui.Menu, path string, onSel bool) {
	if m.Item("在文件夹中显示").Chosen() {
		a.quickLook = false
		if dir := filepath.Dir(path); dir != a.cur().path {
			a.navigate(dir)
		}
		a.selectOnly(path)
		a.cur().reveal = path
	}
	if m.Item("复制路径").Shortcut(ui.Cmd|ui.Shift, ui.KeyC).Chosen() {
		if copyPathText(path) {
			a.statusMsg = "已复制路径"
		}
	}
	if m.Item("复制").Shortcut(ui.Cmd, ui.KeyC).Disabled(!onSel).Chosen() {
		a.doCopy()
	}
	m.Separator()
	if m.Item("分享").Disabled(!onSel).Chosen() {
		a.quickLook = false
		a.shareDialogOpen()
	}
	m.Separator()
	if m.Item("属性").Shortcut(ui.Alt, ui.KeyEnter).Disabled(!onSel).Chosen() {
		a.quickLook = false
		a.showProperties()
	}
}
