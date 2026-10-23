package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocPageWindow(t *testing.T) {
	s, idx := docPageWindow(0, 12)
	if s != 0 || len(idx) != 3 || idx[2] != 2 {
		t.Fatalf("first view: %d %v", s, idx)
	}
	s, idx = docPageWindow(11, 12)
	if s != 9 || idx[0] != 9 || idx[2] != 11 {
		t.Fatalf("clamp end: %d %v", s, idx)
	}
	s, idx = docPageWindow(0, 1)
	if s != 0 || len(idx) != 1 {
		t.Fatalf("single page: %v", idx)
	}
	s, _ = docPageWindow(100, 500)
	if s != docMaxPages-docPagesPerView {
		t.Fatalf("cap at docMaxPages: %d", s)
	}
}

func TestKindByExtDocs(t *testing.T) {
	for _, e := range []string{"pdf", "docx", "xlsx", "pptx", "odt", "rtf"} {
		if kindByExt(false, e) != previewDoc {
			t.Fatalf("%s should be previewDoc", e)
		}
	}
	for _, e := range []string{"html", "htm", "go", "sql"} {
		if kindByExt(false, e) != previewText {
			t.Fatalf("%s should be previewText", e)
		}
	}
}

func TestDocPDFSourceNoOffice(t *testing.T) {
	old := sofficeBin
	sofficeBin = func() string { return "" }
	defer func() { sofficeBin = old }()
	p := filepath.Join(t.TempDir(), "a.docx")
	os.WriteFile(p, []byte("x"), 0o644)
	if _, err := docPDFSource(p); err != errNoOffice {
		t.Fatalf("want errNoOffice, got %v", err)
	}
	a := &App{}
	st := a.buildPreview(p)
	if st.kind != previewDoc || !strings.Contains(st.err, "LibreOffice") {
		t.Fatalf("degrade state: %+v", st)
	}
}

func TestDocOfficeConvertAndPages(t *testing.T) {
	if sofficeBin() == "" {
		t.Skip("LibreOffice not installed")
	}
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm not installed")
	}
	if testing.Short() {
		t.Skip("slow")
	}
	dir := t.TempDir()
	txt := filepath.Join(dir, "doc.odt")
	// Build an .odt from plain text through LibreOffice itself.
	src := filepath.Join(dir, "doc.txt")
	os.WriteFile(src, []byte(strings.Repeat("第一页内容\n", 200)), 0o644)
	cmd := exec.Command(sofficeBin(), "--headless", "--convert-to", "odt", "--outdir", dir, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("soffice cannot build fixture: %v %s", err, out)
	}
	pdf, err := docPDFSource(txt)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	n := pdfPageCount(pdf)
	if n < 2 {
		t.Fatalf("want multi-page, got %d", n)
	}
	img, err := pdfPageImage(pdf, 2, 200)
	if err != nil || img == nil {
		t.Fatalf("page 2: %v", err)
	}
	// Cached second call returns the same file.
	if again, _ := docPDFSource(txt); again != pdf {
		t.Fatalf("cache miss: %s vs %s", again, pdf)
	}
}
