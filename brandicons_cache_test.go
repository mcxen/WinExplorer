package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrandLogoFetchAndCache(t *testing.T) {
	dir := t.TempDir()
	oldDir := brandLogoDirOverride
	brandLogoDirOverride = dir
	t.Cleanup(func() { brandLogoDirOverride = oldDir })

	// The catalogue serves one mark and 404s another, like models.dev.
	mux := http.NewServeMux()
	svgBody := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M2 2h20v20H2z"/></svg>`
	mux.HandleFunc("/logos/openai.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(svgBody))
	})
	mux.HandleFunc("/logos/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	oldLogoBase := brandLogoBaseURL
	brandLogoBaseURL = srv.URL + "/logos/"
	t.Cleanup(func() { brandLogoBaseURL = oldLogoBase })
	brandMissingMu.Lock()
	brandMissing = map[string]bool{}
	brandMissingMu.Unlock()

	// A known provider parses and persists.
	logo := providerLogo("openai")
	if logo.svg == nil || logo.generic || logo.notFound {
		t.Fatalf("openai logo: %+v", logo)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache dir: %v %d entries", err, len(entries))
	}

	// Second ask comes from the in-memory map, not the network.
	if again := providerLogo("openai"); again != logo {
		t.Fatal("logo not memoised")
	}

	// An unknown provider: a 404 is remembered, the answer stays generic
	// and aligned with the branded rows.
	missing := providerLogo("made-up-provider")
	if missing.svg != nil && missing.notFound {
		t.Fatalf("404 marked: %+v", missing)
	}
	if !brandLogoMissing("made-up-provider") {
		t.Fatal("404 not remembered")
	}
	if missing.icon() == nil {
		t.Fatal("missing provider has no fallback icon")
	}

	// A hostile provider id must not climb out of the logos directory.
	_ = providerLogo("../../etc")
	path, err := brandLogoPath("../../etc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(filepath.Base(path), "..") || strings.Contains(path, "/etc") {
		t.Fatalf("unsanitized path: %q", path)
	}
}
