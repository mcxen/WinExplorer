package main

import (
	"fmt"
	"sort"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"

	"sunvale/diskhealth"
)

// diskhealth_chart.go draws a drive's live throughput and the composition
// of its mounted volumes, keeping each chart's interaction state per disk.
//
// diskChart plots the drive's recent throughput, read and write overlaid.
// Before two samples land there is nothing to plot, and the chart says so
// rather than drawing a flat line that would read as an idle drive.
func (a *App) diskChart(c *ui.Context, d diskhealth.Disk, r diskhealth.Report) {
	samples := r.History[d.ID]
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "实时读写").FontSize(11).FontWeight(500).TextColor(colorMuted())
			ui.Text(c, liveRateLabel(d)).FontSize(11).TextColor(colorText()).Grow(1).SingleLine()
		})
		if len(samples) < 2 {
			ui.Box(c).FillWidth().Height(96).Shrink(0).Radius(innerRadius()).
				Background(colorContent()).Children(func() {
				ui.Text(c, "采样中，约 3 秒后出现曲线").
					FontSize(11).TextColor(colorMuted()).Padding(10, 10)
			})
			return
		}
		data := diskChartData(samples)
		opts := chart.Options{
			Label:      "实时读写速率",
			PlotHeight: 96,
			// The chart's own format is used for the axis; bytes per second
			// is the unit both want.
			FormatValue: func(v float64) string { return formatBytes(int64(v)) + "/s" },
			HideLegend:  false,
			// The default detail panel prints an em dash when nothing is
			// selected. Report the newest sample instead, which is what the
			// card is about.
			Detail: func(c *ui.Context, sel chart.Selection) {
				a.diskChartDetail(c, sel, samples)
			},
		}
		chart.Line(c, a.diskChartState(d.ID), data, opts)
	})
}

// diskChartDetail is the line under the throughput chart: the selected
// sample's figures, or the latest one when nothing is selected.
func (a *App) diskChartDetail(c *ui.Context, sel chart.Selection, samples []diskhealth.Sample) {
	if len(samples) == 0 {
		return
	}
	s := samples[len(samples)-1]
	when := "最新"
	if sel.Index >= 0 && sel.Index < len(samples) {
		s = samples[sel.Index]
		when = s.At.Format("15:04:05")
	}
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Text(c, when).FontSize(10).TextColor(colorMuted()).Shrink(0)
		read := ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0)
		read.Children(func() {
			dot := ui.Box(c).Size(6, 6).Shrink(0).Radius(3)
			dot.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(r, colorAccent(), r.H/2) })
			ui.Text(c, "读 "+formatBytes(int64(s.Read))+"/s").
				FontSize(11).TextColor(colorText())
		})
		write := ui.Row(c).Gap(5).AlignItems(ui.Center).Shrink(0)
		write.Children(func() {
			dot := ui.Box(c).Size(6, 6).Shrink(0).Radius(3)
			dot.Draw(func(p *ui.Painter, r ui.Rect) { p.Fill(r, ui.Hex("#C77700"), r.H/2) })
			ui.Text(c, "写 "+formatBytes(int64(s.Write))+"/s").
				FontSize(11).TextColor(colorText())
		})
	})
}

// diskChartState keeps one chart's interaction state per disk, so
// selecting a point on one card does not move another's.
func (a *App) diskChartState(id string) *chart.State {
	if a.diskCharts == nil {
		a.diskCharts = map[string]*chart.State{}
	}
	st := a.diskCharts[id]
	if st == nil {
		st = &chart.State{Index: -1}
		a.diskCharts[id] = st
	}
	return st
}

// diskChartData turns the samples into the chart's categories: one label
// per sample, the sample's wall-clock time, and two series.
func diskChartData(samples []diskhealth.Sample) chart.Data {
	labels := make([]string, len(samples))
	reads := make([]float64, len(samples))
	writes := make([]float64, len(samples))
	for i, s := range samples {
		labels[i] = s.At.Format("15:04:05")
		reads[i] = s.Read
		writes[i] = s.Write
	}
	return chart.Data{
		Labels: labels,
		Series: []chart.Series{
			{ID: "read", Label: "读", Values: reads, Color: colorAccent()},
			{ID: "write", Label: "写", Values: writes, Color: ui.Hex("#C77700")},
		},
	}
}

// liveRateLabel is the current rate, or why there is none.
func liveRateLabel(d diskhealth.Disk) string {
	switch {
	case d.ReadBytes < 0 && d.WriteBytes < 0:
		return "该盘不提供累计读写计数"
	case !d.RateKnown:
		return "采样中…"
	case d.ReadBps <= 0 && d.WriteBps <= 0:
		return "空闲"
	}
	return fmt.Sprintf("读 %s/s · 写 %s/s",
		formatBytes(int64(d.ReadBps)), formatBytes(int64(d.WriteBps)))
}

