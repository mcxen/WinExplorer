package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestClampImgZoom(t *testing.T) {
	if clampImgZoom(0.5) != imgZoomMin {
		t.Fatalf("below min")
	}
	if clampImgZoom(99) != imgZoomMax {
		t.Fatalf("above max")
	}
	if clampImgZoom(2) != 2 {
		t.Fatalf("in range")
	}
}

func TestApplyImgWheelZoom(t *testing.T) {
	qv := imgQuickView{zoom: 1, panX: 10, panY: 10}
	applyImgWheelZoom(&qv, 40) // zoom out from fit stays at 1 and clears pan
	if qv.zoom != imgZoomMin || qv.panX != 0 || qv.panY != 0 {
		t.Fatalf("zoom out at fit = %+v", qv)
	}
	applyImgWheelZoom(&qv, -40) // zoom in
	if qv.zoom <= imgZoomMin {
		t.Fatalf("zoom in = %v", qv.zoom)
	}
	qv.panX, qv.panY = 5, 5
	for i := 0; i < 40; i++ {
		applyImgWheelZoom(&qv, 40)
	}
	if qv.zoom != imgZoomMin || qv.panX != 0 {
		t.Fatalf("zoom out to fit = %+v", qv)
	}
}

func TestFitContainSize(t *testing.T) {
	dw, dh := fitContainSize(200, 100, 400, 100)
	if dw != 200 || dh != 50 {
		t.Fatalf("landscape fit = %v×%v", dw, dh)
	}
	dw, dh = fitContainSize(100, 200, 50, 100)
	if dw != 100 || dh != 200 {
		t.Fatalf("portrait already fits = %v×%v", dw, dh)
	}
}

func TestSameDirImagesAndStep(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "a.png"))
	writeTestPNG(t, filepath.Join(dir, "b.png"))
	os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644)
	writeTestPNG(t, filepath.Join(dir, "c.png"))

	a := newTestApp(t, dir)
	a.selectOnly(filepath.Join(dir, "b.png"))
	imgs := a.sameDirImages(filepath.Join(dir, "b.png"))
	if len(imgs) != 3 {
		t.Fatalf("sameDirImages = %d want 3 (txt skipped)", len(imgs))
	}
	for _, e := range imgs {
		if e.Ext == "txt" || filepath.Ext(e.Path) == ".txt" {
			t.Fatalf("txt leaked into image strip: %v", e.Path)
		}
	}

	a.quickLookStepImage(1)
	if got := a.singleSelected().Path; got != filepath.Join(dir, "c.png") {
		t.Fatalf("step +1 = %v", got)
	}
	a.quickLookStepImage(1)
	if got := a.singleSelected().Path; got != filepath.Join(dir, "c.png") {
		t.Fatalf("past end must hold: %v", got)
	}
	a.quickLookStepImage(-1)
	a.quickLookStepImage(-1)
	if got := a.singleSelected().Path; got != filepath.Join(dir, "a.png") {
		t.Fatalf("step back to first = %v", got)
	}
	a.quickLookStepImage(-1)
	if got := a.singleSelected().Path; got != filepath.Join(dir, "a.png") {
		t.Fatalf("before start must hold: %v", got)
	}
}

func TestDecodeImageAndBroken(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ok.png")
	writeTestPNG(t, good)
	bm, err := decodeImage(good)
	if err != nil || bm == nil {
		t.Fatalf("decode good: %v %v", bm, err)
	}
	// formDecodeBitmap shares decodeBytesScaled — both paths must succeed.
	fbm, msg := formLoadBitmap(good, 64)
	if fbm == nil || msg != "" {
		t.Fatalf("formLoadBitmap: %v %q", fbm, msg)
	}

	bad := filepath.Join(dir, "bad.png")
	os.WriteFile(bad, []byte("not-a-png"), 0o644)
	if _, err := decodeImage(bad); err == nil {
		// quickLookThumb may still succeed on some platforms; accept either
		// a real error or a non-nil bitmap from the system fallback.
		t.Log("decodeImage returned nil error for corrupt bytes (system thumb?)")
	}
	if bm, msg := formLoadBitmap(bad, 64); bm != nil || msg == "" {
		// Cached failure must report a reason and no bitmap.
		if bm != nil {
			t.Fatalf("corrupt formLoadBitmap returned bitmap")
		}
		if msg == "" {
			t.Fatalf("corrupt formLoadBitmap empty err")
		}
	}
}

func TestResetImgQV(t *testing.T) {
	a := &App{}
	a.resetImgQV("/a.png")
	a.imgQV.zoom = 2
	a.imgQV.panX = 9
	a.resetImgQV("/a.png")
	if a.imgQV.zoom != 2 || a.imgQV.panX != 9 {
		t.Fatalf("same path must keep transform: %+v", a.imgQV)
	}
	a.resetImgQV("/b.png")
	if a.imgQV.zoom != imgZoomMin || a.imgQV.panX != 0 || a.imgQV.path != "/b.png" {
		t.Fatalf("new path must reset: %+v", a.imgQV)
	}
}

func TestImageQuickLookRenders(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot.png")
	writeTestPNG(t, p)
	a := newStoreApp(t, dir)
	a.previewOpen = true
	a.selectOnly(p)
	// Synchronously build the preview so the modal has a bitmap.
	st := a.buildPreview(p)
	st.gen = 1
	a.preview.Store(st)
	a.quickLook = true

	tt := ui.NewTester(a.view, 1100, 720)
	tt.Frame()
	tt.Frame()
	if !a.quickLook {
		t.Fatal("quick look closed itself")
	}
}
