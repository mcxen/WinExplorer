package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// statusBar is the 28 DIP strip under the file pane. Left: item counts,
// and statusMsg when an operation has something to say. Right: the view
// mode. A hairline along the top separates it from the body.
func (a *App) statusBar(c *ui.Context) {
	bar := ui.Row(c).Height(28).FillWidth().Padding(0, 12).Gap(8).
		AlignItems(ui.Center).Background(colorStatus()).
		BorderWidth(1, 0, 0, 0).BorderColor(colorDivider())
	bar.Children(func() {
		left := a.statusLeft()
		ui.Text(c, left).FontSize(12).TextColor(colorMuted()).SingleLine().Grow(1).MinWidth(0)
		ui.Text(c, a.viewModeLabel()).FontSize(12).TextColor(colorMuted()).SingleLine().Shrink(0)
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
	text := fmt.Sprintf("%d 个项目", len(visible))
	if a.cur().search.busy {
		text = "正在搜索…  " + text
	} else if strings.TrimSpace(a.cur().query) != "" && !specialPath(a.cur().path) {
		text = "搜索结果  " + text
	}
	if n := len(selected); n > 0 {
		text += fmt.Sprintf("，%d 个项目被选中", n)
		if bytes := selectedFileBytes(selected); bytes > 0 {
			text += " " + formatBytes(bytes)
		}
	}
	if a.statusMsg != "" {
		text = a.statusMsg + "  " + text
	}
	return text
}

// selectedFileBytes sums sizes of selected files only. Directories
// contribute nothing; zero means there is nothing worth showing.
func selectedFileBytes(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		if e.IsDir || e.Size <= 0 {
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
		return "小图标"
	case ViewMediumIcons:
		return "中等图标"
	case ViewLargeIcons:
		return "大图标"
	case ViewExtraLarge:
		return "超大图标"
	default:
		return "详细信息"
	}
}
