package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// openwith.go is Explorer's 打开方式 (Open with) submenu: applications
// that declare support for the file's extension in their Info.plist, run
// with `open -a`. The registry is scanned once in the background at
// startup; until it lands the submenu simply shows the default-open item.

// openWithRegistry maps extensions to the app names that claim them.
type openWithRegistry struct {
	byExt map[string][]string
}

var openWith atomic.Pointer[openWithRegistry]

// ensureOpenWithScan runs the registry scan once, when the first 打开方式
// menu needs it — launching costs none of the plutil calls.
var ensureOpenWithScanOnce atomic.Bool

func ensureOpenWithScan() {
	if ensureOpenWithScanOnce.CompareAndSwap(false, true) {
		go scanOpenWithApps()
	}
}

func openWithApps(ext string) []string {
	if r := openWith.Load(); r != nil {
		return r.byExt[strings.ToLower(ext)]
	}
	return nil
}

// scanOpenWithApps walks the system and user Applications folders, asking
// plutil for each app's Info.plist as XML, and collects the extensions its
// document types claim. The plutil calls run eight at a time — a few
// hundred subprocesses serially would take half a minute — and a fresh
// snapshot is published as results arrive, so the 打开方式 menu fills
// within a second of launch even though the scan continues behind it.
func scanOpenWithApps() {
	var bundles []string
	for _, dir := range []string{"/Applications", "/System/Applications"} {
		apps, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, a := range apps {
			if a.IsDir() && strings.HasSuffix(a.Name(), ".app") {
				bundles = append(bundles, filepath.Join(dir, a.Name()))
			}
		}
	}

	var (
		mu    sync.Mutex
		byExt = map[string][]string{}
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 8)
		done  atomic.Int32
	)
	publish := func() {
		mu.Lock()
		snapshot := make(map[string][]string, len(byExt))
		for ext, apps := range byExt {
			sorted := slices.Clone(apps)
			slices.Sort(sorted)
			snapshot[ext] = slices.Compact(sorted)
		}
		mu.Unlock()
		openWith.Store(&openWithRegistry{byExt: snapshot})
	}
	for _, bundle := range bundles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			appName := strings.TrimSuffix(filepath.Base(bundle), ".app")
			seen := map[string]bool{}
			for _, ext := range appDeclaredExts(bundle) {
				if ext == "" || seen[ext] {
					continue
				}
				seen[ext] = true
				mu.Lock()
				byExt[ext] = append(byExt[ext], appName)
				mu.Unlock()
			}
			if n := done.Add(1); n%16 == 0 {
				publish()
			}
		}()
	}
	wg.Wait()
	publish()
}

// appDeclaredExts converts an app's Info.plist to XML and returns the
// extensions its CFBundleTypeExtensions arrays claim. plutil handles the
// binary plists most apps ship; a failed conversion yields none.
func appDeclaredExts(bundle string) []string {
	plist := filepath.Join(bundle, "Contents", "Info.plist")
	if _, err := os.Stat(plist); err != nil {
		return nil
	}
	out, err := exec.Command("plutil", "-convert", "xml1", "-o", "-", plist).Output()
	if err != nil {
		return nil
	}
	return parseDeclaredExts(string(out))
}

