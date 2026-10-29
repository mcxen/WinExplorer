package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// Monochrome command icons come from Onevra's Lucide set and render through
// components.Icon, inheriting the text color of their container. The colorful
// Windows 11 file, folder and place glyphs below stay hand-drawn with Painter
// paths inside a square box of side px: they carry the Explorer identity that
// a monochrome icon set cannot. Callers pass a *ui.Context; the contract's
// *ui.Context parameter is the element context, not a painter.

func (a *App) fileIcon(c *ui.Context, e Entry, px float32) {
	k := kindOf(e)
	if k == kindApp || appIconExt(iconExt(e)) {
		// The application's own icon, decoded in the background and cached;
		// the drawn glyph shows until (or unless) there is one.
		if bm := appIconThumb(e, px); bm != nil {
			ui.Box(c).Size(px, px).Shrink(0).Children(func() {
				ui.Image(c, bm).Fit(ui.Contain).Size(px, px)
				if e.Link || k == kindShortcut {
					linkBadge(c, px)
				}
			})
			return
		}
	}
	ui.Box(c).Size(px, px).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		drawKind(p, r, k)
	}).Children(func() {
		if e.Link || k == kindShortcut {
			linkBadge(c, px)
		}
	})
}

// linkBadge is the shortcut overlay on a symbolic link's icon: a small
// white box with an arrow at the bottom-left corner, as Explorer's.
func linkBadge(c *ui.Context, px float32) {
	s := max(8, px*0.45)
	ui.Box(c.Key("link")).Absolute().Left(0).Bottom(0).Size(s, s).Radius(2).
		Background(ui.Hex("#FFFFFF")).Border(1, ui.RGBA(0, 0, 0, 0.25)).Center().Children(func() {
		components.Icon(c, lucide.ArrowUpRight).Size(s-2, s-2).TextColor(ui.Hex("#1A5FB4"))
	})
}

func (a *App) placeIcon(c *ui.Context, kind string, px float32) {
	ui.Box(c).Size(px, px).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		switch kind {
		case "home":
			drawHome(p, r)
		case "desktop":
			drawDesktop(p, r)
		case "downloads":
			drawDownloads(p, r)
		case "documents":
			drawDocuments(p, r)
		case "pictures":
			drawPictures(p, r)
		case "music":
			drawMusic(p, r)
		case "videos":
			drawVideos(p, r)
		case "drive":
			drawDrive(p, r)
		case "trash":
			drawTrash(p, r)
		case "gallery":
			drawPictures(p, r)
		case "thispc":
			drawThisPC(p, r)
		default:
			drawFolder(p, r, colorFolder())
		}
	})
}

// chevronIcon is a small Lucide disclosure chevron: right when collapsed,
// down when open, in the muted color the tree node arrows use.
func chevronIcon(c *ui.Context, open bool, px float32) ui.Element {
	icon := lucide.ChevronRight
	if open {
		icon = lucide.ChevronDown
	}
	return components.Icon(c, icon).Size(px, px).TextColor(colorMuted())
}

// chevronIconAnimated keeps one right-facing glyph and rotates it through the
// disclosure transition. The key belongs to the row path so a virtualized
// tree row keeps its phase when it scrolls out and back.
func chevronIconAnimated(c *ui.Context, key string, open bool, px float32) ui.Element {
	target := float32(0)
	if open {
		target = 90
	}
	icon := components.Icon(c, lucide.ChevronRight).Size(px, px).TextColor(colorMuted())
	angle := icon.AnimateWith(key, target, 150*time.Millisecond, ui.EaseOut)
	return icon.Rotate(angle)
}

// chevronUpDown names the details header's sort direction: up when ascending.
func chevronUpDown(ascending bool) func() *ui.SVG {
	if ascending {
		return lucide.ChevronUp
	}
	return lucide.ChevronDown
}

