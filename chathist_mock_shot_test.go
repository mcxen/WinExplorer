package main

import (
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

// chathist_mock_shot_test.go renders the CHATHIST placement mockups
// (docs/specs/13-chat-history.md) when SUNVALE_SHOT_DIR is set; set
// SUNVALE_CHATHIST=a|b|c to render one variant only. Nothing here ships:
// the assistant pane is a static mock beside the real window, and the
// sessions below are demo data.

type mockSess struct {
	group, title, when, folder string
	n                          int
	pin                        bool
}

var mockSessions = []mockSess{
	{"置顶", "按月份归档下载目录", "09:42", "Downloads", 14, true},
	{"今天", "批量重命名京都照片", "11:05", "旅行-京都", 9, false},
	{"今天", "找出重复的 4K 视频", "10:18", "Movies", 6, false},
	{"今天", "解压 project-src 失败原因", "08:51", "Downloads", 4, false},
	{"昨天", "季度报告转 PDF 并压缩", "17:30", "季度报告", 11, false},
	{"昨天", "清理 node_modules 占用", "14:02", "项目", 7, false},
	{"更早", "NAS 备份目录结构建议", "10月21日", "NAS/备份", 18, false},
	{"更早", "预算表按部门拆分", "10月19日", "文档", 5, false},
	{"更早", "整理桌面截图", "10月15日", "Desktop", 3, false},
	{"更早", "照片按拍摄日期分文件夹", "10月12日", "照片", 22, false},
}

var mockChat = []struct {
	user bool
	text string
}{
	{true, "把京都的照片按拍摄日期重命名，格式 2026-09-18 京都 001"},
	{false, "找到 86 张照片（HEIC 62，JPG 24），拍摄日期跨 9/16–9/21。将按 EXIF 时间排序后重命名，同日内顺序编号。"},
	{false, "已完成 86 / 86 项，可在操作卡上撤销。"},
	{true, "再把视频也按同样规则处理"},
}

func chIcon(c *ui.Context, ic *ui.SVG, col ui.Color) {
	components.Icon(c, ic).Size(13, 13).TextColor(col).Shrink(0)
}

// chSearch is a static search field: input radius 4, mist fill.
func chSearch(c *ui.Context, hint string) {
	ui.Row(c.Key("search")).FillWidth().Height(26).Padding(0, 8).Gap(6).AlignItems(ui.Center).
		Radius(4).Background(colorAddress()).Children(func() {
		chIcon(c, lucide.Search(), colorMuted())
		ui.Text(c, hint).FontSize(11.5).TextColor(colorMuted()).Grow(1).SingleLine()
	})
}

// chRow is one dense history row (row radius 4). wide adds count + folder
// columns; hover shows the row actions.
func chRow(c *ui.Context, i int, s mockSess, wide, cur, hover bool) {
	cc := c.Key("h" + itoa(i))
	row := ui.Row(cc).FillWidth().Height(28).Padding(0, 8).Gap(6).AlignItems(ui.Center).Radius(4)
	switch {
	case cur:
		row.Background(colorSelected())
	case hover:
		row.Background(colorHover())
	}
	row.Children(func() {
		if s.pin {
			chIcon(cc, lucide.Pin(), colorAccent())
		}
		t := ui.Text(cc.Key("t"), s.title).FontSize(12).TextColor(colorText()).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if cur {
			t.FontWeight(600)
		}
		if hover {
			for _, ic := range []*ui.SVG{lucide.Pin(), lucide.PencilLine(), lucide.Trash()} {
				chIcon(cc, ic, colorMuted())
			}
			return
		}
		if wide {
			ui.Text(cc.Key("f"), s.folder).FontSize(11).TextColor(colorMuted()).SingleLine().Ellipsis("…").Width(76).Shrink(0)
			ui.Text(cc.Key("n"), itoa(s.n)).FontSize(11).TextColor(colorMuted()).Width(20).Shrink(0)
		}
		ui.Text(cc.Key("w"), s.when).FontSize(11).TextColor(colorMuted()).Shrink(0)
	})
}

func chList(c *ui.Context, wide bool, max int) {
	group := ""
	for i, s := range mockSessions {
		if max > 0 && i >= max {
			break
		}
		if s.group != group {
			group = s.group
			ui.Text(c.Key("g"+group), group).FontSize(10.5).FontWeight(600).TextColor(colorMuted()).Padding(8, 8, 2, 8)
		}
		chRow(c, i, s, wide, i == 1, i == 2)
	}
}

func chHeader(c *ui.Context, title string, histOn bool, caret bool) {
	ui.Row(c.Key("hdr")).FillWidth().Height(40).Padding(0, 10).Gap(6).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(colorDivider()).Children(func() {
		ic := colorMuted()
		if histOn {
			ic = colorAccent()
		}
		chIcon(c, lucide.MessagesSquare(), ic)
		ui.Text(c, title).FontSize(12.5).FontWeight(600).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0)
		if caret {
			chIcon(c, lucide.ChevronDown(), colorMuted())
		}
		ui.Box(c).Grow(1)
		for _, x := range []*ui.SVG{lucide.SquarePen(), lucide.Settings(), lucide.X()} {
			chIcon(c, x, colorMuted())
		}
	})
}

