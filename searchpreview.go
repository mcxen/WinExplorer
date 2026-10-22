package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// searchpreview.go draws the selected quick-search hit beside its list: a
// thumbnail, Explorer-style facts, the complete copyable path, the three
// actions that act on the hit, and the score reasons that put it there.

const (
	searchPreviewW     float32 = 270
	searchPreviewThumb float32 = 120
)

// searchPreview is the right side of the quick-search panel. It follows the
// cursor's hit rather than the pane selection, so keyboard movement updates
// it without touching the file window behind the launcher.
func (a *App) searchPreview(c *ui.Context, e Entry) {
	b := &a.searchBox
	pane := ui.Column(c).Width(searchPreviewW).FillHeight().Shrink(0).
		Background(colorContent()).Clip()
	pane.Children(func() {
		ui.Scroll(c).FillWidth().FillHeight().Padding(8).Gap(6).Children(func() {
			a.searchPreviewThumb(c, e)
			ui.RichText(c, searchNameSpans(b.parsed, e.Name, e.IsDir, 14)...).
				SingleLine().MinWidth(0).FontWeight(600)
			a.searchPreviewFacts(c, e)
			a.searchPreviewPath(c, e)
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				uikitPrimaryButton(c, "打开", func() { a.openSearchHit(e) })
				uikitSecondaryButton(c, "打开所在位置", func() { a.revealSearchHit(e) })
				uikitSecondaryButton(c, "复制路径", func() {
					if copyPathTextWith(e.Path, c.WriteClipboard) {
						a.statusMsg = "已复制路径"
					}
				})
			})
			ui.Box(c).Height(1).FillWidth().Background(colorDivider())
			a.searchPreviewReasons(c, e)
		})
	})
}

// searchPreviewEmpty keeps the preview column's shape while the list has no
// selectable hit, rather than letting the result list jump sideways.
func searchPreviewEmpty(c *ui.Context, height float32) {
	ui.Column(c).Width(searchPreviewW).Height(height).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Gap(8).Background(colorContent()).
		Children(func() {
			components.Icon(c, lucide.FileSearch).Size(36, 36).
				TextColor(colorMuted().Alpha(0.6))
			ui.Text(c, "选择一个结果以查看预览").FontSize(12).
				TextColor(colorMuted()).TextAlign(ui.Center)
		})
}

// searchPreviewThumb uses the icon views' media thumbnail pipeline. Its
// decode is asynchronous and repaints when ready; other entries keep the
// same large file glyph the quick-look fallback uses.
func (a *App) searchPreviewThumb(c *ui.Context, e Entry) {
	box := ui.Box(c).FillWidth().Height(searchPreviewThumb).Shrink(0).Radius(6).
		Background(colorSurface()).Border(1, colorDivider()).Clip()
	box.Children(func() {
		if bm := mediaThumb(e.Path, searchPreviewThumb); bm != nil {
			ui.Image(c, bm).Fit(ui.Contain).FillWidth().Height(searchPreviewThumb)
			return
		}
		ui.Row(c).FillWidth().Height(searchPreviewThumb).
			AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			a.fileIcon(c, e, 56)
		})
	})
}

// searchPreviewFacts is the compact key/value grid under the name. The key
// column stays fixed so a wrapped location does not shift the other values.
func (a *App) searchPreviewFacts(c *ui.Context, e Entry) {
	size := sizeColumnText(e)
	if size == "" {
		size = "—"
	}
	location := searchPathText(searchPathSegments(e.Dir, homeForSearch(), 4))
	for _, row := range []propRow{
		{Key: "类型", Val: typeLabel(e)},
		{Key: "大小", Val: size},
		{Key: "修改", Val: formatFullTime(e.ModTime)},
		{Key: "位置", Val: location},
	} {
		r := row
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
			ui.Text(c, r.Key).FontSize(11).TextColor(colorMuted()).
				Width(56).Shrink(0).SingleLine()
			ui.Text(c, r.Val).FontSize(11).TextColor(colorText()).
				Grow(1).MinWidth(0).Wrap()
		})
	}
}

// searchPreviewPath is deliberately selectable: the path is evidence about
// the hit and a useful value to paste into a terminal or another tool.
func (a *App) searchPreviewPath(c *ui.Context, e Entry) {
	box := ui.Box(c).FillWidth().Shrink(0).Radius(4).Padding(6, 4).
		Background(colorSurface()).Border(1, colorFieldBorder())
	box.Children(func() {
		ui.Text(c, e.Path).FontSize(11).Font("monospace").TextColor(colorText()).
			Wrap().Selectable().MinWidth(0)
	})
}

// searchPreviewReasons lists the public reasons Score returned, in the same
// order it used to rank the hit. A grouped or name-sorted list still shows
// relevance evidence because the score did not stop existing.
func (a *App) searchPreviewReasons(c *ui.Context, e Entry) {
	b := &a.searchBox
	score := b.scores[e.Path]
	reasons := b.reasons[e.Path]
	if reasons == nil {
		now, home := time.Now(), homeForSearch()
		near := ""
		if p := a.cur().path; !specialPath(p) {
			near = p
		}
		score, _, reasons = b.parsed.Score(e.Name, e.Path, e.IsDir, e.ModTime, now, home, near)
	}
	ui.Column(c).FillWidth().Gap(2).Children(func() {
		ui.Text(c, "排序依据（得分 "+itoa(score)+"）").FontSize(12).
			FontWeight(600).TextColor(colorText()).SingleLine()
		if len(reasons) == 0 {
			ui.Text(c, "没有额外排序因素").FontSize(11).TextColor(colorMuted()).
				SingleLine()
			return
		}
		for _, reason := range reasons {
			r := reason
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
				ui.Text(c, "·").FontSize(11).TextColor(colorMuted()).Shrink(0)
				ui.Text(c, r.Text).FontSize(11).TextColor(colorText()).
					Grow(1).MinWidth(0).Wrap()
			})
		}
	})
}
