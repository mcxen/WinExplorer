package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// fileicons.go 把扩展名归到图标类别，并给每类画一个类别记号：同一张折角纸
// 加上图片、播放、音符、拉链、尖括号等，或应用、安装包、磁盘映像、快捷方式
// 的独立形状。匹配不分大小写，.tar.gz 这类复合扩展名按整体识别。

type iconKind uint8

const (
	kindFile iconKind = iota
	kindFolder
	kindApp       // .app 包、.exe、.desktop、.AppImage
	kindInstaller // .msi .pkg .deb .rpm .apk …
	kindDisk      // .dmg .iso .img .vhd …
	kindArchive
	kindImage
	kindVideo
	kindAudio
	kindPDF
	kindHTML
	kindWord
	kindSheet
	kindSlides
	kindText
	kindCode
	kindScript
	kindFont
	kindTorrent
	kindShortcut
)

var extKinds = map[string]iconKind{}

func init() {
	add := func(k iconKind, exts ...string) {
		for _, x := range exts {
			extKinds[x] = k
		}
	}
	add(kindApp, "exe", "com", "scr", "app", "appimage", "desktop", "xapk", "ipa")
	add(kindInstaller, "msi", "msix", "appx", "msixbundle", "appxbundle", "pkg", "mpkg", "deb", "rpm", "apk", "aab", "snap", "flatpak", "flatpakref", "cab")
	add(kindDisk, "dmg", "iso", "img", "vhd", "vhdx", "vmdk", "qcow2", "vdi", "toast", "cdr", "sparseimage", "sparsebundle", "cue", "nrg", "wim", "esd")
	add(kindArchive, "zip", "7z", "rar", "tar", "gz", "tgz", "bz2", "tbz", "tbz2", "xz", "txz", "zst", "tzst", "lz", "lz4", "lzma", "z", "br", "cpio", "jar", "war", "xpi", "crx", "zipx", "arj", "lzh", "ace", "sz")
	add(kindImage, "png", "jpg", "jpeg", "jfif", "gif", "webp", "bmp", "heic", "heif", "tif", "tiff", "svg", "ico", "icns", "avif", "raw", "cr2", "cr3", "nef", "arw", "dng", "psd", "ai", "xcf", "eps", "jxl", "tga", "dds", "sketch", "fig")
	add(kindVideo, "mp4", "m4v", "mov", "mkv", "avi", "webm", "wmv", "flv", "mpg", "mpeg", "3gp", "mts", "m2ts", "vob", "ogv", "rmvb", "rm", "f4v")
	add(kindAudio, "mp3", "wav", "flac", "aac", "m4a", "ogg", "oga", "opus", "wma", "aiff", "aif", "alac", "ape", "mid", "midi", "amr", "caf")
	add(kindPDF, "pdf", "xps", "oxps", "djvu")
	add(kindHTML, "html", "htm", "xhtml", "mht", "mhtml", "url", "webloc")
	add(kindWord, "doc", "docx", "docm", "dot", "dotx", "rtf", "rtfd", "odt", "pages", "wps", "epub", "mobi", "azw3")
	add(kindSheet, "xls", "xlsx", "xlsm", "xlsb", "ods", "csv", "tsv", "numbers", "et")
	add(kindSlides, "ppt", "pptx", "pptm", "pps", "ppsx", "odp", "key", "dps")
	add(kindText, "txt", "md", "markdown", "log", "rst", "tex", "nfo", "org", "adoc", "srt", "ass", "vtt", "lrc")
	add(kindCode, "go", "ts", "mts", "cts", "js", "mjs", "cjs", "jsx", "tsx", "py", "pyw", "ipynb", "rs", "c", "h", "cc", "cpp", "cxx", "hpp", "hh", "m", "mm", "java", "kt", "kts", "scala", "groovy", "gradle", "cs", "fs", "vb", "swift", "dart", "rb", "php", "pl", "lua", "r", "jl", "zig", "nim", "hs", "ex", "exs", "erl", "clj", "elm", "vue", "svelte", "sql", "proto", "graphql",
		"json", "jsonc", "json5", "yaml", "yml", "toml", "ini", "cfg", "conf", "env", "properties", "xml", "plist", "css", "scss", "sass", "less", "lock", "mod", "sum", "diff", "patch", "makefile", "cmake", "dockerfile", "wasm")
	add(kindScript, "sh", "bash", "zsh", "fish", "bat", "cmd", "ps1", "psm1", "vbs", "command", "tool", "applescript", "scpt", "awk")
	add(kindFont, "ttf", "otf", "ttc", "otc", "woff", "woff2", "eot", "fon", "fnt", "pfb", "pfm")
	add(kindTorrent, "torrent", "magnet")
	add(kindShortcut, "lnk", "alias")
}

