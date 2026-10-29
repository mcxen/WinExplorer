package main

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/brandicons"
	"sunvale/diskhealth"
)

// diskhealth_card.go draws one physical drive: identity, health score,
// headline gauges, diagnostics and lifetime facts.
//
// diskCard is one drive. The left column carries identity and the score
// ring; the right column stacks the live chart, the checks and the facts.
func (a *App) diskCard(c *ui.Context, d diskhealth.Disk, r diskhealth.Report) {
	card := ui.Column(c).FillWidth().Radius(cardRadius()).Padding(16).Gap(14).
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
	used := "未知" // no mounted volume reports its space
	if d.Total > 0 && d.Free >= 0 {
		used = fmt.Sprintf("已用 %.0f%%", d.UsedRatio()*100)
	}
	out := []gauge{{
		label: "容量", value: used,
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
		TextColor(tone).Padding(4, 8).Radius(innerRadius())
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
	tile := ui.Column(c).Shrink(0).MinWidth(96).Radius(innerRadius()).
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
