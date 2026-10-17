package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// diskPluginID is the plugin's id in the registry (plugins.go).
const diskPluginID = "disks"

// registerDiskPlugin registers the 磁盘健康 plugin: the page files.go
// dispatches for pathDisks and the sidebar row sidebar.go hides while the
// plugin is off, plus the settings it declares for the 设置 dialog's 插件
// page — the high-temperature warning the cards colour by, its threshold
// kept through pluginGet/pluginSet like every plugin setting.
func registerDiskPlugin() {
	RegisterPlugin(Plugin{
		ID:   diskPluginID,
		Name: "磁盘健康",
		Desc: "查看磁盘信息、温度、读写速度与健康状态",
		Settings: func(a *App) []PluginSetting {
			limit, on := a.diskWarnTemp()
			return []PluginSetting{
				{
					Kind:   PluginSettingSwitch,
					Name:   "高温警告",
					Desc:   "温度超过阈值时在磁盘卡片上标记警告",
					On:     on,
					Change: func(v string) { a.pluginSet(diskPluginID, "tempWarn", v) },
				},
				{
					Kind:    PluginSettingSelect,
					Name:    "警告阈值",
					Desc:    "触发高温警告的温度（°C）",
					Options: []string{"40", "50", "60"},
					Value:   fmt.Sprintf("%.0f", limit),
					Change:  func(v string) { a.pluginSet(diskPluginID, "tempLimit", v) },
				},
			}
		},
	})
}

// DiskInfo is one physical or logical drive's health snapshot.
type DiskInfo struct {
	Name        string // logical name shown to the user: "C:", "disk0"
	Path        string // volume root the capacity is measured at, "" if none
	Model       string
	Serial      string
	Interface   string  // SATA / NVMe / USB / IDE
	Media       string  // 固态硬盘 / 机械硬盘
	Size        int64   // bytes
	Temperature float64 // °C; -1 when the sensor is unavailable
	Health      string  // 良好 / 警告 / 未知
	HealthOK    bool
	ReadSpeed   int64 // cumulative bytes read, from SMART if available
	WriteSpeed  int64 // cumulative bytes written, from SMART if available
	Free, Total int64 // capacity, 0 when unknown
}

// diskHealthCache holds the last read, so a page refresh does not re-run the
// probes more than the poll interval allows.
var diskHealthCache struct {
	sync.Mutex
	at  time.Time
	all []DiskInfo
}

// diskHealthPoll is how often the probes re-run while the page is visible.
const diskHealthPoll = 3 * time.Second

// readDiskHealth lists the machine's disks with their health snapshot,
// falling back to the cache when a fresh read is still within the poll
// window or fails.
func readDiskHealth() []DiskInfo {
	diskHealthCache.Lock()
	defer diskHealthCache.Unlock()
	if time.Since(diskHealthCache.at) < diskHealthPoll {
		return diskHealthCache.all
	}
	got := sysDiskHealth()
	if len(got) > 0 {
		diskHealthCache.all = got
		diskHealthCache.at = time.Now()
	}
	return diskHealthCache.all
}

// disksRefresh forces the next readDiskHealth to probe again.
func disksRefresh() {
	diskHealthCache.Lock()
	diskHealthCache.at = time.Time{}
	diskHealthCache.Unlock()
}

// diskGlyph draws a small hard-drive glyph: the disk platter with a read
// head arm, tinted by the disk's health colour.
func (a *App) diskGlyph(c *ui.Context, px float32, col ui.Color) {
	ui.Box(c).Size(px, px).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		x, y, s := r.X, r.Y, r.W
		ink := col.Mix(ui.RGB(0, 0, 0), 0.15)
		// Platter: a rounded rectangle with a light highlight.
		p.Fill(ui.Rect{X: x + s*0.06, Y: y + s*0.22, W: s * 0.88, H: s * 0.56},
			col, s*0.08)
		p.Fill(ui.Rect{X: x + s*0.14, Y: y + s*0.30, W: s * 0.24, H: s * 0.10},
			ui.RGBA(255, 255, 255, 0.35), s*0.03)
		// The status light.
		p.Fill(ui.Rect{X: x + s*0.68, Y: y + s*0.52, W: s * 0.14, H: s * 0.10},
			ink, s*0.03)
		// The drive tray lines under the platter.
		p.Fill(ui.Rect{X: x + s*0.18, Y: y + s*0.78, W: s * 0.64, H: s * 0.04},
			ink.Alpha(0.5), s*0.02)
	})
}