// diskVolumeComposition is what is using the disk: a donut of the used
// space per volume group with a legend beside it. APFS keeps a dozen
// housekeeping volumes on one store; they fold into 系统 so the chart
// answers "what is using my space" rather than "what are all these
// partitions".
func (a *App) diskVolumeComposition(c *ui.Context, d diskhealth.Disk) {
	groups := volumeGroups(d.Volumes)
	if len(groups) == 0 {
		return
	}
	slices := make([]chart.Slice, 0, len(groups))
	palette := volumePalette()
	for i, g := range groups {
		slices = append(slices, chart.Slice{
			ID: g.name, Label: g.name, Value: float64(g.used),
			Color: palette[i%len(palette)],
		})
	}
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "卷构成").FontSize(11).FontWeight(500).TextColor(colorMuted()).Grow(1)
			ui.Text(c, fmt.Sprintf("已用 %s", formatBytes(totalUsed(groups)))).
				FontSize(11).TextColor(colorMuted()).Shrink(0)
		})
		// The donut keeps the full width and a fixed plot height: a chart
		// squeezed into a Row slot's width lays its legend out one letter
		// per line.
		st := a.volumeChartState(d.ID)
		chart.Donut(c, st, slices, chart.Options{
			Label:       "卷已用空间",
			PlotHeight:  150,
			HideLegend:  true, // the list below is the legend, with numbers
			FormatValue: func(v float64) string { return formatBytes(int64(v)) },
			Detail:      func(*ui.Context, chart.Selection) {}, // the list carries the figures
		})

		// The legend is Onevra's List, so its rows scroll and keep the
		// component's spacing and accessibility, rather than a hand-built
		// column; one row per group, sized to fit them all.
		list := components.List(c, nil, len(groups), func(i int) {
			g := groups[i]
			ui.Row(c).FillWidth().Padding(8, 6).Gap(8).AlignItems(ui.Center).Children(func() {
				dot := ui.Box(c).Size(8, 8).Shrink(0).Radius(rowR())
				dot.Draw(func(p *ui.Painter, r ui.Rect) {
					p.Fill(r, palette[i%len(palette)], r.H/2)
				})
				ui.Text(c, g.name).FontSize(11).TextColor(colorText()).
					Grow(1).MinWidth(0).SingleLine()
				if g.mount != "" {
					ui.Text(c, g.mount).FontSize(10).TextColor(colorMuted()).
						MaxWidth(160).Shrink(0).SingleLine()
				}
				ui.Text(c, formatBytes(g.used)).FontSize(11).
					TextColor(colorMuted()).Shrink(0)
				if share := usedShare(g.used, groups); share > 0 {
					ui.Text(c, fmt.Sprintf("%.0f%%", share*100)).FontSize(10).
						TextColor(colorMuted()).Width(34).Shrink(0)
				}
			})
		})
		list.FillWidth().Height(float32(len(groups)) * 30).Shrink(0)
	})
}

// totalUsed is the used space the donut plots.
func totalUsed(groups []volumeGroup) int64 {
	var n int64
	for _, g := range groups {
		n += g.used
	}
	return n
}

// usedShare is one group's share of the plotted space.
func usedShare(used int64, groups []volumeGroup) float64 {
	total := totalUsed(groups)
	if total <= 0 {
		return 0
	}
	return float64(used) / float64(total)
}

// volumeGroup is one donut slice: a volume, or the housekeeping volumes
// folded together.
type volumeGroup struct {
	name  string
	used  int64
	mount string
}

// volumeGroups folds the volumes into the handful worth naming: the data
// volumes, and everything else under 系统.
func volumeGroups(vols []diskhealth.Volume) []volumeGroup {
	var data []volumeGroup
	var systemUsed int64
	systemCount := 0
	for _, v := range vols {
		used := v.Used
		if used < 0 {
			used = 0
		}
		switch v.Role {
		case "Data", "System":
			name := v.Name
			if name == "" {
				name = v.Role
			}
			data = append(data, volumeGroup{name: name, used: used, mount: v.MountPoint})
		default:
			// Preboot, Recovery, VM, Update, xART: housekeeping APFS
			// volumes, which a user does not reason about individually.
			systemUsed += used
			systemCount++
		}
	}
	if systemUsed > 0 || systemCount > 0 {
		data = append(data, volumeGroup{
			name: fmt.Sprintf("系统（%d 个卷）", systemCount), used: systemUsed,
		})
	}
	// Largest first reads better in a donut and a list alike.
	sort.SliceStable(data, func(i, j int) bool { return data[i].used > data[j].used })
	return data
}

// volumePalette is the donut's colours, theme-aware.
func volumePalette() []ui.Color {
	return []ui.Color{
		colorAccent(),
		ui.Hex("#C77700"),
		ui.Hex("#3F8F5B"),
		ui.Hex("#8B5CF6"),
		colorMuted(),
	}
}

// volumeChartState keeps the donut's selection per disk, so hovering one
// card's chart does not move another's.
func (a *App) volumeChartState(id string) *chart.State {
	if a.diskVolumeCharts == nil {
		a.diskVolumeCharts = map[string]*chart.State{}
	}
	st := a.diskVolumeCharts[id]
	if st == nil {
		st = &chart.State{Index: -1}
		a.diskVolumeCharts[id] = st
	}
	return st
}
