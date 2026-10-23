package main

import (
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
