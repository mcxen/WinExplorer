package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestQuickLookVolSave: in the quick preview of a picture ↑ saves into the
// 快存 folder with a 已保存 toast, a second ↑ only says 已保存过, and ↓
// takes the copy back (已取消). SUNVALE_SHOT_DIR keeps screenshots.
func TestQuickLookVolSave(t *testing.T) {
	volSaveResetForTest(t)
	dir, out := t.TempDir(), t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 160, 255})
		}
	}
	f, _ := os.Create(filepath.Join(dir, "pic.png"))
	_ = png.Encode(f, img)
	f.Close()
	if err := setVolSaveTarget(out); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, dir)
	tt := ui.NewTester(a.view, 1000, 700)
	tt.Frame()
	a.selectOnly(filepath.Join(dir, "pic.png"))
	a.openQuickLook()
	for i := 0; i < 100; i++ {
		tt.Frame()
		if st := a.preview.Load(); st != nil && st.kind == previewImage {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	shot := func(name string) {
		dirS := os.Getenv("SUNVALE_SHOT_DIR")
		if dirS == "" {
			return
		}
		for range 8 {
			tt.Advance(60 * time.Millisecond)
			tt.Frame()
		}
		f, _ := os.Create(filepath.Join(dirS, name))
		defer f.Close()
		_ = png.Encode(f, tt.Image())
	}
	step := func(k ui.Key, want string, files int) {
		t.Helper()
		tt.Key(0, k)
		tt.Frame()
		tt.Frame()
		if !tt.HasText(want) {
			t.Fatalf("no %q toast: %v", want, tt.Texts())
		}
		if a.statusMsg != want {
			t.Fatalf("toast = %q, want %q", a.statusMsg, want)
		}
		if n, _ := os.ReadDir(out); len(n) != files {
			t.Fatalf("%s: %d files in target, want %d", want, len(n), files)
		}
	}
	step(ui.KeyUp, "已保存", 1)
	shot("10-app-saved.png")
	step(ui.KeyUp, "已保存过", 1)
	shot("11-app-dup.png")
	step(ui.KeyDown, "已取消", 0)
	shot("12-app-cancel.png")
}
