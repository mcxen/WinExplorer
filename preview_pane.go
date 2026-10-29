package main

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// previewPane is the Windows 11 preview pane on the right: a header with
// the file name, a body that shows the image / text / media, and a
// properties section. Its left edge is a 1px divider and a drag handle
// that resizes it, clamped to previewMinW–previewMaxW, and it sits a
// hair off the window's right edge so the scrollbar never hugs the frame.
// When a directory or nothing is selected it shows the Explorer
// empty-pane hint.
func (a *App) previewPane(c *ui.Context) {
	st := a.preview.Load()
	pane := ui.Column(c).Width(a.previewDrawW()-previewGapR).FillHeight().Shrink(0).
		Margin(0, previewGapR, 0, 0).
		Background(colorContent()).Clip()
	pane.Children(func() {
		a.previewResizeHandle(c)
		ui.Scroll(c).Grow(1).FillWidth().Padding(12, 12, 12, 12).Gap(10).Children(func() {
			if n := len(a.cur().selected); n > 1 && a.quickLookFile == "" {
				a.previewMulti(c)
				return
			}
			if st == nil || st.kind == previewNone {
				a.previewEmpty(c)
				return
			}
			a.previewHeader(c, st)
			a.previewBody(c, st)
			a.previewProps(c, st)
		})
	})
}

// previewMulti is the pane for a multiple selection: the count and the
// total size of what is known (files, and folders with a cached tally).
func (a *App) previewMulti(c *ui.Context) {
	sel := a.selectedEntries()
	var total int64
	for _, e := range sel {
		switch {
		case !e.IsDir && !e.NoSize:
			total += e.Size
		case e.IsDir:
			if st, ok := lookupDirStat(e.Path); ok {
				total += st.Size
			}
		}
	}
	col := ui.Column(c).FillWidth().Gap(4).AlignItems(ui.Center).Padding(40, 0, 0, 0)
	col.Children(func() {
		components.Icon(c, lucide.Files).Size(44, 44).TextColor(colorMuted().Alpha(0.6))
		ui.Text(c, "已选 "+itoa(len(sel))+" 项").FontSize(14).FontWeight(600).TextColor(colorText())
		ui.Text(c, "共 "+formatBytes(total)).FontSize(12).TextColor(colorMuted())
	})
}

func (a *App) previewEmpty(c *ui.Context) {
	col := ui.Column(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(40, 0, 0, 0)
	col.Children(func() {
		components.Icon(c, lucide.FileSearch).Size(44, 44).TextColor(colorMuted().Alpha(0.6))
		ui.Text(c, "选择一个项目以查看其预览").FontSize(13).TextColor(colorMuted()).TextAlign(ui.Center)
	})
}

func (a *App) previewHeader(c *ui.Context, st *previewState) {
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, baseName(st.path)).FontSize(13).TextColor(colorText()).
			TextAlign(ui.Start).Grow(1).MinWidth(0).MaxLines(2)
	})
}

// previewBody renders the type-specific preview.
func (a *App) previewBody(c *ui.Context, st *previewState) {
	switch st.kind {
	case previewImage:
		// Sidebar stays a simple Contain fit; Space owns zoom/pan
		// (imageview.go) so the two surfaces do not repeat chrome.
		if st.bitmap != nil {
			ui.Box(c).FillWidth().Shrink(0).Radius(4).Clip().Children(func() {
				ui.Image(c, st.bitmap).Fit(ui.Contain)
			})
		} else {
			a.paintImageBroken(c, st.err)
		}
	case previewText:
		if st.text != "" {
			a.previewText(c, st)
		} else {
			a.previewNote(c, st.err)
		}
	case previewHex:
		if st.text != "" {
			a.previewNote(c, "十六进制预览（前 4 KB）")
			a.previewText(c, st)
		} else {
			a.previewNote(c, st.err)
		}
	case previewAudio:
		a.previewAudio(c, st)
		if st.media.player == nil {
			a.previewOpenButton(c, st)
		}
	case previewVideo:
		a.videoSidebarBody(c, st)
	case previewDoc:
		if st.bitmap != nil {
			ui.Box(c).FillWidth().Shrink(0).Children(func() {
				ui.Image(c, st.bitmap).Fit(ui.Contain)
			})
		}
		a.previewOpenButton(c, st)
		if st.bitmap == nil {
			msg := "无法生成预览"
			if st.err != "" {
				msg = st.err
			}
			a.previewNote(c, msg)
		}
	default:
		if st.err != "" {
			a.previewNote(c, st.err)
		}
	}
}

