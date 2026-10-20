package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/brandicons"
	"sunvale/diskhealth"
)

// disksEmpty explains an empty probe: still reading, or nothing found.
func (a *App) disksEmpty(c *ui.Context) {
	message := "正在读取磁盘信息…"
	detail := "首次读取需要调用系统工具，通常不到一秒。"
	if diskhealth.Ready() {
		message = "未能读取到磁盘信息"
		detail = "系统没有返回任何物理磁盘。外置磁盘盒可能需要额外驱动。"
	}
	ui.Column(c).FillWidth().Padding(0, 40).AlignItems(ui.Center).Gap(8).Children(func() {
		components.Icon(c, lucide.HardDrive()).Size(36, 36).TextColor(colorMuted().Alpha(0.6))
		ui.Text(c, message).FontSize(13).TextColor(colorText())
		ui.Text(c, detail).FontSize(11).TextColor(colorMuted()).TextAlign(ui.Center)
	})
}

// disksNotes lists the probe's capability notes under the cards.
func (a *App) disksNotes(c *ui.Context, notes []string) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		for _, n := range notes {
			ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
				components.Icon(c, lucide.Info()).Size(12, 12).
					TextColor(colorMuted()).Shrink(0).Margin(1, 0, 0, 0)
				ui.Text(c, n).FontSize(11).TextColor(colorMuted()).Grow(1).MinWidth(0)
			})
		}
	})
}

// — colours —

// toneGood, toneWarn and toneDanger are the state colours, read from the
// frame theme — appTheme installs the app's Windows 11 Success, Warning
// and Danger tokens there — so they follow the appearance and the
// high-contrast preference like every other control colour.
func toneGood(c *ui.Context) ui.Color   { return c.Theme().Success }
func toneWarn(c *ui.Context) ui.Color   { return c.Theme().Warning }
func toneDanger(c *ui.Context) ui.Color { return c.Theme().Danger }

// diskIconColor is the card badge's fill, tinted by health and heat.
func (a *App) diskIconColor(c *ui.Context, d diskhealth.Disk) ui.Color {
	if a.diskHot(d) || d.Health == diskhealth.HealthWarning {
		return toneWarn(c)
	}
	if d.Health == diskhealth.HealthFailing {
		return toneDanger(c)
	}
	return colorAccent()
}

// diskBadge is the card's leading tile: the drive maker's logo on a tinted
// square when Simple Icons carries the brand, else the generic drive glyph.
// The tint carries the drive's state either way, so a white logo on it stays
// legible whatever the brand's own colour would have been.
func (a *App) diskBadge(c *ui.Context, d diskhealth.Disk) {
	tone := a.diskIconColor(c, d)
	box := ui.Box(c).Size(34, 34).Shrink(0).Radius(8)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, tone, r.H/2)
	})
	box.Children(func() {
		if logo := brandicons.Lookup(d.Model); logo != nil && logo.SVG != nil {
			ui.Image(c, logo.SVG).Size(20, 20).Fit(ui.Contain).
				TextColor(ui.RGBA(255, 255, 255, 0.95))
			box.Label(logo.Label + " " + d.Model)
			components.Tooltip(c, box, logo.Label)
			return
		}
		a.diskGlyph(c, 18, ui.RGBA(255, 255, 255, 0.92))
	})
}

// diskGlyph draws a small hard-drive glyph: the platter with a read-head
// arm and the status light.
func (a *App) diskGlyph(c *ui.Context, px float32, col ui.Color) {
	ui.Box(c).Size(px, px).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		x, y, s := r.X, r.Y, r.W
		ink := col.Mix(ui.RGB(0, 0, 0), 0.35)
		p.Fill(ui.Rect{X: x + s*0.06, Y: y + s*0.22, W: s * 0.88, H: s * 0.56},
			col, s*0.08)
		p.Fill(ui.Rect{X: x + s*0.14, Y: y + s*0.30, W: s * 0.24, H: s * 0.10},
			ui.RGBA(255, 255, 255, 0.45), s*0.03)
		p.Fill(ui.Rect{X: x + s*0.62, Y: y + s*0.52, W: s * 0.16, H: s * 0.10},
			ink, s*0.03)
		p.Fill(ui.Rect{X: x + s*0.18, Y: y + s*0.78, W: s * 0.64, H: s * 0.04},
			ink.Alpha(0.6), s*0.02)
	})
}

