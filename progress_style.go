package main

import (
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// progress_style.go is PROG-STYLE: the refined progress bar (rounded, faint
// track, eased fill, shimmer when indeterminate, tinted by state) and the
// green gauge, a semicircle arc with a needle and the percent in its
// middle. ARCHIVE-PROG's task card, OpenUI TaskProgress and ProgressBar
// paint through here.

const (
	progressStyleBar   = "bar"
	progressStyleGauge = "gauge"

	// progressEase is the fill's time constant: an exponential ease-out
	// reaching 95% of a jump in about 3×, i.e. ~240ms.
	progressEase = 80 * time.Millisecond
	// progressShimmer is one sweep of the indeterminate band.
	progressShimmer = 1200 * time.Millisecond
	gaugeW          = float32(132)
	gaugeH          = float32(76)
	gaugeStroke     = float32(10)
)

// progressStyleOf normalizes a style name; anything else is "".
func progressStyleOf(s string) string {
	switch s {
	case "gauge", "dial", "meter", "仪表盘":
		return progressStyleGauge
	case "bar", "line", "条形":
		return progressStyleBar
	}
	return ""
}

// assistProgressStyle is the user's setting for the assistant's live
// task cards; bar by default.
func (a *App) assistProgressStyle() string {
	if a == nil || a.store == nil {
		return progressStyleBar
	}
	if s := progressStyleOf(a.store.settings().ProgressStyle); s != "" {
		return s
	}
	return progressStyleBar
}

type progressTone int

const (
	toneRunning progressTone = iota
	toneDone
	toneError
	toneCanceled
)

func (t progressTone) color(gauge bool) ui.Color {
	switch t {
	case toneDone:
		return tokSuccess
	case toneError:
		return tokDanger
	case toneCanceled:
		return colorMuted()
	}
	if gauge {
		return tokSuccess
	}
	return colorAccent()
}

func snapTone(s archiveTaskSnap) progressTone {
	switch {
	case s.Err == errArchiveCanceled.Error():
		return toneCanceled
	case s.Err != "":
		return toneError
	case s.Finished:
		return toneDone
	}
	return toneRunning
}

// progressAnim keeps each bar's shown value so the fill eases to the
// target instead of jumping.
var progressAnim struct {
	sync.Mutex
	m map[string]progressShown
}

type progressShown struct {
	v  float64
	at time.Time
}

// progressEased moves key's shown value toward target by dt, reporting
// whether it still moves. A first sight shows the target at once.
func progressEased(key string, target float64, now time.Time, reduced bool) (float64, bool) {
	progressAnim.Lock()
	defer progressAnim.Unlock()
	if progressAnim.m == nil || len(progressAnim.m) > 256 {
		progressAnim.m = map[string]progressShown{}
	}
	st, ok := progressAnim.m[key]
	if !ok || reduced || target < st.v {
		progressAnim.m[key] = progressShown{target, now}
		return target, false
	}
	dt := now.Sub(st.at)
	if dt < 0 {
		dt = 0
	}
	k := 1 - math.Exp(-float64(dt)/float64(progressEase))
	v := st.v + (target-st.v)*k
	moving := target-v > 0.002
	if !moving {
		v = target
	}
	progressAnim.m[key] = progressShown{v, now}
	return v, moving
}

// paintProgressBar is the refined bar: h tall, fully rounded, a 14% tint
// track, the eased fill, and for frac < 0 a 35%-wide band sweeping with
// cosine easing.
func paintProgressBar(c *ui.Context, key string, frac float64, tone progressTone, h float32) ui.Element {
	col := tone.color(false)
	reduced := c.Preferences().ReduceMotion
	e := ui.Box(c.Key(key)).FillWidth().Height(h).Role(ui.RoleProgress).Range(0, 1, max(frac, 0))
	e.Draw(func(p *ui.Painter, r ui.Rect) {
		if r.W <= 0 || r.H <= 0 {
			return
		}
		rad := r.H / 2
		p.Fill(r, col.Alpha(0.14), rad)
		if frac < 0 {
			phase := 0.25
			if !reduced {
				p.AnimationFrame()
				phase = float64(p.Now().UnixMilli()%progressShimmer.Milliseconds()) / float64(progressShimmer.Milliseconds())
			}
			w := r.W * 0.35
			x := r.X - w + (r.W+w)*float32((1-math.Cos(phase*math.Pi))/2)
			p.Clip(r, rad, func() {
				p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, col.Alpha(0.75), rad)
			})
			return
		}
		v, moving := progressEased(key, frac, p.Now(), reduced)
		if moving {
			p.AnimationFrame()
		}
		if w := r.W * float32(v); w > 0 {
			p.Clip(r, rad, func() { p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(w, r.H), H: r.H}, col, rad) })
		}
	})
	return e
}

