package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// marqueeWindow boots the window over a folder of n files with the sidebar
// at its minimum, as a real window has one: the pane then starts right of
// the sidebar and below the chrome — the offset a band drawn from the
// pane's own box would carry.
func marqueeWindow(t *testing.T, n int) (*App, *ui.Tester, string) {
	t.Helper()
	a, tt, dir := keynavWindow(t, n)
	a.navWidth = navMinWidth
	tt.Frame()
	return a, tt, dir
}

// marqueeDrag drags the empty space from (x0, y0) to (x1, y1) and returns
// the band's box in window DIPs.
func marqueeDrag(t *testing.T, tt *ui.Tester, a *App, x0, y0, x1, y1 float32) ui.Rect {
	t.Helper()
	tt.Press(x0, y0)
	tt.Move(x1, y1)
	band := a.cur().marquee.rect()
	tt.Release(x1, y1)
	return band
}

// marqueeIs asserts the pane holds exactly these names.
func marqueeIs(t *testing.T, a *App, what string, want ...string) {
	t.Helper()
	got := navNames(t, a)
	if len(got) != len(want) {
		t.Fatalf("%s selected %v, want %v", what, got, want)
	}
	held := map[string]bool{}
	for _, n := range got {
		held[n] = true
	}
	for _, n := range want {
		if !held[n] {
			t.Fatalf("%s selected %v, want %v", what, got, want)
		}
	}
}

// crossedNames lists the rows whose recorded window boxes touch the band,
// the hit test the band itself runs.
func crossedNames(a *App, band ui.Rect) []string {
	var out []string
	for _, e := range a.visibleEntries() {
		if r, ok := a.cur().itemBounds[e.Path]; ok && rectOverlaps(band, r) {
			out = append(out, e.Name)
		}
	}
	return out
}

// TestMarqueeSelectsWhatItCrosses drags a band over the details view with
// the real pointer and checks the selection against the rows' boxes: a
// plain band replaces, an empty click is no band, and a drag from a row is
// a drag of it. The modifiers are read at the press, which the tester's
// pointer cannot carry, so they are covered by driving applyMarquee below.
func TestMarqueeSelectsWhatItCrosses(t *testing.T) {
	a, tt, dir := marqueeWindow(t, 6)
	row, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatalf("no f000.txt in the pane; texts = %v", tt.Texts())
	}
	// The band starts on the pane's empty space below the last row and
	// drags up into a row's middle, so which rows it crosses is not a
	// question of a pixel either way.
	empty := row.Y + 6*row.H + 40
	mid := func(i float32) float32 { return row.Y + i*row.H + row.H/2 }
	x0, x1 := row.X+500, row.X+10

	band := marqueeDrag(t, tt, a, x0, empty, x1, mid(1))
	if crossed := crossedNames(a, band); len(crossed) != 5 {
		t.Fatalf("the band %v crosses %v, want the five rows above the empty space", band, crossed)
	}
	marqueeIs(t, a, "the band", "f001.txt", "f002.txt", "f003.txt", "f004.txt", "f005.txt")
	if a.cur().marquee.on {
		t.Fatal("the band is still on after the release")
	}
	if a.cur().marquee.base != nil {
		t.Fatal("the band kept its base after the release")
	}

	// A press released where it went down is no drag: the selection stands
	// and no band is left behind.
	tt.ClickAt(x0, empty)
	marqueeIs(t, a, "an empty click", "f001.txt", "f002.txt", "f003.txt", "f004.txt", "f005.txt")
	if a.cur().marquee.on {
		t.Fatal("an empty click left the band on")
	}

	// A drag that begins on a row is a drag of it, never a band.
	tt.Press(row.X+40, mid(0))
	tt.Move(row.X+400, row.Y+400)
	if a.cur().marquee.on {
		t.Fatal("a drag that began on a row started a band")
	}
	tt.Release(row.X+400, row.Y+400)

	// Selecting one row after a band takes: a stale base would show here.
	a.selectOnly(dir + "/f000.txt")
	marqueeIs(t, a, "a click after bands", "f000.txt")
}

