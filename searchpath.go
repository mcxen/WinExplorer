package main

// searchpath.go turns a hit's folder into the breadcrumb shown under a search
// result and in the pane's 位置 column. It keeps the root and the folders
// nearest the hit, collapsing only the middle of deep paths, so a path remains
// identifiable without pushing the hit's own name off screen.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"sunvale/internal/searchquery"

	"github.com/egoist/mygo/ui"
)

// pathSeg is one breadcrumb of a hit's folder: its label and the folder it
// opens.
type pathSeg struct {
	Label, Path string
	Elided      bool
}

// searchPathSegments splits dir for display: home becomes 主文件夹, a volume
// root its volume name, and more than maxSegs segments keep the first two and
// the last two with one elided segment between.
func searchPathSegments(dir, home string, maxSegs int) []pathSeg {
	dir = cleanDisplayPath(dir)
	home = cleanDisplayPath(home)
	if dir == "" || dir == "." {
		return nil
	}
	if maxSegs < 4 {
		maxSegs = 4
	}

	root, parts, underHome := displayRoot(dir, home)
	out := make([]pathSeg, 0, min(maxSegs, len(parts)+1))
	if root != "" {
		out = append(out, pathSeg{Label: rootLabel(root, underHome), Path: root})
	}
	if len(parts) == 0 {
		return out
	}

	rootSlots := boolInt(root != "")
	if len(parts)+rootSlots > maxSegs && len(parts) > 2 {
		// The last two folders identify the hit; the rest of the room goes to
		// the leading folders, with the root and one ellipsis taking a slot.
		keepTail := 2
		keepHead := maxSegs - rootSlots - keepTail - 1
		for i := 0; i < keepHead; i++ {
			out = append(out, pathSeg{
				Label: parts[i],
				Path:  joinBreadcrumb(root, parts[:i+1]),
			})
		}
		tail := len(parts) - keepTail
		out = append(out, pathSeg{
			Label:  "…",
			Path:   joinBreadcrumb(root, parts[:min(tail, len(parts))]),
			Elided: true,
		})
		for i := tail; i < len(parts); i++ {
			out = append(out, pathSeg{
				Label: parts[i],
				Path:  joinBreadcrumb(root, parts[:i+1]),
			})
		}
		return out
	}

	for i := range parts {
		out = append(out, pathSeg{
			Label: parts[i],
			Path:  joinBreadcrumb(root, parts[:i+1]),
		})
	}
	return out
}

