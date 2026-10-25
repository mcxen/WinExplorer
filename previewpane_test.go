package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// previewpane_test.go drives the preview pane's resize divider: where it takes
// a drag, and that the drag moves the pane by what it moved.

// previewApp boots the window with the preview pane open over one file, and
// returns the app, the tester and the pane's left edge in window DIPs.
func previewApp(t *testing.T) (*App, *ui.Tester, float32) {
	t.Helper()
	a, tt, _ := keynavWindow(t, 3)
	// Divider hit geometry is measured in window DIPs against the classic
	// flush-right pane; pin classic so v2's content-card margin does not
	// shift the edge under the asserted coordinates.
	a.applySettings(func(x *appSettings) { x.VisualStyle = "" })
	a.previewOpen = true
	// The bare test app starts with no width; the real window starts at 320
	// (main.go's newAppWithStore).
	a.previewW = 320
	tt.Frame()
	// The tester's window, as the frame shows it: 1200×800 DIPs.
	w := float32(tt.Image().Bounds().Dx())
	if a.previewW < previewMinW || a.previewW > previewMaxW {
		t.Fatalf("preview width %v is outside %v–%v", a.previewW, previewMinW, previewMaxW)
	}
	return a, tt, w - a.previewW
}

// presentForDrag drags the pointer from (x, y) by dx, in steps, and releases.
func dragBy(tt *ui.Tester, x, y, dx float32) {
	tt.Press(x, y)
	tt.Move(x+dx/2, y)
	tt.Move(x+dx, y)
	tt.Release(x+dx, y)
}

// TestPreviewDividerTakesTheDragAtItsEdge: the divider — the strip centred on
// the pane's left edge — resizes the pane by exactly what it was dragged; the
// pane's own middle is the pane's, and resizing there would steal the preview's
// clicks and scrolling.
func TestPreviewDividerTakesTheDragAtItsEdge(t *testing.T) {
	a, tt, edge := previewApp(t)
	start := a.previewW
	// The middle of the pane does not resize it.
	dragBy(tt, edge+a.previewW/2, 400, 40)
	if a.previewW != start {
		t.Fatalf("a drag inside the pane resized it: %v → %v", start, a.previewW)
	}
	// The divider does: dragging right by 40 narrows the pane by 40.
	dragBy(tt, edge, 400, 40)
	if got := a.previewW; got != start-40 {
		t.Fatalf("dragging the divider right by 40 left %v, want %v", got, start-40)
	}
	// And dragging left widens it again.
	dragBy(tt, edge+40, 400, -40)
	if got := a.previewW; got != start {
		t.Fatalf("dragging back left left %v, want %v", got, start)
	}
	// The clamp still holds at the pane's own limits.
	dragBy(tt, edge, 400, 1000)
	if a.previewW != previewMinW {
		t.Fatalf("dragging past the limit left %v, want %v", a.previewW, previewMinW)
	}
}
