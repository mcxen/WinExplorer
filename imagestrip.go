package main

import (
	"github.com/egoist/mygo/ui"
)

// imagestrip.go 是空格看图底部的胶片条：本文件夹的图片排成一行小缩略图，
// 当前这张高亮并始终在中间附近，点一下跳过去。按需加载：只画得下的那一段
// 才会去要缩略图（mediaThumbFor，走内存与磁盘缓存），三百张的文件夹也只解
// 十几张。布局参照 BandiView 的底部缩略图条，不加任何文字。

const (
	imgStripTile float32 = 44
	imgStripGap  float32 = 4
)

// imgViewBackdrop 是看图区的深色底。
var imgViewBackdrop = ui.RGBA(18, 18, 20, 255)

// quickLookStripImages 是胶片条里的图片：当前视图中的图片文件，按视图顺序。
// 开着 OpenUI 的单文件预览时没有条。
func (a *App) quickLookStripImages() []Entry {
	if a.quickLookFile != "" {
		return nil
	}
	vis := a.visibleEntries()
	out := make([]Entry, 0, len(vis))
	for _, e := range vis {
		if !e.IsDir && isImageExt(lowerExt(e.Path)) {
			out = append(out, e)
		}
	}
	return out
}

func stripIndex(list []Entry, path string) int {
	for i := range list {
		if list[i].Path == path {
			return i
		}
	}
	return -1
}

// stripWindow 是宽 width 的条画得下的那一段 [lo, hi)，cur 尽量居中。
func stripWindow(n, cur int, width float32) (int, int) {
	fit := max(1, int((width+imgStripGap)/(imgStripTile+imgStripGap)))
	if fit >= n {
		return 0, n
	}
	lo := max(0, cur-fit/2)
	if lo+fit > n {
		lo = n - fit
	}
	return lo, lo + fit
}

// imageStrip 画胶片条。
func (a *App) imageStrip(c *ui.Context, list []Entry, cur string, width float32) {
	ci := max(0, stripIndex(list, cur))
	lo, hi := stripWindow(len(list), ci, width)
	ui.Row(c).Gap(imgStripGap).Height(imgStripTile + 6).AlignItems(ui.Center).Shrink(0).Children(func() {
		for i := lo; i < hi; i++ {
			e := list[i]
			bm := mediaThumbFor(e, 64)
			on := i == ci
			tile := ui.Box(c).Key(e.Path).Width(imgStripTile).Height(imgStripTile).Shrink(0).Clip().
				Radius(4).Background(imgViewBackdrop).Cursor(ui.CursorPointer)
			if on {
				tile.Border(2, colorAccent())
			} else {
				tile.Opacity(0.6)
			}
			if tile.Clicked() && !on {
				a.selectOnly(e.Path)
			}
			tile.Draw(func(p *ui.Painter, r ui.Rect) {
				if bm == nil {
					return
				}
				p.Image(bm, r, ui.Cover) // 填满方块，裁掉多余，像相册缩略图
			})
			tile.Children(func() {})
		}
	})
}
