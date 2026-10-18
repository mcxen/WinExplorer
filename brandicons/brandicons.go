// Package brandicons shows a drive maker's logo on the disks page.
//
// The logos come from Simple Icons (https://simpleicons.org), which
// licenses them CC0 1.0 — see LICENSE.simple-icons in this directory. They
// are embedded rather than fetched, so the page draws them offline and a
// logo never arrives as a network request.
//
// Matching is by the model string a drive reports, because that is the one
// identifier every platform gives: "APPLE SSD AP0256Z", "WDC WD40EFRX",
// "ST4000DM004". Only makers Simple Icons carries are matched; anything
// else falls back to the generic drive glyph.
package brandicons

import (
	"embed"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

//go:embed *.svg
var files embed.FS

// brand pairs a maker's name with the model-string prefixes that identify
// it. Prefixes are upper-case; matching runs against the upper-cased model,
// so a drive whose name differs in case still matches.
type brand struct {
	name  string // the file's base name, "westerndigital"
	label string // the maker's name as shown in a tooltip
	// prefixes are model-name starts. They are deliberately specific: "HP"
	// alone would match "HP" inside other words, so a prefix match only
	// counts at the start of the model string.
	prefixes []string
}

var brands = []brand{
	{name: "apple", label: "Apple", prefixes: []string{"APPLE SSD", "APPLE HDD", "APPLE FABRIC"}},
	{name: "seagate", label: "Seagate", prefixes: []string{"ST", "SEAGATE", "BARRACUDA", "IRONWOLF", "SKYHAWK", "FIREKUDA"}},
	{name: "westerndigital", label: "Western Digital", prefixes: []string{"WDC", "WD_", "WD ", "WESTERN DIGITAL"}},
	{name: "sandisk", label: "SanDisk", prefixes: []string{"SANDISK", "SDSS", "SD8S", "SDSSDH"}},
	{name: "toshiba", label: "Toshiba", prefixes: []string{"TOSHIBA", "KXG", "THNS", "XG"}},
	{name: "samsung", label: "Samsung", prefixes: []string{"SAMSUNG", "MZ-", "SSD 8", "SSD 9"}},
	{name: "kingstontechnology", label: "Kingston", prefixes: []string{"KINGSTON", "SA400", "SA2000", "SKC", "SNV", "KC"}},
	{name: "hitachi", label: "Hitachi", prefixes: []string{"HITACHI", "HGST", "HDS", "HUA"}},
	{name: "intel", label: "Intel", prefixes: []string{"INTEL", "SSDPE", "SSDSC", "SSDSC2"}},
	{name: "lenovo", label: "Lenovo", prefixes: []string{"LENOVO"}},
	{name: "dell", label: "Dell", prefixes: []string{"DELL"}},
	{name: "hp", label: "HP", prefixes: []string{"HP ", "HPE "}},
	{name: "synology", label: "Synology", prefixes: []string{"SYNOLOGY"}},
	{name: "qnap", label: "QNAP", prefixes: []string{"QNAP"}},
}

// Logo is a matched maker: the parsed SVG and the name to show.
type Logo struct {
	Maker string
	Label string
	SVG   *ui.SVG
}

// empty marks a model no icon matches, so the lookup is not repeated every
// frame.
type cacheEntry struct {
	once sync.Once
	logo *Logo
}

var cache sync.Map // upper-cased model → *cacheEntry

// Lookup returns the maker's logo for a drive model, or nil when the model
// names no maker Simple Icons carries. Parsed SVGs are cached per model.
func Lookup(model string) *Logo {
	key := strings.ToUpper(strings.TrimSpace(model))
	if key == "" {
		return nil
	}
	v, _ := cache.LoadOrStore(key, &cacheEntry{})
	e := v.(*cacheEntry)
	e.once.Do(func() { e.logo = match(key) })
	return e.logo
}

func match(model string) *Logo {
	for _, b := range brands {
		for _, p := range b.prefixes {
			if strings.HasPrefix(model, p) {
				return &Logo{Maker: b.name, Label: b.label, SVG: load(b.name)}
			}
		}
	}
	return nil
}

var (
	svgMu   sync.Mutex
	parsed  = map[string]*ui.SVG{}
	missing = map[string]bool{}
)

// load parses one embedded logo once. A malformed file is remembered as
// missing rather than panicking the page.
func load(name string) *ui.SVG {
	svgMu.Lock()
	defer svgMu.Unlock()
	if s, ok := parsed[name]; ok {
		return s
	}
	if missing[name] {
		return nil
	}
	data, err := files.ReadFile(name + ".svg")
	if err != nil {
		missing[name] = true
		return nil
	}
	s, err := ui.ParseSVG(data)
	if err != nil {
		missing[name] = true
		return nil
	}
	parsed[name] = s
	return s
}

// Makers lists the brands this package can recognise, for the page's
// documentation and tests.
func Makers() []string {
	out := make([]string, 0, len(brands))
	for _, b := range brands {
		out = append(out, b.label)
	}
	return out
}
