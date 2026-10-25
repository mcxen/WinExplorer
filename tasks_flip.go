package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// tasks_flip.go turns the middle file area over to the task view and back
// (TASKSYS T4): a Y-axis card flip with perspective, 360ms ease-in-out,
// the face turning edge-on darkened; under Reduce Motion a crossfade of
// the same length. Only one face is built at a time — the file list up to
// 90°, the task view past it — so the hidden face costs nothing.

const (
	flipDuration    = 360 * time.Millisecond
	flipPerspective = 1400
	flipShade       = 0.22
	flipLand        = 80 * time.Millisecond // live content fades in after
	flipBudget      = 20 * time.Millisecond // frame time that counts as slow
	flipBlurMax     = 2.5                   // DIP sigma at the edge-on peak
	flipCards       = 8                     // task cards built mid-flip
	taskListPage    = 100                   // cards before 显示其余
)

// flipState lives for one turn (FLIP-PERF): the data each face shows,
// gathered once when the turn begins — the toolkit has no offscreen
// snapshot, so the faces are frozen at the data level instead, and the
// back face builds only the cards that fit — and the frame clock that
// drives the adaptive fallback.
type flipState struct {
	entries []Entry
	path    string
	lists   *[4][]archiveTaskSnap
	last    time.Time
	fast    int // frames under budget so far; blur waits for two
}

// flipAdaptive enables the low-end fallback (off in the frame-time test
// so it measures the full 3D path).
var flipAdaptive, flipBlurOn = true, true

// flipFrame times the frame: one over budget marks the machine low-end
// for the rest of the session (crossfade, no blur); blur only starts
// after two frames came in under budget, so a software renderer never
// pays for it.
func (a *App) flipFrame(now time.Time) {
	f := a.flip
	if !f.last.IsZero() {
		if now.Sub(f.last) > flipBudget {
			if flipAdaptive {
				a.flipLowEnd = true
			}
		} else {
			f.fast++
		}
	}
	f.last = now
}

// toggleTasksView flips the middle area: the status bar indicator, the
// toolbar's 任务, Ctrl+Shift+J, and Esc on the back.
func (a *App) toggleTasksView() { a.tasksFlipped = !a.tasksFlipped }

// flipPane is the middle area: the file pane in front, the task view on
// the back.
func (a *App) flipPane(c *ui.Context) {
	box := ui.Box(c.Key("tasks-flip")).Grow(1).FillHeight().MinWidth(0).MinHeight(0)
	target := float32(0)
	if a.tasksFlipped {
		target = 1
	}
	v := box.AnimateWith("flip", target, flipDuration, ui.EaseInOut)
	back := v >= 0.5
	turning := v > 0 && v < 1
	switch {
	case turning && a.flip == nil:
		// Pre-warm: gather both faces' data before the first turned frame.
		a.flip = &flipState{}
		l := taskViewLists()
		a.flip.lists = &l
	case !turning && a.flip != nil:
		a.flip = nil
		a.flipLanded = time.Now()
	}
	if turning {
		a.flipFrame(time.Now())
	}
	if c.Preferences().ReduceMotion || a.flipLowEnd {
		// Crossfade: the front fades out over the first half, the back in
		// over the second.
		box.Opacity(float32(math.Abs(float64(v)*2 - 1)))
	} else if v > 0 && v < 1 {
		angle := v * 180
		if back {
			angle -= 180
		}
		box.TransformOrigin(0.5, 0.5).RotateY(angle).Perspective(flipPerspective)
		shade := flipShade * float32(math.Sin(float64(v)*math.Pi))
		box.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(r, ui.Hex("#000000").Alpha(shade), 0)
		})
		// Smart blur: the face sweeps fastest edge-on, so it blurs with
		// the angular speed there (sin² of the turn), nothing near flat.
		if sg := flipBlurMax * shade / flipShade * shade / flipShade; sg > 0.4 && flipBlurOn && (a.flip.fast >= 2 || !flipAdaptive) {
			box.Blur(sg)
		}
	}
	if since := time.Since(a.flipLanded); !turning && since < flipLand {
		// Landed: the live content replaces the frozen one in a quick fade.
		box.Opacity(0.75 + 0.25*float32(since)/float32(flipLand))
		c.After(16 * time.Millisecond)
	}
	if v > 0 && v < 1 {
		c.After(16 * time.Millisecond)
	}
	box.Children(func() {
		ui.Row(c).Fill().MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if back {
				a.tasksView(c)
			} else {
				a.filePane(c)
			}
		})
		if v > 0 && v < 1 {
			// Mid-flip the face under the pointer is a moving picture:
			// a shield on top takes clicks and hover so neither
			// face acts on them until the turn has landed.
			shield := ui.Box(c.Key("flip-shield")).Absolute().Top(0).Bottom(0).Left(0).Right(0)
			shield.Clicked()
			shield.Hovered()
		}
	})
}
