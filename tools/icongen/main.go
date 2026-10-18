// Command icongen regenerates resources/icon.png, the app icon: the Lucide
// folder-open glyph in white, centered on the app's accent-blue gradient in a
// rounded square with transparent corners. Run from the module root:
//
//	go run ./tools/icongen
//
// The glyph renders through ui.Tester — the same headless rasterizer the
// tests use — so it is exactly what Onevra's Lucide set draws in the app.
// The window that rasterizes fills every pixel, so the rounded corners are
// cut afterwards by an analytic mask, 4× supersampled: mygo's pngToICO and
// pngToICNS keep the alpha, and neither they nor the platforms round the
// tile on their own.
package main

import (
	"image"
	"image/png"
	"log"
	"math"
	"os"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// side is the shipped icon's edge: 1024², what tray.go embeds and the mygo
// CLI converts to .icns / .ico / desktop entry icons.
const side = 1024

// glyph is the folder-open edge inside the tile. Lucide strokes are 2/24 of
// the box, so 0.575 keeps the tray's 32 px shrink legible.
const glyph = side * 0.575

// radius is the rounded square's corner radius, the Windows-11 tile
// proportion of about 22.5%.
const radius = side * 0.225

func main() {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(side, side).Shrink(0).
			LinearGradient(ui.LinearGradient{
				From:  ui.Hex("#45A1E6"),
				To:    ui.Hex("#0057A8"),
				Angle: 180,
				Oklab: true,
			}).
			Center().
			Children(func() {
				components.Icon(c, lucide.FolderOpen).Size(glyph, glyph).
					TextColor(ui.RGB(255, 255, 255)).Label("folder-open")
			})
	}, side, side)
	src := tt.Image()

	// The scene painted the gradient edge to edge, so every pixel holds the
	// tile's color; the mask turns the square into the rounded square.
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := (y*side + x) * 4
			dst.Pix[i] = src.Pix[i]
			dst.Pix[i+1] = src.Pix[i+1]
			dst.Pix[i+2] = src.Pix[i+2]
			dst.Pix[i+3] = uint8(coverage(x, y) * 255)
		}
	}
	out, err := os.Create("resources/icon.png")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, dst); err != nil {
		log.Fatal(err)
	}
}

// coverage is the fraction of pixel x,y inside the rounded square, from 16
// subsamples of the pixel's area.
func coverage(x, y int) float64 {
	inside := 0
	for _, dx := range [4]float64{0.125, 0.375, 0.625, 0.875} {
		for _, dy := range [4]float64{0.125, 0.375, 0.625, 0.875} {
			if inRoundedSquare(float64(x)+dx, float64(y)+dy) {
				inside++
			}
		}
	}
	return float64(inside) / 16
}

// inRoundedSquare reports whether the point is inside the centered rounded
// square: within radius of the inset cross of straight edges and corner
// circle centers.
func inRoundedSquare(px, py float64) bool {
	inner := side/2 - radius
	dx := math.Max(math.Abs(px-side/2)-inner, 0)
	dy := math.Max(math.Abs(py-side/2)-inner, 0)
	return dx*dx+dy*dy <= radius*radius
}