func (a *App) previewNote(c *ui.Context, msg string) {
	ui.Text(c, msg).FontSize(12).TextColor(colorMuted()).TextAlign(ui.Center).
		FillWidth().Padding(8, 4, 8, 4)
}

// previewOpenButton is the “打开” affordance for files the pane can show
// but not play inline (video, PDF, unsupported audio), matching Explorer's
// preview-pane open button.
func (a *App) previewOpenButton(c *ui.Context, st *previewState) {
	btn := ui.ButtonBase(c).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(4).
		Background(colorAddress()).Border(1, colorDivider()).Label("打开").Tooltip("用系统应用打开")
	btn.Children(func() {
		ui.Text(c, "打开").FontSize(12).TextColor(colorText())
	})
	if btn.Clicked() {
		if err := openWithOS(st.path); err != nil {
			a.statusMsg = "无法打开：" + err.Error()
		}
		repaint()
	}
}

// previewText shows the first chunk of a text/code file in a read-only
// area, with a "约 N" hint when truncated.
func (a *App) previewText(c *ui.Context, st *previewState) {
	body := ui.Box(c).FillWidth().Shrink(0).Radius(4).
		Background(colorAddress()).Border(1, colorFieldBorder()).Padding(8, 6).MinHeight(120)
	body.Children(func() {
		value := expandTabs(st.text)
		ui.TextArea(c, &value).ReadOnly(true).Font(monoFont).FontSize(12).TextColor(colorText())
	})
	if st.textFull > len(st.text) {
		ui.Text(c, fmt.Sprintf("约 %s · 已显示前 %s", formatBytes(int64(st.textFull)), formatBytes(int64(len(st.text))))).
			FontSize(11).TextColor(colorMuted()).FillWidth()
	}
}

// previewAudio shows a transport (play/pause, seek) and elapsed/total
// time for a decoded audio file; MyGo's audio package plays it.
func (a *App) previewAudio(c *ui.Context, st *previewState) {
	p := st.media.player
	if p == nil || p.player == nil {
		a.previewNote(c, "此音频格式无法在内置播放器中播放，可点击“打开”")
		return
	}
	pl := p.player
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		play := ui.ButtonBase(c).Size(32, 32).Radius(16).Shrink(0).Label("播放/暂停")
		if play.Hovered() || play.Pressed() {
			play.Background(colorHover())
		}
		play.Children(func() {
			icon := lucide.Play
			if pl.Playing() {
				icon = lucide.Pause
			}
			components.Icon(c, icon).Size(14, 14).TextColor(colorAccent())
		})
		if play.Clicked() {
			if pl.Playing() {
				pl.Pause()
			} else {
				pl.Play()
			}
			p.startTicker()
			repaint()
		}
		ui.Text(c, fmt.Sprintf("%s / %s", formatDuration(pl.Position()), formatDuration(p.dur))).
			FontSize(12).TextColor(colorMuted()).Grow(1).MinWidth(0)
	})
	// Seek bar: click to seek within the track.
	track := ui.Box(c).FillWidth().Height(18).Shrink(0)
	var trackRect ui.Rect
	track.Children(func() {
		track.Draw(func(pa *ui.Painter, r ui.Rect) {
			trackRect = r
			pad := float32(6)
			x0, w := r.X+pad, r.W-2*pad
			y0, h := r.Y+r.H/2-2, float32(4)
			pa.Fill(ui.Rect{X: x0, Y: y0, W: w, H: h}, colorMuted().Alpha(0.3), 2)
			var frac float64
			if d := p.dur.Seconds(); d > 0 {
				frac = pl.Position().Seconds() / d
				if frac > 1 {
					frac = 1
				}
			}
			pa.Fill(ui.Rect{X: x0, Y: y0, W: float32(frac) * w, H: h}, colorAccent(), 2)
			cx := x0 + float32(frac)*w
			pa.Fill(ui.Rect{X: cx - 5, Y: r.Y + r.H/2 - 5, W: 10, H: 10}, colorAccent(), 5)
		})
		if track.Clicked() {
			if px, _, ok := track.PointerPosition(); ok && p.dur > 0 {
				pad := float32(6)
				w := trackRect.W - 2*pad
				// PointerPosition is relative to the track's own box.
				rel := px - pad
				if w > 0 {
					frac := float64(rel / w)
					if frac < 0 {
						frac = 0
					}
					if frac > 1 {
						frac = 1
					}
					pl.Seek(time.Duration(frac * p.dur.Seconds()))
					repaint()
				}
			}
		}
	})
}

