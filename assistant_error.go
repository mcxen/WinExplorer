package main

import (
	"strings"
	"time"

	"sunvale/internal/applog"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistErrorMax caps a run error kept in the transcript: providers and
// proxies answer failures with whole HTML pages, which would otherwise
// become one tall reply.
const assistErrorMax = 2000

// assistErrorText normalizes a run error for the transcript: Windows line
// endings become \n, blank runs collapse, and the text is capped.
func assistErrorText(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ReplaceAll(err.Error(), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > assistErrorMax {
		s = string(r[:assistErrorMax]) + "…"
	}
	if s == "" {
		s = "请求失败"
	}
	return s
}

// assistErrorLine draws a failed run's message inside the conversation: one
// ellipsized line with a disclosure, expanded into a height-capped scroll.
// It never floats over the composer.
func (a *App) assistErrorLine(c *ui.Context, i int, text string, at time.Time) {
	open := a.assist.errOpen[i]
	first, _, more := strings.Cut(text, "\n")
	key := "assist-err-" + itoa(i)
	ui.Column(c.Key(key)).FillWidth().MinWidth(0).Gap(4).Children(func() {
		row := ui.ButtonBase(c).Key("toggle").FillWidth().MinWidth(0).Height(24).Gap(6).
			AlignItems(ui.Center).Padding(0, 6).Radius(6).Label("出错了：" + first)
		if row.Hovered() {
			row.Background(colorHover())
		}
		row.Children(func() {
			components.Icon(c, lucide.CircleAlert).Size(13, 13).TextColor(assistErrorColor).Shrink(0)
			ui.Text(c, "出错了："+first).FontSize(12).TextColor(assistErrorColor).
				SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Shrink(1)
			if more || len([]rune(first)) > 60 {
				label := "展开"
				if open {
					label = "收起"
				}
				ui.Text(c, label).FontSize(11).TextColor(colorMuted()).Shrink(0)
			}
		})
		full := assistErrorReport(text, at, applog.Path())
		row.ContextMenu(func(m *ui.Menu) {
			if m.Item("复制错误信息").Chosen() {
				c.WriteClipboard(full)
				a.assist.copiedKey, a.assist.copiedAt = key+"-copy", time.Now()
			}
		})
		if row.Clicked() {
			if a.assist.errOpen == nil {
				a.assist.errOpen = map[int]bool{}
			}
			a.assist.errOpen[i] = !open
		}
		// 查看日志 (applog_glue.go): the full request trace for this failure.
		ui.Row(c).Key("log").FillWidth().Padding(0, 6).Gap(8).AlignItems(ui.Center).Children(func() {
			a.assistCopyButton(c, key+"-copy", full)
			if ui.ButtonBase(c).Key("view-log").Label("查看日志").Children(func() {
				ui.Text(c, "查看日志").FontSize(11).TextColor(colorAccent())
			}).Clicked() {
				openLogFile()
			}
		})
		if open {
			ui.Scroll(c).Key("detail").FillWidth().MaxHeight(160).MinHeight(0).Children(func() {
				ui.Text(c, text).FontSize(12).TextColor(colorText()).Wrap().Selectable().Padding(0, 6)
			})
		}
	})
}

var assistErrorColor = ui.Hex("#D14343")

// assistErrorReport is what 复制 puts on the clipboard: the full error plus
// when it happened and where the log lives, ready to paste into feedback.
func assistErrorReport(text string, at time.Time, logPath string) string {
	var b strings.Builder
	b.WriteString("出错了：" + text)
	if !at.IsZero() {
		b.WriteString("\n时间：" + at.Format("2006-01-02 15:04:05 -0700"))
	}
	if logPath != "" {
		b.WriteString("\n日志：" + logPath)
	}
	return b.String()
}
