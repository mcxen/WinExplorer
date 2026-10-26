package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// imageview.go is the image-specific Space quick look: fit-to-view at
// zoom 1, mouse-wheel zoom, drag to pan when zoomed in, and ←/→ (or the
// chrome buttons) among pictures in the same folder. The right-hand
// preview pane stays a simple Contain fit so the two surfaces do not
// duplicate interactive chrome. Decoding still goes through
// decodeBytesScaled — the same path OpenUI ImageField uses via
// formDecodeBitmap — and lands asynchronously through requestPreview.

const (
	imgZoomMin  float32 = 1
	imgZoomMax  float32 = 8
	imgZoomStep float32 = 1.12
)

// imgQuickView is the per-window zoom/pan for the open Space preview.
// path tracks which file the transform belongs to so a step to the next
// picture resets it.
type imgQuickView struct {
	path       string
	zoom       float32
	panX, panY float32
}

// resetImgQV keeps the transform when the same file is still showing and
// clears it when the selection moved.
func (a *App) resetImgQV(path string) {
	if a.imgQV.path == path && a.imgQV.zoom >= imgZoomMin {
		return
	}
	a.imgQV = imgQuickView{path: path, zoom: imgZoomMin}
}

// clampImgZoom keeps zoom in [imgZoomMin, imgZoomMax].
func clampImgZoom(z float32) float32 {
	if z < imgZoomMin {
		return imgZoomMin
	}
	if z > imgZoomMax {
		return imgZoomMax
	}
	return z
}

// applyImgWheelZoom changes zoom from a scroll delta. Positive DY (view
// moves down the content, typical wheel-down) zooms out; negative zooms in.
// At fit (zoom 1) the pan offsets clear so the picture recentres.
func applyImgWheelZoom(qv *imgQuickView, dy float32) {
	if dy == 0 {
		return
	}
	factor := imgZoomStep
	if dy > 0 {
		factor = 1 / imgZoomStep
	}
	qv.zoom = clampImgZoom(qv.zoom * factor)
	if qv.zoom <= imgZoomMin {
		qv.panX, qv.panY = 0, 0
	}
}

// paintImageBroken is the shared bad-image placeholder used by the Space
// viewer and the preview pane — same icon and muted copy as OpenUI's
// paintFormMediaBroken.
func (a *App) paintImageBroken(c *ui.Context, msg string) {
	if msg == "" {
		msg = "无法预览图片"
	}
	ui.Column(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(24, 16).Children(func() {
		components.Icon(c, lucide.ImageOff()).Size(36, 36).TextColor(colorMuted().Alpha(0.7))
		ui.Text(c, msg).FontSize(12).TextColor(colorMuted()).TextAlign(ui.Center)
	})
}

// imageQuickContent is the Space modal body for a picture: nav chrome,
// the zoomable viewport, and a short hint line.
func (a *App) imageQuickContent(c *ui.Context, st *previewState, maxW, maxH float32) {
	a.resetImgQV(st.path)
	a.imageQuickNav(c, st.path)
	a.imageQuickViewport(c, st, maxW, maxH-36)
	pct := int(a.imgQV.zoom*100 + 0.5)
	ui.Text(c, fmt.Sprintf("滚轮缩放 · 拖拽平移 · ←/→ 上一张/下一张 · 空格/Esc 关闭 · %d%%", pct)).
		FontSize(11).TextColor(colorMuted())
}

// imageQuickNav is the prev/next row above the picture.
func (a *App) imageQuickNav(c *ui.Context, path string) {
	imgs := a.sameDirImages(path)
	idx := imageIndexOf(imgs, path)
	label := filepath.Base(path)
	if idx >= 0 && len(imgs) > 0 {
		label = fmt.Sprintf("%s（%d / %d）", filepath.Base(path), idx+1, len(imgs))
	}
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		prev := ui.ButtonBase(c).Size(28, 28).Radius(rowR()).Shrink(0).Label("上一张").
			Tooltip("上一张图片（←）")
		if idx <= 0 {
			prev.Disabled(true)
		} else if prev.Hovered() || prev.Pressed() {
			prev.Background(colorHover())
		}
		prev.Children(func() {
			components.Icon(c, lucide.ChevronLeft()).Size(14, 14).TextColor(colorText())
		})
		if prev.Clicked() {
			a.quickLookStepImage(-1)
		}
		ui.Text(c, label).FontSize(12).TextColor(colorText()).Grow(1).MinWidth(0).
			TextAlign(ui.Center).MaxLines(1)
		next := ui.ButtonBase(c).Size(28, 28).Radius(rowR()).Shrink(0).Label("下一张").
			Tooltip("下一张图片（→）")
		if idx < 0 || idx >= len(imgs)-1 {
			next.Disabled(true)
		} else if next.Hovered() || next.Pressed() {
			next.Background(colorHover())
		}
		next.Children(func() {
			components.Icon(c, lucide.ChevronRight()).Size(14, 14).TextColor(colorText())
		})
		if next.Clicked() {
			a.quickLookStepImage(1)
		}
	})
}

