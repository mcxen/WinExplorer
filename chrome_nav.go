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
	windowBG(c, ui.Column(c).FillWidth().Shrink(0)).Children(func() {
		navH, padY := float32(44), float32(6)
		if visualV2 {
			navH, padY = 38, 4
		}
		ui.Row(c).Height(navH).FillWidth().Padding(padY, 10).Gap(6).
			AlignItems(ui.Center).Children(func() {
			a.toolGroup(c, func() {
				a.navIconButton(c, lucide.ArrowLeft, "后退", a.canBack(), a.back)
				a.navIconButton(c, lucide.ArrowRight, "前进", a.canForward(), a.forward)
				a.navIconButton(c, lucide.ArrowUp, "向上", a.canUp(), a.up)
			})
			a.addressField(c)
			a.searchField(c)
		})
		if !visualV2 {
			ui.Box(c).Height(1).FillWidth().Background(colorDivider())
		}
	})
}

func (a *App) navIconButton(c *ui.Context, icon func() *ui.SVG, label string, enabled bool, act func()) {
	btn := components.IconButton(c, icon(), label, components.IconButtonPlain).Disabled(!enabled)
	if btn.Clicked() {
		act()
	}
}

func (a *App) refreshButton(c *ui.Context) {
	btn := ui.ButtonBase(c).Size(28, 28).Radius(rowR()).Shrink(0).
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
		Padding(0, 8).Gap(6).Radius(fieldRadius()).AlignItems(ui.Center).
		Background(colorAddress()).Border(1, colorDivider()).
		Label("搜索")
	if visualV2 {
		keroFace(field.Border(0, ui.Transparent))
	}
	field.Children(func() {
		// Match the input's 24 DIP height so the glyph shares its vertical center.
		ui.Box(c).Size(14, 24).Shrink(0).Center().Children(func() {
			components.Icon(c, lucide.Search).Size(14, 14).TextColor(colorMuted())
		})
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
