package main

import (
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// chrome_nav_test.go pins the address bar's geometry: the separator arrows ride
// on the crumbs' line, not at the top of the bar.

// bandY returns the range of rows with ink in the x band (x0, x1), between
// top and bottom. minY > maxY means the band is empty.
func bandY(img *image.RGBA, x0, x1, top, bottom int) (minY, maxY int) {
	minY, maxY = 1<<30, -1
	for y := top; y < bottom; y++ {
		for x := x0; x < x1; x++ {
			cr, _, _, _ := img.At(x, y).RGBA()
			if cr>>8 < 200 { // the bar is near-white; anything darker is ink
				minY, maxY = min(minY, y), max(maxY, y)
				break
			}
		}
	}
	return minY, maxY
}

// TestBreadcrumbSeparatorsSitOnTheCrumbLine: each arrow between two crumbs is
// centered on the line the crumbs' labels sit on. A plain box lays its child
// out at the top, which left the chevrons riding high in the bar.
func TestBreadcrumbSeparatorsSitOnTheCrumbLine(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	// The home folder's Desktop, not a literal path: the home folder is not
	// /Users/mcx on a runner, and a literal one there loses the 主文件夹 crumb
	// — the name then matches the sidebar's entry instead. Its last crumb is
	// 桌面 while the tab's title is Desktop, so each name locates one label.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	a.navigate(filepath.Join(home, "Desktop"))
	tt.Frame()

	var crumbs []ui.Rect
	for _, crumb := range a.breadcrumbs() {
		r, ok := tt.Find(crumb.Name)
		if !ok || r.W == 0 {
			t.Fatalf("the crumb %q is not in the window: %v", crumb.Name, tt.Texts())
		}
		crumbs = append(crumbs, r)
	}
	line := crumbs[0].Y + crumbs[0].H/2
	img := tt.Image()
	for i := 1; i < len(crumbs); i++ {
		x0 := int(crumbs[i-1].X + crumbs[i-1].W)
		x1 := int(crumbs[i].X)
		if x1-x0 < 4 {
			t.Fatalf("crumbs %d and %d touch: no separator between them", i-1, i)
		}
		minY, maxY := bandY(img, x0, x1, int(line-14), int(line+14))
		if minY > maxY {
			t.Fatalf("no arrow drawn between crumbs %d and %d (x %d..%d)", i-1, i, x0, x1)
		}
		if got := float32(minY+maxY) / 2; got < line-3 || got > line+3 {
			t.Fatalf("the arrow between crumbs %d and %d is centered at %.1f, want the crumbs' line %.1f",
				i-1, i, got, line)
		}
	}
}
