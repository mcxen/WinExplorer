// COLOR TYPE: ui.Color
package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// darkMode reports the effective appearance. mygo.Theme.IsDark follows the
// system, or Theme.SetSource, and waits for App.Run when called off the
// main goroutine. On the main goroutine before Run it is not yet available.
func darkMode() bool {
	if mygo.App.IsReady() {
		return mygo.Theme.IsDark()
	}
	return false
}

func pick(light, dark string) ui.Color {
	if darkMode() {
		return ui.Hex(dark)
	}
	return ui.Hex(light)
}

// Design tokens. Each resolves its pick pair once, on first use — the
// appearance is constant once known, and ui.Color is a value type, so a
// plain package var is safe. Add new tokens here instead of scattering new
// pick pairs across the code.
var (
	tokWindow  = pick("#F3F3F3", "#202020") // window chrome background
	tokChrome  = tokWindow                  // toolbar/tab strip background
	tokNav     = tokWindow                  // sidebar background
	tokContent = pick("#FFFFFF", "#272727") // file list background
	tokSel     = pick("#CCE4F7", "#004C87") // selected item
	tokText    = pick("#1A1A1A", "#F3F3F3") // primary foreground
	tokMuted   = pick("#5C5C5C", "#A0A0A0") // secondary foreground
	tokAccent  = pick("#0067C0", "#4CC2FF") // primary action
	tokDivider = pick("#E5E5E5", "#383838") // hairlines, secondary borders
	tokFolder  = pick("#F0C14D", "#E8B84A") // folder badge
	tokStatus  = tokWindow                  // status bar background
	tokAddress = pick("#FFFFFF", "#2D2D2D") // address bar, raised surfaces

	// tokBtnText is the foreground on an accent-filled button: white reads
	// well on the light accent but not on the dark one, so the dark theme
	// uses near-black.
	tokBtnText = pick("#FFFFFF", "#1A1A1A")

	// tokFieldBorder is the border of editable fields (text areas), a touch
	// darker than tokDivider in the light theme so inputs read as inputs.
	tokFieldBorder = pick("#D6D6D6", "#383838")
)

var tokHover = func() ui.Color {
	if darkMode() {
		return ui.RGBA(255, 255, 255, 0.06)
	}
	return ui.RGBA(0, 0, 0, 0.06)
}()

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