// fileTint is the sheet color of an extension's category.
func fileTint(ext string) ui.Color {
	return kindTint(kindOf(Entry{Name: "x." + ext, Ext: ext}))
}

func drawFolder(p *ui.Painter, r ui.Rect, fill ui.Color) {
	x, y, s := r.X, r.Y, r.W
	tabH := s * 0.16
	body := ui.Rect{X: x + s*0.06, Y: y + s*0.28, W: s * 0.88, H: s * 0.58}
	rad := s * 0.08
	var tab ui.Path
	tab.MoveTo(x+s*0.08, y+s*0.30)
	tab.LineTo(x+s*0.08, y+s*0.16)
	tab.QuadTo(x+s*0.08, y+s*0.10, x+s*0.16, y+s*0.10)
	tab.LineTo(x+s*0.40, y+s*0.10)
	tab.QuadTo(x+s*0.48, y+s*0.10, x+s*0.50, y+s*0.18)
	tab.LineTo(x+s*0.54, y+s*0.30)
	tab.Close()
	p.FillPath(&tab, fill.Mix(ui.RGB(0, 0, 0), 0.12))
	p.Fill(body, fill, rad)
	// A lip only once the box is large enough for it to be a separate edge.
	if s >= 24 {
		p.Fill(ui.Rect{X: body.X, Y: body.Y, W: body.W, H: tabH * 0.45}, ui.RGBA(255, 255, 255, 0.28), rad)
	}
}

func drawFileSheet(p *ui.Painter, r ui.Rect, tint, ink ui.Color) {
	x, y, s := r.X, r.Y, r.W
	fold := s * 0.30
	sheet := ui.Rect{X: x + s*0.16, Y: y + s*0.06, W: s * 0.68, H: s * 0.88}
	p.Fill(sheet, ui.RGB(255, 255, 255), s*0.06)
	p.Stroke(sheet, tint, s*0.06, s*0.045)
	var ear ui.Path
	ear.MoveTo(x+s*0.16+sheet.W-fold, y+s*0.06)
	ear.LineTo(x+s*0.16+sheet.W, y+s*0.06+fold)
	ear.LineTo(x+s*0.16+sheet.W-fold, y+s*0.06+fold)
	ear.Close()
	p.FillPath(&ear, tint.Alpha(0.85))
	if s >= 28 {
		line := ink.Alpha(0.55)
		for i, w := range []float32{0.62, 0.50, 0.58} {
			ly := y + s*(0.50+float32(i)*0.12)
			p.Fill(ui.Rect{X: x + s*0.28, Y: ly, W: s * w * 0.55, H: s * 0.04}, line, s*0.02)
		}
	}
}

// drawPDFSheet is the list/large-icon glyph for PDFs: a white folded sheet
// edged in Adobe red, with a red label strip that reads as a type badge even
// at 16 px where lettering would not fit.
func drawPDFSheet(p *ui.Painter, r ui.Rect) {
	tint := fileTint("pdf")
	drawFileSheet(p, r, tint, colorText())
	x, y, s := r.X, r.Y, r.W
	badge := ui.Rect{X: x + s*0.28, Y: y + s*0.62, W: s * 0.44, H: s * 0.18}
	p.Fill(badge, tint, s*0.03)
	if s >= 28 {
		// Three short white ticks stand in for the "PDF" mark without text.
		ink := ui.RGB(255, 255, 255)
		for i, w := range []float32{0.28, 0.22, 0.26} {
			lx := x + s*(0.34+float32(i)*0.10)
			p.Fill(ui.Rect{X: lx, Y: y + s*0.67, W: s * 0.055, H: s * w * 0.35}, ink, s*0.01)
		}
	}
}

