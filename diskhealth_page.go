package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/diskhealth"
)

// diskhealth_page.go owns plugin registration, the page shell and its fleet
// summary; cards and charts live beside it in their own files.
//
// diskPluginID is the plugin's id in the registry (plugins.go).
const diskPluginID = "disks"

// registerDiskPlugin registers the 磁盘健康 plugin: the page files.go
// dispatches for pathDisks, the sidebar row sidebar.go hides while the
// plugin is off, and the settings it declares for the 设置 dialog's 插件
// page — the high-temperature warning the cards colour by, its threshold
// kept through pluginGet/pluginSet like every plugin setting.
func registerDiskPlugin() {
	RegisterPlugin(Plugin{
		ID:        diskPluginID,
		Name:      "磁盘健康",
		Desc:      "查看磁盘型号、健康评分、温度、寿命与实时读写",
		OnEnable:  func(a *App) { a.diskPluginEnabled() },
		OnDisable: func(a *App) { a.diskPluginDisabled() },
		Settings: func(a *App) []PluginSetting {
			limit, on := a.diskWarnTemp()
			return []PluginSetting{
				{
					Kind:   PluginSettingSwitch,
					Name:   "高温警告",
					Desc:   "温度达到阈值时把该盘判为「注意」",
					On:     on,
					Change: func(v string) { a.pluginSet(diskPluginID, "tempWarn", v) },
				},
				{
					Kind:    PluginSettingSelect,
					Name:    "警告阈值",
					Desc:    "触发高温警告的温度（°C）",
					Options: []string{"40", "50", "60", "70"},
					Value:   fmt.Sprintf("%.0f", limit),
					Change:  func(v string) { a.pluginSet(diskPluginID, "tempLimit", v) },
				},
				{
					Kind:   PluginSettingAction,
					Name:   "复制磁盘报告",
					Desc:   "把全部磁盘的详细信息复制到剪贴板",
					Action: "复制",
					Run: func() {
						mygo.Clipboard.WriteText(diskReport(diskhealth.Read()))
						a.statusMsg = "已复制磁盘报告"
					},
				},
			}
		},
	})
}

// diskWarnTemp is the high-temperature warning the plugin's settings keep:
// the threshold a disk's temperature crosses to read 注意, and whether the
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
	case "70":
		limit = 70
	}
	return limit, on
}

// diskWarnLimit is the threshold the score and checks evaluate against, or
// 0 when the warning is switched off — which disables the temperature
// check rather than judging every drive against a threshold the user
// turned off.
func (a *App) diskWarnLimit() float64 {
	limit, on := a.diskWarnTemp()
	if !on {
		return 0
	}
	return limit
}

// diskHot reports whether the disk's temperature reaches the warning
// threshold. A disk without a sensor is never hot.
func (a *App) diskHot(d diskhealth.Disk) bool {
	if !d.HasTemperature() {
		return false
	}
	limit, on := a.diskWarnTemp()
	return on && d.Temperature >= limit
}

// disksWatch starts the page's background probe and stops it again when the
// tab leaves the page or the plugin is switched off.
func (a *App) disksWatch() {
	if a.cur().path != pathDisks || !a.pluginEnabled(diskPluginID) {
		return
	}
	if a.diskWatch == nil {
		a.diskWatch = diskhealth.Watch()
	}
}

// disksUnwatch stops the page's probe, if it is running.
func (a *App) disksUnwatch() {
	if a.diskWatch != nil {
		a.diskWatch()
		a.diskWatch = nil
	}
}

func (a *App) diskPluginEnabled()  { a.disksWatch() }
func (a *App) diskPluginDisabled() { a.disksUnwatch() }

// — the 磁盘健康 page —

// disksView is the page: a summary strip, then one card per drive. The
// card leads with a health score ring, because that is the one number a
// person opens this page for; everything else either explains the score
// (the checks), shows live activity (the throughput chart) or records the
// drive's identity and history (the facts table).
func (a *App) disksView(c *ui.Context) {
	if !a.pluginEnabled(diskPluginID) {
		a.disksUnwatch()
		a.paneMessage(c, "插件已停用")
		return
	}
	a.disksWatch()
	r := a.readDiskReport()

	ui.Scroll(c).Grow(1).FillWidth().Padding(24, 20, 24, 24).Gap(16).Children(func() {
		a.disksHeader(c, r)
		if len(r.Disks) == 0 {
			a.disksEmpty(c)
			return
		}
		a.disksSummary(c, r)
		for _, d := range r.Disks {
			a.diskCard(c, d, r)
		}
		if len(r.Notes) > 0 {
			a.disksNotes(c, r.Notes)
		}
	})
}

// readDiskReport returns the page's current report. Tests inject a fixed
// report so layout checks never depend on the host's drives or smartctl.
func (a *App) readDiskReport() diskhealth.Report {
	if a.diskRead != nil {
		return a.diskRead()
	}
	return diskhealth.ReadAsync()
}

// disksHeader is the page title, the refresh control and the provenance
// line: what answered, and when.
func (a *App) disksHeader(c *ui.Context, r diskhealth.Report) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
		components.Icon(c, lucide.HardDrive()).Size(22, 22).TextColor(colorAccent()).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, "磁盘健康").FontSize(18).FontWeight(600).TextColor(colorText())
			ui.Text(c, diskProvenance(r)).FontSize(11).TextColor(colorMuted()).SingleLine()
		})
		a.diskRefreshButton(c)
	})
}

