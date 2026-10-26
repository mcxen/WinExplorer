package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// archive_progress_paint.go paints archiveTaskSnap rows: the status bar's
// one-line strip and the assistant's TaskProgress card.

const archiveRepaint = 200 * time.Millisecond

func archiveVerb(kind string) string {
	switch kind {
	case "compress":
		return "压缩"
	case "copy":
		return "复制"
	case "move":
		return "移动"
	case "delete":
		return "删除"
	}
	return "解压"
}

// archiveClock is m:ss, or h:mm:ss past the hour.
func archiveClock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// archiveMeta is the dense numbers line: "42% · 12 MB / 30 MB · 8.1 MB/s · 0:12".
func archiveMeta(s archiveTaskSnap) string {
	parts := []string{}
	if f := s.Frac(); f >= 0 {
		parts = append(parts, strconv.Itoa(int(f*100))+"%")
	}
	switch {
	case s.Total > 0:
		parts = append(parts, formatBytes(s.Done)+" / "+formatBytes(s.Total))
	case s.Done > 0:
		parts = append(parts, formatBytes(s.Done))
	}
	if s.Finished {
		parts = append(parts, archiveClock(s.Elapsed))
	} else {
		if s.Speed > 0 {
			parts = append(parts, formatBytes(int64(s.Speed))+"/s")
		}
		if s.ETA > 0 {
			parts = append(parts, "剩余 "+archiveClock(s.ETA))
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}

func archiveSnapTone(s archiveTaskSnap) (ui.Color, *ui.SVG) {
	switch {
	case s.Err == errArchiveCanceled.Error():
		return colorMuted(), lucide.Ban()
	case s.Err != "":
		return tokDanger, lucide.CircleAlert()
	case s.Finished:
		return tokSuccess, lucide.Check()
	}
	switch s.Kind {
	case "copy":
		return colorAccent(), lucide.Copy()
	case "move":
		return colorAccent(), lucide.FolderInput()
	case "delete":
		return colorAccent(), lucide.Trash()
	}
	return colorAccent(), lucide.FileArchive()
}

// paintArchiveTaskCard is one task: icon, verb + name, numbers, cancel,
// then the bar and the current member (or the error).
func paintArchiveTaskCard(c *ui.Context, s archiveTaskSnap) {
	tint, icon := archiveSnapTone(s)
	card := ui.Column(c).FillWidth().MinWidth(0).Gap(5).Padding(8, 10).Radius(8).
		Border(1, colorDivider()).Background(colorSurface()).Label("OpenUI task progress")
	card.Children(func() {
		ui.Row(c.Key("head")).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			components.Icon(c, icon).Size(15, 15).TextColor(tint).Shrink(0)
			title := s.Title
			if title == "" {
				title = archiveVerb(s.Kind) + " " + s.Name
			}
			meta := s.Meta
			if meta == "" && !s.Static {
				meta = archiveMeta(s)
			}
			name := ui.Text(c.Key("n"), title).FontSize(13).FontWeight(600).
				TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
			components.Tooltip(c, name, title)
			ui.Text(c.Key("m"), meta).FontSize(11.5).TextColor(colorMuted()).SingleLine().Shrink(0)
			if !s.Finished && !s.Canceled && !s.Static {
				if components.IconButton(c.Key("x"), lucide.X(), "取消"+archiveVerb(s.Kind)+" "+s.Name, components.IconButtonPlain).
					Size(22, 22).Clicked() {
					archiveTaskByID(s.ID).cancel()
				}
			}
		})
		frac := s.Frac()
		if s.Static {
			frac = s.Speed
		}
		if s.Err != "" && s.Total > 0 {
			frac = float64(s.Done) / float64(s.Total)
		}
		paintProgressBar(c, "bar-"+s.key(), frac, snapTone(s), 5).Label(archiveVerb(s.Kind) + "进度")
		line, col := s.Cur, colorMuted()
		if s.Err != "" {
			line, col = s.Err, tint
		}
		if line != "" {
			t := ui.Text(c.Key("cur"), line).FontSize(11).TextColor(col).SingleLine().Ellipsis("…").MinWidth(0)
			components.Tooltip(c, t, line)
		}
	})
	if !s.Finished && !s.Static {
		c.After(archiveRepaint)
	}
}