// drawHTMLSheet is the glyph for .html/.htm: the same folded sheet in
// browser orange, topped with a three-dot chrome bar so it reads as a page
// in a browser rather than a code file.
func drawHTMLSheet(p *ui.Painter, r ui.Rect) {
	tint := fileTint("html")
	drawFileSheet(p, r, tint, colorText())
	x, y, s := r.X, r.Y, r.W
	bar := ui.Rect{X: x + s*0.22, Y: y + s*0.42, W: s * 0.48, H: s * 0.12}
	p.Fill(bar, tint.Alpha(0.85), s*0.02)
	dot := ui.RGB(255, 255, 255)
	for _, f := range []float32{0.28, 0.38, 0.48} {
		var c ui.Path
		c.Circle(x+s*f, y+s*0.48, s*0.025)
		p.FillPath(&c, dot)
	}
	if s >= 28 {
		p.Fill(ui.Rect{X: x + s*0.28, Y: y + s*0.60, W: s * 0.36, H: s * 0.04}, tint.Alpha(0.45), s*0.02)
		p.Fill(ui.Rect{X: x + s*0.28, Y: y + s*0.70, W: s * 0.28, H: s * 0.04}, tint.Alpha(0.35), s*0.02)
	}
}

func drawDrive(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	body := ui.Rect{X: x + s*0.08, Y: y + s*0.22, W: s * 0.84, H: s * 0.56}
	p.Fill(body, ui.Hex("#4B5563"), s*0.1)
	p.Fill(ui.Rect{X: x + s*0.18, Y: y + s*0.34, W: s * 0.28, H: s * 0.12}, ui.Hex("#D1D5DB"), s*0.04)
	p.Fill(ui.Rect{X: x + s*0.62, Y: y + s*0.56, W: s * 0.16, H: s * 0.10}, ui.Hex("#60A5FA"), s*0.04)
}

// drawTrash is the 回收站 bin: the Win11 recycle bin's translucent slate
// can, with a lid, a handle and lighter ribs down its body.
func drawTrash(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	ink := ui.Hex("#64748B")
	p.Fill(ui.Rect{X: x + s*0.38, Y: y + s*0.08, W: s * 0.24, H: s * 0.08}, ink, s*0.02)
	p.Fill(ui.Rect{X: x + s*0.10, Y: y + s*0.17, W: s * 0.80, H: s * 0.10}, ink, s*0.03)
	body := ui.Rect{X: x + s*0.17, Y: y + s*0.30, W: s * 0.66, H: s * 0.58}
	p.Fill(body, ink.Alpha(0.55), s*0.05)
	p.Stroke(body, ink, 1, s*0.05)
	for _, f := range []float32{0.34, 0.5, 0.66} {
		p.Line(x+s*f, y+s*0.38, x+s*f, y+s*0.80, s*0.055, ui.RGBA(255, 255, 255, 0.4))
	}
}

func drawDesktop(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	screen := ui.Rect{X: x + s*0.08, Y: y + s*0.10, W: s * 0.84, H: s * 0.58}
	p.Fill(screen, ui.Hex("#1F2937"), s*0.08)
	p.Fill(ui.Rect{X: x + s*0.14, Y: y + s*0.18, W: s * 0.72, H: s * 0.42}, ui.Hex("#60A5FA"), s*0.04)
	p.Fill(ui.Rect{X: x + s*0.40, Y: y + s*0.70, W: s * 0.20, H: s * 0.10}, colorMuted(), s*0.02)
	p.Fill(ui.Rect{X: x + s*0.26, Y: y + s*0.80, W: s * 0.48, H: s * 0.07}, colorMuted(), s*0.03)
}

func drawDownloads(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	ink := colorAccent()
	cx := x + s*0.5
	p.Line(cx, y+s*0.12, cx, y+s*0.58, s*0.09, ink)
	var head ui.Path
	head.MoveTo(cx-s*0.22, y+s*0.46)
	head.LineTo(cx, y+s*0.70)
	head.LineTo(cx+s*0.22, y+s*0.46)
	head.Close()
	p.FillPath(&head, ink)
	p.Line(x+s*0.18, y+s*0.82, x+s*0.82, y+s*0.82, s*0.09, ink)
}