func chChat(c *ui.Context) {
	ui.Column(c.Key("chat")).Grow(1).FillWidth().FillHeight().MinWidth(0).MinHeight(0).Padding(12).Gap(10).Children(func() {
		for i, m := range mockChat {
			cc := c.Key("m" + itoa(i))
			b := ui.Box(cc).Padding(7, 10).Radius(8).MaxWidth(300)
			if m.user {
				b.Background(colorSelected()).AlignSelf(ui.End)
			} else {
				b.Background(c.Theme().Surface).Border(1, colorDivider())
			}
			b.Children(func() { ui.Text(cc, m.text).FontSize(12).TextColor(colorText()).Wrap() })
		}
		ui.Box(c).Grow(1)
		ui.Row(c.Key("comp")).FillWidth().Height(64).Padding(8, 10).Radius(8).Border(1, colorDivider()).
			Background(c.Theme().Surface).AlignItems(ui.End).Children(func() {
			ui.Text(c, "让助手整理这个文件夹…").FontSize(12).TextColor(colorMuted()).Grow(1).AlignSelf(ui.Start)
			chIcon(c, lucide.ArrowUp(), colorAccent())
		})
	})
}

func chPane(c *ui.Context, variant string) {
	ui.Column(c).Fill().Background(c.Theme().Background).BorderWidth(0, 0, 0, 1).BorderColor(colorDivider()).Children(func() {
		switch variant {
		case "a": // flip: history replaces the conversation
			chHeader(c, "历史会话 · 38", true, false)
			ui.Column(c.Key("body")).Grow(1).FillWidth().Padding(8).Gap(2).Children(func() {
				ui.Row(c.Key("tools")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c.Key("s")).Grow(1).Children(func() { chSearch(c, "搜索标题与内容") })
					v := 0
					ui.Segmented(c, &v, "全部", "本文件夹").Shrink(0)
				})
				chList(c, true, 0)
			})
		case "b": // collapsible column beside the conversation
			chHeader(c, "批量重命名京都照片", true, false)
			ui.Row(c.Key("split")).Grow(1).FillWidth().MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				ui.Column(c.Key("col")).Width(176).Shrink(0).FillHeight().Padding(6).Gap(1).Background(c.Theme().Surface).Children(func() {
					chSearch(c, "搜索")
					chList(c, false, 0)
				})
				ui.Box(c).Width(1).FillHeight().Background(colorDivider())
				chChat(c)
			})
		case "c": // header switcher: inline list pushes the chat down
			chHeader(c, "批量重命名京都照片", false, true)
			ui.Column(c.Key("drop")).FillWidth().Padding(6, 8).Gap(1).BorderWidth(0, 0, 1, 0).BorderColor(colorDivider()).
				Background(c.Theme().Surface).Children(func() {
				chSearch(c, "切换会话")
				chList(c, true, 6)
				ui.Text(c.Key("more"), "全部 38 个会话").FontSize(11.5).TextColor(colorAccent()).Padding(6, 8, 2, 8)
			})
			chChat(c)
		}
	})
}

func TestChatHistMockShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	only := os.Getenv("SUNVALE_CHATHIST")
	t.Cleanup(func() { visualV2 = false; testDark = nil; resolveTokens() })
	files := t.TempDir()
	for _, n := range []string{"旅行-京都", "Movies", "项目"} {
		_ = os.Mkdir(filepath.Join(files, n), 0o755)
	}
	for _, n := range []string{"季度报告.pdf", "预算-2026.xlsx", "会议纪要.md"} {
		_ = os.WriteFile(filepath.Join(files, n), []byte("x"), 0o644)
	}
	const W, H, sideW = 1280, 800, 460
	shots := []struct {
		v, name string
		dark    bool
	}{
		{"a", "01-a-flip-light.png", false},
		{"b", "02-b-column-light.png", false},
		{"c", "03-c-switcher-light.png", false},
		{"b", "04-b-column-dark.png", true},
		{"a", "05-a-flip-dark.png", true},
	}
	for _, s := range shots {
		if only != "" && only != s.v {
			continue
		}
		d := s.dark
		testDark = &d
		visualV2 = true
		resolveTokens()
		a := newStoreApp(t, files)
		a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
		a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
		left := renderTester(a.view, W-sideW, H)
		v := s.v
		side := renderTester(func(c *ui.Context) { chPane(c, v) }, sideW, H)
		out := image.NewRGBA(image.Rect(0, 0, W, H))
		draw.Draw(out, image.Rect(0, 0, W-sideW, H), left, image.Point{}, draw.Src)
		draw.Draw(out, image.Rect(W-sideW, 0, W, H), side, image.Point{}, draw.Src)
		f, err := os.Create(filepath.Join(dir, s.name))
		if err != nil {
			t.Fatal(err)
		}
		_ = png.Encode(f, out)
		f.Close()
	}
}
