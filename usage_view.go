package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// usage_view.go paints the 占用 dialog: status, treemap cells, top list and
// buttons. Split from usage.go (LAY1).

func (a *App) usageModal(c *ui.Context) {
	flag := a.usageOpenFlag()
	if !*flag {
		return
	}
	floatDialog(c, flag, "可视化查看占用", 560, false, func() {
		a.usageBody(c, flag)
	})
}

// usageBody is the dialog: the folder's status, the drill position row,
// the treemap, the picked cell's line, the largest-first list and the
// buttons.
func (a *App) usageBody(c *ui.Context, flag *bool) {
	res := usageResult.Load()
	here := a.usageHereNode()
	if here == nil && res != nil {
		here = res.tree
		a.usageSetHere(here)
	}
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		ui.Text(c, "可视化查看占用").FontSize(14).FontWeight(600).TextColor(colorText())
		a.usageStatus(c, res)
		a.usagePlaceRow(c, here, flag)
		a.usageMap(c, res, here)
		a.usagePickedInfo(c, here)
		a.usageTopList(c, here)
		a.usageButtons(c, res, flag)
	})
}

// usageStatus is the muted line under the title: what the scan is doing
// with its live folder count, and what it covered.
func (a *App) usageStatus(c *ui.Context, res *usageScan) {
	line := "正在扫描…"
	switch {
	case usageRunningNow():
		line = fmt.Sprintf("正在扫描…（已访问 %d 个文件夹）", usageVisited.Load())
	case res == nil:
	case res.err != "":
		line = res.err
	case len(res.tree.Children) == 0:
		line = "这个文件夹是空的"
	default:
		line = fmt.Sprintf("共 %s · %d 个文件 · %d 个文件夹",
			formatBytes(res.tree.Size), res.files, res.dirs)
		if res.truncated {
			line += "（已达上限，结果可能不全）"
		}
	}
	ui.Text(c, line).FontSize(12).TextColor(colorMuted()).SingleLine().FillWidth()
}

// usagePlaceRow is the drill position's path with 上一级 and 在窗口中打开.
func (a *App) usagePlaceRow(c *ui.Context, here *usageNode, flag *bool) {
	if here == nil {
		return
	}
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, here.Path).FontSize(11).TextColor(colorMuted()).
			SingleLine().Grow(1).MinWidth(0)
		if a.usageParent() != nil {
			if usageButton(c, "上一级", false) {
				a.usageSetHere(a.usageParent())
				a.usageSetPick(nil)
			}
		}
		if usageButton(c, "在窗口中打开", false) {
			*flag = false
			a.navigate(here.Path)
		}
	})
}

// usageMap is the treemap: cells squarified over the canvas, one
// rectangle per child of the drill position.
func (a *App) usageMap(c *ui.Context, res *usageScan, here *usageNode) {
	shell := ui.Box(c).FillWidth().Height(280).Shrink(0).Radius(innerRadius()).
		Border(1, colorDivider()).Background(colorContent()).Clip()
	shell.Children(func() {
		if res == nil || here == nil {
			ui.Text(c, "正在扫描…").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		if len(here.Children) == 0 {
			ui.Text(c, "这个文件夹是空的").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		if here.Size <= 0 {
			ui.Text(c, "没有可显示的占用").FontSize(12).TextColor(colorMuted()).Padding(16)
			return
		}
		b := shell.Bounds()
		if b.W <= 0 {
			return
		}
		r := ui.Rect{X: 2, Y: 2, W: b.W - 4, H: b.H - 4}
		for _, cell := range usageCells(here.Children, r) {
			a.usageCellView(c, cell, here)
		}
	})
}

// usageCellView is one treemap rectangle: the pastel fill, the label when
// the cell has room, click to pick, double-click to drill in.
func (a *App) usageCellView(c *ui.Context, cell usageCell, parent *usageNode) {
	if cell.w < 3 || cell.h < 3 {
		return
	}
	n := cell.node
	col := usageColor(n)
	box := ui.Box(c.Key(n.Path)).Absolute().
		Left(cell.x+1).Top(cell.y+1).
		Size(max(cell.w-2, 1), max(cell.h-2, 1)).Radius(2).Clip()
	switch {
	case a.usagePickedNode() == n:
		box.Background(col.Mix(ui.RGB(0, 0, 0), 0.18)).Border(1.5, colorAccent())
	case box.Hovered():
		box.Background(col.Mix(ui.RGB(255, 255, 255), 0.3))
	default:
		box.Background(col)
	}
	box.Children(func() {
		a.usageCellLabel(c, cell, n, parent)
	})
	if box.Clicked() {
		a.usageSetPick(n)
	}
	if box.DoubleClicked() && n.IsDir {
		a.usageSetHere(n)
		a.usageSetPick(nil)
	}
}

// usageCellLabel overlays the name and size when the cell has room.
func (a *App) usageCellLabel(c *ui.Context, cell usageCell, n, parent *usageNode) {
	if cell.w < 44 || cell.h < 16 {
		return
	}
	ui.Column(c).Fill().Padding(2, 4).Gap(1).AlignItems(ui.Start).Children(func() {
		ui.Text(c, n.Name).FontSize(11).TextColor(usageInk(n)).MaxLines(1).FillWidth()
		if cell.w >= 64 && cell.h >= 30 {
			line := formatBytes(n.Size)
			if share := usageShare(n, parent); share > 0 {
				line += fmt.Sprintf(" · %.1f%%", share*100)
			}
			ui.Text(c, line).FontSize(10).TextColor(usageInk(n)).MaxLines(1).FillWidth()
		}
	})
}

// usagePickedInfo is the picked cell's line: its name, size and share of
// the drilled folder.
func (a *App) usagePickedInfo(c *ui.Context, here *usageNode) {
	n := a.usagePickedNode()
	if n == nil {
		return
	}
	line := fmt.Sprintf("选中：%s · %s", n.Name, formatBytes(n.Size))
	if share := usageShare(n, here); share > 0 {
		line += fmt.Sprintf(" · 占 %s 的 %.1f%%", here.Name, share*100)
	}
	ui.Text(c, line).FontSize(12).TextColor(colorText()).SingleLine().FillWidth()
}

// usageTopList is the largest-first list of the drill position's children:
// the rows click and double-click like the map's cells.
func (a *App) usageTopList(c *ui.Context, here *usageNode) {
	if here == nil || len(here.Children) == 0 {
		return
	}
	top := sortedUsage(here.Children)
	if len(top) > 30 {
		top = top[:30]
	}
	list := ui.Scroll(c).FillWidth().MaxHeight(180).Gap(2)
	list.Children(func() {
		for _, n := range top {
			a.usageTopRow(c, n, here)
		}
	})
}

// usageTopRow is one list row: the colour chip, the name, the share, the
// size and the share bar.
func (a *App) usageTopRow(c *ui.Context, n, here *usageNode) {
	row := ui.Row(c.Key(n.Path)).FillWidth().Height(24).Shrink(0).
		Padding(0, 8).AlignItems(ui.Center).Gap(8).Radius(4)
	switch {
	case a.usagePickedNode() == n:
		row.Background(colorSelected())
	case row.Hovered():
		row.Background(colorHover())
	}
	row.Children(func() {
		chip := ui.Box(c).Size(8, 8).Shrink(0).Radius(2)
		chip.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, usageColor(n), 2)
		})
		name := n.Name
		if n.IsDir {
			name += "/"
		}
		ui.Text(c, name).FontSize(12).TextColor(colorText()).
			Grow(1).MinWidth(0).MaxLines(1)
		ui.Text(c, fmt.Sprintf("%.1f%%", usageShare(n, here)*100)).
			FontSize(11).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, formatBytes(n.Size)).FontSize(11).TextColor(colorMuted()).Shrink(0)
		bar := ui.Box(c).Width(90).Height(6).Shrink(0).Radius(3)
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, colorDivider(), 3)
			if w := r.W * float32(usageShare(n, here)); w > 0 {
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: min(w, r.W), H: r.H}, usageColor(n), 3)
			}
		})
	})
	if row.Clicked() {
		a.usageSetPick(n)
	}
	if row.DoubleClicked() && n.IsDir {
		a.usageSetHere(n)
		a.usageSetPick(nil)
	}
}