// imageQuickViewport draws the picture (or a broken placeholder) inside a
// clipped box that takes wheel zoom and drag pan.
func (a *App) imageQuickViewport(c *ui.Context, st *previewState, maxW, maxH float32) {
	if maxW < 80 {
		maxW = 80
	}
	if maxH < 80 {
		maxH = 80
	}
	bm := st.bitmap
	if isGifPath(st.path) {
		if g := a.gifFor(st.path); g != nil {
			bm = g
		}
	}
	cur := ui.CursorGrab
	if a.imgQV.zoom > imgZoomMin {
		cur = ui.CursorGrab
	}
	box := ui.Box(c).Width(maxW).Height(maxH).Shrink(0).Clip().Cursor(cur).
		Background(colorHover().Alpha(0.2)).Radius(innerRadius())
	if dx, dy, ok := box.Dragged(); ok && a.imgQV.zoom > imgZoomMin {
		a.imgQV.panX += dx
		a.imgQV.panY += dy
		box.Cursor(ui.CursorGrabbing)
	}
	box.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputScroll {
			return false
		}
		applyImgWheelZoom(&a.imgQV, ev.DY)
		return true
	})
	if bm == nil {
		box.Children(func() {
			a.paintImageBroken(c, st.err)
		})
		return
	}
	qv := a.imgQV
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		iw, ih := bm.Size()
		if iw <= 0 || ih <= 0 {
			return
		}
		dw, dh := fitContainSize(r.W, r.H, float32(iw), float32(ih))
		dw *= qv.zoom
		dh *= qv.zoom
		dx := r.X + (r.W-dw)/2 + qv.panX
		dy := r.Y + (r.H-dh)/2 + qv.panY
		p.Image(bm, ui.Rect{X: dx, Y: dy, W: dw, H: dh}, ui.FillBox)
	})
	box.Children(func() {})
}

// fitContainSize is the size that fits (iw, ih) inside (vw, vh) keeping
// aspect ratio, the same rule ui.Contain uses for Image.
func fitContainSize(vw, vh, iw, ih float32) (dw, dh float32) {
	if iw <= 0 || ih <= 0 || vw <= 0 || vh <= 0 {
		return max(vw, 1), max(vh, 1)
	}
	s := min(vw/iw, vh/ih)
	return max(iw*s, 1), max(ih*s, 1)
}

// sameDirImages lists the picture files in the same folder as path, in the
// order the file pane currently shows them (visibleEntries). Non-images
// and files from other folders are skipped so ←/→ stay on a photo strip.
func (a *App) sameDirImages(path string) []Entry {
	dir := filepath.Dir(path)
	vis := a.visibleEntries()
	out := make([]Entry, 0, len(vis))
	for _, e := range vis {
		if e.IsDir || e.Path == "" {
			continue
		}
		if filepath.Dir(e.Path) != dir {
			continue
		}
		ext := e.Ext
		if ext == "" {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(e.Path), "."))
		}
		if !isImageExt(ext) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func imageIndexOf(imgs []Entry, path string) int {
	for i, e := range imgs {
		if e.Path == path {
			return i
		}
	}
	return -1
}

// quickLookStepImage moves the selection to the neighbouring picture in
// the same folder. The ends hold; it never wraps and never lands on a
// non-image the way the generic quickLookStep can.
func (a *App) quickLookStepImage(dir int) {
	cur := a.singleSelected()
	if cur.Path == "" {
		return
	}
	imgs := a.sameDirImages(cur.Path)
	if len(imgs) == 0 {
		return
	}
	i := imageIndexOf(imgs, cur.Path)
	if i < 0 {
		i = 0
	} else {
		i += dir
	}
	if i < 0 || i >= len(imgs) {
		return
	}
	a.selectOnly(imgs[i].Path)
}