// — shared text helpers —

func orUnknown(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}

func labelBytes(v int64) string {
	if v < 0 {
		return "未知"
	}
	return formatBytes(v)
}

// formatCount abbreviates a command count: 6.1B, 2.9M.
func formatCount(n int64) string {
	switch {
	case n < 0:
		return "未知"
	case n >= 1e9:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// diskHoursLabel renders hours as days and hours, with the raw hours
// beside it — a support report wants both.
func diskHoursLabel(hours int64) string {
	if hours < 0 {
		return "未知"
	}
	if hours < 24 {
		return fmt.Sprintf("%d 小时", hours)
	}
	return fmt.Sprintf("%d 天 %d 小时（%d h）", hours/24, hours%24, hours)
}

// diskReport is the 复制磁盘报告 payload: every field the page knows,
// labelled, so a support case can carry the drives' state without a
// screenshot. Missing readings are written as 未知, never as zero.
func diskReport(r diskhealth.Report) string {
	var b strings.Builder
	b.WriteString("Sunvale 磁盘报告\n")
	b.WriteString("读取于 " + r.At.Format("2006-01-02 15:04:05") + "\n")
	if len(r.Sources) > 0 {
		b.WriteString("来源：" + strings.Join(r.Sources, " + ") + "\n")
	}
	for _, n := range r.Notes {
		b.WriteString("备注：" + n + "\n")
	}
	for _, d := range r.Disks {
		b.WriteString("\n")
		b.WriteString(orUnknown(d.Model) + " (" + d.ID + ")\n")
		line := func(k, v string) { b.WriteString("  " + k + "：" + v + "\n") }
		line("接口", orUnknown(d.Bus))
		line("介质", orUnknown(d.Media))
		line("连接", internalLabel(d))
		line("容量", formatBytes(d.Size))
		line("序列号", orUnknown(d.Serial))
		line("固件", orUnknown(d.Firmware))
		line("SMART", orUnknown(d.HealthNote))
		line("健康评分", scoreText(d))
		line("温度", d.TemperatureLabel())
		line("写入寿命", d.LifeLabel())
		line("备用块", d.SpareLabel())
		line("通电时长", diskHoursLabel(d.PowerOnHours))
		line("通电次数", labelInt(d.PowerCycles))
		line("异常断电", labelInt(d.UnsafeShutdowns))
		line("介质错误", labelInt(d.MediaErrors))
		line("累计读取", labelBytes(d.ReadBytes))
		line("累计写入", labelBytes(d.WriteBytes))
		line("可用/总量", labelBytes(d.Free)+" / "+labelBytes(d.Total))
		b.WriteString("  检测项：\n")
		for _, ch := range d.Checks(50) {
			b.WriteString(fmt.Sprintf("    %s：%s（%s）%s\n",
				ch.Name, ch.Value, ch.State, ch.Note))
		}
		for _, v := range d.Volumes {
			b.WriteString(fmt.Sprintf("  卷 %s (%s) %s %s\n",
				v.Name, v.Role, orUnknown(v.MountPoint), formatBytes(v.Size)))
		}
	}
	return b.String()
}

func internalLabel(d diskhealth.Disk) string {
	if d.Internal {
		return "内置"
	}
	return "外置"
}

func scoreText(d diskhealth.Disk) string {
	score, ok := d.Score(50)
	if !ok {
		return "未知（无可用指标）"
	}
	return fmt.Sprintf("%d 分 · %s", score, diskhealth.ScoreLabel(score))
}

func labelInt(v int64) string {
	if v < 0 {
		return "未知"
	}
	return fmt.Sprintf("%d", v)
}
