package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// tasksys_mock_shot_test.go renders the TASKSYS placement mockups
// (docs/specs/10-task-system.md) when SUNVALE_SHOT_DIR is set. Nothing
// here ships: the pane is a static mock composed with the real window.

type mockTask struct {
	icon          *ui.SVG
	title, source string
	frac          float64
	meta, cur     string
	tone          progressTone
}

var mockTasks = []mockTask{
	{lucide.FileArchive(), "解压 照片备份-2026.zip", "我", 0.42, "342 MB / 812 MB · 8.5 MB/s · 剩余 0:55", "2026/09/IMG_20260918_183021.HEIC", toneRunning},
	{lucide.Copy(), "复制 214 项到 NAS/备份", "我", 0.71, "152 / 214 项 · 1.9 GB / 2.7 GB · 剩余 1:40", "Movies/旅行-京都-4K.mp4", toneRunning},
	{lucide.PencilLine(), "批量重命名 86 张照片", "AI", 0.30, "26 / 86 项", "IMG_0412.jpg → 2026-09-18 京都 012.jpg", toneRunning},
	{lucide.FileArchive(), "压缩 季度报告", "定时", -1, "排队 · IO 槽 2/2", "", toneCanceled},
	{lucide.Trash(), "移到回收站 1,204 项", "AI", 1, "1,204 项 · 0:12", "", toneDone},
	{lucide.FileArchive(), "解压 project-src.tar.gz", "我", 0.32, "31 MB / 96 MB · 3 项失败", "磁盘空间不足：还需 65 MB", toneError},
}

func mockTaskRow(c *ui.Context, i int, t mockTask, narrow bool) {
	cc := c.Key("r" + itoa(i))
	row := ui.Row(cc).FillWidth().MinWidth(0).Height(34).Padding(0, 10).Gap(8).AlignItems(ui.Center)
	if i == 0 {
		row.Background(colorHover())
	}
	row.Children(func() {
		components.Icon(cc, t.icon).Size(15, 15).TextColor(t.tone.color(false)).Shrink(0)
		w := float32(230)
		if narrow {
			w = 150
		}
		ui.Text(cc.Key("t"), t.title).FontSize(12.5).FontWeight(600).TextColor(colorText()).SingleLine().Ellipsis("…").Width(w).Shrink(0)
		ui.Text(cc.Key("s"), t.source).FontSize(10.5).TextColor(colorMuted()).Padding(1, 5).Radius(4).Background(colorAddress()).Shrink(0)
		ui.Box(cc.Key("b")).Width(110).Shrink(0).Children(func() { paintProgressBar(cc, fmt.Sprintf("mock-%d", i), t.frac, t.tone, 5) })
		if !narrow {
			ui.Text(cc.Key("m"), t.meta).FontSize(11.5).TextColor(colorMuted()).SingleLine().Width(250).Shrink(0)
		}
		col := colorMuted()
		if t.tone == toneError {
			col = tokDanger
		}
		ui.Text(cc.Key("c"), t.cur).FontSize(11).TextColor(col).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		for _, ic := range []*ui.SVG{lucide.Pause(), lucide.X()} {
			components.Icon(cc, ic).Size(13, 13).TextColor(colorMuted()).Shrink(0)
		}
	})
}

func mockTasksHeader(c *ui.Context) {
	ui.Row(c.Key("hdr")).FillWidth().Height(32).Padding(0, 10).Gap(10).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colorDivider()).Children(func() {
		v := 0
		ui.Segmented(c, &v, "进行中 3", "排队 1", "失败 1", "历史 128").Shrink(0)
		ui.Box(c).Grow(1)
		ui.Text(c, "2 路 IO · 合计 10.4 MB/s").FontSize(11.5).TextColor(colorMuted())
		for _, ic := range []*ui.SVG{lucide.Pause(), lucide.ListX(), lucide.ChevronDown()} {
			components.Icon(c, ic).Size(14, 14).TextColor(colorMuted())
		}
	})
}

func mockTasksPane(c *ui.Context, narrow bool) {
	ui.Column(c).Fill().Background(c.Theme().Background).BorderWidth(1, 0, 0, 0).BorderColor(colorDivider()).Children(func() {
		mockTasksHeader(c)
		for i, t := range mockTasks {
			mockTaskRow(c, i, t, narrow)
		}
	})
}

