package main

import (
	"path/filepath"
	"strings"

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
func (a *App) quickLookModal(c *ui.Context) {
	if !a.quickLook {
		return
	}
	ui.Modal(c, &a.quickLook, func() {
		a.quickLookBody(c)
	})
}

func (a *App) quickLookBody(c *ui.Context) {
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
		ui.Text(c, "空格 或 Esc 关闭 · ←/→ 快退快进").FontSize(11).TextColor(colorMuted())
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
		if st.bitmap != nil {
			box := ui.Box(c).Width(maxW).Height(maxH).Shrink(0)
			box.Children(func() {
				ui.Image(c, st.bitmap).Fit(ui.Contain)
			})
			return
		}
	case previewText:
		if st.text != "" {
			body := ui.Box(c).Width(maxW).Height(maxH).Shrink(0).Radius(8).
				Background(colorAddress()).Border(1, colorDivider()).Padding(12, 10).Clip()
			body.Children(func() {
				value := st.text
				ui.TextArea(c, &value).ReadOnly(true).FontSize(13).TextColor(colorText())
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
	case previewDoc:
		if st.bitmap != nil {
			poster := ui.Box(c).Width(maxW).Height(maxH * 0.8).Shrink(0)
			poster.Children(func() {
				ui.Image(c, st.bitmap).Fit(ui.Contain)
			})
			a.quickLookOpenButton(c, st)
			return
		}
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
