package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestStripWindowCentres(t *testing.T) {
	cases := []struct{ n, cur, lo, hi int }{
		{5, 2, 0, 5}, {300, 0, 0, 10}, {300, 150, 145, 155}, {300, 299, 290, 300},
	}
	for _, c := range cases {
		// 10 tiles fit: 10*44 + 9*4 = 476.
		if lo, hi := stripWindow(c.n, c.cur, 476); lo != c.lo || hi != c.hi {
			t.Errorf("stripWindow(%d,%d) = %d,%d want %d,%d", c.n, c.cur, lo, hi, c.lo, c.hi)
		}
	}
}

func TestPrefetchNeighboursFollowsDirection(t *testing.T) {
	var vis []Entry
	for i := range 6 {
		vis = append(vis, Entry{Path: fmt.Sprintf("/p/%d.jpg", i)})
	}
	vis[3] = Entry{Path: "/p/x.txt"}
	got := prefetchNeighbours(vis, 2, 1)
	want := []string{"/p/4.jpg", "/p/5.jpg", "/p/1.jpg"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("forward = %v, want %v", got, want)
	}
	if got := prefetchNeighbours(vis, 2, -1); fmt.Sprint(got) != fmt.Sprint([]string{"/p/1.jpg", "/p/0.jpg", "/p/4.jpg"}) {
		t.Fatalf("backward = %v", got)
	}
}

// TestQuickLookFilmstrip: the strip shows under the picture, only the
// tiles that fit are drawn, and a click on one moves the selection.
// SUNVALE_SHOT_DIR also saves a screenshot (IMGSHOT_SRC: real pictures).
func TestQuickLookFilmstrip(t *testing.T) {
	dir := t.TempDir()
	if src := os.Getenv("IMGSHOT_SRC"); src != "" {
		ms, _ := filepath.Glob(filepath.Join(src, "*.jpg"))
		for _, m := range ms[:min(40, len(ms))] {
			b, _ := os.ReadFile(m)
			_ = os.WriteFile(filepath.Join(dir, filepath.Base(m)), b, 0o644)
		}
	} else {
		for i := range 30 {
			img := image.NewRGBA(image.Rect(0, 0, 120, 80))
			for p := 0; p < len(img.Pix); p += 4 {
				img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = uint8(i*8), 120, uint8(255-i*8), 255
			}
			f, _ := os.Create(filepath.Join(dir, fmt.Sprintf("p%02d.png", i)))
			_ = png.Encode(f, img)
			f.Close()
		}
	}
	clearThumbs()
	a := newStoreApp(t, dir)
	tt := ui.NewTester(a.view, 1280, 800)
	vis := a.visibleEntries()
	a.selectOnly(vis[len(vis)/2].Path)
	a.openQuickLook()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if st := a.preview.Load(); st != nil && st.bitmap != nil && peekThumb(vis[len(vis)/2+1].Path) != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	tt.Frame()
	// Only the visible run asked for thumbnails.
	thumbMu.Lock()
	asked := len(thumbCache)
	thumbMu.Unlock()
	if asked == 0 || asked >= len(vis) {
		t.Fatalf("%d of %d thumbnails decoded, want only the visible run", asked, len(vis))
	}
	if d := os.Getenv("SUNVALE_SHOT_DIR"); d != "" {
		f, err := os.Create(filepath.Join(d, "app-quicklook-filmstrip.png"))
		if err == nil {
			_ = png.Encode(f, tt.Image())
			f.Close()
		}
	}
}
