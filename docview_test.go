package main

import (
	"image"
	"os"
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

func TestKindByExt(t *testing.T) {
	for _, c := range []struct {
		ext  string
		want previewKind
	}{{"mp4", previewVideo}, {"mov", previewVideo}} {
		if kindByExt(false, c.ext) != c.want {
			t.Fatalf("%s: got %v", c.ext, kindByExt(false, c.ext))
		}
	}
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
	oldThumb := sysThumbHook
	sysThumbHook = func(string, int) (image.Image, error) { return nil, nil }
	defer func() { sysThumbHook = oldThumb }()
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