// previewProps lists the file properties the way Explorer's details pane
// does: one row per attribute, with the plugins' metadata rows last.
func (a *App) previewProps(c *ui.Context, st *previewState) {
	if len(st.meta) == 0 {
		return
	}
	ui.Box(c).Height(1).FillWidth().Background(colorDivider())
	list := ui.Column(c).FillWidth().Gap(4)
	list.Children(func() {
		for _, r := range st.meta {
			row := ui.Row(c).FillWidth().Gap(8)
			row.Children(func() {
				ui.Text(c, r.Key).FontSize(12).TextColor(colorMuted()).
					Width(64).Shrink(0).SingleLine()
				ui.Text(c, r.Val).FontSize(12).TextColor(colorText()).
					Grow(1).MinWidth(0).MaxLines(3)
			})
		}
		// The plugins' rows render for the single selected file the pane
		// is showing; rendering here is what fires the hash plugin's
		// background request, which is a no-op once the facts are current.
		if e := a.singleSelected(); e.Path == st.path {
			for _, r := range a.pluginMetaRows(e) {
				a.pluginMetaRowValue(c, r, 64)
			}
		}
	})
}

// pluginMetaRowValue renders one plugin metadata row: the label at keyW
// DIPs, the value, and a 复制 button beside it when the row says the value
// is copyable.
func (a *App) pluginMetaRowValue(c *ui.Context, r PluginMetaRow, keyW float32) {
	row := ui.Row(c).FillWidth().Gap(8)
	row.Children(func() {
		ui.Text(c, r.Key).FontSize(12).TextColor(colorMuted()).
			Width(keyW).Shrink(0).SingleLine()
		ui.Text(c, r.Val).FontSize(12).TextColor(colorText()).
			Grow(1).MinWidth(0).MaxLines(3)
		if r.Copy {
			btn := ui.ButtonBase(c).Height(22).Padding(0, 10).MinWidth(56).Shrink(0).Radius(4).
				Background(colorAddress()).Border(1, colorDivider()).Label("复制")
			btn.Children(func() {
				ui.Text(c, "复制").FontSize(11).TextColor(colorText())
			})
			if btn.Clicked() {
				mygo.Clipboard.WriteText(r.Val)
				a.statusMsg = "已复制 " + r.Key
			}
		}
	})
}

func (a *App) previewResizeHandle(c *ui.Context) {
	// The 1px rule down the pane's left edge separates it from the file
	// list, as Explorer's pane divider does.
	ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(1).
		Background(colorDivider())
	// The grip is a narrow strip centred on that edge — the divider the drag
	// moves, as the navigation pane's handle is. It reaches 3 DIP into the
	// file list and 3 into the pane, and goes no further: a wider target
	// would take the preview's own clicks and scrolling, and would put the
	// 1px line it draws (the middle of its box) wherever that width ends.
	// Keep the grip inside the pane (Left 0): v2's content-card Clip would
	// drop hits on an Absolute strip that overhangs into the file list.
	grip := ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Width(6).
		Cursor(ui.CursorResizeEW)
	if dx, _, ok := grip.Dragged(); ok && dx != 0 {
		a.previewW = clampPreviewWidth(a.previewW - dx)
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		if !grip.Hovered() && !grip.Dragging() {
			return
		}
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: 1, H: r.H}, colorAccent(), 0)
	})
}

func clampPreviewWidth(w float32) float32 {
	if w < previewMinW {
		return previewMinW
	}
	if w > previewMaxW {
		return previewMaxW
	}
	return w
}

func baseName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// previewDrawW is the preview's drawn width: the user's, unless a narrow
// window squeezes it (layout.go).
func (a *App) previewDrawW() float32 {
	if a.lay.prevW > 0 {
		return a.lay.prevW
	}
	return a.previewW
}
