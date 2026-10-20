package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chart"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/brandicons"
	"sunvale/diskhealth"
)

// diskCard is one drive. The left column carries identity and the score
// ring; the right column stacks the live chart, the checks and the facts.
func (a *App) diskCard(c *ui.Context, d diskhealth.Disk, r diskhealth.Report) {
	card := ui.Column(c).FillWidth().Radius(12).Padding(16).Gap(14).
		Background(colorAddress()).Shrink(0)
	card.Children(func() {
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
			a.diskIdentityColumn(c, d)
			ui.Box(c).Width(1).FillHeight().Shrink(0).Background(colorDivider())
			ui.Column(c).Grow(1).MinWidth(0).Gap(14).Children(func() {
				a.diskGaugeRow(c, d)
				a.diskChart(c, d, r)
			})
		})
		ui.Box(c).Height(1).FillWidth().Shrink(0).Background(colorDivider())
		a.diskChecksColumn(c, d)
		a.diskFactsRow(c, d)
		if len(d.Volumes) > 0 {
			a.diskVolumeComposition(c, d)
		}
	})
}

// diskIdentityColumn is the model, its identifiers, the score ring and the
// capacity bar.
func (a *App) diskIdentityColumn(c *ui.Context, d diskhealth.Disk) {
	ui.Column(c).Width(260).Shrink(0).Gap(10).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			a.diskBadge(c, d)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				model := d.Model
				if model == "" {
					model = d.ID
				}
				ui.Text(c, model).FontSize(14).FontWeight(600).
					TextColor(colorText()).SingleLine()
				sub := d.ID
				if logo := brandicons.Lookup(d.Model); logo != nil {
					sub = logo.Label + " · " + sub
				}
				ui.Text(c, sub).FontSize(11).TextColor(colorMuted()).SingleLine()
			})
		})

		// The score ring: the page's headline number.
		a.diskScoreRing(c, d)

		// Identity facts, the ones a support case asks for first.
		for _, row := range [][2]string{
			{"接口", orUnknown(d.Bus)},
			{"介质", orUnknown(d.Media)},
			{"序列号", orUnknown(d.Serial)},
			{"固件", orUnknown(d.Firmware)},
		} {
			a.diskFactRow(c, row[0], row[1])
		}
		if !d.Internal {
			a.diskFactRow(c, "连接", "外置")
		}
	})
}

// diskScoreRing is the health score: a ring filled to the score with the
// number in its middle, the band's name beside it. The ring is drawn here
// rather than with components.ProgressRing because its colour must carry
// the verdict — ProgressRing always fills in the frame accent.
func (a *App) diskScoreRing(c *ui.Context, d diskhealth.Disk) {
	score, ok := d.Score(a.diskWarnLimit())
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ring := ui.Box(c).Size(56, 56).Shrink(0)
		if !ok {
			ring.Draw(func(p *ui.Painter, r ui.Rect) {
				drawScoreRing(p, r, 0, colorMuted())
			})
		} else {
			ring.Draw(func(p *ui.Painter, r ui.Rect) {
				drawScoreRing(p, r, float64(score)/100, scoreTone(c, score))
			})
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			if !ok {
				ui.Text(c, "无可测指标").FontSize(12).TextColor(colorMuted()).SingleLine()
				return
			}
			ui.Row(c).Gap(4).AlignItems(ui.End).Children(func() {
				ui.Text(c, fmt.Sprintf("%d", score)).FontSize(22).FontWeight(700).
					TextColor(scoreTone(c, score)).SingleLine()
				ui.Text(c, "分").FontSize(11).TextColor(colorMuted()).Shrink(0)
			})
			badge := components.Badge(c, diskhealth.ScoreLabel(score))
			badge.Background(scoreTone(c, score).Alpha(0.14)).Border(0, scoreTone(c, score)).
				TextColor(scoreTone(c, score))
		})
	})
}

// drawScoreRing paints a progress ring in a caller-chosen colour: a track
// circle, then the filled arc clockwise from twelve o'clock.
func drawScoreRing(p *ui.Painter, r ui.Rect, frac float64, col ui.Color) {
	d := min(r.W, r.H)
	if d <= 0 {
		return
	}
	width := float32(6)
	radius := (d - width) / 2
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	var track ui.Path
	track.Circle(cx, cy, radius)
	p.StrokePath(&track, width, colorDivider())
	if frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	// MyGo's Path has no arc primitive, so the sweep is a run of short line
	// segments: 96 of them read as a smooth ring at these sizes.
	var arc ui.Path
	const steps = 96
	sweep := frac * 2 * math.Pi
	for i := 0; i <= steps; i++ {
		a := -math.Pi/2 + sweep*float64(i)/steps
		x := cx + radius*float32(math.Cos(a))
		y := cy + radius*float32(math.Sin(a))
		if i == 0 {
			arc.MoveTo(x, y)
		} else {
			arc.LineTo(x, y)
		}
	}
	p.StrokePath(&arc, width, col)
}

