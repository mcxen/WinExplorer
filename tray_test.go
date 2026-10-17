package main

import (
	"bytes"
	"image/png"
	"testing"
)

// The tray itself cannot be made here: NewTray panics before the app is
// ready, and these tests never run App.Run. So they cover the icon alone —
// what ships must decode as a PNG, at the size the tray is given.

func TestTrayIconPNG(t *testing.T) {
	icon, err := trayIconPNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		t.Fatalf("tray icon does not decode as PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("tray icon is %dx%d, want 32x32", b.Dx(), b.Dy())
	}
}
