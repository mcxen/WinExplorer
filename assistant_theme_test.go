package main

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// assistant_theme_test.go pins the assistant panel's two colored spots to
// theme tokens: the failed-run line's danger red and the drop hint's text on
// the accent pill. Both were hardcoded, which painted the same color in dark
// mode and washed the hint out.

func withDark(t *testing.T, dark bool) *bool {
	t.Helper()
	old := testDark
	testDark = &dark
	resolveTokens()
	t.Cleanup(func() {
		testDark = old
		resolveTokens()
	})
	return testDark
}

// dominantRed returns the most common clearly red pixel in img. The error
// line's text and icon are its only red strokes.
func dominantRed(img image.Image) (r, g, b uint8) {
	counts := map[[3]uint8]int{}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pr, pg, pb, pa := img.At(x, y).RGBA()
			if pa>>8 < 200 {
				continue
			}
			cr, cg, cb := uint8(pr>>8), uint8(pg>>8), uint8(pb>>8)
			if int(cr) <= int(cg)+25 || int(cr) <= int(cb)+25 || cr < 120 {
				continue
			}
			counts[[3]uint8{cr, cg, cb}]++
		}
	}
	best, n := [3]uint8{}, 0
	for c, v := range counts {
		if v > n {
			best, n = c, v
		}
	}
	return best[0], best[1], best[2]
}

// TestAssistErrorLineFollowsTheme: the failed-run line takes the theme's
// danger color, so its red is lighter in dark mode. A hardcoded red would
// render the same in both appearances.
func TestAssistErrorLineFollowsTheme(t *testing.T) {
	render := func(dark bool) image.Image {
		withDark(t, dark)
		a := &App{}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Children(func() {
				a.assistErrorLine(c, 0, "502 Bad Gateway\n上游返回了错误页", time.Now())
			})
		}, 420, 120)
		tt.Frame()
		return tt.Image()
	}
	lr, _, _ := dominantRed(render(false))
	dr, _, _ := dominantRed(render(true))
	if lr == 0 || dr == 0 {
		t.Fatalf("no red error pixels: light=%d dark=%d", lr, dr)
	}
	if dr <= lr {
		t.Fatalf("dark error red %d is not lighter than light %d", dr, lr)
	}
}

// TestAssistDropHintKeepsContrast: the hint's pill sits on the accent color,
// so its text is white in light mode and near-black in dark mode. Both stay
// legible; a hardcoded white would leave the dark pill's text washed out.
func TestAssistDropHintKeepsContrast(t *testing.T) {
	shot := func(dark bool) (image.Image, ui.Rect) {
		withDark(t, dark)
		a := &App{}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Column(c).FillWidth().Padding(8).Children(func() {
				a.assistantDropHint(c)
			})
		}, 420, 60)
		tt.Frame()
		r, ok := tt.Find("松开以附加到对话")
		if !ok {
			t.Fatal("drop hint text not drawn")
		}
		return tt.Image(), r
	}
	extremes := func(img image.Image, r ui.Rect) (minL, maxL int) {
		minL, maxL = 999, -1
		for y := int(r.Y) - 2; y < int(r.Y+r.H)+2; y++ {
			for x := int(r.X) - 2; x < int(r.X+r.W)+2; x++ {
				pr, pg, pb, pa := img.At(x, y).RGBA()
				if pa>>8 < 128 {
					continue
				}
				l := (int(pr>>8)*299 + int(pg>>8)*587 + int(pb>>8)*114) / 1000
				if l < minL {
					minL = l
				}
				if l > maxL {
					maxL = l
				}
			}
		}
		return minL, maxL
	}
	darkImg, darkRect := shot(true)
	if darkMin, _ := extremes(darkImg, darkRect); darkMin > 80 {
		t.Fatalf("dark drop hint text is too light on the accent pill: min luminance %d", darkMin)
	}
	lightImg, lightRect := shot(false)
	if _, lightMax := extremes(lightImg, lightRect); lightMax < 220 {
		t.Fatalf("light drop hint text is too dark on the accent pill: max luminance %d", lightMax)
	}
}
