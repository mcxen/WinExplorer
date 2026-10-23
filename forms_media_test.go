package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"sunvale/internal/forms"
)

func TestFormLoadBitmapPathAndDataURL(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "x.png")
	if err := os.WriteFile(pngPath, formsDemoPNG(), 0o644); err != nil {
		t.Fatal(err)
	}
	bm, errMsg := formLoadBitmap(pngPath, 64)
	if bm == nil || errMsg != "" {
		t.Fatalf("path: bm=%v err=%q", bm != nil, errMsg)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(formsDemoPNG())
	bm2, err2 := formLoadBitmap(dataURL, 64)
	if bm2 == nil || err2 != "" {
		t.Fatalf("dataURL: bm=%v err=%q", bm2 != nil, err2)
	}
	bm3, err3 := formLoadBitmap(filepath.Join(dir, "missing.png"), 64)
	if bm3 != nil || err3 == "" {
		t.Fatalf("missing should fail: %v %q", bm3 != nil, err3)
	}
}

func TestFormLoadBitmapBrokenCached(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	_ = os.WriteFile(bad, []byte("nope"), 0o644)
	_, e1 := formLoadBitmap(bad, 32)
	_, e2 := formLoadBitmap(bad, 32)
	if e1 == "" || e1 != e2 {
		t.Fatalf("%q vs %q", e1, e2)
	}
}

func formsDemoPNG() []byte {
	// Reuse sandbox generator via a tiny write round-trip.
	root := forms.EnsureDemoSandbox()
	data, err := os.ReadFile(filepath.Join(root, "Downloads", "demo-photo.png"))
	if err != nil {
		panic(err)
	}
	return data
}