// — The 磁盘健康 page —

// disksView renders the disk health page: one card per disk with a capacity
// bar, temperature gauge and health badge. The disk info plugin owns the
// page: switched off, the pane keeps a quiet note instead of the cards.
func (a *App) disksView(c *ui.Context) {
	if !a.pluginEnabled(diskPluginID) {
		a.paneMessage(c, "插件已停用")
		return
	}
	disks := readDiskHealth()
	ui.Scroll(c).Grow(1).FillWidth().Padding(24).Gap(12).Children(func() {
		ui.Text(c, "磁盘健康").FontSize(18).FontWeight(600).
			TextColor(colorText())
		ui.Text(c, "磁盘信息、温度、读写速度与健康状态。按 F5 刷新。").
			FontSize(12).TextColor(colorMuted())
		ui.Box(c).Width(0).Height(4).Shrink(0)
		if len(disks) == 0 {
			ui.Text(c, "未能读取到磁盘信息").FontSize(13).
				TextColor(colorMuted())
			return
		}
		for _, d := range disks {
			a.diskCard(c, d)
		}
	})
}

// diskCard is one disk's card: the identity row, the capacity bar, then a
// three-cell strip of temperature, speed and health.
func (a *App) diskCard(c *ui.Context, d DiskInfo) {
	card := ui.Column(c).FillWidth().Radius(8).Padding(16).Gap(10).
		Background(colorAddress()).Shrink(0)
	card.Children(func() {
		// Identity row: name + model on the left, health badge on the right.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			icon := ui.Box(c).Size(28, 28).Shrink(0).Radius(6)
			icon.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, a.diskIconColor(d), r.H/2)
			})
			icon.Children(func() {
				a.diskGlyph(c, 16, a.diskIconColor(d))
			})
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				title := d.Name
				if d.Model != "" {
					title += " · " + d.Model
				}
				ui.Text(c, title).FontSize(13).FontWeight(500).
					TextColor(colorText()).SingleLine()
				sub := d.Interface + " · " + d.Media
				if d.Serial != "" {
					sub += " · 序列号 " + d.Serial
				}
				ui.Text(c, sub).FontSize(11).TextColor(colorMuted()).
					SingleLine()
			})
			a.healthBadge(c, d)
		})
		// Capacity bar.
		if d.Total > 0 {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				bar := ui.Box(c).Grow(1).Height(8).Shrink(0)
				bar.Draw(func(p *ui.Painter, r ui.Rect) {
					drawDiskBar(p, r, d)
				})
				ui.Text(c, fmt.Sprintf("可用 %s，共 %s",
					formatBytes(d.Free), formatBytes(d.Total))).
					FontSize(11).TextColor(colorMuted()).Shrink(0)
			})
		}
		// The three stat cells: temperature, speed, size. A hot disk's
		// temperature cell turns amber and says 偏高.
		tempText, tempCol := d.tempText(), colorAccent()
		if a.diskHot(d) {
			tempText += " 偏高"
			tempCol = ui.Hex("#C77700")
		}
		ui.Row(c).FillWidth().Gap(8).Shrink(0).Children(func() {
			a.diskStatCell(c, "温度", tempText, tempCol, true)
			a.diskStatCell(c, "读写速度", d.speedText(), colorFolder(), true)
			a.diskStatCell(c, "容量", formatBytes(d.Size), colorText(), false)
		})
	})
}

// diskStatCell is one labelled value with a colored marker dot.
func (a *App) diskStatCell(c *ui.Context, label, value string, col ui.Color, colored bool) {
	cell := ui.Column(c).Grow(1).MinWidth(0).Radius(6).
		Background(colorContent()).Padding(10).Gap(3)
	cell.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			dot := ui.Box(c).Size(6, 6).Shrink(0).Radius(3)
			dot.Draw(func(p *ui.Painter, r ui.Rect) {
				if colored {
					p.Fill(r, col, r.H/2)
				} else {
					p.Fill(r, colorMuted(), r.H/2)
				}
			})
			ui.Text(c, label).FontSize(11).TextColor(colorMuted()).SingleLine()
		})
		ui.Text(c, value).FontSize(13).FontWeight(500).
			TextColor(colorText()).SingleLine()
	})
}

