package main

// layout.go fits the panes to the window (docs/specs/17 B): as the window
// narrows, the preview pane and then the sidebar give way so the file
// list keeps fileMinW. Only what is drawn moves; the widths the user set
// (navWidth, previewW) and previewOpen stay as they are.

const (
	fileMinW       float32 = 360 // the file list's least width
	previewAutoW   float32 = 900 // below this window width the preview hides
	navNarrowMin   float32 = 140 // the sidebar's least width when squeezed
	previewNarrowW float32 = 200 // the preview's least width when squeezed
)

// paneLayout is this frame's drawn widths.
type paneLayout struct {
	navW, prevW float32
	prevShown   bool
}

// fitPanes returns the widths to draw for window width w: the preview hides
// under previewAutoW; then, while the file list has less than fileMinW, the
// preview and the sidebar shrink in proportion to the room each can give,
// down to their least widths.
func fitPanes(w, nav, prev, assist float32, prevOpen bool) paneLayout {
	l := paneLayout{navW: nav, prevW: prev, prevShown: prevOpen && w >= previewAutoW}
	if !l.prevShown {
		l.prevW = 0
	}
	if w <= 0 {
		return l
	}
	deficit := fileMinW - (w - l.navW - l.prevW - assist)
	if deficit <= 0 {
		return l
	}
	navRoom := max(0, l.navW-navNarrowMin)
	prevRoom := float32(0)
	if l.prevShown {
		prevRoom = max(0, l.prevW-previewNarrowW)
	}
	room := navRoom + prevRoom
	if room <= 0 {
		return l
	}
	take := min(deficit, room)
	l.navW -= take * navRoom / room
	l.prevW -= take * prevRoom / room
	return l
}