// diskProvenance says how fresh the report is and what answered.
func diskProvenance(r diskhealth.Report) string {
	if r.At.IsZero() {
		return "正在读取磁盘信息…"
	}
	parts := []string{"读取于 " + r.At.Format("15:04:05")}
	if len(r.Sources) > 0 {
		parts = append(parts, "来源 "+strings.Join(r.Sources, " + "))
	}
	if r.HistorySpan > 0 {
		parts = append(parts, "采样 "+shortSpan(r.HistorySpan))
	}
	return strings.Join(parts, " · ")
}

// shortSpan renders a sampling window as "3 分钟".
func shortSpan(d time.Duration) string {
	secs := int(d.Seconds())
	switch {
	case secs < 60:
		return fmt.Sprintf("%d 秒", secs)
	case secs < 3600:
		return fmt.Sprintf("%d 分钟", secs/60)
	}
	return fmt.Sprintf("%d 小时", secs/3600)
}

// diskRefreshButton reloads now: it clears the cache and asks for a frame.
func (a *App) diskRefreshButton(c *ui.Context) {
	btn := components.IconButton(c, lucide.RefreshCw(), "重新读取", components.IconButtonPlain)
	if btn.Clicked() {
		diskhealth.Refresh()
		repaint()
	}
}

// disksSummary is the fleet line: how many drives, their worst state, the
// hottest reading and the total space. It is one row of tiles so a machine
// with several drives reads at a glance.
func (a *App) disksSummary(c *ui.Context, r diskhealth.Report) {
	var good, warn, fail, unknown int
	var hottest float64 = -1
	var total, free int64
	for _, d := range r.Disks {
		score, ok := d.Score(a.diskWarnLimit())
		if !ok {
			unknown++
		} else {
			switch {
			case score >= 90:
				good++
			case score >= 50:
				warn++
			default:
				fail++
			}
		}
		if d.HasTemperature() && d.Temperature > hottest {
			hottest = d.Temperature
		}
		// The summary counts the capacity the page can actually show: the
		// volume pools' total, which for APFS is smaller than the drive's
		// raw size because the partition map and slack are not usable
		// space. A disk with no mounted volume contributes its size.
		if d.Total > 0 {
			total += d.Total
		} else {
			total += d.Size
		}
		if d.Free > 0 {
			free += d.Free
		}
	}
	tiles := []struct {
		icon  *ui.SVG
		label string
		value string
		tone  ui.Color
	}{
		{lucide.HardDrive(), "磁盘", fmt.Sprintf("%d 块", len(r.Disks)), colorText()},
		{lucide.CircleCheck(), "健康", summaryHealth(good, warn, fail, unknown), summaryTone(c, good, warn, fail)},
		{lucide.Thermometer(), "最高温度", tempSummary(hottest), tempTone(c, hottest)},
		{lucide.Database(), "总容量", capacitySummary(total, free), colorText()},
	}
	ui.Row(c).FillWidth().Gap(10).Shrink(0).Children(func() {
		for _, t := range tiles {
			a.diskSummaryTile(c, t.icon, t.label, t.value, t.tone)
		}
	})
}

func summaryHealth(good, warn, fail, unknown int) string {
	parts := make([]string, 0, 3)
	if good > 0 {
		parts = append(parts, fmt.Sprintf("%d 正常", good))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d 注意", warn))
	}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d 异常", fail))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d 未知", unknown))
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

func summaryTone(c *ui.Context, good, warn, fail int) ui.Color {
	switch {
	case good == 0 && warn == 0 && fail == 0:
		return colorMuted() // all 未知: nothing to call healthy
	case fail > 0:
		return toneDanger(c)
	case warn > 0:
		return toneWarn(c)
	}
	return toneGood(c)
}

func tempSummary(t float64) string {
	if t < 0 {
		return "无传感器"
	}
	return fmt.Sprintf("%.0f °C", t)
}

func tempTone(c *ui.Context, t float64) ui.Color {
	switch {
	case t < 0:
		return colorMuted()
	case t >= 60:
		return toneDanger(c)
	case t >= 50:
		return toneWarn(c)
	}
	return toneGood(c)
}

func capacitySummary(total, free int64) string {
	if total <= 0 {
		return "—"
	}
	if free <= 0 {
		return formatBytes(total)
	}
	return fmt.Sprintf("%s / %s", formatBytes(free), formatBytes(total))
}

// diskSummaryTile is one summary tile: an icon, a label and the value.
func (a *App) diskSummaryTile(c *ui.Context, icon *ui.SVG, label, value string, tone ui.Color) {
	tile := ui.Column(c).Grow(1).MinWidth(0).Radius(8).Padding(12).
		Background(colorAddress()).Gap(6)
	tile.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			components.Icon(c, icon).Size(14, 14).TextColor(colorMuted()).Shrink(0)
			ui.Text(c, label).FontSize(11).TextColor(colorMuted()).SingleLine()
		})
		ui.Text(c, value).FontSize(14).FontWeight(600).
			TextColor(tone).SingleLine().Grow(1)
	})
}