// usageButtons is the dialog's button row: 重新扫描 and 关闭.
func (a *App) usageButtons(c *ui.Context, res *usageScan, flag *bool) {
	btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
	btns.Children(func() {
		if res != nil && res.err == "" {
			if usageButton(c, "重新扫描", false) {
				a.requestUsage(res.root)
			}
		}
		if usageButton(c, "关闭", true) {
			*flag = false
		}
	})
}

// usageButton is a dialog button in the primary or secondary style,
// reporting whether it was clicked. Height 28, one under uikitSecondaryButton,
// for the space-tight scan dialog — with that button's padding and floor width,
// so 重新扫描 and 关闭 come out even.
func usageButton(c *ui.Context, text string, primary bool) bool {
	btn := ui.ButtonBase(c).Height(28).Padding(0, btnPadX).MinWidth(btnMinW).Shrink(0).Radius(4).Label(text)
	if primary {
		btn = btn.Background(colorAccent())
	} else {
		btn = btn.Background(colorAddress()).Border(1, colorDivider())
	}
	btn.Children(func() {
		fg := colorText()
		if primary {
			fg = colorBtnText()
		}
		ui.Text(c, text).FontSize(12).TextColor(fg)
	})
	return btn.Clicked()
}

// — Colours —

// usageColor is a cell's pastel fill: folders one blue, files tinted by
// their extension's hue, 其他 gray — light enough for dark text.
func usageColor(n *usageNode) ui.Color {
	if n.IsDir && n.Path != "" {
		return hslColor(211, 0.58, 0.76)
	}
	if !n.IsDir && n.Ext != "" {
		return hslColor(float64(extHue(n.Ext)), 0.58, 0.76)
	}
	return hslColor(0, 0, 0.82)
}

// usageInk is the label ink on a cell's pastel fill: dark enough to read
// on every tint the map uses.
func usageInk(*usageNode) ui.Color {
	return ui.RGB(28, 32, 44)
}

// extHue spreads extensions around the wheel by their bytes, so the same
// type keeps its colour and different types spread apart.
func extHue(ext string) int {
	h := 0
	for i := 0; i < len(ext); i++ {
		h = h*31 + int(ext[i])
	}
	if h < 0 {
		h = -h
	}
	return h % 360
}

// hslColor converts HSL — h in degrees, s and l from 0 to 1 — to the UI's
// color.
func hslColor(h, s, l float64) ui.Color {
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(math.Mod(h, 360)+360, 360) / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	r, g, b := 0.0, 0.0, 0.0
	switch {
	case hp < 1:
		r, g, b = c, x, 0
	case hp < 2:
		r, g, b = x, c, 0
	case hp < 3:
		r, g, b = 0, c, x
	case hp < 4:
		r, g, b = 0, x, c
	case hp < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	return ui.RGB(uint8((r+m)*255+0.5), uint8((g+m)*255+0.5), uint8((b+m)*255+0.5))
}

// usageShare is the node's share of its parent, 0 when the parent has no
// bytes to divide.
func usageShare(n, parent *usageNode) float64 {
	if parent == nil || parent.Size <= 0 {
		return 0
	}
	return float64(n.Size) / float64(parent.Size)
}