// compoundExts 是整体识别的多段扩展名（小写，不带前导点）。
var compoundExts = []string{"tar.gz", "tar.bz2", "tar.xz", "tar.zst", "tar.lz4", "tar.lz", "tar.lzma", "tar.z", "tar.br", "pkg.tar.zst", "pkg.tar.xz"}

// iconExt is the extension an icon is chosen by: a compound one such as
// tar.gz when the name ends with it, else e.Ext (or the name's own when
// the caller left Ext empty).
func iconExt(e Entry) string {
	l := strings.ToLower(e.Name)
	for _, c := range compoundExts {
		if strings.HasSuffix(l, "."+c) {
			return c
		}
	}
	if e.Ext != "" {
		return strings.ToLower(e.Ext)
	}
	return lowerExt(e.Name)
}

// kindOf is the icon category of e. Directories are folders except macOS
// application bundles (.app), which show as applications.
func kindOf(e Entry) iconKind {
	if e.IsDir {
		if isAppBundleName(e.Name) {
			return kindApp
		}
		return kindFolder
	}
	x := iconExt(e)
	if strings.HasPrefix(x, "tar.") || strings.HasPrefix(x, "pkg.tar") {
		return kindArchive
	}
	if k, ok := extKinds[x]; ok {
		return k
	}
	switch strings.ToLower(e.Name) {
	case "makefile", "dockerfile", "cmakelists.txt", "go.mod", "go.sum", "cargo.lock":
		return kindCode
	case "readme", "license", "changelog", "authors":
		return kindText
	}
	return kindFile
}

// kindTint is the sheet edge color of a category.
func kindTint(k iconKind) ui.Color {
	switch k {
	case kindImage:
		return ui.Hex("#3B82F6")
	case kindVideo:
		return ui.Hex("#7C3AED")
	case kindAudio:
		return ui.Hex("#DB2777")
	case kindArchive:
		return ui.Hex("#B45309")
	case kindPDF:
		return ui.Hex("#DC2626")
	case kindHTML:
		return ui.Hex("#EA580C")
	case kindWord:
		return ui.Hex("#2563EB")
	case kindSheet:
		return ui.Hex("#15803D")
	case kindSlides:
		return ui.Hex("#C2410C")
	case kindCode:
		return ui.Hex("#0F766E")
	case kindScript:
		return ui.Hex("#334155")
	case kindFont:
		return ui.Hex("#9333EA")
	case kindTorrent:
		return ui.Hex("#16A34A")
	case kindText:
		return ui.Hex("#4B5563")
	default:
		return ui.Hex("#6B7280")
	}
}

// drawKind paints e's category glyph into the square r.
func drawKind(p *ui.Painter, r ui.Rect, k iconKind) {
	switch k {
	case kindFolder:
		drawFolder(p, r, colorFolder())
	case kindApp:
		drawAppGlyph(p, r)
	case kindInstaller:
		drawInstaller(p, r)
	case kindDisk:
		drawDiskImage(p, r)
	case kindPDF:
		drawPDFSheet(p, r)
	case kindHTML:
		drawHTMLSheet(p, r)
	case kindShortcut:
		drawFileSheet(p, r, kindTint(kindFile), colorText())
	case kindWord, kindText, kindFile:
		drawFileSheet(p, r, kindTint(k), colorText())
	default:
		drawMarkedSheet(p, r, k)
	}
}

