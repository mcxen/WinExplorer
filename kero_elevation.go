package main

// Elevation recipes adapted from Kero (https://github.com/egoist/kero,
// elevation.go, MIT License, Copyright (c) 2026 EGOIST): crisp edges on
// whole pixels — in the light a small soft shadow and no line, in the dark
// a lit top-to-bottom hairline over three tight shadows.

import "github.com/egoist/mygo/ui"

// keroFace is Kero's control face: a hairline ring and a 0.5/1.5 shadow in
// the light; lit edge + dark shadow in the dark.
func keroFace(e ui.Element) ui.Element {
	if darkMode() {
		return keroDarkShadow(keroLitEdge(e))
	}
	return e.Shadow(0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.13)).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.05))
}

// keroFloating lifts a pane above the window (Kero's floating).
func keroFloating(e ui.Element) ui.Element {
	if darkMode() {
		return keroDarkShadow(keroLitEdge(e)).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.35))
	}
	return e.Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.07)).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.06))
}

// keroMenu is Kero's macOS menu chrome: a near-white fill lighter at the
// top, white inner rims top and bottom, a 30% hairline and a soft drop.
func keroMenu(e ui.Element) ui.Element {
	if darkMode() {
		return e.Background(ui.Hex("#2C2C2E")).InsetShadow(0, 0, 0, 1, ui.RGBA(255, 255, 255, 0.12)).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.25)).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.4))
	}
	return e.LinearGradient(ui.LinearGradient{Angle: 180, From: ui.Hex("#fbfbfb"), To: ui.Hex("#f4f4f5")}).
		InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.9)).InsetShadow(0, -1, 0, 0, ui.RGBA(255, 255, 255, 0.9)).
		InsetShadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.3)).Shadow(0, 5, 10, 0, ui.RGBA(0, 0, 0, 0.22))
}

func keroLitEdge(e ui.Element) ui.Element {
	return e.Border(1, tokText.Alpha(0.12)).BorderLinearGradient(ui.LinearGradient{
		Angle: 180, From: tokText.Alpha(0.18), To: tokText.Alpha(0.06),
	})
}

func keroDarkShadow(e ui.Element) ui.Element {
	return e.Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 2, 2, -1, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 4, 6, -3, ui.RGBA(0, 0, 0, 0.24))
}
