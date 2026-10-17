package main

import (
	"github.com/egoist/mygo/ui"
)

// marqueeRect is the band's box in window DIPs, the drag's corners sorted
// so a leftward or upward drag still reads as a rectangle.
func (m marqueeState) rect() ui.Rect {
	x0, x1 := min(m.x0, m.x1), max(m.x0, m.x1)
	y0, y1 := min(m.y0, m.y1), max(m.y0, m.y1)
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// bandStart is how far the pointer leaves the point it went down at before
// the gesture is a band rather than a click — the toolkit's own dragStart,
// the distance that makes a press a drag. Element.Dragged reports the press
// as a drag from its first frame, by zero, so without this a click on the
// empty space would draw no box yet still run the band's empty selection
// over the pane and wipe what was selected.
const bandStart float32 = 4

// moved reports whether the pointer has left the press's point by bandStart
// in either axis: under that the press is still a click on the empty space,
// which selects nothing.
func (m marqueeState) moved() bool {
	dx, dy := m.x1-m.x0, m.y1-m.y0
	return max(dx, -dx) >= bandStart || max(dy, -dy) >= bandStart
}

// rectOverlaps reports whether a and b share any area, the test the band
// runs on each item's box: a touch selects, as Finder's band does.
func rectOverlaps(a, b ui.Rect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X && a.Y < b.Y+b.H && a.Y+a.H > b.Y
}

// bindMarquee turns a drag on the pane's empty space into a rubber band
// selecting the rows and tiles it crosses. The pane's background is the
// only element getting the press when no row or tile took it, so a drag
// that begins here is a band by definition; the itemBounds hit-test is the
// guard for the frame a row's bounds were stale. Modifiers follow
// Explorer's clicks: none replaces, Shift adds, Ctrl toggles.
//
// The pane and every view in it (list, grid, scroll) each bind this, but a
// press lands on one element alone: the engine's pressed target. Only that
// element's Pressed, Dragged and DragEnded report true, so the rest return
// before touching the gesture's state and never disturb it.
//
// A press the pointer never leaves is a click on the empty space, not a
// band: nothing is drawn and nothing is selected (bandStart, moved).
func (a *App) bindMarquee(c *ui.Context, el ui.Element) {
	t := a.cur()
	b := el.Bounds()
	// The press's origin, kept in window DIPs on the frame it went down. A
	// press on a row or tile is a drag-and-drop of it, not a band: the
	// itemBounds the frame before recorded say which the point is on.
	if el.Pressed() && !t.marquee.on {
		if lx, ly, ok := el.PointerPosition(); ok {
			wx, wy := b.X+lx, b.Y+ly
			if !a.pointOnItem(wx, wy) {
				t.marquee.on = true
				t.marquee.x0, t.marquee.y0 = wx, wy
				t.marquee.x1, t.marquee.y1 = wx, wy
				t.marquee.mods = c.Modifiers()
				t.marquee.base = copySelection(t.selected)
			}
		}
	}
	if !t.marquee.on {
		return
	}
	// From here the gesture belongs to one element, the press's target. An
	// element that is not it reads no Pressed, Dragged, DragEnded or Clicked
	// this frame, so it runs through without touching the band.
	if _, _, ok := el.Dragged(); ok {
		// PointerPosition's coordinates are valid even off the element; only
		// its over flag says the pointer left, which a band does not care
		// about — it follows the pointer wherever it goes.
		lx, ly, _ := el.PointerPosition()
		t.marquee.x1, t.marquee.y1 = b.X+lx, b.Y+ly
		// A press the pointer has not left yet is still a click, and its
		// band would be a point: no selection change until it is one.
		if !t.marquee.moved() {
			return
		}
		a.applyMarquee()
		return
	}
	// The drag let go: the selection it built stands, and the gesture ends.
	// A press released under the drag threshold is no drag but a click, and
	// Clicked ends the band the same way — a click on the empty space
	// leaves the selection as it was.
	if _, _, ok := el.DragEnded(); ok || el.Clicked() {
		t.marquee.on = false
		t.marquee.base = nil
	}
}

// pointOnItem reports whether the window point sits on a row or tile, from
// the boxes the frame before recorded: a drag starting on one is a
// drag-and-drop of it, not a band.
func (a *App) pointOnItem(x, y float32) bool {
	for _, r := range a.cur().itemBounds {
		if r.Contains(x, y) {
			return true
		}
	}
	return false
}

// applyMarquee rebuilds the selection from the band's current box: every
// item whose recorded window box touches it, folded into the base the drag
// started from according to its modifiers.
func (a *App) applyMarquee() {
	t := a.cur()
	band := t.marquee.rect()
	hit := map[string]bool{}
	for path, r := range t.itemBounds {
		if rectOverlaps(band, r) {
			hit[path] = true
		}
	}
	next := map[string]bool{}
	switch {
	case t.marquee.mods&ui.Ctrl != 0 || t.marquee.mods&ui.Cmd != 0:
		// Ctrl toggles: keep the base, flip the items the band touches.
		for path := range t.marquee.base {
			next[path] = true
		}
		for path := range hit {
			if next[path] {
				delete(next, path)
			} else {
				next[path] = true
			}
		}
	case t.marquee.mods&ui.Shift != 0:
		// Shift adds the band's items to what was already selected.
		for path := range t.marquee.base {
			next[path] = true
		}
		for path := range hit {
			next[path] = true
		}
	default:
		next = hit
	}
	// Skip the preview refresh while the box crosses nothing new: the
	// selection only changes when an item enters or leaves the band.
	if selectionEqual(t.selected, next) {
		return
	}
	t.selected = next
	// Anchor a later Shift range at the band's first hit, as a click would.
	// The replacing band owns the anchor outright; the adding ones keep the
	// anchor the base selection had, or take the first hit if it had none.
	first := ""
	for _, e := range a.visibleEntries() {
		if next[e.Path] {
			first = e.Path
			break
		}
	}
	if t.marquee.mods == 0 || t.anchor == "" || !next[t.anchor] {
		t.anchor, t.cursor = first, first
	}
	a.requestPreview()
}

// selectionEqual reports whether two selection sets hold the same paths.
func selectionEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for path := range a {
		if !b[path] {
			return false
		}
	}
	return true
}