// drawMarkedSheet is the folded sheet with the category's mark in its lower
// half instead of text lines: the mark reads even at 16 px.
func drawMarkedSheet(p *ui.Painter, r ui.Rect, k iconKind) {
	tint := kindTint(k)
	drawSheetOnly(p, r, tint)
	x, y, s := r.X, r.Y, r.W
	// The mark's box: centered on the sheet below the fold.
	m := ui.Rect{X: x + s*0.30, Y: y + s*0.46, W: s * 0.40, H: s * 0.36}
	lw := max(1, s*0.06)
	switch k {
	case kindImage:
		var sun ui.Path
		sun.Circle(m.X+m.W*0.25, m.Y+m.H*0.22, m.W*0.13)
		p.FillPath(&sun, ui.Hex("#F59E0B"))
		var hill ui.Path
		hill.MoveTo(m.X, m.Y+m.H)
		hill.LineTo(m.X+m.W*0.40, m.Y+m.H*0.40)
		hill.LineTo(m.X+m.W*0.62, m.Y+m.H*0.70)
		hill.LineTo(m.X+m.W*0.80, m.Y+m.H*0.50)
		hill.LineTo(m.X+m.W, m.Y+m.H)
		hill.Close()
		p.FillPath(&hill, tint)
	case kindVideo:
		var tri ui.Path
		tri.MoveTo(m.X+m.W*0.20, m.Y)
		tri.LineTo(m.X+m.W*0.95, m.Y+m.H*0.5)
		tri.LineTo(m.X+m.W*0.20, m.Y+m.H)
		tri.Close()
		p.FillPath(&tri, tint)
	case kindAudio:
		p.Line(m.X+m.W*0.62, m.Y, m.X+m.W*0.62, m.Y+m.H*0.80, lw, tint)
		p.Line(m.X+m.W*0.62, m.Y+lw/2, m.X+m.W*0.95, m.Y+m.H*0.18, lw, tint)
		var n ui.Path
		n.Circle(m.X+m.W*0.42, m.Y+m.H*0.80, m.W*0.22)
		p.FillPath(&n, tint)
	case kindArchive:
		// A zipper down the middle: alternating teeth.
		cx := m.X + m.W*0.5
		tooth := m.H / 5
		for i := 0; i < 5; i++ {
			off := -m.W * 0.16
			if i%2 == 1 {
				off = 0
			}
			p.Fill(ui.Rect{X: cx + off, Y: m.Y - s*0.30 + float32(i)*tooth, W: m.W * 0.16, H: tooth * 0.8}, tint, 0)
		}
		p.Fill(ui.Rect{X: cx - m.W*0.20, Y: m.Y + m.H*0.45, W: m.W * 0.40, H: m.H * 0.50}, tint, s*0.03)
	case kindSheet:
		g := tint
		p.Stroke(m, g, s*0.02, max(1, s*0.04))
		p.Line(m.X, m.Y+m.H/2, m.X+m.W, m.Y+m.H/2, max(1, s*0.04), g)
		p.Line(m.X+m.W/2, m.Y, m.X+m.W/2, m.Y+m.H, max(1, s*0.04), g)
	case kindSlides:
		p.Fill(ui.Rect{X: m.X, Y: m.Y, W: m.W, H: m.H * 0.70}, tint, s*0.03)
		p.Line(m.X+m.W/2, m.Y+m.H*0.70, m.X+m.W/2, m.Y+m.H, max(1, s*0.04), tint)
	case kindCode, kindScript:
		var l, rr ui.Path
		l.MoveTo(m.X+m.W*0.35, m.Y+m.H*0.10)
		l.LineTo(m.X, m.Y+m.H*0.5)
		l.LineTo(m.X+m.W*0.35, m.Y+m.H*0.90)
		rr.MoveTo(m.X+m.W*0.65, m.Y+m.H*0.10)
		rr.LineTo(m.X+m.W, m.Y+m.H*0.5)
		rr.LineTo(m.X+m.W*0.65, m.Y+m.H*0.90)
		p.StrokePath(&l, lw, tint)
		p.StrokePath(&rr, lw, tint)
		if k == kindScript {
			p.Line(m.X+m.W*0.40, m.Y+m.H, m.X+m.W*0.60, m.Y, max(1, s*0.04), tint)
		}
	case kindFont:
		// A capital A.
		var a ui.Path
		a.MoveTo(m.X+m.W*0.05, m.Y+m.H)
		a.LineTo(m.X+m.W*0.5, m.Y)
		a.LineTo(m.X+m.W*0.95, m.Y+m.H)
		p.StrokePath(&a, lw, tint)
		p.Line(m.X+m.W*0.25, m.Y+m.H*0.62, m.X+m.W*0.75, m.Y+m.H*0.62, lw, tint)
	case kindTorrent:
		cx := m.X + m.W/2
		p.Line(cx, m.Y, cx, m.Y+m.H*0.6, lw, tint)
		var head ui.Path
		head.MoveTo(m.X+m.W*0.15, m.Y+m.H*0.45)
		head.LineTo(cx, m.Y+m.H*0.85)
		head.LineTo(m.X+m.W*0.85, m.Y+m.H*0.45)
		head.Close()
		p.FillPath(&head, tint)
		p.Line(m.X, m.Y+m.H, m.X+m.W, m.Y+m.H, max(1, s*0.04), tint)
	}
}

