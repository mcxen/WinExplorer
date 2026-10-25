package main

import "github.com/egoist/mygo/ui"

// visual.go is the 视觉风格 layer (VISUAL2, docs/specs/11): 经典 keeps the
// Windows 11 tokens; v2 takes Kero's surfaces — a soft vertical wash
// behind the chrome, the content raised as one rounded card with Kero's
// two-layer shadow (a lit hairline in the dark), the sidebar's chosen row
// a white chip on the wash, controls on soft faces — and Kero's blue.
// Density stays Explorer's: row heights, fonts and paddings do not move.

// visualV2 is the style the tokens were resolved for.
var visualV2 bool

const (
	v2CardRadius = 12
	v2RowRadius  = 6
	v2Gutter     = 8 // wash around the content card
)

// syncVisual re-resolves the tokens when 视觉风格 changed.
func (a *App) syncVisual() {
	v := a.store != nil && a.store.settings().VisualStyle == "v2"
	if v != visualV2 {
		visualV2 = v
		resolveTokens()
	}
}

// resolveV2 overrides the classic tokens with v2's (called from
// resolveTokens).
func resolveV2() {
	tokWindow = pick("#ECEEF4", "#141418")
	tokChrome, tokNav, tokStatus = tokWindow, tokWindow, tokWindow
	tokContent = pick("#FFFFFF", "#1E1E23")
	tokSurface = pick("#FFFFFF", "#232328")
	tokAddress = pick("#FFFFFF", "#2A2A30")
	tokText = pick("#1D1D1F", "#ECECF0")
	tokMuted = pick("#6E6E78", "#A9A9B4")
	tokAccent = pick("#3B6CF6", "#6B93FF")
	tokSel = tokAccent.Alpha(0.13)
	tokFieldBorder = pick("#DCDCE2", "#3A3A42")
	tokDanger = pick("#D93A35", "#FF6961")
	tokWarning = pick("#C27B0F", "#F0A33A")
	tokSuccess = pick("#1F9D55", "#4FD17A")
	tokBtnText = pick("#FFFFFF", "#0F1530")
	if darkMode() {
		tokDivider = ui.RGBA(255, 255, 255, 0.07)
		tokHover = ui.RGBA(255, 255, 255, 0.05)
	} else {
		tokDivider = ui.RGBA(0, 0, 0, 0.07)
		tokHover = ui.RGBA(0, 0, 0, 0.04)
	}
}

// v2Wash is Kero's "mist" backdrop: cool blue-grey above, warm grey below.
func v2Wash() ui.LinearGradient {
	hex := []string{"#E3E9F4", "#EBEFF6", "#F1F2F3", "#F2F1EC"}
	if darkMode() {
		hex = []string{"#12151B", "#14171D", "#17181B"}
	}
	stops := make([]ui.GradientStop, len(hex))
	for i, h := range hex {
		stops[i] = ui.GradientStop{Color: ui.Hex(h), At: float32(i) / float32(len(hex)-1)}
	}
	return ui.LinearGradient{Angle: 180, Oklab: true, Stops: stops}
}

// v2Raised is Kero's raised level: two soft shadows in the light; a lit
// top-to-bottom hairline over three tight shadows in the dark.
func v2Raised(e ui.Element) ui.Element {
	if darkMode() {
		return e.Border(1, tokText.Alpha(0.1)).BorderLinearGradient(ui.LinearGradient{
			Angle: 180, From: tokText.Alpha(0.16), To: tokText.Alpha(0.05),
		}).Shadow(0, 1, 1, 0, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 2, 2, -1, ui.RGBA(0, 0, 0, 0.2)).Shadow(0, 4, 6, -3, ui.RGBA(0, 0, 0, 0.24))
	}
	return e.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.07)).Shadow(0, 1, 6, 0, ui.RGBA(0, 0, 0, 0.035))
}

// v2Chosen is the sidebar's chosen row: a white chip with an even ring
// and a faint close shadow (Kero's chosen); raised in the dark.
func v2Chosen(e ui.Element) ui.Element {
	if darkMode() {
		return v2Raised(e.Background(ui.RGBA(255, 255, 255, 0.10)))
	}
	return e.Background(ui.Hex("#FFFFFF")).Shadow(0, 0, 0, 0.5, ui.RGBA(0, 0, 0, 0.06)).Shadow(0, 0.5, 2, 0, ui.RGBA(0, 0, 0, 0.05))
}

// v2Card styles e as the content card.
func v2Card(e ui.Element) ui.Element {
	return v2Raised(e.Radius(v2CardRadius).Clip().Background(tokContent))
}
