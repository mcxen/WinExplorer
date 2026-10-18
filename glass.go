package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// glass.go is the app's Windows 11 material layer. The window is created
// with a system backdrop (mygo.VibrancyTabbed — Mica for a window with
// tabs in its title bar; see newAppWindow), and a material shows wherever
// the view draws no background. So the chrome bands — tab strip, command
// bar, address row, sidebar and status bar — paint their token background
// only when the window shows no material, and let the Mica through
// otherwise. Where a material can never show (older Windows, Linux) the
// tokens paint exactly as before.
//
// Transient surfaces get the app-drawn kind of material instead: the
// dialogs are Onevra's glass panels floating over a dimmed window — the
// tint and the hairline come from the frame theme appTheme installs
// (theme.go), so they follow 外观 — with MyGo's focus trapping and its
// entrance and exit transitions.

// windowBG gives e the chrome's token background where the window shows
// no material, and leaves it clear — the material shows — where it does.
func windowBG(c *ui.Context, e ui.Element) ui.Element {
	if c.Vibrancy() {
		return e
	}
	return e.Background(colorWindow())
}

// floatDialog shows fn's content in a floating glass dialog while *open is
// true, on components.Dialog: a dimmed backdrop, a frosted panel with the
// soft shadow and the 12 DIP corners the frame theme builds, focus trapping
// and restoration MyGo owns, Escape and backdrop dismissal, and a short
// rise-and-fade entrance and exit. label is the dialog's accessible name.
//
// width is the room the body gets in DIPs. width may not be the panel's own:
// components.Dialog pads the panel by Space(5) on every side, and MyGo does
// not clamp a child to its parent's content box, so a body needing width
// would otherwise hang out of the glass. floatDialog adds the padding back.
//
// clear is for panels whose body is media (Quick Look), which floats on
// barely-toned glass instead of a frost.
func floatDialog(c *ui.Context, open *bool, label string, width float32, clear bool, fn func()) {
	// The panel's own padding, read from the theme so a changed Spacing keeps
	// the glass fitted to the body.
	pad := c.Theme().Space(5)
	components.Dialog(c, open, components.DialogOptions{Label: label}, func(panel ui.Element) {
		panel.Width(width + 2*pad)
		if clear {
			panel.Material(glass.Glass{Style: glass.Clear})
		}
		fn()
	})
}
