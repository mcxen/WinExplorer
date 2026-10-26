package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// videoview.go is the video-specific Space quick look: a keyframe still,
// a thin scrub bar that pulls frames through ffmpeg (media.go), and a
// 「用系统播放器打开」button. There is no embedded player — Space is for
// glancing at frames; Enter / the button / the play hint hand playback
// to the OS, matching docs/UI.md and the PREVIEW-DEMO mockup.

const (
	vidScrubDebounce = 120 * time.Millisecond
	vidFrameSide     = 1024
)

// vidQuickView is the per-window scrub state for the open Space preview.
// path tracks which clip the transform belongs to so a step to another
// file resets it. frame is the scrubbed still (or the poster until the
// first scrub lands); frac is 0..1 along the timeline.
type vidQuickView struct {
	path     string
	frac     float64
	duration float64
	frame    *ui.Bitmap
	err      string
	loading  bool
	// applied is the gen of the frame currently shown; request bumps
	// pending so a slow ffmpeg cannot overwrite a newer scrub.
	applied uint64
}

// resetVidQV keeps scrub position when the same clip is still showing and
// clears it when the selection moved. Seeds the frame from the poster.
func (a *App) resetVidQV(path string, duration float64, poster *ui.Bitmap) {
	if a.vidQV.path == path && a.vidQV.duration == duration {
		if a.vidQV.frame == nil && poster != nil {
			a.vidQV.frame = poster
		}
		return
	}
	frac := 0.0
	if duration > 2 {
		// Match videoPosterImage's one-second sample when the clip is long
		// enough that 1s is a meaningful start.
		frac = 1 / duration
		if frac > 1 {
			frac = 1
		}
	}
	a.vidQV = vidQuickView{
		path:     path,
		frac:     frac,
		duration: duration,
		frame:    poster,
	}
}

// videoAtSec is the timeline second the scrub knob maps to.
func videoAtSec(frac, duration float64) float64 {
	if duration <= 0 {
		return 0
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	return frac * duration
}

// clampVidFrac keeps a scrub fraction in [0, 1].
func clampVidFrac(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// requestVideoFrame schedules an ffmpeg extract at atSec after a short
// debounce. A newer scrub cancels an in-flight one via gen.
func (a *App) requestVideoFrame(path string, atSec float64) {
	gen := atomic.AddUint64(&a.vidFrameGen, 1)
	a.vidQV.loading = true
	go func() {
		time.Sleep(vidScrubDebounce)
		if atomic.LoadUint64(&a.vidFrameGen) != gen {
			return
		}
		img, err := videoFrameAt(path, atSec, vidFrameSide)
		if atomic.LoadUint64(&a.vidFrameGen) != gen {
			return
		}
		var bm *ui.Bitmap
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		} else if img != nil {
			bm = ui.NewBitmap(img)
		} else {
			errMsg = "无法提取关键帧"
		}
		apply := func() {
			if atomic.LoadUint64(&a.vidFrameGen) != gen || a.vidQV.path != path {
				return
			}
			a.vidQV.applied = gen
			a.vidQV.loading = false
			if bm != nil {
				a.vidQV.frame = bm
				a.vidQV.err = ""
			} else {
				a.vidQV.err = errMsg
			}
			repaint()
		}
		if mygo.App.IsReady() {
			mygo.RunOnMain(apply)
		} else {
			apply()
		}
	}()
}

// setVidScrubFrac updates the knob and asks for a new frame.
func (a *App) setVidScrubFrac(frac float64) {
	frac = clampVidFrac(frac)
	if a.vidQV.frac == frac && a.vidQV.frame != nil && !a.vidQV.loading {
		return
	}
	a.vidQV.frac = frac
	a.requestVideoFrame(a.vidQV.path, videoAtSec(frac, a.vidQV.duration))
}

// videoQuickContent is the Space modal body for a clip: frame, scrub,
// system-open button, and a short hint.
func (a *App) videoQuickContent(c *ui.Context, st *previewState, maxW, maxH float32) {
	poster := st.bitmap
	if poster == nil {
		poster = st.media.poster
	}
	a.resetVidQV(st.path, st.media.duration, poster)
	a.videoQuickViewport(c, st, maxW, maxH-100)
	a.videoQuickScrub(c, maxW)
	a.videoQuickOpenButton(c, st)
	hint := ""
	if a.vidQV.loading {
		hint = "正在抽帧…"
	} else if a.vidQV.err != "" && a.vidQV.frame == nil {
		hint = a.vidQV.err
	}
	if hint != "" {
		ui.Text(c, hint).FontSize(11).TextColor(colorMuted())
	}
}

// videoQuickViewport draws the current keyframe (or a broken placeholder)
// with a centred play hint that opens the system player — never an
// in-app decoder.
func (a *App) videoQuickViewport(c *ui.Context, st *previewState, maxW, maxH float32) {
	if maxW < 80 {
		maxW = 80
	}
	if maxH < 80 {
		maxH = 80
	}
	bm := a.vidQV.frame
	box := ui.Box(c).Width(maxW).Height(maxH).Shrink(0).Clip().
		Background(colorHover().Alpha(0.2)).Radius(innerRadius()).Cursor(ui.CursorPointer)
	if bm == nil {
		box.Children(func() {
			msg := a.vidQV.err
			if msg == "" {
				msg = st.err
			}
			if msg == "" {
				msg = "无法提取关键帧（需 ffmpeg）"
			}
			a.paintVideoBroken(c, msg)
		})
		return
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		iw, ih := bm.Size()
		if iw <= 0 || ih <= 0 {
			return
		}
		dw, dh := fitContainSize(r.W, r.H, float32(iw), float32(ih))
		dx := r.X + (r.W-dw)/2
		dy := r.Y + (r.H-dh)/2
		p.Image(bm, ui.Rect{X: dx, Y: dy, W: dw, H: dh}, ui.FillBox)
		// Play hint circle — visual only; click handled below.
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		p.Fill(ui.Rect{X: cx - 28, Y: cy - 28, W: 56, H: 56}, ui.RGBA(0, 0, 0, 0.45), 28)
		p.Stroke(ui.Rect{X: cx - 28, Y: cy - 28, W: 56, H: 56}, ui.RGBA(255, 255, 255, 0.7), 28, 2)
	})
	box.Children(func() {
		// Transparent hit target over the whole frame so a click opens
		// the system player (same as the footer button).
		hit := ui.Box(c).Fill().Cursor(ui.CursorPointer).Label("用系统播放器打开")
		if hit.Clicked() {
			a.openVideoWithSystem(st.path)
		}
	})
}

