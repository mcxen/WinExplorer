package fileview

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgilbir/spine/docx"
	"github.com/mgilbir/spine/pptx"
	"github.com/mgilbir/spine/xlsx"
)

// The tests use Options{NoScript: true, NoExternal: true} so they never
// depend on what the machine has installed: CI runs in a container without
// ffmpeg or python, and a regression there must still say what is wrong.

func plainOpts() Options {
	return Options{NoScript: true, NoExternal: true}
}

func TestPlainTextAndEncoding(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "note.txt")
	writeFile(t, text, "第一行 alpha\n第二行 beta\n")
	res, err := Extract(text, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindText || res.Method != "内置解析" {
		t.Fatalf("kind/method = %v/%q", res.Kind, res.Method)
	}
	if !strings.Contains(res.Text, "第二行 beta") {
		t.Fatalf("text = %q", res.Text)
	}

	big := filepath.Join(dir, "big.txt")
	writeFile(t, big, strings.Repeat("x", 100000))
	res, err = Extract(big, Options{TextRunes: 1000, NoScript: true, NoExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || !strings.Contains(res.Text, "已截断") {
		t.Fatalf("truncation not reported: %q", res.Text)
	}

	utf16 := filepath.Join(dir, "win.log")
	body := []byte{0xff, 0xfe}
	for _, r := range "Windows 日志 entry" {
		body = binary.LittleEndian.AppendUint16(body, uint16(r))
	}
	writeBytes(t, utf16, body)
	res, err = Extract(utf16, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Windows 日志 entry") {
		t.Fatalf("utf16 text = %q", res.Text)
	}
}

func TestOfficeText(t *testing.T) {
	dir := t.TempDir()

	docPath := filepath.Join(dir, "note.docx")
	d := docx.Create()
	d.AddParagraphWithText("季度报告正文")
	saveDoc(t, docPath, d.SaveBytes, d.Close)
	res, err := Extract(docPath, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindDoc || !strings.Contains(res.Text, "季度报告正文") {
		t.Fatalf("docx = %v %q", res.Kind, res.Text)
	}

	sheetPath := filepath.Join(dir, "sheet.xlsx")
	w := xlsx.Create()
	sh, err := w.AddSheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sh.SetCellValue("A1", "预算表"); err != nil {
		t.Fatal(err)
	}
	saveDoc(t, sheetPath, w.SaveBytes, w.Close)
	res, err = Extract(sheetPath, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "预算表") {
		t.Fatalf("xlsx = %q", res.Text)
	}

	deckPath := filepath.Join(dir, "deck.pptx")
	p := pptx.Create()
	p.AddSlide().AddTextBox().SetText("开场页")
	saveDoc(t, deckPath, p.SaveBytes, p.Close)
	res, err = Extract(deckPath, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "开场页") {
		t.Fatalf("pptx = %q", res.Text)
	}
}

func TestImageBecomesFrame(t *testing.T) {
	dir := t.TempDir()

	// An opaque JPEG: the frame must stay JPEG so the payload stays small.
	jpgPath := filepath.Join(dir, "photo.jpg")
	var jpg bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 8), uint8(y * 16), 128, 255})
		}
	}
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, jpgPath, jpg.Bytes())
	res, err := Extract(jpgPath, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindImage || len(res.Frames) != 1 {
		t.Fatalf("kind/frames = %v/%d", res.Kind, len(res.Frames))
	}
	if res.Frames[0].MIME != "image/jpeg" || res.Meta.Width != 32 || res.Meta.Height != 16 {
		t.Fatalf("frame = %+v meta = %+v", res.Frames[0], res.Meta)
	}
	if !strings.Contains(res.Text, "32 × 16") {
		t.Fatalf("text = %q", res.Text)
	}

	// Transparency is kept: the frame becomes PNG instead of a matte JPEG.
	pngPath := filepath.Join(dir, "logo.png")
	out := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	out.Set(2, 2, color.NRGBA{255, 0, 0, 0})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, out); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, pngPath, pngBuf.Bytes())
	res, err = Extract(pngPath, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Frames) != 1 || res.Frames[0].MIME != "image/png" {
		t.Fatalf("png frame = %+v", res.Frames)
	}
}

func TestVideoWithoutToolsDegrades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	writeFile(t, path, strings.Repeat("\x00\x01\x02\x03", 1024))
	res, err := Extract(path, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindVideo {
		t.Fatalf("kind = %v", res.Kind)
	}
	if len(res.Frames) != 0 {
		t.Fatalf("frames without tools = %+v", res.Frames)
	}
	if !strings.Contains(res.Text, "视频") || len(res.Warnings) == 0 {
		t.Fatalf("text = %q warnings = %v", res.Text, res.Warnings)
	}
}

func TestArchiveListing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("docs/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	res, err := Extract(path, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindArchive || !strings.Contains(res.Text, "docs/a.txt") {
		t.Fatalf("archive = %v %q", res.Kind, res.Text)
	}
}

func TestBinaryAndKinds(t *testing.T) {
	dir := t.TempDir()
	blob := filepath.Join(dir, "blob.bin")
	// ELF magic so the note names the format instead of leaving hex alone.
	body := append([]byte{0x7f, 'E', 'L', 'F'}, bytes.Repeat([]byte{0xab}, 300)...)
	writeBytes(t, blob, body)
	res, err := Extract(blob, plainOpts())
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindBinary || !strings.Contains(res.Text, "可执行文件") {
		t.Fatalf("binary = %v %q", res.Kind, res.Text)
	}

	writeFile(t, filepath.Join(dir, "Makefile"), "run:\n\tgo run .\n")
	for _, tc := range []struct {
		name string
		want Kind
	}{
		{"a.go", KindCode}, {"b.md", KindText}, {"c.png", KindImage},
		{"d.mkv", KindVideo}, {"e.flac", KindAudio}, {"f.pdf", KindDoc},
		{"g.7z", KindArchive}, {"Makefile", KindText}, {"x.unknown", KindBinary},
	} {
		if got := KindOf(filepath.Join(dir, tc.name)); got != tc.want {
			t.Fatalf("KindOf(%s) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestExtractorStatusAlwaysAnswers(t *testing.T) {
	// The status is machine-dependent by design; only its shape matters.
	lines := ExtractorStatus()
	if len(lines) == 0 {
		t.Fatal("no status lines")
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Fatal("blank status line")
		}
	}
}

// saveDoc writes a spine document's bytes without repeating the boilerplate.
func saveDoc(t *testing.T, path string, save func() ([]byte, error), close func() error) {
	t.Helper()
	b, err := save()
	if err != nil {
		t.Fatal(err)
	}
	if err := close(); err != nil {
		t.Fatal(err)
	}
	writeBytes(t, path, b)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	writeBytes(t, path, []byte(body))
}

func writeBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
