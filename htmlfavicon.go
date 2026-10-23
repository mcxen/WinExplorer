package main

import (
	"encoding/base64"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// htmlfavicon.go finds a local favicon for an .html/.htm tile. Large icon
// views show the favicon when one is next to the file or declared in a
// <link rel=icon> inside the first few KB; 16 px tiles keep the hand-drawn
// browser glyph. Remote http(s) icons are skipped so a folder of pages
// cannot stall the thumb workers on the network.

const (
	// htmlFaviconReadMax is how much of an HTML file we scan for <link>
	// tags. Favicons live in <head>; past this the page body is noise.
	htmlFaviconReadMax = 48 << 10
	// htmlFaviconFileMax rejects absurd icon files so a mispointed path
	// cannot blow the thumb budget.
	htmlFaviconFileMax = 256 << 10
)

var (
	reHTMLLinkTag = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	reHTMLRel     = regexp.MustCompile(`(?i)\brel\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	reHTMLHref    = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// htmlFaviconImage returns a favicon picture for htmlPath, or an error when
// none can be decoded. Candidates: <link rel="icon|shortcut icon|…"> href
// (local or data URI), then favicon.ico beside the HTML file.
func htmlFaviconImage(htmlPath string, side int) (image.Image, error) {
	side = max(side, 16)
	baseDir := filepath.Dir(htmlPath)
	var hrefs []string
	if f, err := os.Open(htmlPath); err == nil {
		buf := make([]byte, htmlFaviconReadMax)
		n, _ := f.Read(buf)
		f.Close()
		hrefs = findFaviconHrefs(buf[:n])
	}
	hrefs = append(hrefs, "favicon.ico")
	seen := make(map[string]bool, len(hrefs))
	var last error
	for _, href := range hrefs {
		key := strings.ToLower(strings.TrimSpace(href))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		img, err := loadFaviconRef(baseDir, href, side)
		if err == nil && img != nil {
			return img, nil
		}
		if err != nil {
			last = err
		}
	}
	if last == nil {
		last = fmt.Errorf("无 favicon")
	}
	return nil, last
}

// findFaviconHrefs returns icon hrefs from HTML head markup, preferred
// order: rel=icon / shortcut icon first, then any other *icon* rel
// (apple-touch-icon etc.).
func findFaviconHrefs(html []byte) []string {
	var preferred, other []string
	for _, raw := range reHTMLLinkTag.FindAll(html, -1) {
		tag := string(raw)
		relM := reHTMLRel.FindStringSubmatch(tag)
		if relM == nil {
			continue
		}
		rel := strings.ToLower(firstGroup(relM[1:]))
		if !strings.Contains(rel, "icon") {
			continue
		}
		hrefM := reHTMLHref.FindStringSubmatch(tag)
		if hrefM == nil {
			continue
		}
		href := htmlAttrUnescape(strings.TrimSpace(firstGroup(hrefM[1:])))
		if href == "" {
			continue
		}
		if rel == "icon" || strings.Contains(rel, "shortcut") {
			preferred = append(preferred, href)
		} else {
			other = append(other, href)
		}
	}
	return append(preferred, other...)
}

func firstGroup(groups []string) string {
	for _, g := range groups {
		if g != "" {
			return g
		}
	}
	return ""
}

func htmlAttrUnescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&",
		"&quot;", `"`,
		"&#39;", "'",
		"&lt;", "<",
		"&gt;", ">",
	)
	return r.Replace(s)
}

// loadFaviconRef decodes a favicon referenced from an HTML file. Only local
// paths (relative to the HTML's directory, or absolute) and data: URIs are
// accepted; http(s) and other schemes are refused.
func loadFaviconRef(baseDir, href string, side int) (image.Image, error) {
	href = strings.TrimSpace(href)
	if href == "" {
		return nil, fmt.Errorf("empty href")
	}
	lower := strings.ToLower(href)
	if strings.HasPrefix(lower, "data:") {
		return decodeDataURIImage(href, side)
	}
	if i := strings.Index(href, "://"); i >= 0 {
		return nil, fmt.Errorf("remote favicon skipped")
	}
	path := resolveFaviconPath(baseDir, href)
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > htmlFaviconFileMax {
		return nil, fmt.Errorf("favicon unusable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeBytesScaled(data, side)
}

// resolveFaviconPath maps an href to a filesystem path. Windows drive
// paths stay absolute; a leading "/" is treated as site-root relative to
// the HTML file's directory (local pages rarely mean the OS root).
func resolveFaviconPath(baseDir, href string) string {
	if len(href) >= 2 && href[1] == ':' {
		return filepath.Clean(href)
	}
	clean := strings.TrimPrefix(href, "/")
	clean = strings.TrimPrefix(clean, "./")
	return filepath.Clean(filepath.Join(baseDir, filepath.FromSlash(clean)))
}

// decodeDataURIImage decodes data:image/…;base64,… (and a rare URL-encoded
// form) into a picture fitting side px.
func decodeDataURIImage(uri string, side int) (image.Image, error) {
	comma := strings.IndexByte(uri, ',')
	if comma < 0 {
		return nil, fmt.Errorf("bad data URI")
	}
	meta, payload := uri[:comma], uri[comma+1:]
	var (
		data []byte
		err  error
	)
	if strings.Contains(strings.ToLower(meta), ";base64") {
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(payload)
		}
	} else {
		var s string
		s, err = url.QueryUnescape(payload)
		data = []byte(s)
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > htmlFaviconFileMax {
		return nil, fmt.Errorf("data URI empty or too large")
	}
	return decodeBytesScaled(data, side)
}
