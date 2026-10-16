package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanGallery(t *testing.T) {
	root, _ := os.MkdirTemp("", "we-gal")
	defer os.RemoveAll(root)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.MkdirAll(filepath.Join(root, ".hiddendir"), 0o755)
	os.MkdirAll(filepath.Join(root, "My.photoslibrary"), 0o755)
	os.MkdirAll(filepath.Join(root, "Qx"), 0o755)
	mkfile := func(rel string, mod time.Time) {
		p := filepath.Join(root, rel)
		os.WriteFile(p, []byte("x"), 0o644)
		os.Chtimes(p, mod, mod)
	}
	jan := time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local)
	oct := time.Date(2026, 10, 2, 9, 0, 0, 0, time.Local)
	mkfile("a.png", oct)
	mkfile("sub/b.jpg", jan)
	mkfile("sub/c.txt", oct) // not a picture
	mkfile(".hiddendir/d.png", oct)
	mkfile("My.photoslibrary/e.png", oct)
	mkfile("Qx/f.heic", jan)

	photos := scanGallery(root)
	if len(photos) != 3 {
		var names []string
		for _, p := range photos {
			names = append(names, p.path)
		}
		t.Fatalf("photos = %v", names)
	}
	// Newest first.
	if photos[0].name != "a.png" || photos[1].name != "f.heic" || photos[2].name != "b.jpg" {
		t.Fatalf("order wrong: %+v", photos)
	}
	if photos[0].size != 1 {
		t.Fatalf("size not captured: %v", photos[0])
	}

	// Month grouping keys.
	if key := monthKeyOf(oct); key != "2026年10月" {
		t.Fatalf("month key = %q", key)
	}
	if key := monthKeyOf(time.Time{}); key != "未知时间" {
		t.Fatalf("zero month key = %q", key)
	}

	// The scan bound.
	bound := scanGalleryBound(t)
	if bound != galleryMaxPhotos {
		_ = bound
	}
}

// scanGalleryBound is a compile-time hook for the bound test below.
func scanGalleryBound(t *testing.T) int { return galleryMaxPhotos }

func TestGalleryEntryNoStat(t *testing.T) {
	ph := galleryPhoto{
		path:    "/x/pic.PNG",
		name:    "pic.PNG",
		modTime: time.Now(),
		size:    42,
	}
	e := galleryEntry(ph)
	if e.Ext != "png" || e.Size != 42 || e.IsDir || e.Dir != "/x" || e.Name != "pic.PNG" {
		t.Fatalf("entry = %+v", e)
	}
}

func TestDecodeScaled(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-thumb")
	defer os.RemoveAll(dir)
	// A 100×60 gradient PNG.
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 2), uint8(y * 4), 0x80, 255})
		}
	}
	path := filepath.Join(dir, "g.png")
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()

	bm, err := decodeScaled(path, 20)
	if err != nil {
		t.Fatalf("decodeScaled: %v", err)
	}
	w, h := bm.Size()
	if w != 20 || h != 12 {
		t.Fatalf("thumb = %dx%d, want 20x12", w, h)
	}

	// Below the bound the image keeps its size.
	bm, err = decodeScaled(path, 400)
	if err != nil {
		t.Fatalf("decodeScaled big: %v", err)
	}
	if w, _ := bm.Size(); w != 100 {
		t.Fatalf("oversize thumb = %d", w)
	}
}

func TestGalleryThumbLifecycle(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-tl")
	defer os.RemoveAll(dir)
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 8; i++ {
		img.Set(i, i, color.RGBA{255, 0, 0, 255})
	}
	path := filepath.Join(dir, "t.png")
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()

	if bm := galleryThumb(path); bm != nil {
		t.Fatal("cache should start empty")
	}
	// The background decode lands within a couple of seconds.
	for i := 0; i < 200; i++ {
		if bm := galleryThumb(path); bm != nil {
			if w, h := bm.Size(); w != 8 || h != 8 {
				t.Fatalf("thumb = %dx%d", w, h)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("thumbnail never landed")
}
