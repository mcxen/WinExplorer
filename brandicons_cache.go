package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/oneo-me/onevra/source/icons/lucide"
)

// brandicons_cache.go is the provider logo cache. Logos come from
// models.dev/logos/<provider>.svg and land in the user cache dir, so the
// model picker shows each provider's own mark after one fetch and nothing
// but disk reads after that. A missing or broken logo is not an error the
// user sees: the picker falls back to a lucide glyph, the same one it used
// before this cache existed.

const brandLogoTTL = 30 * 24 * time.Hour

// brandLogoBaseURL and brandLogoDirOverride let a test run the cache
// against its own server and its own directory.
var (
	brandLogoBaseURL     = "https://models.dev/logos/"
	brandLogoDirOverride string
)

// brandLogo holds one parsed SVG and why it looks the way it does.
type brandLogo struct {
	svg      *ui.SVG
	generic  bool // fell back to lucide; retry the network next open
	notFound bool // models.dev has no such logo; stop asking
	fetched  time.Time
}

var (
	brandLogosMu sync.Mutex
	brandLogos   = map[string]*brandLogo{}
)

// brandLogoDir is where logos persist: one file per provider id.
func brandLogoDir() (string, error) {
	if brandLogoDirOverride != "" {
		return brandLogoDirOverride, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sunvale", "logos"), nil
}

// brandLogoPath is the sanitized cache file for one provider id. Provider
// ids are catalogue slugs, but a hostile catalogue value must not climb out
// of the logos directory, so everything but letters, digits and dashes is
// flattened.
func brandLogoPath(provider string) (string, error) {
	dir, err := brandLogoDir()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range provider {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := b.String()
	if name == "" || name == "-" {
		return "", fmt.Errorf("空 provider")
	}
	return filepath.Join(dir, name+".svg"), nil
}

// providerLogo returns the drawable mark for a provider id. The parse is
// cached in memory; the bytes are cached on disk; the network is only asked
// when both are empty or stale. Never blocks the frame for long: the
// timeout is short and the failure path is a generic icon.
func providerLogo(provider string) *brandLogo {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return genericLogo()
	}
	brandLogosMu.Lock()
	cached, ok := brandLogos[provider]
	brandLogosMu.Unlock()
	if ok && cached != nil && time.Since(cached.fetched) < brandLogoTTL {
		return cached
	}
	logo := loadBrandLogo(provider)
	brandLogosMu.Lock()
	brandLogos[provider] = logo
	brandLogosMu.Unlock()
	return logo
}

func loadBrandLogo(provider string) *brandLogo {
	path, err := brandLogoPath(provider)
	if err == nil {
		if data, rerr := os.ReadFile(path); rerr == nil && time.Since(modTime(path)) < brandLogoTTL {
			if svg, perr := ui.ParseSVG(data); perr == nil {
				return &brandLogo{svg: svg, fetched: time.Now()}
			}
		}
	}
	// No usable disk copy: one short, bounded fetch.
	if data, ok := fetchBrandLogoSVG(provider); ok {
		if svg, perr := ui.ParseSVG(data); perr == nil {
			if err == nil {
				_ = os.MkdirAll(filepath.Dir(path), 0o755)
				_ = os.WriteFile(path, data, 0o644)
			}
			return &brandLogo{svg: svg, fetched: time.Now()}
		}
	}
	// The catalogue answered 404: stop asking for this id for a while.
	if brandLogoMissing(provider) {
		return &brandLogo{notFound: true, fetched: time.Now()}
	}
	return &brandLogo{generic: true, fetched: time.Now()}
}

// brandLogoMissing reports whether the last fetch for this id saw a 404.
// The answer lives in a small in-memory map: a persistent negative cache
// would outlive a logo that models.dev adds later.
var (
	brandMissingMu sync.Mutex
	brandMissing   = map[string]bool{}
)

func brandLogoMissing(provider string) bool {
	brandMissingMu.Lock()
	defer brandMissingMu.Unlock()
	return brandMissing[provider]
}

// fetchBrandLogoSVG fetches one logo, remembering 404s so the picker does
// not re-ask for a provider the catalogue has no mark for.
func fetchBrandLogoSVG(provider string) ([]byte, bool) {
	url := brandLogoBaseURL + provider + ".svg"
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		brandMissingMu.Lock()
		brandMissing[provider] = true
		brandMissingMu.Unlock()
		return nil, false
	}
	if res.StatusCode != http.StatusOK {
		return nil, false
	}
	data := make([]byte, 0, 64<<10)
	buf := make([]byte, 32<<10)
	for {
		n, err := res.Body.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil || len(data) > 512<<10 {
			break
		}
	}
	if len(data) == 0 {
		return nil, false
	}
	return data, true
}

func modTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// genericLogo is the fallback mark. It is created once: the lucide icons
// are static data, and the picker asks for this on every paint of every
// row that has no logo yet.
var (
	genericOnce  sync.Once
	genericBrand *brandLogo
)

func genericLogo() *brandLogo {
	genericOnce.Do(func() {
		genericBrand = &brandLogo{generic: true, fetched: time.Now()}
	})
	return genericBrand
}

// logoOption converts a cached logo into the picker's leading icon. A
// generic fallback keeps its slot: rows stay aligned whether or not the
// catalogue had a mark.
func (l *brandLogo) icon() func() *ui.SVG {
	if l == nil || l.svg == nil {
		return lucide.Bot
	}
	svg := l.svg
	return func() *ui.SVG { return svg }
}
