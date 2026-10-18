package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// samplePicture is a w×h picture with a gradient, big enough that scaling
// has real work to do.
func samplePicture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return img
}

// TestScaleToFitKeepsAspect checks the target size: the longest edge is the
// side asked for and the aspect ratio survives, at every size the app uses.
func TestScaleToFitKeepsAspect(t *testing.T) {
	src := samplePicture(1600, 1200)
	for _, side := range []int{48, 200, 512, 1024} {
		got := scaleToFit(src, side)
		b := got.Bounds()
		if max(b.Dx(), b.Dy()) != side {
			t.Fatalf("side %d: longest edge %d", side, max(b.Dx(), b.Dy()))
		}
		// 4:3 in, 4:3 out (allow a pixel of rounding).
		if diff := absInt(b.Dx()*3 - b.Dy()*4); diff > 4 {
			t.Fatalf("side %d: aspect %dx%d is not 4:3", side, b.Dx(), b.Dy())
		}
	}
}

// TestScaleToFitNeverEnlarges checks that a small picture is returned as it
// is: upscaling would cost memory and add nothing but blur.
func TestScaleToFitNeverEnlarges(t *testing.T) {
	src := samplePicture(100, 80)
	got := scaleToFit(src, 512)
	if got.Bounds() != src.Bounds() {
		t.Fatalf("100×80 scaled to %v, want unchanged", got.Bounds())
	}
}

// TestDecodeBytesScaled checks the decode path end to end for the two
// formats the app reads most, including a large JPEG that the old code
// would have kept at full size.
func TestDecodeBytesScaled(t *testing.T) {
	src := samplePicture(4000, 3000)
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, src, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	var pb bytes.Buffer
	if err := png.Encode(&pb, src); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"jpeg": jb.Bytes(), "png": pb.Bytes()} {
		img, err := decodeBytesScaled(data, 200)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b := img.Bounds()
		if max(b.Dx(), b.Dy()) != 200 {
			t.Fatalf("%s: got %dx%d, want longest edge 200", name, b.Dx(), b.Dy())
		}
	}
}

// TestScaleMatchesKernelRoughly checks the two-pass reduction did not
// wreck the picture: every output pixel stays close to the source region
// it came from. The gradient makes a gross error — flipped rows, a
// swapped channel — show up immediately.
func TestScaleMatchesKernelRoughly(t *testing.T) {
	src := samplePicture(800, 600)
	got := scaleToFit(src, 100)
	if got.Bounds().Dx() != 100 {
		t.Fatalf("width %d", got.Bounds().Dx())
	}
	// Spot-check a few points against their source pixels (each output
	// pixel averages an 8×8 block, so the gradient should land within it).
	for _, p := range []image.Point{{0, 0}, {50, 37}, {99, 74}} {
		o := color.RGBAModel.Convert(got.At(p.X, p.Y)).(color.RGBA)
		s := color.RGBAModel.Convert(src.At(p.X*8, p.Y*8)).(color.RGBA)
		if absInt(int(o.R)-int(s.R)) > 16 || absInt(int(o.B)-int(s.B)) > 16 {
			t.Fatalf("pixel %v = %v, source %v", p, o, s)
		}
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