func (a *App) paintVideoBroken(c *ui.Context, msg string) {
	ui.Column(c).FillWidth().Gap(6).AlignItems(ui.Center).Padding(24, 16).Children(func() {
		components.Icon(c, lucide.VideoOff()).Size(36, 36).TextColor(colorMuted().Alpha(0.7))
		ui.Text(c, msg).FontSize(12).TextColor(colorMuted()).TextAlign(ui.Center)
	})
}

// videoQuickScrub is the thin progress bar under the frame: drag or click
// to scrub; left clock is current, right is total.
func (a *App) videoQuickScrub(c *ui.Context, maxW float32) {
	dur := a.vidQV.duration
	cur := videoAtSec(a.vidQV.frac, dur)
	row := ui.Row(c).Width(maxW).Gap(10).AlignItems(ui.Center).Padding(0, 4)
	row.Children(func() {
		ui.Text(c, formatDuration(time.Duration(cur*float64(time.Second)))).
			FontSize(11).TextColor(colorMuted()).Shrink(0).MinWidth(44)
		track := ui.Box(c).Grow(1).Height(18).MinWidth(80).Shrink(0).Cursor(ui.CursorPointer).
			Label("进度").Tooltip("拖动抽帧")
		var trackRect ui.Rect
		track.Draw(func(pa *ui.Painter, r ui.Rect) {
			trackRect = r
			pad := float32(4)
			x0, w := r.X+pad, r.W-2*pad
			y0, h := r.Y+r.H/2-2, float32(4)
			pa.Fill(ui.Rect{X: x0, Y: y0, W: w, H: h}, colorMuted().Alpha(0.3), 2)
			frac := a.vidQV.frac
			pa.Fill(ui.Rect{X: x0, Y: y0, W: float32(frac) * w, H: h}, colorAccent(), 2)
			cx := x0 + float32(frac)*w
			pa.Fill(ui.Rect{X: cx - 5, Y: r.Y + r.H/2 - 5, W: 10, H: 10}, colorAccent(), 5)
		})
		applyPointer := func() {
			if px, _, ok := track.PointerPosition(); ok {
				pad := float32(4)
				w := trackRect.W - 2*pad
				if w > 0 {
					a.setVidScrubFrac(float64((px - pad) / w))
				}
			}
		}
		if _, _, ok := track.Dragged(); ok {
			applyPointer()
		}
		if track.Clicked() {
			applyPointer()
		}
		totalLabel := "—"
		if dur > 0 {
			totalLabel = formatDuration(time.Duration(dur * float64(time.Second)))
		}
		ui.Text(c, totalLabel).FontSize(11).TextColor(colorMuted()).Shrink(0).MinWidth(44).
			TextAlign(ui.End)
	})
}

// videoQuickOpenButton is the footer CTA — system player only.
func (a *App) videoQuickOpenButton(c *ui.Context, st *previewState) {
	btn := ui.ButtonBase(c).Height(32).FillWidth().MaxWidth(240).Radius(innerRadius()).
		Background(colorAccent()).Label("用系统播放器打开").
		Tooltip("用系统默认播放器打开")
	btn.Children(func() {
		ui.Text(c, "用系统播放器打开").FontSize(13).TextColor(ui.RGBA(255, 255, 255, 1))
	})
	if btn.Clicked() {
		a.openVideoWithSystem(st.path)
	}
}

func (a *App) openVideoWithSystem(path string) {
	if path == "" {
		return
	}
	if err := openWithOS(path); err != nil {
		a.statusMsg = "无法打开：" + err.Error()
	}
}

// videoSidebarBody is the right-hand pane for a video: still + open CTA.
// Scrub lives only in the Space overlay so the two surfaces do not
// duplicate interactive chrome (same split as IMAGE1).
func (a *App) videoSidebarBody(c *ui.Context, st *previewState) {
	if st.bitmap != nil {
		ui.Box(c).FillWidth().Shrink(0).Radius(rowR()).Clip().Children(func() {
			ui.Image(c, st.bitmap).Fit(ui.Contain)
		})
	} else {
		a.paintVideoBroken(c, "无法提取关键帧（需 ffmpeg）")
	}
	btn := ui.ButtonBase(c).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(rowR()).
		Background(colorAddress()).Border(1, colorDivider()).
		Label("用系统播放器打开").Tooltip("用系统默认播放器打开")
	btn.Children(func() {
		ui.Text(c, "用系统播放器打开").FontSize(12).TextColor(colorText())
	})
	if btn.Clicked() {
		a.openVideoWithSystem(st.path)
	}
	if st.bitmap == nil {
		a.previewNote(c, "无法生成预览")
	} else if st.media.duration > 0 {
		ui.Text(c, fmt.Sprintf("时长 %s",
			formatDuration(time.Duration(st.media.duration*float64(time.Second))))).
			FontSize(11).TextColor(colorMuted()).FillWidth()
	}
}