func scoreTone(c *ui.Context, score int) ui.Color {
	switch {
	case score >= 90:
		return toneGood(c)
	case score >= 70:
		return ui.Hex("#0E7C43")
	case score >= 50:
		return toneWarn(c)
	}
	return toneDanger(c)
}

// gauge is one headline reading drawn as a labelled bar.
type gauge struct {
	label string
	value string
	frac  float64
	tone  ui.Color
	note  string
}

// diskGauges are the readings worth a bar: used space, temperature and
// wear. Each maps a physical range onto 0–1, so the bars compare against
// what the drive can actually do rather than against each other.
func (a *App) diskGauges(c *ui.Context, d diskhealth.Disk) []gauge {
	out := []gauge{{
		label: "容量", value: fmt.Sprintf("已用 %.0f%%", d.UsedRatio()*100),
		frac: d.UsedRatio(), tone: capacityTone(c, d), note: capacityNote(d),
	}}
	if d.HasTemperature() {
		// 20–80 °C is the range a drive lives in; below 20 the bar is empty
		// and above 80 it is full, so an alarm reads as full, not as "more
		// than another drive".
		out = append(out, gauge{
			label: "温度", value: d.TemperatureLabel(),
			frac: clamp01((d.Temperature - 20) / 60),
			tone: a.tempTone(c, d), note: a.tempNote(d),
		})
	}
	if d.LifeKnown {
		out = append(out, gauge{
			label: "写入寿命", value: d.LifeLabel(),
			frac: clamp01(d.LifeUsed / 100), tone: wearTone(c, d),
		})
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func capacityTone(c *ui.Context, d diskhealth.Disk) ui.Color {
	switch {
	case d.UsedRatio() >= 0.95:
		return toneDanger(c)
	case d.UsedRatio() >= 0.9:
		return toneWarn(c)
	}
	return colorAccent()
}

func capacityNote(d diskhealth.Disk) string {
	if d.Total <= 0 || d.Free < 0 {
		return ""
	}
	return fmt.Sprintf("可用 %s / 共 %s", formatBytes(d.Free), formatBytes(d.Total))
}

func (a *App) tempTone(c *ui.Context, d diskhealth.Disk) ui.Color {
	limit, on := a.diskWarnTemp()
	switch {
	case on && d.Temperature >= limit+10:
		return toneDanger(c)
	case on && d.Temperature >= limit:
		return toneWarn(c)
	}
	return toneGood(c)
}

func (a *App) tempNote(d diskhealth.Disk) string {
	limit, on := a.diskWarnTemp()
	if on && d.Temperature >= limit {
		return fmt.Sprintf("高于设定阈值 %.0f °C", limit)
	}
	return ""
}

func wearTone(c *ui.Context, d diskhealth.Disk) ui.Color {
	if d.SpareThreshold > 0 && d.SparePercent < d.SpareThreshold {
		return toneDanger(c)
	}
	if d.LifeUsed >= 80 {
		return toneWarn(c)
	}
	return toneGood(c)
}

// diskGaugeRow lays the gauges in one column: a bar needs the window's
// width to read as a proportion, and three side by side shrink each to a
// stub whose fill no longer means anything.
func (a *App) diskGaugeRow(c *ui.Context, d diskhealth.Disk) {
	ui.Column(c).FillWidth().Gap(8).Shrink(0).Children(func() {
		for _, g := range a.diskGauges(c, d) {
			a.diskGauge(c, g)
		}
	})
}

// diskGauge is one labelled reading: the name, the value and the Onevra
// Progress track under them. The bar itself is the component; the caption
// above carries the colour, because Progress draws in the frame accent and
// these readings need to show their verdict.
func (a *App) diskGauge(c *ui.Context, g gauge) {
	ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, g.label).FontSize(11).TextColor(colorMuted()).Grow(1).SingleLine()
			ui.Text(c, g.value).FontSize(12).FontWeight(500).TextColor(g.tone).Shrink(0).SingleLine()
		})
		// Onevra's Progress paints in the frame accent, so its face is
		// repainted with the reading's own colour: same track, same fill,
		// same geometry — only the colour carries the verdict.
		bar := components.Progress(c, clamp01(g.frac)).Height(6).FillWidth()
		bar.Draw(func(p *ui.Painter, r ui.Rect) {
			if r.W <= 0 || r.H <= 0 {
				return
			}
			rad := r.H / 2
			p.Fill(r, c.Theme().Border, rad)
			w := r.W * float32(clamp01(g.frac))
			if w > 0 {
				p.Clip(r, rad, func() {
					p.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, g.tone, rad)
				})
			}
		})
		if g.note != "" {
			ui.Text(c, g.note).FontSize(10).TextColor(g.tone).SingleLine()
		}
	})
}

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
			ui.Box(c).FillWidth().Height(96).Shrink(0).Radius(6).
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

