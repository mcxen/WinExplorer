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

func colorWindow() ui.Color  { return pick("#F3F3F3", "#202020") }
func colorChrome() ui.Color  { return pick("#F3F3F3", "#202020") }
func colorNav() ui.Color     { return pick("#F3F3F3", "#202020") }
func colorContent() ui.Color { return pick("#FFFFFF", "#272727") }

func colorHover() ui.Color {
	if darkMode() {
		return ui.RGBA(255, 255, 255, 0.06)
	}
	return ui.RGBA(0, 0, 0, 0.06)
}

func colorSelected() ui.Color { return pick("#CCE4F7", "#004C87") }
func colorText() ui.Color     { return pick("#1A1A1A", "#F3F3F3") }
func colorMuted() ui.Color    { return pick("#5C5C5C", "#A0A0A0") }
func colorAccent() ui.Color   { return pick("#0067C0", "#4CC2FF") }
func colorDivider() ui.Color  { return pick("#E5E5E5", "#383838") }
func colorFolder() ui.Color   { return pick("#F0C14D", "#E8B84A") }
func colorStatus() ui.Color   { return pick("#F3F3F3", "#202020") }
func colorAddress() ui.Color  { return pick("#FFFFFF", "#2D2D2D") }

// colorCloseHover is the caption close button's hover, the red Windows 11
// paints over it in both appearances.
func colorCloseHover() ui.Color { return ui.Hex("#C42B1C") }
