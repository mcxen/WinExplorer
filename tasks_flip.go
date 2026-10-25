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
)

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
	if c.Preferences().ReduceMotion {
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
