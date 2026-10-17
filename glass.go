package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
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
// Transient surfaces get the app-drawn kind of material instead: dialogs
// are frosted glass panels floating over a dimmed window, with a soft
// shadow and a small rise-and-fade entrance.

// windowBG gives e the chrome's token background where the window shows
// no material, and leaves it clear — the material shows — where it does.
func windowBG(c *ui.Context, e ui.Element) ui.Element {
	if c.Vibrancy() {
		return e
	}
	return e.Background(colorWindow())
}

// dialogRadius rounds the floating dialogs' glass panels, and
// dialogMargin keeps them off the window's edges.
const (
	dialogRadius float32 = 12
	dialogMargin float32 = 48
)

// floatDialog shows fn's content in a floating glass dialog while *open
// is true: a dimmed backdrop, a frosted panel with a soft shadow, and a
// short rise-and-fade entrance. clear is for panels whose body is media
// (Quick Look), which floats on barely-toned glass instead of a frost.
// fn builds the panel's content, as for ui.Modal. The panel is capped to
// the window minus a margin, as ui.Modal's was, so a long dialog cannot
// run off the edges; closing is immediate, as the overlay stops building.
func floatDialog(c *ui.Context, open *bool, clear bool, fn func()) {
	w, h := c.Size()
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		g := glass.Glass{}
		if clear {
			g.Style = glass.Clear
		}
		panel.Radius(dialogRadius).Padding(12, 14).Clip().
			MaxWidth(w-dialogMargin).MaxHeight(h-dialogMargin).
			Material(g).
			Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.35)).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 12, Opacity: 0}})
		fn()
	})
}