// drawSheetOnly is drawFileSheet without the text lines.
func drawSheetOnly(p *ui.Painter, r ui.Rect, tint ui.Color) {
	x, y, s := r.X, r.Y, r.W
	fold := s * 0.30
	sheet := ui.Rect{X: x + s*0.16, Y: y + s*0.06, W: s * 0.68, H: s * 0.88}
	p.Fill(sheet, ui.RGB(255, 255, 255), s*0.06)
	p.Stroke(sheet, tint, s*0.06, s*0.045)
	var ear ui.Path
	ear.MoveTo(sheet.X+sheet.W-fold, sheet.Y)
	ear.LineTo(sheet.X+sheet.W, sheet.Y+fold)
	ear.LineTo(sheet.X+sheet.W-fold, sheet.Y+fold)
	ear.Close()
	p.FillPath(&ear, tint.Alpha(0.85))
}

// drawAppGlyph is the generic application: a rounded blue square holding a
// window with a title bar, the stand-in until (or instead of) the app's own.
func drawAppGlyph(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	body := ui.Rect{X: x + s*0.08, Y: y + s*0.08, W: s * 0.84, H: s * 0.84}
	p.Fill(body, ui.Hex("#2563EB"), s*0.20)
	win := ui.Rect{X: x + s*0.24, Y: y + s*0.28, W: s * 0.52, H: s * 0.44}
	p.Fill(win, ui.RGB(255, 255, 255), s*0.05)
	p.Fill(ui.Rect{X: win.X, Y: win.Y, W: win.W, H: win.H * 0.26}, ui.Hex("#93C5FD"), s*0.05)
}

// drawInstaller is a package: a brown box with a tape band and a down arrow.
func drawInstaller(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	box := ui.Rect{X: x + s*0.10, Y: y + s*0.22, W: s * 0.80, H: s * 0.66}
	p.Fill(box, ui.Hex("#D97706"), s*0.06)
	p.Fill(ui.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H * 0.24}, ui.Hex("#B45309"), s*0.06)
	p.Fill(ui.Rect{X: x + s*0.42, Y: box.Y, W: s * 0.16, H: box.H}, ui.Hex("#FDE68A"), 0)
	ink := ui.Hex("#1D4ED8")
	cx := x + s*0.5
	p.Line(cx, y+s*0.06, cx, y+s*0.40, max(1, s*0.09), ink)
	var head ui.Path
	head.MoveTo(cx-s*0.16, y+s*0.30)
	head.LineTo(cx, y+s*0.48)
	head.LineTo(cx+s*0.16, y+s*0.30)
	head.Close()
	p.FillPath(&head, ink)
}

// drawDiskImage is a disc: a silver platter with a hole and a sheen.
func drawDiskImage(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	cx, cy := x+s*0.5, y+s*0.5
	var disc ui.Path
	disc.Circle(cx, cy, s*0.42)
	p.FillPath(&disc, ui.Hex("#CBD5E1"))
	p.StrokePath(&disc, max(1, s*0.04), ui.Hex("#64748B"))
	var ring ui.Path
	ring.Circle(cx, cy, s*0.18)
	p.FillPath(&ring, ui.Hex("#94A3B8"))
	var hole ui.Path
	hole.Circle(cx, cy, s*0.07)
	p.FillPath(&hole, ui.Hex("#F8FAFC"))
	if s >= 24 {
		var sheen ui.Path
		sheen.MoveTo(cx+s*0.10, cy-s*0.34)
		sheen.QuadTo(cx+s*0.30, cy-s*0.28, cx+s*0.34, cy-s*0.10)
		p.StrokePath(&sheen, s*0.04, ui.RGBA(255, 255, 255, 0.8))
	}
}