// diskChecksColumn lists the diagnostics as a row of chips: each one names
// what was measured, its value and whether it reads as normal.
func (a *App) diskChecksColumn(c *ui.Context, d diskhealth.Disk) {
	checks := d.Checks(a.diskWarnLimit())
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, "检测项").FontSize(11).FontWeight(500).TextColor(colorMuted())
		ui.Row(c).FillWidth().Wrap().Gap(8).Shrink(0).Children(func() {
			for _, ch := range checks {
				a.diskCheckChip(c, ch)
			}
		})
	})
}

// diskCheckChip is one check, drawn as an Onevra badge: the reading with
// its state word, tinted by that state. A non-pass check carries its
// explanation as a tooltip so the row stays one line high.
func (a *App) diskCheckChip(c *ui.Context, ch diskhealth.Check) {
	tone := checkTone(c, ch.State)
	text := fmt.Sprintf("%s · %s · %s", ch.Name, ch.Value, ch.State)
	badge := components.Badge(c, text)
	badge.Background(tone.Alpha(0.12)).Border(1, tone.Alpha(0.35)).
		TextColor(tone).Padding(4, 8).Radius(6)
	if ch.Note != "" {
		components.Tooltip(c, badge, ch.Note)
	}
}

func checkTone(c *ui.Context, s diskhealth.CheckState) ui.Color {
	switch s {
	case diskhealth.CheckPass:
		return toneGood(c)
	case diskhealth.CheckWarn:
		return toneWarn(c)
	case diskhealth.CheckFail:
		return toneDanger(c)
	}
	return colorMuted()
}

// diskFactsRow is the lifetime record: the counters a wear question turns
// on. Fields the platform did not report are omitted, so the row never
// claims a zero it did not read.
func (a *App) diskFactsRow(c *ui.Context, d diskhealth.Disk) {
	facts := [][2]string{{"总容量", formatBytes(d.Size)}}
	add := func(k, v string) { facts = append(facts, [2]string{k, v}) }
	if d.PowerOnHours >= 0 {
		add("通电时长", diskHoursLabel(d.PowerOnHours))
	}
	if d.PowerCycles >= 0 {
		add("通电次数", fmt.Sprintf("%d 次", d.PowerCycles))
	}
	if d.UnsafeShutdowns >= 0 {
		add("异常断电", fmt.Sprintf("%d 次", d.UnsafeShutdowns))
	}
	if d.MediaErrors >= 0 {
		add("介质错误", fmt.Sprintf("%d 次", d.MediaErrors))
	}
	if d.ReadBytes >= 0 || d.WriteBytes >= 0 {
		add("累计读写", fmt.Sprintf("%s / %s",
			labelBytes(d.ReadBytes), labelBytes(d.WriteBytes)))
	}
	if d.ReadOps >= 0 || d.WriteOps >= 0 {
		add("读写次数", fmt.Sprintf("%s / %s",
			formatCount(d.ReadOps), formatCount(d.WriteOps)))
	}
	if d.SparePercent > 0 && d.LifeKnown {
		add("备用块", d.SpareLabel())
	}
	ui.Row(c).FillWidth().Wrap().Gap(8).Shrink(0).Children(func() {
		for _, f := range facts {
			a.diskFactTile(c, f[0], f[1])
		}
	})
}

// diskFactTile is one labelled value in the facts row.
func (a *App) diskFactTile(c *ui.Context, label, value string) {
	tile := ui.Column(c).Shrink(0).MinWidth(96).Radius(6).
		Background(colorContent()).Padding(8, 10).Gap(2)
	tile.Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(colorMuted()).SingleLine()
		ui.Text(c, value).FontSize(12).FontWeight(500).TextColor(colorText()).SingleLine()
	})
}

// diskFactRow is one label/value line in the identity column.
func (a *App) diskFactRow(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colorMuted()).Width(46).Shrink(0).SingleLine()
		ui.Text(c, value).FontSize(11).TextColor(colorText()).
			Grow(1).MinWidth(0).SingleLine()
	})
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
				dot := ui.Box(c).Size(8, 8).Shrink(0).Radius(4)
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