func gaugeArc(path *ui.Path, cx, cy, rad float32, from, to float64) {
	const steps = 24
	for i := 0; i <= steps; i++ {
		a := from + (to-from)*float64(i)/steps
		x, y := cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
}

// paintGauge is the green dial: a 180° arc track, the eased value arc,
// a needle tick at its end, and the big percent inside. frac < 0
// sweeps a 60° segment back and forth.
func paintGauge(c *ui.Context, key string, frac float64, tone progressTone) {
	col := tone.color(true)
	reduced := c.Preferences().ReduceMotion
	label := "—"
	if frac >= 0 {
		label = strconv.Itoa(int(frac*100)) + "%"
	}
	g := ui.Column(c.Key(key)).Size(gaugeW, gaugeH).Shrink(0).Justify(ui.End).AlignItems(ui.Center).
		Role(ui.RoleProgress).Range(0, 1, max(frac, 0))
	g.Draw(func(p *ui.Painter, r ui.Rect) {
		rad := min(r.W/2, r.H) - gaugeStroke/2 - 1
		cx, cy := r.X+r.W/2, r.Y+rad+gaugeStroke/2+1
		var track ui.Path
		gaugeArc(&track, cx, cy, rad, math.Pi, 2*math.Pi)
		p.StrokePath(&track, gaugeStroke, col.Alpha(0.16))
		from, to := math.Pi, math.Pi
		if frac < 0 {
			phase := 0.25
			if !reduced {
				p.AnimationFrame()
				phase = float64(p.Now().UnixMilli()%(2*progressShimmer.Milliseconds())) / float64(2*progressShimmer.Milliseconds())
			}
			mid := math.Pi + math.Pi/6 + (math.Pi*2/3)*(1-math.Cos(phase*2*math.Pi))/2
			from, to = mid-math.Pi/6, mid+math.Pi/6
		} else {
			v, moving := progressEased(key, frac, p.Now(), reduced)
			if moving {
				p.AnimationFrame()
			}
			to = math.Pi + math.Pi*v
		}
		if to > from {
			var arc ui.Path
			gaugeArc(&arc, cx, cy, rad, from, to)
			p.StrokePath(&arc, gaugeStroke, col)
		}
		if frac >= 0 {
			// The needle is a short tick just inside the arc's end, clear
			// of the percent in the middle.
			r0, r1 := rad-gaugeStroke/2-2, rad-gaugeStroke/2-11
			cs, sn := float32(math.Cos(to)), float32(math.Sin(to))
			var needle ui.Path
			needle.MoveTo(cx+r0*cs, cy+r0*sn).LineTo(cx+r1*cs, cy+r1*sn)
			p.StrokePath(&needle, 2.5, colorText().Alpha(0.75))
		}
	})
	g.Children(func() {
		ui.Text(c.Key("pct"), label).FontSize(20).FontWeight(700).TextColor(col).Padding(0, 0, 10, 0)
	})
}

// paintArchiveGaugeCard is the task card in gauge style: the dial left;
// title, numbers, speed · ETA and the current member right.
func paintArchiveGaugeCard(c *ui.Context, s archiveTaskSnap) {
	tone := snapTone(s)
	frac := s.Frac()
	if s.Static {
		frac = s.Speed
	}
	title := s.Title
	if title == "" {
		title = archiveVerb(s.Kind) + " " + s.Name
	}
	ui.Row(c).FillWidth().MinWidth(0).Gap(12).Padding(8, 10).Radius(8).AlignItems(ui.Center).
		Border(1, colorDivider()).Background(colorSurface()).Label("OpenUI task gauge").Children(func() {
		paintGauge(c, "gauge-"+s.key(), frac, tone)
		ui.Column(c.Key("info")).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c.Key("head")).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
				name := ui.Text(c.Key("n"), title).FontSize(13).FontWeight(600).TextColor(colorText()).
					SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
				components.Tooltip(c, name, title)
				if !s.Finished && !s.Canceled && !s.Static {
					if components.IconButton(c.Key("x"), lucide.X(), "取消"+archiveVerb(s.Kind)+" "+s.Name, components.IconButtonPlain).
						Size(22, 22).Clicked() {
						archiveTaskByID(s.ID).cancel()
					}
				}
			})
			lines := []string{}
			if s.Static {
				lines = append(lines, s.Meta)
			} else {
				if s.Total > 0 {
					lines = append(lines, formatBytes(s.Done)+" / "+formatBytes(s.Total))
				}
				speed := ""
				switch {
				case s.Finished:
					speed = archiveClock(s.Elapsed)
				case s.Speed > 0:
					speed = formatBytes(int64(s.Speed)) + "/s"
					if s.ETA > 0 {
						speed += " · 剩余 " + archiveClock(s.ETA)
					}
				}
				lines = append(lines, speed)
			}
			for i, l := range lines {
				if l != "" {
					ui.Text(c.Key("l"+itoa(i)), l).FontSize(12).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0)
				}
			}
			line, col := s.Cur, colorMuted()
			if s.Err != "" {
				line, col = s.Err, tone.color(true)
			}
			if line != "" {
				t := ui.Text(c.Key("cur"), line).FontSize(11).TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
				components.Tooltip(c, t, line)
			}
		})
	})
	if !s.Finished && !s.Static {
		c.After(archiveRepaint)
	}
}

// key names a snap's animation slot.
func (s archiveTaskSnap) key() string {
	if s.ID != "" {
		return "t" + s.ID
	}
	return "o" + s.Title
}
