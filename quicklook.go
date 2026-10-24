package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// quicklook.go is the 空格 quick preview, Finder's Quick Look in this
// Explorer window: pressing Space on the single selection opens a modal
// over the dimmed window showing the file as large as it fits — image,
// text, an inline audio player with the same transport, a video/PDF
// poster with an open button — with the file's facts along the bottom.
// Space or Esc closes; a click outside closes too, as Modal does.
//
// It renders from the preview pane's own loaded state (preview.go), so a
// file is decoded once for both.

// openQuickLook opens the preview for the single selected file, if any.
func (a *App) openQuickLook() {
	if e := a.singleSelected(); e.Path != "" && !e.IsDir {
		a.quickLook = true
		a.requestPreview()
	}
}

// quickLookModal shows the overlay while it is open. Called from view.
// When it closes — Space, Escape, a click outside — the gif animations
// it was playing stop.
func (a *App) quickLookModal(c *ui.Context) {
	if !a.quickLook {
		if a.quickLookFile != "" {
			a.quickLookFile = ""
			a.requestPreview()
		}
		if quickLookWasOpen {
			stopGifPlay()
			quickLookWasOpen = false
		}
		return
	}
	quickLookWasOpen = true
	floatDialog(c, &a.quickLook, "快速预览", quickLookWidth(c), true, func() {
		a.quickLookBody(c)
	})
}

var quickLookWasOpen bool

// quickLookWidth is the room the preview body gets: the window less the two
// 20 DIP insets components.Dialog keeps and the body's own 40 DIP of
// padding, so the media it shows — capped at window−120 by quickLookContent —
// fits inside the glass.
func quickLookWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return max(w-80, 280)
}

// isGifPath reports whether the file is an animated-capable GIF.
func isGifPath(path string) bool {
	return strings.EqualFold(strings.TrimPrefix(filepath.Ext(path), "."), "gif")
}

// quickLookKeys handles the keys inside the open modal. Context.Shortcut
// is inert behind a dialog, so these bindings must live within it, not
// in the window's handleKeys: Space closes; ←/→ seek a playing audio
// preview, Finder's Quick Look behavior, and otherwise step the
// selection to the neighbouring file, whose preview loads in place.
func (a *App) quickLookKeys(c *ui.Context) {
	if c.Shortcut(0, ui.KeySpace) {
		a.quickLook = false
		return
	}
	// Enter keeps its list meaning inside the overlay: open with the
	// system default app (DOCPREV1), then close the preview.
	if c.Shortcut(0, ui.KeyEnter) {
		if e := a.singleSelected(); e.Path != "" {
			if err := openWithOS(e.Path); err != nil {
				a.statusMsg = "无法打开：" + err.Error()
			}
		}
		a.quickLook = false
		return
	}
	left, right := c.Shortcut(0, ui.KeyLeft), c.Shortcut(0, ui.KeyRight)
	if !left && !right {
		return
	}
	if a.quickLookFile != "" {
		return // a MediaList card previews one file; there is no neighbour
	}
	if st := a.preview.Load(); st != nil && st.kind == previewAudio && st.media.player != nil {
		pl := st.media.player
		delta := -5 * time.Second
		if right {
			delta = 5 * time.Second
		}
		if pl.player != nil {
			pl.player.Seek(pl.player.Position() + delta)
			repaint()
		}
		return
	}
	// Pictures step among same-folder images; everything else keeps the
	// generic neighbour walk (including audio seek handled above).
	st := a.preview.Load()
	if st != nil && st.kind == previewImage {
		if left {
			a.quickLookStepImage(-1)
		} else {
			a.quickLookStepImage(1)
		}
		return
	}
	if left {
		a.quickLookStep(-1)
	} else {
		a.quickLookStep(1)
	}
}

// quickLookStep moves the selection to the neighbouring item of the view
// in the cursor's order — the shared keyboard cursor keynav keeps — and
// the preview follows, as Finder's Quick Look steps through files.
func (a *App) quickLookStep(dir int) {
	vis := a.visibleEntries()
	if len(vis) == 0 {
		return
	}
	i := a.navCursor(vis)
	if i < 0 {
		i = 0
	} else {
		i += dir
	}
	if i < 0 || i >= len(vis) {
		return // the ends hold; a move never wraps
	}
	a.selectOnly(vis[i].Path)
}

func (a *App) quickLookBody(c *ui.Context) {
	a.quickLookKeys(c)
	st := a.preview.Load()
	e := a.singleSelected()
	loading := st == nil || st.path != e.Path || st.kind == previewNone
	ui.Column(c).Gap(10).AlignItems(ui.Center).Padding(20, 16, 20, 12).Children(func() {
		if loading {
			ui.Text(c, "正在加载预览…").FontSize(13).TextColor(colorMuted())
			ui.Text(c, e.Name).FontSize(12).TextColor(colorMuted())
			return
		}
		a.quickLookContent(c, st)
		a.quickLookFacts(c, st)
	})
}