// healthBadge is the disk's health pill: 良好 / 警告 / 未知.
func (a *App) healthBadge(c *ui.Context, d DiskInfo) {
	col, bg := colorHealth(d)
	pill := ui.Row(c).Shrink(0).Radius(10).Padding(8, 10).Gap(5).
		AlignItems(ui.Center)
	pill.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, bg, r.H/2)
	})
	pill.Children(func() {
		dot := ui.Box(c).Size(6, 6).Shrink(0).Radius(3)
		dot.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, col, r.H/2)
		})
		ui.Text(c, d.Health).FontSize(11).FontWeight(500).
			TextColor(col).SingleLine()
	})
}

// drawDiskBar paints a capacity bar with the used share in a health-aware
// colour: healthy disks fill accent, a nearly full or unhealthy disk fills
// the warning colour.
func drawDiskBar(p *ui.Painter, r ui.Rect, d DiskInfo) {
	p.Fill(r, colorDivider(), r.H/2)
	if d.Total <= 0 {
		return
	}
	used := float64(d.Total-d.Free) / float64(d.Total)
	if used <= 0.002 {
		return
	}
	if used > 1 {
		used = 1
	}
	col := colorAccent()
	if !d.HealthOK && d.Health != "未知" {
		col, _ = colorHealth(d)
	} else if used > 0.9 {
		col = ui.Hex("#C77700")
	}
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(used), H: r.H}, col, r.H/2)
}

// diskWarnTemp is the high-temperature warning the plugin's settings keep:
// the threshold a disk's temperature crosses to show 偏高, and whether the
// warning is on at all. Defaults: on, 50 °C — a value the 插件 page never
// offers falls back to them.
func (a *App) diskWarnTemp() (limit float64, on bool) {
	on = a.pluginGet(diskPluginID, "tempWarn") != "off"
	limit = 50
	switch a.pluginGet(diskPluginID, "tempLimit") {
	case "40":
		limit = 40
	case "60":
		limit = 60
	}
	return limit, on
}

// diskHot reports whether the disk's temperature crosses the warning
// threshold: the 温度 cell and the card icon colour amber then. A disk
// without a sensor is never hot.
func (a *App) diskHot(d DiskInfo) bool {
	if d.Temperature < 0 {
		return false
	}
	limit, on := a.diskWarnTemp()
	return on && d.Temperature >= limit
}

// diskIconColor is the card icon's fill, tinted by health: good disks get
// the accent, a warning disk — or a hot one, once the high-temperature
// warning says so — the amber.
func (a *App) diskIconColor(d DiskInfo) ui.Color {
	if d.HealthOK && !a.diskHot(d) {
		return colorAccent()
	}
	return ui.Hex("#C77700")
}

// tempText formats the temperature, "未知" when the sensor is unavailable.
func (d DiskInfo) tempText() string {
	if d.Temperature < 0 {
		return "未知"
	}
	return fmt.Sprintf("%.0f °C", d.Temperature)
}

// speedText shows the cumulative read/write totals SMART reports, or the
// interface's nominal speed when SMART is silent.
func (d DiskInfo) speedText() string {
	if d.ReadSpeed > 0 || d.WriteSpeed > 0 {
		return fmt.Sprintf("读 %s / 写 %s",
			formatBytes(d.ReadSpeed), formatBytes(d.WriteSpeed))
	}
	return nominalSpeed(d.Interface)
}

// nominalSpeed maps a disk interface to its rated throughput.
func nominalSpeed(iface string) string {
	switch {
	case iface == "NVMe":
		return "最高 3.5 GB/s"
	case iface == "SATA":
		return "最高 600 MB/s"
	case iface == "USB":
		return "最高 5 Gbps"
	case iface == "IDE":
		return "最高 133 MB/s"
	}
	return "未知"
}

// colorHealth maps a disk's health to its text and background colour.
func colorHealth(d DiskInfo) (fg, bg ui.Color) {
	switch {
	case d.HealthOK && d.Health == "良好":
		return ui.Hex("#0E7C43"), ui.RGBA(14, 124, 67, 0.14)
	case !d.HealthOK && d.Health == "警告":
		return ui.Hex("#C77700"), ui.RGBA(199, 119, 0, 0.14)
	default:
		return colorMuted(), ui.RGBA(0, 0, 0, 0.06)
	}
}
