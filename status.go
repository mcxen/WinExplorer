package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/diskhealth"
)

// statusBar is the 28 DIP strip under the file pane. Left: item counts,
// and statusMsg when an operation has something to say — view also shows a
// fresh statusMsg as a toast. Right: the view
// mode. A hairline along the top separates it from the body.
// statusRecentDone is how long the bar keeps the finished run's tally.
const statusRecentDone = 4 * time.Second

func (a *App) statusBar(c *ui.Context) {
	bar := windowBG(c, ui.Row(c).Height(28).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center)).
		BorderWidth(1, 0, 0, 0).BorderColor(colorDivider())
	if visualV2 {
		// macOS 26: no rule; the floating panes end above a quiet strip.
		bar.Height(24).Padding(0, 16).BorderWidth(0, 0, 0, 0)
	}
	bar.Children(func() {
		left := a.statusLeft()
		ui.Text(c, left).FontSize(statusFont()).TextColor(colorMuted()).SingleLine().Grow(1).MinWidth(0)
		// The busy heat: while a run — the assistant's, or the user's own
		// unpack — touches files the dot breathes beside the count; right
		// after, the tally stays a few seconds.
		if paintArchiveStatus(c, a.toggleTasksView) {
			// The archive strip says more than the busy count.
		} else if n := aiFx.activeNow(); n > 0 {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Shrink(0).Children(func() {
				breathDot(c, 7)
				ui.Text(c, "处理中 "+itoa(n)+" 项").FontSize(statusFont()).TextColor(colorAccent()).SingleLine()
			})
		} else if n, rem := aiFx.recentDone(statusRecentDone); n > 0 {
			c.After(rem)
			ui.Text(c, "刚处理了 "+itoa(n)+" 项").FontSize(statusFont()).TextColor(colorMuted()).SingleLine().Shrink(0)
		}
		ui.Text(c, a.viewModeLabel()).FontSize(statusFont()).TextColor(colorMuted()).SingleLine().Shrink(0)
	})
}

// statusLeft is "N 个项目", plus the selection when any, plus a short
// status message when one is set. Counts stay even then.
func (a *App) statusLeft() string {
	visible := a.visibleEntries()
	selected := a.selectedEntries()
	if a.cur().path == pathGallery {
		if st := galleryScan.Load(); st != nil && st.loaded {
			return fmt.Sprintf("%d 张图片  画廊（按月分组）", len(st.photos))
		}
		return "正在扫描图片…"
	}
	if a.cur().path == pathDisks {
		if !a.pluginEnabled(diskPluginID) {
			return "磁盘信息插件已停用"
		}
		r := diskhealth.ReadAsync()
		if len(r.Disks) == 0 {
			return "磁盘健康页 · 正在读取…"
		}
		good, warn := 0, 0
		for _, d := range r.Disks {
			switch d.Health {
			case diskhealth.HealthGood:
				good++
			case diskhealth.HealthWarning, diskhealth.HealthFailing:
				warn++
			}
		}
		if warn > 0 {
			return fmt.Sprintf("%d 块磁盘，%d 正常，%d 需注意  磁盘健康页", len(r.Disks), good, warn)
		}
		return fmt.Sprintf("%d 块磁盘，%d 正常  磁盘健康页", len(r.Disks), good)
	}
	text := fmt.Sprintf("%d 个项目", len(visible))
	// The search line is Everything's shape: count, elapsed, truncated
	// query — streamed results keep it moving while the walk runs.
	if label := a.searchStatusLabel(); label != "" {
		text = label + "  " + text
	}
	if n := len(selected); n > 0 {
		text += fmt.Sprintf("，%d 个项目被选中", n)
		if bytes := selectedBytes(selected); bytes > 0 {
			text += " " + formatBytes(bytes)
		}
	}
	if a.statusMsg != "" {
		text = a.statusMsg + "  " + text
	}
	return text
}

// selectedBytes sums the selection's sizes: files their own bytes, folders
// the cached tally the size engine keeps in e.Size once a walk lands. A
// folder whose size is not known yet counts nothing; zero means there is
// nothing worth showing.
func selectedBytes(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		if e.Size <= 0 {
			continue
		}
		n += e.Size
	}
	return n
}

// formatBytes is B, or one decimal under 10 for KB/MB/GB, matching the
// size column. 1024-based, Windows-style labels.
func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n) / 1024
	for i, unit := range units {
		if v < 1024 || i == len(units)-1 {
			if v < 10 {
				return fmt.Sprintf("%.1f %s", v, unit)
			}
			return fmt.Sprintf("%.0f %s", v, unit)
		}
		v /= 1024
	}
	return fmt.Sprintf("%d B", n)
}

func (a *App) viewModeLabel() string {
	switch a.mode {
	case ViewList:
		return "列表"
	case ViewSmallIcons:
		return a.iconLabel("小图标")
	case ViewMediumIcons:
		return a.iconLabel("中等图标")
	case ViewLargeIcons:
		return a.iconLabel("大图标")
	case ViewExtraLarge:
		return a.iconLabel("超大图标")
	default:
		return "详细信息"
	}
}

// iconLabel names an icon view, with the tile size once 放大/缩小 has
// moved it off the mode's own.
func (a *App) iconLabel(name string) string {
	if a.iconZoom <= 1 {
		return name
	}
	return fmt.Sprintf("%s（%d 像素）", name, int(a.iconBoxSize(a.mode)+0.5))
}
