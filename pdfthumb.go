package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// pdfthumb.go turns a PDF's first page into a picture for the icon tiles
// and the preview pane. macOS uses Quick Look; Linux (and anywhere
// poppler-utils is installed) uses pdftoppm. When neither is available the
// caller keeps the hand-drawn PDF glyph — the list must never block on a
// missing tool.

// pdfThumbTimeout caps how long a single PDF render may run so a hung
// pdftoppm cannot pin a thumb worker forever while the list scrolls.
const pdfThumbTimeout = 3 * time.Second

// pdfPosterImage renders the first page of path to a picture fitting side
// px on its longest edge. It tries the system thumbnail first (Quick Look
// on macOS), then pdftoppm, and returns an error when neither works.
func pdfPosterImage(path string, side int) (image.Image, error) {
	side = max(side, 16)
	if img, err := quickLookImage(path, side*2); err == nil && img != nil {
		return scaleToFit(img, side), nil
	}
	if img, err := pdftoppmImage(path, side); err == nil && img != nil {
		return img, nil
	}
	return nil, fmt.Errorf("PDF 缩略图不可用")
}

// pdftoppmImage shells out to poppler's pdftoppm when it is on PATH. The
// command is bounded by pdfThumbTimeout; a missing binary is a soft miss.
func pdftoppmImage(path string, side int) (image.Image, error) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "we-pdf")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	outPrefix := filepath.Join(dir, "page")
	ctx, cancel := context.WithTimeout(context.Background(), pdfThumbTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftoppm",
		"-png", "-f", "1", "-l", "1", "-singlefile",
		"-scale-to", fmt.Sprint(side),
		path, outPrefix,
	)
	if b, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = err.Error()
		} else if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("pdftoppm: %s", msg)
	}
	f, err := os.Open(outPrefix + ".png")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return scaleToFit(img, side), nil
}
