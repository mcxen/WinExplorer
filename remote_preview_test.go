package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mgilbir/spine/docx"
	"github.com/mgilbir/spine/pptx"
	"github.com/mgilbir/spine/xlsx"
	"github.com/mholt/archives"
)

func TestBuildFilePreviewCoverage(t *testing.T) {
	dir := t.TempDir()

	note := filepath.Join(dir, "note.docx")
	writeDocx(t, note, "Hello 文档")
	got := buildFilePreview(note, mustSize(t, note))
	if got.Kind != "document" || !strings.Contains(got.Text, "Hello 文档") {
		t.Fatalf("docx preview = %+v", got)
	}

	book := filepath.Join(dir, "sheet.xlsx")
	writeXlsx(t, book, map[string]string{"A1": "标题", "B1": "正文"})
	got = buildFilePreview(book, mustSize(t, book))
	if !strings.Contains(got.Text, "标题") || !strings.Contains(got.Text, "正文") {
		t.Fatalf("xlsx preview = %+v", got)
	}

	pack := filepath.Join(dir, "pack.zip")
	writeZip(t, pack, map[string]string{"a.txt": "A", "sub/b.txt": "B"})
	got = buildFilePreview(pack, mustSize(t, pack))
	if got.Kind != "archive" || len(got.Entries) != 2 {
		t.Fatalf("zip preview = %+v", got)
	}

	rtf := filepath.Join(dir, "note.rtf")
	// \u20320? is 你 plus the one-character fallback Word writes after \uN.
	if err := os.WriteFile(rtf, []byte(`{\rtf1 Hello \par world \u20320?}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(rtf, mustSize(t, rtf))
	if !strings.Contains(got.Text, "Hello") || !strings.Contains(got.Text, "world") || !strings.Contains(got.Text, "你") {
		t.Fatalf("rtf preview = %+v", got)
	}

	license := filepath.Join(dir, "LICENSE")
	if err := os.WriteFile(license, []byte("permission is granted"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(license, mustSize(t, license))
	if got.Kind != "text" || !strings.Contains(got.Text, "permission") {
		t.Fatalf("extensionless preview = %+v", got)
	}

	bin := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(bin, []byte{0, 1, 2, 3, 4, 5}, 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(bin, mustSize(t, bin))
	if got.Kind != "hex" || !strings.Contains(got.Text, "00000000") {
		t.Fatalf("binary preview = %+v", got)
	}

	docm := filepath.Join(dir, "macro.docm")
	writeDocx(t, docm, "宏文档")
	got = buildFilePreview(docm, mustSize(t, docm))
	if got.Kind != "document" || !strings.Contains(got.Text, "宏文档") {
		t.Fatalf("docm preview = %+v", got)
	}

	apk := filepath.Join(dir, "app.apk")
	writeZip(t, apk, map[string]string{"AndroidManifest.xml": "manifest", "classes.dex": "dex"})
	got = buildFilePreview(apk, mustSize(t, apk))
	if got.Kind != "archive" || len(got.Entries) != 2 {
		t.Fatalf("apk preview = %+v", got)
	}

	story := filepath.Join(dir, "story.epub")
	writeZip(t, story, map[string]string{
		"OEBPS/chap2.xhtml": `<html><body><p>第二章</p><script>secret()</script></body></html>`,
		"OEBPS/chap1.xhtml": `<html><body><h1>第一章</h1><p>正文</p></body></html>`,
	})
	got = buildFilePreview(story, mustSize(t, story))
	if got.Kind != "document" || !strings.Contains(got.Text, "第一章") || !strings.Contains(got.Text, "第二章") || strings.Contains(got.Text, "secret") {
		t.Fatalf("epub preview = %+v", got)
	}
	if strings.Index(got.Text, "第一章") > strings.Index(got.Text, "第二章") {
		t.Fatalf("epub chapters out of order: %q", got.Text)
	}

	deck := filepath.Join(dir, "deck.pptx")
	writePptx(t, deck, []string{"第一页", "第二页", "第十页"})
	got = buildFilePreview(deck, mustSize(t, deck))
	if i, j := strings.Index(got.Text, "第一页"), strings.Index(got.Text, "第二页"); i < 0 || j < 0 || i > j || strings.Index(got.Text, "第十页") < j {
		t.Fatalf("pptx order = %q", got.Text)
	}

	odt := filepath.Join(dir, "note.odt")
	writeZip(t, odt, map[string]string{
		"content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0">` +
			`<office:body><office:text><text:p>开放文档</text:p></office:text></office:body></office:document-content>`,
	})
	got = buildFilePreview(odt, mustSize(t, odt))
	if got.Kind != "document" || !strings.Contains(got.Text, "开放文档") {
		t.Fatalf("odt preview = %+v", got)
	}

	ods := filepath.Join(dir, "sheet.ods")
	writeZip(t, ods, map[string]string{
		"content.xml": `<?xml version="1.0" encoding="UTF-8"?>` +
			`<office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0">` +
			`<office:body><office:spreadsheet><table:table><table:table-row>` +
			`<table:table-cell><text:p>格子</text:p></table:table-cell>` +
			`</table:table-row></table:table></office:spreadsheet></office:body></office:document-content>`,
	})
	got = buildFilePreview(ods, mustSize(t, ods))
	if got.Kind != "document" || !strings.Contains(got.Text, "格子") {
		t.Fatalf("ods preview = %+v", got)
	}

	utf := filepath.Join(dir, "unicode.txt")
	// UTF-16LE BOM + "你好"
	raw := []byte{0xFF, 0xFE, 0x60, 0x4F, 0x7D, 0x59}
	if err := os.WriteFile(utf, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(utf, mustSize(t, utf))
	if got.Kind != "text" || !strings.Contains(got.Text, "你好") {
		t.Fatalf("utf-16 preview = %+v", got)
	}

	plainGz := filepath.Join(dir, "one.gz")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write([]byte("just one file")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plainGz, gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(plainGz, mustSize(t, plainGz))
	if got.Kind != "text" || !strings.Contains(got.Text, "just one file") {
		t.Fatalf("plain gz preview = %+v", got)
	}

	packed := filepath.Join(dir, "photos.tar.xz")
	writeTarXz(t, packed, map[string]string{"a.txt": "A", "sub/b.txt": "B"})
	got = buildFilePreview(packed, mustSize(t, packed))
	if got.Kind != "archive" || len(got.Entries) != 2 {
		t.Fatalf("tar.xz preview = %+v", got)
	}

	cab := filepath.Join(dir, "old.cab")
	if err := os.WriteFile(cab, []byte("MSCF"), 0o644); err != nil {
		t.Fatal(err)
	}
	got = buildFilePreview(cab, mustSize(t, cab))
	if got.Kind != "archive" || got.Note == "" || len(got.Entries) != 0 {
		t.Fatalf("cab preview = %+v", got)
	}
}

func TestPreview7zListsNames(t *testing.T) {
	bin, err := exec.LookPath("7zz")
	if err != nil {
		t.Skip("7zz not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "pack.7z")
	cmd := exec.Command(bin, "a", "-bso0", "-bsp0", archive, "hello.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7zz: %v %s", err, out)
	}
	got := buildFilePreview(archive, mustSize(t, archive))
	if got.Kind != "archive" {
		t.Fatalf("7z preview = %+v", got)
	}
	found := false
	for _, e := range got.Entries {
		if strings.HasSuffix(e.Name, "hello.txt") {
			found = true
		}
	}
	if !found {
		t.Fatalf("7z entries = %+v", got.Entries)
	}
}

func TestRemotePreviewAndNearSearch(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Photos/sub")
	e.mkdir("Other")
	e.writeFile("Photos/z-report.txt", "z")
	e.writeFile("Photos/sub/a-report.txt", "a")
	e.writeFile("Other/m-report.txt", "m")
	docxPath := filepath.Join(e.home, "Photos", "note.docx")
	writeDocx(t, docxPath, "远程正文")

	tok := e.login()
	rr := e.api(http.MethodPost, "/api/fs/preview", tok, map[string]any{"path": "/Photos/note.docx"})
	if rr.Code != 200 {
		t.Fatalf("preview code %d (%s)", rr.Code, rr.Message)
	}
	var preview previewPayload
	if err := json.Unmarshal(rr.Data, &preview); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Text, "远程正文") {
		t.Fatalf("preview data = %+v", preview)
	}
	guest := e.api(http.MethodPost, "/api/fs/preview", "", map[string]any{"path": "/Photos/note.docx"})
	if guest.Code != 401 {
		t.Fatalf("guest preview code %d, want 401", guest.Code)
	}

	rr = e.api(http.MethodPost, "/api/fs/search", tok, map[string]any{
		"parent": "/", "keywords": "report", "scope": 0, "page": 1, "per_page": 20,
		"global": true, "near": "/Photos",
	})
	if rr.Code != 200 {
		t.Fatalf("search code %d (%s)", rr.Code, rr.Message)
	}
	var data remoteFSSearchData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Content) != 3 {
		t.Fatalf("hits = %+v, want 3", data.Content)
	}
	if data.Content[0].Name != "z-report.txt" || data.Content[0].Parent != "/Photos" {
		t.Fatalf("first hit = %+v, want the file in /Photos", data.Content[0])
	}
	if data.Content[1].Parent != "/Photos/sub" {
		t.Fatalf("second hit = %+v, want the file under /Photos", data.Content[1])
	}
	if data.Content[2].Parent != "/Other" {
		t.Fatalf("third hit = %+v, want the file outside", data.Content[2])
	}
}

func writeDocx(t *testing.T, path, text string) {
	t.Helper()
	d := docx.Create()
	d.AddParagraphWithText(text)
	b, err := d.SaveBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeXlsx(t *testing.T, path string, cells map[string]string) {
	t.Helper()
	w := xlsx.Create()
	sh, err := w.AddSheet("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 0, len(cells))
	for ref := range cells {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		if err := sh.SetCellValue(ref, cells[ref]); err != nil {
			t.Fatal(err)
		}
	}
	b, err := w.SaveBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writePptx(t *testing.T, path string, slides []string) {
	t.Helper()
	p := pptx.Create()
	for _, text := range slides {
		p.AddSlide().AddTextBox().SetText(text)
	}
	b, err := p.SaveBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTarXz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	xw, err := archives.Xz{}.OpenWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(xw)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		body := []byte(files[name])
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := xw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
