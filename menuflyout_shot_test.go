package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	xdraw "golang.org/x/image/draw"
)

// menuflyout_shot_test.go renders the command bar's flyouts for
// docs/assets/shots/CLASSIC-FLUENT when SUNVALE_SHOT_DIR is set.

func TestMenuFlyoutShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	files := t.TempDir()
	for _, n := range []string{"季度报告", "旅行-京都", "会议纪要.md", "预算.xlsx", "照片.jpg"} {
		p := filepath.Join(files, n)
		if filepath.Ext(n) == "" {
			_ = os.Mkdir(p, 0o755)
		} else {
			_ = os.WriteFile(p, []byte("x"), 0o644)
		}
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_ = png.Encode(f, img)
	}
	defer func() { visualV2 = false; testDark = nil; resolveTokens() }()
	shot := func(dark bool, open string, before bool) (*image.RGBA, ui.Rect) {
		testDark = &dark
		resolveTokens()
		a := newStoreApp(t, files)
		a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
		a.applySettings(func(x *appSettings) { x.VisualStyle = "" })
		a.reload()
		var btn ui.Rect
		view := a.view
		if before {
			// The system menu MyGo drew before (internal/windows/menudraw.go:
			// no glyphs, a check column always kept, rows 32), rebuilt with
			// the same panel so only the layout differs.
			view = func(c *ui.Context) {
				a.view(c)
				ui.Overlay(c, func() {
					old := []*menuEntry{
						(&menuEntry{label: "撤销"}).shortcut(ui.Cmd, ui.KeyZ).off(true),
						{sep: true},
						(&menuEntry{label: "全选"}).shortcut(ui.Cmd, ui.KeyA),
						(&menuEntry{label: "全部取消", toggle: true}).off(true),
						{sep: true},
						{label: "插件", sub: []*menuEntry{}},
						{sep: true},
						(&menuEntry{label: "属性"}).shortcut(ui.Alt, ui.KeyEnter).off(true),
						(&menuEntry{label: "设置"}).shortcut(ui.Cmd, ui.KeyComma),
					}
					a.flyPanel(c.Key("old"), old, nil).Absolute().Left(btn.X + btn.W - 218).Top(btn.Y + btn.H + 2)
				})
			}
		}
		tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(appTheme(c)); view(c) }, 1200, 800)
		tt.Frame()
		r, _ := tt.Find("查看更多")
		btn = r
		if !before {
			if err := tt.Click(open); err != nil {
				t.Fatal(err)
			}
			if open == "查看" {
				if p, ok := tt.Find("显示"); ok {
					tt.Move(p.X+20, p.Y+p.H/2)
				}
			}
		}
		for range 3 {
			tt.Frame()
		}
		return tt.Image(), btn
	}
	before, btn := shot(false, "", true)
	save("07-more-before.png", before)
	after, _ := shot(false, "查看更多", false)
	save("08-more-after-light.png", after)
	dark, _ := shot(true, "查看更多", false)
	save("09-more-after-dark.png", dark)
	sortImg, _ := shot(false, "排序", false)
	save("11-sort-after.png", sortImg)
	viewImg, _ := shot(false, "查看", false)
	save("12-view-after.png", viewImg)

	// 10: the menu's area of 07 and 08 side by side at 2x.
	crop := image.Rect(int(btn.X+btn.W)-300, int(btn.Y)-8, int(btn.X+btn.W)+8, int(btn.Y)+340)
	zw, zh := crop.Dx()*2, crop.Dy()*2
	out := image.NewRGBA(image.Rect(0, 0, zw*2+16, zh))
	for i := range out.Pix {
		out.Pix[i] = 0xff
	}
	xdraw.NearestNeighbor.Scale(out, image.Rect(0, 0, zw, zh), before, crop, xdraw.Src, nil)
	xdraw.NearestNeighbor.Scale(out, image.Rect(zw+16, 0, zw*2+16, zh), after, crop, xdraw.Src, nil)
	for y := range zh {
		for x := zw; x < zw+16; x++ {
			out.Set(x, y, color.RGBA{0xc8, 0xc8, 0xc8, 0xff})
		}
	}
	save("10-more-zoom.png", out)
}
