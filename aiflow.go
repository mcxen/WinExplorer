package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// aiflow.go is the assistant pane's lightwork: the flowing strip under the
// header that says a run is alive, and the small breathing dot status
// surfaces borrow. Rows and chips that breathe for one file live in
// aifx.go; this file is the conversation-wide signals.

const (
	// assistFlowStripH is the strip's footprint under the header.
	assistFlowStripH float32 = 2
	// assistFlowSweep is one left-to-right pass while a run is alive.
	assistFlowSweep = 1100 * time.Millisecond
	// assistFlowCool is the flourish after the run's end: one last fast
	// sweep that fades, rather than the light simply going out.
	assistFlowCool      = 700 * time.Millisecond
	assistFlowSweepFast = 500 * time.Millisecond
)

// assistFlowStrip is the 2 DIP band under the header. While a run is alive
// a highlight travels it; at the run's end it makes one faster pass and
// fades, the visual full stop of the turn. The desktop's reduce-motion
// preference holds a steady dim bar instead of the travel.
func (a *App) assistFlowStrip(c *ui.Context) {
	running := a.assist.running
	if !running && !time.Now().Before(a.assist.flowCool) {
		return
	}
	bar := ui.Box(c).FillWidth().Height(assistFlowStripH).Shrink(0)
	calm := c.Preferences().ReduceMotion
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		acc := colorAccent()
		fade := float32(1)
		sweep := assistFlowSweep
		if !a.assist.running {
			rem := float32(time.Until(a.assist.flowCool).Seconds())
			fade = max(0, min(1, rem/float32(assistFlowCool.Seconds())))
			sweep = assistFlowSweepFast
			// The cool sweep opens with a flash along the whole band, then
			// only the traveling light remains as it fades.
			p.Fill(r, acc.Alpha(0.25*fade*fade), 0)
		}
		if fade <= 0 {
			return
		}
		p.Fill(r, acc.Alpha(0.10+0.06*fade), 0)
		if calm {
			p.Fill(r, acc.Alpha(0.25*fade), 0)
			return
		}
		phase := float32(p.Now().UnixMilli()%sweep.Milliseconds()) / float32(sweep.Milliseconds())
		span := r.W*0.25 + 40
		x := -span + phase*(r.W+2*span)
		hi := acc.Alpha(0.85 * fade)
		p.FillGradient(ui.Rect{X: x - span, Y: r.Y, W: span, H: r.H}, ui.LinearGradient{
			From: acc.Alpha(0), To: hi, Angle: 90,
		}, 0)
		p.FillGradient(ui.Rect{X: x, Y: r.Y, W: span, H: r.H}, ui.LinearGradient{
			From: hi, To: acc.Alpha(0), Angle: 90,
		}, 0)
		p.AnimationFrame()
	})
}

// breathDot is a d-DIP accent dot on one breath per aiFxPulse: the wait
// indicator on the queue chip and the busy count on the status bar, built
// on the same rhythm the file rows breathe at.
func breathDot(c *ui.Context, d float32) ui.Element {
	calm := c.Preferences().ReduceMotion
	return ui.Box(c).Size(d, d).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		glow := float32(0.65)
		if !calm {
			glow = breath(time.Duration(p.Now().UnixMilli()) * time.Millisecond)
			p.AnimationFrame()
		}
		p.Fill(r, colorAccent().Alpha(0.35+0.65*glow), d/2)
	})
}

// assistToolVerb is the pending tool's line in its own words.
func assistToolVerb(name string) string {
	switch name {
	case "read_file", "read_files", "read_outside":
		return "正在读取文件"
	case "list_dir", "list_outside":
		return "正在列目录"
	case "search_names":
		return "正在搜索文件名"
	case "grep_text":
		return "正在查找文字"
	case "folder_summary":
		return "正在统计文件夹"
	case "web_search":
		return "正在搜索网页"
	case "web_read":
		return "正在读取网页"
	case "rename_plan":
		return "正在生成重命名方案"
	case "rename_item", "move_items", "copy_items", "trash_items",
		"new_text_file", "write_text", "mkdir":
		return "正在整理文件"
	case "zip_files":
		return "正在压缩"
	case "unzip":
		return "正在解压"
	case "properties":
		return "正在查看属性"
	case "disk_space":
		return "正在查看磁盘"
	case "current_selection":
		return "正在查看选中项"
	case "offer_choices", "present_form":
		return "正在准备选项"
	case "remember", "forget", "memory_list":
		return "正在更新记忆"
	case "schedule_list", "schedule_create", "schedule_update", "schedule_delete", "schedule_run":
		return "正在处理定时任务"
	case "assistant_task_run":
		return "正在派发任务"
	default:
		if name == "" {
			return "正在思考"
		}
		return "正在调用 " + name
	}
}
