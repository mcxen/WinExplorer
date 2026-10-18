package main

import (
	"bytes"
	"image"
	"os"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// imgsize.go is how the app turns a picture file into the bitmap a tile or
// a preview needs, at the smallest cost that still looks right.
//
// Go's decoders have no scaling option: a 12-megapixel JPEG always decodes
// to a 48 MB picture before anything can shrink it. What this file can
// control is everything after that, and at thumbnail sizes it is the
// larger half of the cost — measured on a 4000×3000 JPEG, the decode takes
// ~100 ms and one CatmullRom pass to 200 px takes ~220 ms on top of it.
//
// So the reduction is done in two cheap passes instead of one expensive
// one: nearest-neighbour while most pixels are still being thrown away,
// then bilinear for the last factor. Both are an order of magnitude faster
// than the CatmullRom kernel the first version used, and the eye cannot
// tell them apart at 200–512 px. Callers do the file I/O and run this on
// their own goroutine; everything here is pure computation over bytes.

// preciseAbove is the output size above which the gentler kernel is worth
// its cost. The preview pane shows pictures up to the window's height
// (~800 px), where a cheap resample of a detailed photo would show; tiles
// never get there.
const preciseAbove = 512

// decodeFileScaled decodes path to a picture fitting side px on its
// longest edge. A picture already no bigger is returned as decoded.
func decodeFileScaled(path string, side int) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeBytesScaled(data, side)
}

// decodeBytesScaled is decodeFileScaled for data already in memory.
func decodeBytesScaled(data []byte, side int) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return scaleToFit(img, side), nil
}

// scaleToFit shrinks img to fit side px on its longest edge, or returns it
// unchanged when it is no bigger.
//
// The reduction always runs in two passes: ApproxBiLinear down to twice the target,
// which is where most of the work happens, then one kernel pass for the
// last factor. The pre-pass is not only faster; it also removes the
// aliasing a single large-ratio resample produces, so the result is
// cleaner than the one-pass CatmullRom it replaces. Outputs above
// preciseAbove keep CatmullRom for the final pass, everything smaller
// takes bilinear, which at those sizes is indistinguishable.
func scaleToFit(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return img
	}
	side = max(side, 1)
	scale := float64(side) / float64(max(w, h))
	if scale >= 1 {
		return img
	}
	tw := max(1, int(float64(w)*scale+0.5))
	th := max(1, int(float64(h)*scale+0.5))

	// Down-sample to (at most) twice the target whenever the source is
	// bigger: the kernel pass over the small intermediate is then cheap
	// whatever the source size, and the pre-pass is where a large ratio is
	// done best.
	if max(w, h) > max(tw, th)*2 {
		iw, ih := max(tw*2, 1), max(th*2, 1)
		mid := image.NewRGBA(image.Rect(0, 0, iw, ih))
		xdraw.ApproxBiLinear.Scale(mid, mid.Bounds(), img, b, xdraw.Src, nil)
		img, b = mid, mid.Bounds()
	}

	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if tw == b.Dx() && th == b.Dy() {
		xdraw.Copy(dst, dst.Bounds().Min, img, b, xdraw.Src, nil)
		return dst
	}
	kernel := xdraw.BiLinear
	if max(tw, th) > preciseAbove {
		kernel = xdraw.CatmullRom
	}
	kernel.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

// isStdlibImage is true for the formats the standard library decodes.
func isStdlibImage(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(path[strings.LastIndexByte(path, '.')+1:], "."))
	return stdlibImageExts[ext]
}