// TestApplyMarqueeComposesWithModifiers drives the band's selection math as
// the gesture leaves it: the box crosses rows 1 to 3, and the selection it
// started from holds rows 0 and 1 — one of them inside the box, so a toggle
// shows.
func TestApplyMarqueeComposesWithModifiers(t *testing.T) {
	a, tt, _ := marqueeWindow(t, 6)
	row, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatalf("no f000.txt in the pane; texts = %v", tt.Texts())
	}
	entries := a.visibleEntries()
	if len(entries) != 6 {
		t.Fatalf("the pane lists %d rows, want 6", len(entries))
	}
	base := map[string]bool{entries[0].Path: true, entries[1].Path: true}
	cases := []struct {
		name string
		mods ui.Modifiers
		want []string
	}{
		{"no modifier replaces", 0, []string{"f001.txt", "f002.txt", "f003.txt"}},
		{"Shift adds", ui.Shift, []string{"f000.txt", "f001.txt", "f002.txt", "f003.txt"}},
		{"Ctrl toggles", ui.Ctrl, []string{"f000.txt", "f002.txt", "f003.txt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t2 := a.cur()
			t2.selected = copySelection(base)
			// The band's corners: the press inside row 3, the pointer inside
			// row 1, as a drag up from below the rows leaves them.
			t2.marquee = marqueeState{
				on: true,
				x0: row.X + 40, y0: row.Y + row.H/2 + 3*row.H,
				x1: row.X + 300, y1: row.Y + row.H/2 + row.H,
				mods: tc.mods,
				base: copySelection(base),
			}
			a.applyMarquee()
			marqueeIs(t, a, tc.name, tc.want...)
		})
	}
}

// accentFrame is the box of the band's dashed frame as painted: the accent
// strokes of the last frame within area. A dash is solid accent, which over
// the content reads as the blue below, while the 12% fill stays near white.
// area leaves out the chrome, where an icon of its own may wear the accent.
func accentFrame(tt *ui.Tester, area ui.Rect) (ui.Rect, int) {
	img := tt.Image()
	b := img.Bounds()
	minX, minY, maxX, maxY, count := 1<<30, 1<<30, -1, -1, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			fx, fy := float32(x), float32(y)
			if fx < area.X || fx >= area.X+area.W || fy < area.Y || fy >= area.Y+area.H {
				continue
			}
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r8, g8, b8 := cr>>8, cg>>8, cb>>8
			if r8 < 90 && g8 > 80 && g8 < 160 && b8 > 150 {
				count++
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	return ui.Rect{X: float32(minX), Y: float32(minY), W: float32(maxX - minX + 1), H: float32(maxY - minY + 1)}, count
}

// TestMarqueeBandDrawsOnItsBox is the position check: the frame painted is
// the band's own box in the window rather than one carrying the pane's
// offset, and it stays within the pane when the drag leaves the pane.
func TestMarqueeBandDrawsOnItsBox(t *testing.T) {
	a, tt, _ := marqueeWindow(t, 6)
	row, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatalf("no f000.txt in the pane; texts = %v", tt.Texts())
	}
	if row.X < navMinWidth {
		t.Fatalf("the first row is at x = %v, want the pane right of the %v sidebar", row.X, navMinWidth)
	}
	// The pane's box: a row sits rowInset inside it, below the header.
	paneLeft, paneTop := row.X-rowInset, row.Y-a.detailsRowH()
	win := tt.Image().Bounds()
	w, h := float32(win.Dx()), float32(win.Dy())
	pane := ui.Rect{X: paneLeft, Y: paneTop, W: w - paneLeft, H: h - paneTop}

	x0, y0 := row.X+500, row.Y+6*row.H+40
	x1, y1 := row.X+10, row.Y+row.H

	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	band := a.cur().marquee.rect()
	frame, count := accentFrame(tt, pane)
	if count == 0 {
		t.Fatalf("no frame painted for the band %v", band)
	}
	if !rectOverlaps(frame, band) {
		t.Fatalf("the painted frame %v misses the band %v", frame, band)
	}
	// Each edge is the band's, within the pixel a stroke rounds to.
	edges := []struct {
		name      string
		got, want float32
	}{
		{"left", frame.X, band.X},
		{"top", frame.Y, band.Y},
		{"right", frame.X + frame.W, band.X + band.W},
		{"bottom", frame.Y + frame.H, band.Y + band.H},
	}
	for _, e := range edges {
		if math.Abs(float64(e.got-e.want)) > 2 {
			t.Fatalf("the frame's %s edge is at %v, want %v (frame %v, band %v)", e.name, e.got, e.want, frame, band)
		}
	}

	// Dragged past the pane's top-left corner, the band covers the pane's
	// box and paints nothing above it: the chrome there is its own.
	tt.Move(row.X-200, row.Y-100)
	band = a.cur().marquee.rect()
	if band.X >= row.X || band.Y >= row.Y {
		t.Fatalf("the band %v did not leave the pane (the first row is at %v)", band, row)
	}
	chrome := ui.Rect{X: paneLeft, W: w - paneLeft, H: paneTop}
	if _, n := accentFrame(tt, chrome); n != 0 {
		t.Fatalf("the band painted %d pixels above the pane, which starts at y = %v", n, paneTop)
	}
	if frame, _ := accentFrame(tt, pane); frame.X < paneLeft-2 || frame.Y < paneTop-2 {
		t.Fatalf("the frame %v ran outside the pane, which starts at (%v, %v)", frame, paneLeft, paneTop)
	}
	tt.Release(row.X-200, row.Y-100)
}

