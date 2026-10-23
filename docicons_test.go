package main

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFindFaviconHrefs(t *testing.T) {
	html := []byte(`<!doctype html><html><head>
<link rel="stylesheet" href="app.css">
<link rel="shortcut icon" href="icons/app.png">
<link rel="apple-touch-icon" href="touch.png">
<link rel=icon href='data:image/png;base64,aaa'>
</head><body></body></html>`)
	got := findFaviconHrefs(html)
	if len(got) < 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "icons/app.png" {
		t.Fatalf("preferred shortcut icon first, got %v", got)
	}
	// data URI icon also preferred (rel=icon)
	foundData := false
	for _, h := range got {
		if stringsHasPrefixFold(h, "data:") {
			foundData = true
		}
	}
	if !foundData {
		t.Fatalf("expected data URI among %v", got)
	}
}

func stringsHasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		a, b := s[i], prefix[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func TestHtmlFaviconImageLocal(t *testing.T) {
	dir := t.TempDir()
	iconPath := filepath.Join(dir, "mark.png")
	solidPNG(t, iconPath, 32, color.RGBA{R: 200, G: 40, B: 40, A: 255})
	htmlPath := filepath.Join(dir, "page.html")
	if err := os.WriteFile(htmlPath, []byte(`<html><head>
<link rel="icon" href="mark.png">
</head><body>hi</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := htmlFaviconImage(htmlPath, 64)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		t.Fatalf("empty image %#v", b)
	}
}

func TestHtmlFaviconImageDefaultIcoFallbackPNG(t *testing.T) {
	// When the HTML has no <link>, favicon.ico beside it is tried; we write a
	// PNG under that name so decode succeeds without an ICO parser.
	dir := t.TempDir()
	solidPNG(t, filepath.Join(dir, "favicon.ico"), 16, color.RGBA{G: 180, A: 255})
	htmlPath := filepath.Join(dir, "index.htm")
	if err := os.WriteFile(htmlPath, []byte(`<html><body>no link</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := htmlFaviconImage(htmlPath, 32)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() <= 0 {
		t.Fatal("empty")
	}
}

func TestHtmlFaviconDataURI(t *testing.T) {
	dir := t.TempDir()
	// 1×1 red PNG
	raw, _ := encodeTinyPNG(color.RGBA{R: 255, A: 255})
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	htmlPath := filepath.Join(dir, "x.html")
	if err := os.WriteFile(htmlPath, []byte(`<link rel="icon" href="`+uri+`">`), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := htmlFaviconImage(htmlPath, 16)
	if err != nil {
		t.Fatal(err)
	}
	if img == nil {
		t.Fatal("nil image")
	}
}

func TestHtmlFaviconSkipsRemote(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "x.html")
	if err := os.WriteFile(htmlPath, []byte(`<link rel="icon" href="https://example.com/favicon.ico">`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := htmlFaviconImage(htmlPath, 32); err == nil {
		t.Fatal("remote-only favicon should fail closed")
	}
}

func encodeTinyPNG(c color.RGBA) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	var buf []byte
	w := &sliceWriter{b: &buf}
	if err := png.Encode(w, img); err != nil {
		return nil, err
	}
	return buf, nil
}

type sliceWriter struct{ b *[]byte }

func (w *sliceWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}

func TestMediaThumbStartsPDFAndHTML(t *testing.T) {
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "a.pdf")
	_ = os.WriteFile(pdfPath, []byte(minimalPDF), 0o644)
	iconPath := filepath.Join(dir, "f.png")
	solidPNG(t, iconPath, 24, color.RGBA{B: 200, A: 255})
	htmlPath := filepath.Join(dir, "a.html")
	_ = os.WriteFile(htmlPath, []byte(`<link rel="icon" href="f.png">`), 0o644)

	clearThumbs()
	deadline := time.Now().Add(4 * time.Second)
	var gotPDF, gotHTML bool
	for time.Now().Before(deadline) {
		if mediaThumb(pdfPath, 96) != nil {
			gotPDF = true
		}
		if mediaThumb(htmlPath, 96) != nil {
			gotHTML = true
		}
		if gotPDF && gotHTML {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, err := exec.LookPath("pdftoppm"); err == nil && !gotPDF {
		t.Fatal("expected PDF thumb via pdftoppm")
	}
	if !gotHTML {
		t.Fatal("expected HTML favicon thumb")
	}
	// Cache key is by path; a later smaller box still hits the entry.
	if mediaThumb(htmlPath, 16) == nil {
		t.Fatal("cached HTML thumb should remain")
	}
}

// minimalPDF is a tiny one-page PDF with a filled rectangle, enough for
// pdftoppm to rasterize.
const minimalPDF = `%PDF-1.1
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources<< >> >>endobj
4 0 obj<< /Length 48 >>stream
1 0 0 rg 20 20 160 160 re f
endstream
endobj
xref
0 5
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000230 00000 n 
trailer<< /Size 5 /Root 1 0 R >>
startxref
327
%%EOF
`