func drawDocuments(p *ui.Painter, r ui.Rect) {
	drawFileSheet(p, r, ui.Hex("#2563EB"), colorText())
}

func drawPictures(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	frame := ui.Rect{X: x + s*0.10, Y: y + s*0.14, W: s * 0.80, H: s * 0.70}
	p.Fill(frame, ui.Hex("#DBEAFE"), s*0.08)
	p.Stroke(frame, ui.Hex("#3B82F6"), s*0.08, s*0.05)
	var sun ui.Path
	sun.Circle(x+s*0.32, y+s*0.36, s*0.08)
	p.FillPath(&sun, ui.Hex("#F59E0B"))
	var hill ui.Path
	hill.MoveTo(x+s*0.16, y+s*0.72)
	hill.LineTo(x+s*0.40, y+s*0.48)
	hill.LineTo(x+s*0.56, y+s*0.64)
	hill.LineTo(x+s*0.74, y+s*0.44)
	hill.LineTo(x+s*0.84, y+s*0.72)
	hill.Close()
	p.FillPath(&hill, ui.Hex("#10B981"))
}

func drawMusic(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	ink := ui.Hex("#DB2777")
	p.Line(x+s*0.38, y+s*0.18, x+s*0.38, y+s*0.62, s*0.07, ink)
	p.Line(x+s*0.70, y+s*0.28, x+s*0.70, y+s*0.70, s*0.07, ink)
	p.Fill(ui.Rect{X: x + s*0.38, Y: y + s*0.16, W: s * 0.34, H: s * 0.08}, ink, s*0.02)
	var n1, n2 ui.Path
	n1.Circle(x+s*0.30, y+s*0.68, s*0.12)
	n2.Circle(x+s*0.62, y+s*0.76, s*0.12)
	p.FillPath(&n1, ink)
	p.FillPath(&n2, ink)
}

func drawVideos(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	body := ui.Rect{X: x + s*0.06, Y: y + s*0.24, W: s * 0.62, H: s * 0.52}
	p.Fill(body, ui.Hex("#7C3AED"), s*0.08)
	var lens ui.Path
	lens.MoveTo(x+s*0.70, y+s*0.34)
	lens.LineTo(x+s*0.94, y+s*0.24)
	lens.LineTo(x+s*0.94, y+s*0.76)
	lens.LineTo(x+s*0.70, y+s*0.66)
	lens.Close()
	p.FillPath(&lens, ui.Hex("#7C3AED"))
}

func drawThisPC(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	p.Fill(ui.Rect{X: x + s*0.16, Y: y + s*0.10, W: s * 0.68, H: s * 0.46}, ui.Hex("#334155"), s*0.06)
	p.Fill(ui.Rect{X: x + s*0.22, Y: y + s*0.16, W: s * 0.56, H: s * 0.32}, ui.Hex("#38BDF8"), s*0.04)
	p.Fill(ui.Rect{X: x + s*0.30, Y: y + s*0.62, W: s * 0.40, H: s * 0.22}, ui.Hex("#64748B"), s*0.08)
}

// drawHome is the 主文件夹 house glyph.
func drawHome(p *ui.Painter, r ui.Rect) {
	x, y, s := r.X, r.Y, r.W
	ink := ui.Hex("#0EA5E9")
	var roof ui.Path
	roof.MoveTo(x+s*0.10, y+s*0.52)
	roof.LineTo(x+s*0.50, y+s*0.14)
	roof.LineTo(x+s*0.90, y+s*0.52)
	p.StrokePath(&roof, s*0.10, ink)
	body := ui.Rect{X: x + s*0.22, Y: y + s*0.50, W: s * 0.56, H: s * 0.36}
	p.Stroke(body, ink, s*0.04, s*0.10)
	door := ui.Rect{X: x + s*0.44, Y: y + s*0.64, W: s * 0.12, H: s * 0.22}
	p.Fill(door, ink, s*0.02)
}
