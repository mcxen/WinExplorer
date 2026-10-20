package main

import (
	"image"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// searchmark_test.go checks what Everything's search looks like on screen: the
// yellow mark behind the runs a live query matched — in the pane's rows and in
// the Ctrl+K palette's list alike.

// markPixels counts the pixels of the hit color within area, which is the
// background the marked runs carry.
func markPixels(img *image.RGBA, area ui.Rect) int {
	return colorPixels(img, area, colorHit())
}

// colorPixels counts the pixels of one exact colour within area, for the
// painted states the tests pin down.
func colorPixels(img *image.RGBA, area ui.Rect, want ui.Color) int {
	bounds := img.Bounds()
	n := 0
	for y := max(int(area.Y), bounds.Min.Y); y < min(int(area.Y+area.H), bounds.Max.Y); y++ {
		for x := max(int(area.X), bounds.Min.X); x < min(int(area.X+area.W), bounds.Max.X); x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			if uint8(cr>>8) == want.R && uint8(cg>>8) == want.G && uint8(cb>>8) == want.B {
				n++
			}
		}
	}
	return n
}

// listedEntry is the pane's entry with this name, for the text its row shows.
func listedEntry(t *testing.T, a *App, name string) Entry {
	t.Helper()
	for _, e := range a.visibleEntries() {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("the pane does not list %q", name)
	return Entry{}
}

// TestSearchMarksThePaneRows: a live query in the pane's search box paints the
// hit mark behind what its rows matched.
func TestSearchMarksThePaneRows(t *testing.T) {
	a, tt, _ := keynavWindow(t, 3)
	a.cur().query = "f00"
	tt.Frame()
	waitSearch(t, a)
	tt.Frame()
	row, ok := tt.Find(listedEntry(t, a, "f000.txt").Name)
	if !ok {
		t.Fatalf("the row is not in the window: %v", tt.Texts())
	}
	if n := markPixels(tt.Image(), row); n == 0 {
		t.Fatalf("the pane's row at %v carries no hit mark", row)
	}
}

// TestSearchMarksTheTiles: the icon views label their tiles through the same
// marking, so a search shows its hits there too.
func TestSearchMarksTheTiles(t *testing.T) {
	a, tt, _ := keynavWindow(t, 3)
	a.mode = ViewMediumIcons
	a.cur().query = "f00"
	tt.Frame()
	waitSearch(t, a)
	tt.Frame()
	tile, ok := tt.Find(listedEntry(t, a, "f000.txt").Name)
	if !ok {
		t.Fatalf("the tile is not in the window: %v", tt.Texts())
	}
	if n := markPixels(tt.Image(), tile); n == 0 {
		t.Fatalf("the tile label at %v carries no hit mark", tile)
	}
}

// TestSearchMarksTheBoxRows: the Ctrl+K palette marks its hits too. The tab's
// own search is left empty, so nothing but the palette can paint a mark.
func TestSearchMarksTheBoxRows(t *testing.T) {
	a, tt, dir := keynavWindow(t, 3)
	if _, ok := tt.Find(a.displayName(Entry{Name: "f000.txt", Path: filepath.Join(dir, "f000.txt"), Ext: "txt"})); !ok {
		t.Fatalf("the pane is not showing its folder: %v", tt.Texts())
	}
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("f00")
	tt.Frame()
	waitSearchBox(t, tt, a)
	tt.Frame()
	if n := markPixels(tt.Image(), ui.Rect{X: 0, Y: 0, W: 1200, H: 800}); n == 0 {
		t.Fatal("the Ctrl+K list painted no hit marks")
	}
}
