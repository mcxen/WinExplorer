package main

import (
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// navRow is the Windows 11 address row: back, forward, up, a rounded
// address field (breadcrumbs or a path editor, plus refresh), and a search
// field. Height is 44, and a 1px rule under the row separates the chrome
// from the content, as in Explorer.
func (a *App) navRow(c *ui.Context) {
	ui.Column(c).FillWidth().Shrink(0).Background(colorChrome()).Children(func() {
		ui.Row(c).Height(44).FillWidth().Padding(6, 10).Gap(6).
			AlignItems(ui.Center).Children(func() {
			a.navIconButton(c, lucide.ArrowLeft, "后退", a.canBack(), a.back)
			a.navIconButton(c, lucide.ArrowRight, "前进", a.canForward(), a.forward)
			a.navIconButton(c, lucide.ArrowUp, "向上", a.canUp(), a.up)
			a.addressField(c)
			a.searchField(c)
		})
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
	})
}

func (a *App) navIconButton(c *ui.Context, icon func() *ui.SVG, label string, enabled bool, act func()) {
	btn := components.IconButton(c, icon(), label, components.IconButtonPlain).Disabled(!enabled)
	if btn.Clicked() {
		act()
	}
}

// addressField is the rounded bar. Breadcrumbs fill it until the user
// clicks empty space or the edit affordance, which swaps in a path input.
func (a *App) addressField(c *ui.Context) {
	field := ui.Row(c).Grow(1).Height(32).MinWidth(0).Radius(4).
		AlignItems(ui.Center).Background(colorAddress()).
		Border(1, colorDivider()).Clip()
	field.Children(func() {
		if a.cur().editingPath {
			a.pathEditor(c)
			return
		}
		a.breadcrumbBar(c)
		a.refreshButton(c)
	})
}

// breadcrumbBar shows breadcrumbs() separated by chevrons. Each crumb,
// including the last, navigates. A click on the bar itself (padding past
// the crumbs) or on the edit affordance opens the path editor.
func (a *App) breadcrumbBar(c *ui.Context) {
	bar := ui.Row(c).Grow(1).Height(32).MinWidth(0).Gap(2).
		Padding(0, 6).AlignItems(ui.Center)
	if bar.Clicked() {
		a.beginPathEdit()
	}
	bar.Children(func() {
		crumbs := a.breadcrumbs()
		ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(32).Children(func() {
			for i, crumb := range crumbs {
				if i > 0 {
					sep := ui.Box(c).Size(14, 32).Shrink(0)
					sep.Children(func() {
						components.Icon(c, lucide.ChevronRight).Size(12, 12).TextColor(colorMuted())
					})
				}
				path := crumb.Path
				b := ui.ButtonBase(c).Key(path).Height(24).Radius(4).
					Padding(0, 6).Shrink(1).MinWidth(24).Label(crumb.Name)
				if b.Hovered() {
					b.Background(colorHover())
				}
				b.Children(func() {
					ui.Text(c, crumb.Name).FontSize(12).SingleLine()
				})
				if b.Clicked() {
					a.navigate(path)
				}
				// Dropping files on a crumb moves them to that level.
				a.bindDropFolder(c, b, path)
			}
		})
		edit := ui.ButtonBase(c).Size(22, 22).Radius(4).Shrink(0).
			Label("编辑地址").Tooltip("编辑地址")
		if edit.Hovered() {
			edit.Background(colorHover())
		}
		edit.Children(func() {
			components.Icon(c, lucide.ChevronDown).Size(12, 12).TextColor(colorMuted())
		})
		if edit.Clicked() {
			a.beginPathEdit()
		}
	})
}

func (a *App) beginPathEdit() {
	a.cur().editingPath = true
	switch a.cur().path {
	case pathThisPC:
		a.cur().pathDraft = "此电脑"
	case pathDisks:
		a.cur().pathDraft = "磁盘健康"
	default:
		a.cur().pathDraft = a.cur().path
	}
}

// pathEditor replaces the crumbs with one line. Enter navigates, Escape
// cancels. The field itself consumes those keys so the window does not.
func (a *App) pathEditor(c *ui.Context) {
	in := ui.TextInputBase(c.Key("path-edit"), &a.cur().pathDraft).
		Grow(1).Height(28).FontSize(12).Padding(0, 8).
		AutoFocus().Placeholder("输入路径")
	in.OnTextKey(func(k ui.TextKey) bool {
		switch k.Key {
		case ui.KeyEnter:
			a.commitPathEdit(k.Text())
			return true
		case ui.KeyEscape:
			a.cancelPathEdit()
			return true
		}
		return false
	})
	if in.Submitted() {
		a.commitPathEdit(a.cur().pathDraft)
	}
	a.refreshButton(c)
}

func (a *App) commitPathEdit(text string) {
	a.cur().editingPath = false
	a.cur().pathDraft = ""
	if text == "" || text == "此电脑" || text == pathThisPC {
		a.navigate(pathThisPC)
		return
	}
	if text == "磁盘健康" || text == pathDisks {
		a.navigate(pathDisks)
		return
	}
	a.navigate(text)
}

func (a *App) cancelPathEdit() {
	a.cur().editingPath = false
	a.cur().pathDraft = ""
}

func (a *App) refreshButton(c *ui.Context) {
	btn := ui.ButtonBase(c).Size(28, 28).Radius(4).Shrink(0).
		Label("刷新").Tooltip("刷新")
	if btn.Hovered() {
		btn.Background(colorHover())
	}
	btn.Children(func() {
		components.Icon(c, lucide.RotateCw).Size(14, 14).TextColor(colorText())
	})
	if btn.Clicked() {
		a.doRefresh()
	}
}

// searchField filters the current folder as the user types. Built on
// TextInputBase rather than SearchField so the placeholder is the Chinese
// string: SearchField hard-codes its own and ignores Placeholder.
func (a *App) searchField(c *ui.Context) {
	field := ui.Row(c.Key("folder-search")).Width(220).Height(32).Shrink(0).
		Padding(0, 8).Gap(6).Radius(4).AlignItems(ui.Center).
		Background(colorAddress()).Border(1, colorDivider()).
		Label("搜索")
	field.Children(func() {
		components.Icon(c, lucide.Search).Size(14, 14).TextColor(colorMuted())
		in := ui.TextInputBase(c, &a.cur().query).Grow(1).MinWidth(0).Height(24).
			FontSize(12).Placeholder("搜索")
		a.fieldFocused = in.FocusWithin()
		if a.focusSearch {
			in.Focus()
			a.focusSearch = false
		}
		if a.cur().query != "" {
			// Escape clears, as SearchField does, and is consumed so the
			// window's key handler does not also see it.
			if in.Shortcut(0, ui.KeyEscape) {
				a.cur().query = ""
			}
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Shrink(0).Label("清除搜索")
			clear.Background(colorMuted().Alpha(0.45))
			clear.Children(func() {
				components.Icon(c, lucide.X).Size(9, 9).TextColor(colorAddress())
			})
			if clear.Clicked() {
				a.cur().query = ""
			}
		}
	})
}