// TestMarqueeBandDrawsOnItsBoxInTheIconView is the same position check over
// the icon view, whose tiles the report's window showed: the frame there is
// the band's box too, and the tiles it holds are the tiles it crossed.
func TestMarqueeBandDrawsOnItsBoxInTheIconView(t *testing.T) {
	a, tt, _ := marqueeWindow(t, 12)
	a.mode = ViewMediumIcons
	tt.Frame()
	tile, ok := tt.Find("f000.txt")
	if !ok {
		t.Fatalf("no f000.txt tile; texts = %v", tt.Texts())
	}
	last, ok := tt.Find("f011.txt")
	if !ok {
		t.Fatalf("no f011.txt tile; texts = %v", tt.Texts())
	}
	if tile.X < navMinWidth {
		t.Fatalf("the first tile is at x = %v, want the pane right of the %v sidebar", tile.X, navMinWidth)
	}
	// From the empty space below the last tile up into the first row's
	// middle, so the band crosses tiles rather than just touching them.
	x0, y0 := tile.X+400, last.Y+last.H+40
	x1, y1 := tile.X+5, tile.Y+tile.H/2
	tt.Press(x0, y0)
	tt.Move((x0+x1)/2, (y0+y1)/2)
	tt.Move(x1, y1)
	band := a.cur().marquee.rect()

	// The pane lies right of the sidebar, where nothing else wears the
	// accent the frame is drawn in.
	win := tt.Image().Bounds()
	frame, count := accentFrame(tt, ui.Rect{X: navMinWidth, W: float32(win.Dx()) - navMinWidth, H: float32(win.Dy())})
	if count == 0 {
		t.Fatalf("no frame painted for the band %v", band)
	}
	tt.Release(x1, y1)

	crossed := crossedNames(a, band)
	if len(crossed) == 0 {
		t.Fatalf("the band %v crossed no tile", band)
	}
	marqueeIs(t, a, "the band", crossed...)

	edges := []struct {
		name      string
		got, want float32
	}{
		{"left", frame.X, band.X},
		{"top", frame.Y, band.Y},
		{"right", frame.X + frame.W, band.X + band.W},
		{"bottom", frame.Y + frame.H, band.Y + band.H},
	}
	for _, e := range edges {
		if math.Abs(float64(e.got-e.want)) > 2 {
			t.Fatalf("the frame's %s edge is at %v, want %v (frame %v, band %v)", e.name, e.got, e.want, frame, band)
		}
	}
}
