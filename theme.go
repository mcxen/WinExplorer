// COLOR TYPE: ui.Color
package main

import (
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// systemDark reports the system's appearance. mygo.Theme.IsDark follows the
// system, or Theme.SetSource, and waits for App.Run when called off the
// main goroutine. On the main goroutine before Run it is not yet available.
func systemDark() bool {
	if mygo.App.IsReady() {
		return mygo.Theme.IsDark()
	}
	return false
}

// darkMode reports the appearance to paint with. 外观's 浅色/深色 wins over
// the system: mygo sets the backend appearance asynchronously and only then
// knows it (Theme.IsDark is what the backend reports), while Theme.Source
// knows the choice as soon as it is made — so the tokens can be re-resolved
// in the same turn as the click, and the window never paints one appearance
// while the widgets under it paint the other.
func darkMode() bool {
	switch mygo.Theme.Source() {
	case mygo.ThemeLight:
		return false
	case mygo.ThemeDark:
		return true
	}
	return systemDark()
}

func pick(light, dark string) ui.Color {
	if darkMode() {
		return ui.Hex(dark)
	}
	return ui.Hex(light)
}

// Design tokens. resolveTokens fills them once the effective appearance is
// known: at package init the app is not ready, so that pass resolves the
// light pair as the fallback; mygo.App.WhenReady resolves them again with
// the real appearance before the first window builds (see main.go), so a
// dark system gets the dark tokens. Every later appearance change resolves
// them again — useAppearance for a choice made in 设置 > 外观, the theme's
// OnUpdated listener (main.go) for one the system makes. ui.Color is a
// value type, so plain package vars are safe once resolved. Add new tokens
// here instead of scattering new pick pairs across the code.
var (
	tokWindow  ui.Color // window chrome background
	tokChrome  ui.Color // toolbar/tab strip background
	tokNav     ui.Color // sidebar background
	tokContent ui.Color // file list background
	tokSel     ui.Color // selected item
	tokText    ui.Color // primary foreground
	tokMuted   ui.Color // secondary foreground
	tokAccent  ui.Color // primary action
	tokDivider ui.Color // hairlines, secondary borders
	tokFolder  ui.Color // folder badge
	tokStatus  ui.Color // status bar background
	tokAddress ui.Color // address bar, raised surfaces

	// tokBtnText is the foreground on an accent-filled button: white reads
	// well on the light accent but not on the dark one, so the dark theme
	// uses near-black.
	tokBtnText ui.Color

	// tokFieldBorder is the border of editable fields (text areas), a touch
	// darker than tokDivider in the light theme so inputs read as inputs.
	tokFieldBorder ui.Color

	tokHover ui.Color
)

func init() { resolveTokens() }

// resolveTokens fills the design tokens from the effective appearance.
// Called at init (light fallback) and again when the app is ready, before
// the first window builds, so the windows show the system appearance.
func resolveTokens() {
	tokWindow = pick("#F3F3F3", "#202020")
	tokChrome = tokWindow
	tokNav = tokWindow
	tokContent = pick("#FFFFFF", "#272727")
	tokSel = pick("#CCE4F7", "#004C87")
	tokText = pick("#1A1A1A", "#F3F3F3")
	tokMuted = pick("#5C5C5C", "#A0A0A0")
	tokAccent = pick("#0067C0", "#4CC2FF")
	tokDivider = pick("#E5E5E5", "#383838")
	tokFolder = pick("#F0C14D", "#E8B84A")
	tokStatus = tokWindow
	tokAddress = pick("#FFFFFF", "#2D2D2D")
	tokBtnText = pick("#FFFFFF", "#1A1A1A")
	tokFieldBorder = pick("#D6D6D6", "#383838")
	if darkMode() {
		tokHover = ui.RGBA(255, 255, 255, 0.06)
	} else {
		tokHover = ui.RGBA(0, 0, 0, 0.06)
	}
}

// colorBtnText is the foreground for labels sitting on an accent fill.
func colorBtnText() ui.Color { return tokBtnText }

// colorFieldBorder borders editable fields.
func colorFieldBorder() ui.Color { return tokFieldBorder }

func colorWindow() ui.Color  { return tokWindow }
func colorChrome() ui.Color  { return tokChrome }
func colorNav() ui.Color     { return tokNav }
func colorContent() ui.Color { return tokContent }
func colorHover() ui.Color   { return tokHover }

func colorSelected() ui.Color { return tokSel }
func colorText() ui.Color     { return tokText }
func colorMuted() ui.Color    { return tokMuted }
func colorAccent() ui.Color   { return tokAccent }
func colorDivider() ui.Color  { return tokDivider }
func colorFolder() ui.Color   { return tokFolder }
func colorStatus() ui.Color   { return tokStatus }
func colorAddress() ui.Color  { return tokAddress }

// colorCloseHover is the caption close button's hover, the red Windows 11
// paints over it in both appearances.
func colorCloseHover() ui.Color { return ui.Hex("#C42B1C") }

// The 外观 setting as it is stored in state.json: "" is 跟随系统, the way an
// absent field reads.
const (
	appearanceLight = "light"
	appearanceDark  = "dark"
)

// appearanceSource is the theme source a stored 外观 setting names.
func appearanceSource(setting string) mygo.ThemeSource {
	switch setting {
	case appearanceLight:
		return mygo.ThemeLight
	case appearanceDark:
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// appearanceSetting is the stored 外观 setting a theme source names.
func appearanceSetting(source mygo.ThemeSource) string {
	switch source {
	case mygo.ThemeLight:
		return appearanceLight
	case mygo.ThemeDark:
		return appearanceDark
	}
	return ""
}

// useAppearance switches the app to source and remembers the choice. mygo
// repaints its own widgets from the new theme on its own, but the tokens
// this app paints with are resolved once, so without the re-resolve here the
// window would keep the appearance it started in while its widgets switched
// under it. Every open window redraws with the tokens.
func useAppearance(store *persistedState, source mygo.ThemeSource) {
	store.updateSettings(func(s *appSettings) { s.Appearance = appearanceSetting(source) })
	mygo.Theme.SetSource(source)
	repaintAppearance()
}

// repaintAppearance re-resolves the tokens for the effective appearance and
// redraws every open window with them. Called for the choices made in
// 设置 > 外观 and for the ones the system makes while the app runs.
func repaintAppearance() {
	resolveTokens()
	appsMu.Lock()
	all := slices.Clone(apps)
	appsMu.Unlock()
	for _, app := range all {
		if win := app.win; win != nil && !win.IsDestroyed() {
			win.Invalidate()
		}
	}
}