// displayRoot separates a folder into the root breadcrumb and the names below
// it. A relative path has no root. A UNC share is a root in itself: its server
// and share never become two clickable crumbs.
func displayRoot(dir, home string) (root string, parts []string, underHome bool) {
	underHome = home != "" && searchPathWithin(dir, home)
	switch {
	case underHome:
		root = home
		rel, _ := searchPathBelow(dir, home)
		parts = splitPath(rel)
	case isUNC(dir):
		fields := splitPath(dir)
		if len(fields) >= 2 {
			root = `\\` + fields[0] + `\` + fields[1]
			parts = fields[2:]
		} else if len(fields) == 1 {
			root = `\\` + fields[0]
		}
	case filepath.IsAbs(dir):
		vol := filepath.VolumeName(dir)
		switch {
		case vol != "" && len(vol) == 2 && vol[1] == ':':
			root = vol + `\`
		case vol != "":
			root = vol
		default:
			root = string(filepath.Separator)
		}
		parts = splitPath(strings.TrimPrefix(dir, root))
	default:
		parts = splitPath(dir)
	}
	return root, parts, underHome
}

// rootLabel names a breadcrumb's root the way the sidebar and address bar do:
// the home folder, a drive, or the platform's root volume.
func rootLabel(root string, underHome bool) string {
	switch {
	case underHome:
		return "主文件夹"
	case isUNC(root):
		return root
	case runtime.GOOS == "windows" && filepath.VolumeName(root) != "":
		return filepath.VolumeName(root)
	case root == string(filepath.Separator):
		return sysRootName()
	}
	return root
}

// searchPathText renders breadcrumb segments with the same separator the
// address bar uses, while keeping the empty result readable.
func searchPathText(segs []pathSeg) string {
	if len(segs) == 0 {
		return ""
	}
	labels := make([]string, 0, len(segs))
	for _, s := range segs {
		labels = append(labels, s.Label)
	}
	return strings.Join(labels, " › ")
}

// searchNameSpans is a search result's complete name: the query's matched
// runs keep their hit background and the extension is muted, so report.docx,
// report.md and report.txt stay distinguishable even with known extensions
// hidden elsewhere in the app.
func searchNameSpans(q searchquery.Query, name string, isDir bool, font float32) []ui.Span {
	ext := ""
	if !isDir {
		ext = filepath.Ext(name)
		if ext == name || ext == "." {
			ext = ""
		}
	}
	stem := strings.TrimSuffix(name, ext)
	spans := q.Highlight(name)
	out := make([]ui.Span, 0, len(spans)+1)
	extAt := utf8.RuneCountInString(stem)
	at := 0
	for _, s := range spans {
		if s.Text == "" {
			continue
		}
		end := at + utf8.RuneCountInString(s.Text)
		if at < extAt {
			out = append(out, nameRunSpan(name, at, min(end, extAt), s.Hit, font, false))
		}
		if end > extAt {
			out = append(out, nameRunSpan(name, max(at, extAt), end, s.Hit, font, true))
		}
		at = end
	}
	// A query that is only path: terms highlights no part, but the complete
	// name and the muted extension still have to be painted.
	if len(out) == 0 {
		if stem != "" {
			out = append(out, ui.Span{Text: stem, Size: font, Color: colorText()})
		}
		if ext != "" {
			out = append(out, ui.Span{Text: ext, Size: font, Color: colorMuted()})
		}
	}
	return out
}

func nameRunSpan(name string, from, to int, hit bool, font float32, ext bool) ui.Span {
	r := []rune(name)
	sp := ui.Span{Text: string(r[from:to]), Size: font, Color: colorText()}
	if ext {
		sp.Color = colorMuted()
	}
	if hit {
		sp.Background = colorHit()
	}
	return sp
}

func cleanDisplayPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "/" || isUNC(p) {
		return strings.TrimSuffix(p, `\`)
	}
	if vol := filepath.VolumeName(p); vol != "" && filepath.Clean(p) == vol {
		return vol + string(filepath.Separator)
	}
	p = filepath.Clean(p)
	if p == "." || p == string(filepath.Separator) {
		return p
	}
	return strings.TrimSuffix(p, string(filepath.Separator))
}

func isUNC(p string) bool {
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")
}

func joinBreadcrumb(root string, parts []string) string {
	if root == "" {
		return filepath.Join(parts...)
	}
	if isUNC(root) {
		if len(parts) == 0 {
			return root
		}
		return root + `\` + strings.Join(parts, `\`)
	}
	all := make([]string, 0, len(parts)+1)
	all = append(all, root)
	all = append(all, parts...)
	return filepath.Join(all...)
}

// searchPathWithin reports whether p is root or below it. Windows paths
// compare without case, and a separator ends the parent so /foo does not
// contain /foobar.
func searchPathWithin(p, root string) bool {
	_, ok := searchPathBelow(p, root)
	return ok
}

// searchPathBelow returns p relative to root, keeping p's spelling. Windows
// compares the two without case while the relative path still shows the names
// as the filesystem does.
func searchPathBelow(p, root string) (string, bool) {
	if root == "" {
		return "", false
	}
	origP, origRoot := p, root
	if runtime.GOOS == "windows" {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	p, root = filepath.Clean(p), filepath.Clean(root)
	if p == root {
		return "", true
	}
	sep := string(filepath.Separator)
	prefix := strings.TrimSuffix(root, sep) + sep
	if !strings.HasPrefix(p, prefix) {
		return "", false
	}
	rel := origP[len(strings.TrimSuffix(origRoot, sep))+1:]
	return rel, true
}

// searchLocationText is the pane's 位置 cell for a search hit: a compact
// breadcrumb of the folder relative to the current search root. The current
// folder itself is named rather than shown as an empty string.
func searchLocationText(e Entry, root, home string) string {
	if e.Dir == "" {
		return ""
	}
	if e.Dir == root {
		return "当前文件夹"
	}
	rel := e.Dir
	if below, ok := searchPathBelow(e.Dir, root); ok {
		rel = below
		if rel == "" {
			return "当前文件夹"
		}
	}
	return searchPathText(searchPathSegments(rel, home, 4))
}

var searchHome = sync.OnceValue(func() string {
	if home, err := os.UserHomeDir(); err == nil {
		return cleanDisplayPath(home)
	}
	return ""
})

// homeForSearch is the home folder display paths compare against. It is read
// once: the user cannot change it inside this process, and every visible row
// asks for it each frame.
func homeForSearch() string { return searchHome() }

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
