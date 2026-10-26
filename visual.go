package main

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// visual.go is the 视觉风格 layer (VISUAL2, docs/specs/11): 经典 keeps the
// Windows 11 tokens; v2 takes Kero's surfaces — a soft vertical wash
// behind the chrome, the content raised as one rounded card with Kero's
// two-layer shadow (a lit hairline in the dark), the sidebar's chosen row
// a white chip on the wash, controls on soft faces — and Kero's blue.
// Density stays Explorer's: row heights, fonts and paddings do not move.

// visualV2 is the style the tokens were resolved for.
var visualV2 bool

// macOS 26 (Tahoe) tokens. Corners are concentric: a shape inset by d
// inside a parent of radius R takes R-d, so a row inset 6 in a 14-radius
// floating pane is 8 → rounded to the row token.
const (
	v2PaneRadius  = 14 // floating sidebar / content card / dialogs
	v2CardRadius  = 12 // cards inside a pane (tasks, disks, OpenUI)
	v2RowRadius   = 7  // rows, menu items, selection highlight
	v2InputRadius = 16 // toolbar fields are capsules (height 32)
	v2PillRadius  = 16 // toolbar button groups (height 32)
	v2Gutter      = 8  // window → floating pane inset
)

// visualStyleLabel is the user-facing name of the "v2" setting value; the
// stored value stays "v2" so existing configs keep the style.
const visualStyleLabel = "macOS 26"

// syncVisual re-resolves the tokens when 视觉风格 changed.
func (a *App) syncVisual() {
	fontsOnce.Do(registerFonts)
	v := a.store != nil && a.store.settings().VisualStyle == "v2"
	sf := a.store != nil && a.store.settings().SystemFont
	if v != visualV2 || sf != systemFontPref {
		visualV2, systemFontPref = v, sf
		resolveTokens()
	}
}

// resolveV2 overrides the classic tokens with v2's (called from
// resolveTokens).
func resolveV2() {
	tokWindow = pick("#ECECEF", "#1C1C1E")
	tokChrome, tokNav, tokStatus = tokWindow, tokWindow, tokWindow
	tokContent = pick("#FFFFFF", "#232325")
	tokSurface = pick("#FFFFFF", "#2A2A2D")
	tokAddress = pick("#FFFFFF", "#323235").Alpha(0.72)
	tokText = pick("#1D1D1F", "#F2F2F7")
	tokMuted = pick("#7C7C82", "#98989F")
	// Apple system colors (controlAccentColor blue, systemRed/Orange/Green).
	tokAccent = pick("#007AFF", "#0A84FF")
	tokSel = tokAccent.Alpha(0.16)
	tokFieldBorder = pick("#D1D1D6", "#3A3A3C")
	tokDanger = pick("#FF3B30", "#FF453A")
	tokWarning = pick("#FF9500", "#FF9F0A")
	tokSuccess = pick("#34C759", "#30D158")
	tokBtnText = pick("#FFFFFF", "#FFFFFF")
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
	hex := []string{"#E6E9F0", "#ECEDF1", "#EFEFF1"}
	if darkMode() {
		hex = []string{"#17181C", "#1A1A1D", "#1C1C1E"}
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
		return e.Background(ui.RGBA(255, 255, 255, 0.12))
	}
	return e.Background(ui.RGBA(0, 0, 0, 0.075))
}

// v2Card styles e as the content card.
func v2Card(e ui.Element) ui.Element {
	return v2Raised(e.Radius(v2PaneRadius).Clip().Background(tokContent))
}

// fieldRadius is the address and search field corner radius: v2's tighter
// input token, classic's 6.
func fieldRadius() float32 {
	if visualV2 {
		return v2InputRadius
	}
	return 6
}

// cardRadius is the page-card corner radius: a disk card, an occupant of
// the content card. v2's content card is 8, so a 12-radius classic card
// floating inside it would clash corners; classic keeps the dialog pair 12.
func cardRadius() float32 {
	if visualV2 {
		return v2CardRadius
	}
	return 12
}

// innerRadius is the corner radius of the soft faces inside a card —
// thumbnail boxes, charts, badges, assistant cards. Classic drew them at 6;
// v2 tightens them to the row token so they stay under the card's 8.
func innerRadius() float32 {
	if visualV2 {
		return v2RowRadius
	}
	return 6
}

// sidebarListPad is the top inset of the nav list. v2 matches the content
// card's flush top so the first sidebar row lines up with the details header.
func sidebarListPad() float32 {
	if visualV2 {
		return 6
	}
	return 8
}

// macGlass fills e with the macOS 26 material: MyGo's Liquid Glass (a real
// backdrop blur, GPU shader on Metal / D3D / GL, CPU in headless) over what
// is painted beneath it — the wash, or the window's native vibrancy (Mica on
// Windows 11, NSVisualEffectView on macOS) where the window has one. It
// falls back to a translucent tint when the desktop asks for contrast, when
// the app turned glass off, or when frames already ran over budget.
func macGlass(c *ui.Context, e ui.Element, lowEnd bool) ui.Element {
	if !glassOn || lowEnd || c.Preferences().HighContrast {
		return e.Background(glassTint())
	}
	return e.Material(glass.Glass{Style: glass.Regular, Tint: glassTint().Alpha(0.35)})
}

// glassOn is the material switch; tests and the perf fallback flip it.
var glassOn = true

// glassTint is the fallback (and the glass's own) tone: Tahoe's sidebar
// grey, translucent so the wash still shows.
func glassTint() ui.Color {
	if darkMode() {
		return ui.RGBA(44, 44, 48, 0.78)
	}
	return ui.RGBA(246, 246, 248, 0.78)
}

// macHairline is the 0.5-1 DIP rim Tahoe draws around floating glass.
func macHairline(e ui.Element) ui.Element {
	if darkMode() {
		return e.Border(1, ui.RGBA(255, 255, 255, 0.10))
	}
	return e.Border(1, ui.RGBA(0, 0, 0, 0.06))
}

// macFloat is a floating pane: glass, hairline, soft shadow, pane radius.
func macFloat(c *ui.Context, e ui.Element, lowEnd bool) ui.Element {
	e = macGlass(c, e.Radius(v2PaneRadius).Clip(), lowEnd)
	if !darkMode() {
		e = macHairline(e)
	}
	return keroFloating(e)
}

// macPill is a toolbar capsule grouping buttons, as Tahoe's toolbar does.
func macPill(c *ui.Context, e ui.Element, lowEnd bool) ui.Element {
	return keroFace(macGlass(c, e.Height(32).Radius(v2PillRadius).Padding(0, 4).Gap(0).AlignItems(ui.Center), lowEnd))
}

// sidebarSidePad insets the nav rows inside the floating pane so the
// rounded selection reads as a concentric shape (pane 14 − inset 6 ≈ row 7).
func sidebarSidePad() float32 {
	if visualV2 {
		return 6
	}
	return 0
}

// cmdRadius rounds a toolbar text button: a capsule inside a pill.
func cmdRadius() float32 {
	if visualV2 {
		return 14
	}
	return 4
}

// rowR is the small-shape radius (rows, chips, icon buttons): 4 classic,
// the row token under macOS 26.
func rowR() float32 {
	if visualV2 {
		return v2RowRadius
	}
	return 4
}

// ctlR is the control radius (buttons, inputs inside panes): 6 classic, 8.
func ctlR() float32 {
	if visualV2 {
		return 8
	}
	return 6
}