// copySelection duplicates a selection set, so the band's base survives the
// drag's rewrites of the live one.
func copySelection(sel map[string]bool) map[string]bool {
	out := make(map[string]bool, len(sel))
	for path := range sel {
		out[path] = true
	}
	return out
}

// marqueeOverlay paints the band over the pane: an accent-tinted fill with
// the dashed frame a marquee classically wears. The box is PassThrough, so
// it never takes a pointer event from the rows under it.
func (a *App) marqueeOverlay(c *ui.Context) {
	t := a.cur()
	// The box shows exactly while the band does: a press the pointer has not
	// left is a click, with nothing drawn and nothing selected.
	if !t.marquee.on || !t.marquee.moved() {
		return
	}
	band := t.marquee.rect()
	// The painter draws in the window's coordinates, the same ones the band
	// is kept in — so the band goes down as it is. It is not mapped through
	// WindowToLocal: that turns a window point into one of the box's own, an
	// offset the box already has, and would move the band up and left by the
	// pane's place in the window.
	overlay := ui.Box(c).Absolute().Left(0).Top(0).Fill().PassThrough()
	overlay.DrawOver(func(p *ui.Painter, r ui.Rect) {
		// The band's corners may be dragged past the pane; the pane's box
		// clips it, so it stops at the edge rather than running over the
		// sidebar and the chrome.
		p.Clip(r, 0, func() {
			p.Fill(band, colorAccent().Alpha(0.12), 0)
			p.StrokeDashed(band, colorAccent().Alpha(0.9), 0, 1)
		})
	})
}