func renderTester(fn func(c *ui.Context), w, h int) *image.RGBA {
	tt := ui.NewTester(func(c *ui.Context) { c.SetTheme(appTheme(c)); fn(c) }, w, h)
	for range 3 {
		tt.Frame()
	}
	return tt.Image()
}

func TestTaskSysMockShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	files := t.TempDir()
	for i, n := range []string{"照片备份-2026.zip", "project-src.tar.gz", "季度报告", "旅行-京都", "会议纪要.md", "预算.xlsx"} {
		p := filepath.Join(files, n)
		if filepath.Ext(n) == "" || i == 3 {
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
	const W, H = 1280, 800
	a := newStoreApp(t, files)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	// A: the window above, the docked Tasks pane across the bottom.
	paneH := 32 + 34*len(mockTasks) + 2
	top := renderTester(a.view, W, H-paneH)
	pane := renderTester(func(c *ui.Context) { mockTasksPane(c, false) }, W, paneH)
	outA := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(outA, image.Rect(0, 0, W, H-paneH), top, image.Point{}, draw.Src)
	draw.Draw(outA, image.Rect(0, H-paneH, W, H), pane, image.Point{}, draw.Src)
	save("01-option-a-bottom-dock.png", outA)
	// B: the right dock shared with the assistant, a 任务 tab.
	const sideW = 440
	left := renderTester(a.view, W-sideW, H)
	side := renderTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(c.Theme().Background).BorderWidth(0, 0, 0, 1).BorderColor(colorDivider()).Children(func() {
			ui.Row(c.Key("tabs")).FillWidth().Height(44).Padding(0, 10).Gap(8).AlignItems(ui.Center).Children(func() {
				v := 1
				ui.Segmented(c, &v, "助手", "任务 4").Shrink(0)
			})
			mockTasksHeader(c)
			for i, t := range mockTasks {
				mockTaskRow(c, i, t, true)
			}
		})
	}, sideW, H)
	outB := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(outB, image.Rect(0, 0, W-sideW, H), left, image.Point{}, draw.Src)
	draw.Draw(outB, image.Rect(W-sideW, 0, W, H), side, image.Point{}, draw.Src)
	save("02-option-b-side-tab.png", outB)
	// C: the collapsed state — status bar indicator only — and one failed
	// task expanded to its per-item errors.
	detail := renderTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
			mockTasksHeader(c)
			mockTaskRow(c, 5, mockTasks[5], false)
			ui.Column(c.Key("errs")).FillWidth().Padding(4, 10, 8, 42).Gap(3).Children(func() {
				for i, l := range []string{
					"src/vendor/big.bin  磁盘空间不足：还需 65 MB",
					"src/assets/logo.psd  磁盘空间不足",
					"src/.git/objects/pack/pack-7f3a.pack  磁盘空间不足",
				} {
					ui.Text(c.Key("e"+itoa(i)), l).FontSize(11.5).TextColor(colorMuted()).SingleLine()
				}
				ui.Row(c.Key("act")).Gap(8).Padding(4, 0, 0, 0).Children(func() {
					components.Button(c.Key("retry"), "重试失败项", components.ButtonSecondary)
					components.Button(c.Key("log"), "复制日志", components.ButtonSecondary)
				})
			})
			ui.Box(c).Grow(1)
			ui.Row(c.Key("sb")).FillWidth().Height(28).Padding(0, 12).Gap(8).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(colorDivider()).Children(func() {
				ui.Text(c, "6 个项目").FontSize(12).TextColor(colorMuted()).Grow(1)
				ui.Box(c.Key("sbb")).Width(90).Children(func() { paintProgressBar(c, "mock-sb", 0.55, toneRunning, 4) })
				ui.Text(c, "3 个任务 · 55% · 10.4 MB/s · 剩余 1:40").FontSize(12).TextColor(colorAccent())
				components.Icon(c, lucide.CircleAlert()).Size(13, 13).TextColor(tokDanger)
				ui.Text(c, "1").FontSize(12).TextColor(tokDanger)
				components.Icon(c, lucide.ChevronUp()).Size(13, 13).TextColor(colorMuted())
				ui.Text(c, "详细信息").FontSize(12).TextColor(colorMuted())
			})
		})
	}, W, 250)
	save("03-failed-detail-and-status.png", detail)
}