// openTaskSnap turns an OpenUI TaskProgress into a static snap; Speed
// carries the bar fraction (-1 indeterminate).
func openTaskSnap(v *ouiView) archiveTaskSnap {
	s := archiveTaskSnap{Title: v.Text, Meta: v.Label, Cur: v.Sub, Static: true, Speed: v.Min}
	switch v.Variant {
	case "done":
		s.Finished, s.Speed = true, 1
	case "error":
		s.Finished, s.Err = true, v.Sub
		s.Cur = ""
		if s.Err == "" {
			s.Err = "失败"
		}
	}
	if s.Meta == "" && v.Min >= 0 {
		s.Meta = strconv.Itoa(int(v.Min*100)) + "%"
	}
	return s
}

// paintArchiveStatus is the status bar strip for the newest live task:
// a 90 DIP bar, "解压 a.zip 42% · 8 MB/s · 剩余 0:12" and a cancel cross.
// It reports whether anything was painted.
func paintArchiveStatus(c *ui.Context, open func()) bool {
	live := archiveSnaps(func(s archiveTaskSnap) bool { return !s.Finished })
	if len(live) == 0 {
		return false
	}
	s := live[len(live)-1]
	ui.Row(c.Key("archive-status")).Gap(6).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
		ob := ui.ButtonBase(c.Key("open")).Radius(rowR()).Padding(2, 4).Shrink(1).MinWidth(0).Label("任务视图")
		ob.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(func() { paintArchiveStatusBody(c, s, live) })
		})
		if ob.Clicked() && open != nil {
			open()
		}
		if components.IconButton(c.Key("x"), lucide.X(), "取消"+archiveVerb(s.Kind)+" "+s.Name, components.IconButtonPlain).
			Size(20, 20).Clicked() {
			archiveTaskByID(s.ID).cancel()
		}
	})
	c.After(archiveRepaint)
	return true
}

func paintArchiveStatusBody(c *ui.Context, s archiveTaskSnap, live []archiveTaskSnap) {
	{
		ui.Box(c.Key("barbox")).Width(90).Height(4).Shrink(0).AlignSelf(ui.Center).Children(func() {
			paintProgressBar(c, "sbar-"+s.key(), s.Frac(), toneRunning, 4).Label(archiveVerb(s.Kind) + "进度")
		})
		text := archiveVerb(s.Kind) + " " + s.Name
		if m := archiveMeta(s); m != "" {
			text += "  " + m
		}
		if len(live) > 1 {
			text += "  +" + itoa(len(live)-1)
		}
		t := ui.Text(c.Key("t"), text).FontSize(12).TextColor(colorAccent()).SingleLine().Ellipsis("…").MinWidth(0)
		tip := s.Name
		if s.Cur != "" {
			tip += "\n" + s.Cur
		}
		components.Tooltip(c, t, tip)
	}
}

// paintAssistArchiveTasks lists the assistant's tasks since since (the
// exchange's request), live ones repainting until they end.
func (a *App) paintAssistArchiveTasks(c *ui.Context, since time.Time) {
	tasks := archiveSnaps(func(s archiveTaskSnap) bool {
		return s.Origin == "ai" && !s.Start.Before(since)
	})
	if len(tasks) == 0 {
		return
	}
	ui.Column(c.Key("archive-tasks")).FillWidth().MinWidth(0).Gap(6).Children(func() {
		gauge := a.assistProgressStyle() == progressStyleGauge
		for _, s := range tasks {
			cc := c.Key("t" + s.ID)
			if gauge {
				paintArchiveGaugeCard(cc, s)
			} else {
				paintArchiveTaskCard(cc, s)
			}
		}
	})
}
