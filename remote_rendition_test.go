package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestRemoteRendition: /d/…?w= sends a smaller picture with its own ETag and
// a week's Cache-Control, answers If-None-Match with 304, and without w the
// original is untouched.
func TestRemoteRendition(t *testing.T) {
	e := newRemoteTestEnv(t)
	img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	if err := os.WriteFile(filepath.Join(e.home, "big.jpg"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	url := e.srv.URL + "/d/big.jpg?sign=" + e.a.remoteSign("/big.jpg")
	resp, body := e.fetch(url + "&w=200")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("rendition: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader([]byte(body)))
	if err != nil || cfg.Width != 256 {
		t.Fatalf("rendition is %dx%d (%v), want 256 wide", cfg.Width, cfg.Height, err)
	}
	if resp.Header.Get("Cache-Control") == "" {
		t.Fatal("no Cache-Control on the rendition")
	}
	etag := resp.Header.Get("Etag")
	req, _ := http.NewRequest(http.MethodGet, url+"&w=200", nil)
	req.Header.Set("If-None-Match", etag)
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusNotModified {
		t.Fatalf("revalidation = %d, want 304", r2.StatusCode)
	}
	orig, ob := e.fetch(url)
	if orig.StatusCode != 200 || len(ob) != buf.Len() || orig.Header.Get("Etag") == etag {
		t.Fatalf("original: %d, %d bytes (want %d)", orig.StatusCode, len(ob), buf.Len())
	}
}