// utiExts maps the standard UTIs apps declare in LSItemContentTypes to
// the file extensions they cover; unknown public.* UTIs fall back to
// their last dot-segment.
var utiExts = map[string][]string{
	"public.png": {"png"}, "public.jpeg": {"jpg", "jpeg"}, "public.gif": {"gif"},
	"public.tiff": {"tif", "tiff"}, "public.bmp": {"bmp"}, "public.heic": {"heic"},
	"public.svg-image": {"svg"}, "public.webp": {"webp"}, "public.ico": {"ico"},
	"public.plain-text": {"txt"}, "public.html": {"html"}, "public.xml": {"xml"},
	"public.json": {"json"}, "public.yaml": {"yaml", "yml"},
	"public.shell-script": {"sh"}, "com.sun.xml-property-list": {"plist"},
	"public.rtf": {"rtf"}, "com.netscape.javascript-source": {"js"},
	"public.python-script": {"py"}, "public.c-source": {"c"},
	"public.c-header": {"h"}, "public.c-plus-plus-source": {"cpp", "cc"},
	"public.objective-c-source": {"m"}, "public.swift-source": {"swift"},
	"public.mp3": {"mp3"}, "public.mpeg-4-audio": {"m4a"}, "public.wav-audio": {"wav"},
	"public.aiff-audio": {"aiff", "aif"}, "public.au-audio": {"au"},
	"public.mpeg-4-movie": {"mp4", "m4v"}, "public.quicktime-movie": {"mov"},
	"public.avi": {"avi"}, "public.mpeg": {"mpg", "mpeg"}, "public.mkv": {"mkv"},
	"public.zip-archive": {"zip"}, "public.tar-archive": {"tar"},
	"public.pdf": {"pdf"}, "public.rust-source": {"rs"},
	// Generic families: an app declaring one opens every member.
	"public.image":      {"png", "jpg", "jpeg", "gif", "tif", "tiff", "bmp", "webp", "heic"},
	"public.movie":      {"mov", "mp4", "m4v"},
	"public.audio":      {"mp3", "wav", "m4a", "aiff", "flac"},
	"public.text":       {"txt"},
	"public.disk-image": {"dmg", "iso"},
}

// parseDeclaredExts collects the <string> values of every
// CFBundleTypeExtensions block plus the extensions the LSItemContentTypes
// UTIs cover: the segment after that key until the next key, minus "*",
// empty values and anything with a slash.
func parseDeclaredExts(plistXML string) []string {
	var out []string
	for _, key := range []string{"CFBundleTypeExtensions", "LSItemContentTypes"} {
		// segments[0] precedes the key — and when the key is absent, it is
		// the whole document — so parse only what follows an occurrence.
		for _, seg := range strings.Split(plistXML, "<key>"+key+"</key>")[1:] {
			array, ok := cutFirst(seg, "<array>")
			if !ok {
				continue
			}
			if end := strings.Index(array, "</array>"); end >= 0 {
				array = array[:end]
			}
			for len(array) > 0 {
				rest, ok := cutFirst(array, "<string>")
				if !ok {
					break
				}
				end := strings.Index(rest, "</string>")
				if end < 0 {
					break
				}
				v := strings.TrimSpace(rest[:end])
				array = rest[end+len("</string>"):]
				if v == "" || v == "*" || strings.ContainsAny(v, "/\\") {
					continue
				}
				if exts, ok := utiExts[v]; ok {
					out = append(out, exts...)
					continue
				}
				if strings.HasPrefix(v, "public.") {
					last := v[strings.LastIndex(v, ".")+1:]
					if last != "" && len(last) <= 8 && isAlphaNum(last) {
						out = append(out, last)
					}
					continue
				}
				out = append(out, strings.TrimPrefix(strings.ToLower(v), "."))
			}
		}
	}
	return out
}

// isAlphaNum reports whether s is ASCII letters or digits.
func isAlphaNum(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// cutFirst returns what follows the first sep in s.
func cutFirst(s, sep string) (string, bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", false
	}
	return s[i+len(sep):], true
}

// openWithApp launches path with the named application.
func openWithApp(app, path string) error {
	return exec.Command("open", "-a", app, path).Run()
}

// openWithMenu builds the 打开方式 submenu for the selected file: the
// declaring applications, then a way to browse for more.
func (a *App) openWithMenu(m *ui.Menu, e Entry) {
	ensureOpenWithScan()
	shown := 0
	for _, app := range openWithApps(e.Ext) {
		if shown >= 8 {
			break
		}
		if m.Item(app).Chosen() {
			if err := openWithApp(app, e.Path); err != nil {
				a.statusMsg = "无法用 " + app + " 打开：" + err.Error()
			}
		}
		shown++
	}
	if shown > 0 {
		m.Separator()
	}
	if m.Item("在 /Applications 中查看…").Chosen() {
		a.navigate("/Applications")
	}
}