// quickLookContent renders the preview itself, as large as the window
// allows.
func (a *App) quickLookContent(c *ui.Context, st *previewState) {
	w, h := c.Size()
	maxW := max(w-120, 240)
	maxH := max(h-200, 200)
	switch st.kind {
	case previewImage:
		// Interactive viewer: fit, wheel zoom, drag pan, same-folder strip.
		// GIF animation still feeds through gifFor inside imageQuickViewport.
		a.imageQuickContent(c, st, maxW, maxH)
		return
	case previewText:
		if st.text != "" {
			body := ui.Box(c).Width(maxW).Height(maxH).Shrink(0).Radius(8).
				Background(colorAddress()).Border(1, colorDivider()).Padding(12, 10).Clip()
			body.Children(func() {
				value := expandTabs(st.text)
				ui.TextArea(c, &value).ReadOnly(true).Font(monoFont).FontSize(13).TextColor(colorText())
			})
			if st.textFull > len(st.text) {
				ui.Text(c, fmt.Sprintf("仅显示前 %[2]s / 共 %[1]s",
					formatBytes(int64(st.textFull)), formatBytes(int64(len(st.text))))).
					FontSize(11).TextColor(colorMuted())
			}
			return
		}
	case previewHex:
		if st.text != "" {
			body := ui.Box(c).Width(maxW).Height(maxH).Shrink(0).Radius(8).
				Background(colorAddress()).Border(1, colorDivider()).Padding(12, 10).Clip()
			body.Children(func() {
				value := expandTabs(st.text)
				ui.TextArea(c, &value).ReadOnly(true).Font(monoFont).FontSize(12).TextColor(colorText())
			})
			return
		}
	case previewAudio:
		card := ui.Box(c).Width(320).Shrink(0).Radius(8).
			Background(colorAddress()).Border(1, colorDivider()).Padding(16, 14)
		card.Children(func() {
			ui.Text(c, filepath.Base(st.path)).FontSize(13).TextColor(colorText()).
				FillWidth().MaxLines(1).Padding(0, 0, 0, 6)
			a.previewAudio(c, st)
		})
		return
	case previewVideo:
		// Keyframe + scrub + system-open CTA (videoview.go). No embedded player.
		a.videoQuickContent(c, st, maxW, maxH)
		return
	case previewDoc:
		// Paged raster of PDF / Office (docview.go); degrades in place.
		a.docQuickContent(c, st, maxW, maxH)
		return
	}
	// No renderer: an icon and the type stand in.
	col := ui.Column(c).Gap(12).AlignItems(ui.Center).Padding(24, 16)
	col.Children(func() {
		glyph := ui.Box(c).Size(96, 96).Shrink(0)
		glyph.Children(func() {
			a.fileIcon(c, Entry{Ext: stExt(st)}, 96)
		})
		if st.err != "" {
			ui.Text(c, st.err).FontSize(12).TextColor(colorMuted()).TextAlign(ui.Center)
		}
		a.quickLookOpenButton(c, st)
	})
}

// quickLookOpenButton opens the file with the system default app, for
// what the pane can show but not play.
func (a *App) quickLookOpenButton(c *ui.Context, st *previewState) {
	btn := ui.ButtonBase(c).Height(30).FillWidth().MaxWidth(220).Radius(4).
		Background(colorAddress()).Border(1, colorDivider()).Label("打开")
	btn.Children(func() {
		ui.Text(c, "打开").FontSize(12).TextColor(colorText())
	})
	if btn.Clicked() {
		if err := openWithOS(st.path); err != nil {
			a.statusMsg = "无法打开：" + err.Error()
		}
	}
}

// quickLookFacts is the line of details under the preview: size, dates,
// dimensions — the preview pane's property list, condensed.
func (a *App) quickLookFacts(c *ui.Context, st *previewState) {
	if len(st.meta) == 0 {
		return
	}
	parts := make([]string, 0, 4)
	for _, r := range st.meta {
		switch r.Key {
		case "大小", "修改日期", "尺寸", "时长", "类型":
			parts = append(parts, r.Val)
		}
		if len(parts) >= 4 {
			break
		}
	}
	if len(parts) == 0 {
		return
	}
	ui.Text(c, strings.Join(parts, " · ")).FontSize(12).TextColor(colorMuted()).SingleLine()
}

func stExt(st *previewState) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(st.path)), ".")
}
