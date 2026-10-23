package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/internal/assistant"
)

// assistant_opcard.go paints what the assistant does to files as cards in
// the reply: the kind of operation, the path, a diff or a preview, a status
// pill and the 撤销 / 确认 buttons. Tool output stays in the folded log; the
// card is what a reader checks. It also paints the plan list, the speaker
// rows with time and copy, and the folder context above the composer.

const (
	opCardRadius   = 8
	opPreviewLines = 8
	opDiffLines    = 12
	opListLines    = 6
)

type opLook struct {
	title string
	icon  func() *ui.SVG
}

func opLookOf(op *assistant.FileOp) opLook {
	switch op.Kind {
	case assistant.OpCreate:
		return opLook{"新建文件", lucide.FilePlus}
	case assistant.OpEdit:
		return opLook{"修改内容", lucide.FilePen}
	case assistant.OpRename:
		return opLook{"重命名", lucide.TextCursorInput}
	case assistant.OpMove:
		return opLook{"移动", lucide.FolderInput}
	case assistant.OpDelete:
		return opLook{"删除到回收站", lucide.Trash}
	case assistant.OpBatch:
		return opLook{"批量操作", lucide.Layers}
	case assistant.OpRead:
		return opLook{"读取内容", lucide.FileText}
	}
	return opLook{"文件操作", lucide.File}
}

// assistRel shows a path relative to the folder the chat works in, so the
// card names 图片/海边.jpg rather than the whole temp path; outside paths
// stay absolute with the middle collapsed (UI.md §7.6).
func (a *App) assistRel(p string) string {
	root := a.assist.folder
	if root == "" && a.activeTab >= 0 && a.activeTab < len(a.tabs) {
		root = a.cur().path
	}
	if root != "" {
		if r, err := filepath.Rel(root, p); err == nil && r == "." {
			return "当前目录 " + filepath.Base(root)
		} else if err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return middleEllipsisText(p, 48)
}

// opStatusPill is the right-hand status of a card.
func opStatusPill(c *ui.Context, st assistant.OpStatus) {
	t := c.Theme()
	var label string
	var color ui.Color
	var icon func() *ui.SVG
	switch st {
	case assistant.OpPending:
		label, color, icon = "待确认", t.Warning, lucide.CircleDot
	case assistant.OpRunning:
		label, color = "执行中", colorAccent()
	case assistant.OpDone:
		label, color, icon = "完成", t.Success, lucide.Check
	case assistant.OpFailed:
		label, color, icon = "失败", t.Danger, lucide.CircleX
	case assistant.OpUndone:
		label, color, icon = "已撤销", colorMuted(), lucide.Undo2
	default:
		label, color, icon = "已取消", colorMuted(), lucide.Circle
	}
	ui.Row(c).Height(20).Padding(0, 8).Radius(10).Gap(4).AlignItems(ui.Center).Shrink(0).
		Background(color.Alpha(0.12)).Children(func() {
		if icon == nil {
			breathDot(c, 7)
		} else {
			components.Icon(c, icon).Size(11, 11).TextColor(color)
		}
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(color).SingleLine()
	})
}

// assistOpCard paints one file-operation card for transcript line i.
func (a *App) assistOpCard(c *ui.Context, i int, op *assistant.FileOp) {
	t := c.Theme()
	look := opLookOf(op)
	tint := colorAccent()
	if op.Kind == assistant.OpDelete || op.Danger {
		tint = t.Danger
	}
	border := colorDivider()
	switch {
	case op.Status == assistant.OpPending && op.Danger:
		border = t.Danger.Alpha(0.6)
	case op.Status == assistant.OpFailed:
		border = t.Danger.Alpha(0.6)
	case op.Status == assistant.OpRunning:
		border = colorAccent().Alpha(0.5)
	}
	card := ui.Column(c.Key("op-"+op.ID)).FillWidth().MinWidth(0).Radius(opCardRadius).
		Border(1, border).Background(colorAddress())
	if op.Status == assistant.OpRunning {
		// The card breathes with the rows it is working on (aifx.go).
		breathOverlay(c, card, a.assist.opSince(op.ID))
	}
	card.Children(func() {
		ui.Row(c).FillWidth().Padding(8, 10, 6, 10).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(24, 24).Radius(6).Shrink(0).Center().Background(tint.Alpha(0.12)).Children(func() {
				components.Icon(c, look.icon).Size(14, 14).TextColor(tint)
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				title := look.title
				if n := len(op.Items); n > 0 {
					title += " · " + itoa(n) + " 项"
				}
				ui.Text(c, title).FontSize(12.5).FontWeight(600).TextColor(colorText()).SingleLine()
				if op.Path != "" {
					ui.Text(c, a.assistRel(op.Path)).FontSize(11).TextColor(colorMuted()).SingleLine().Ellipsis("…")
				}
			})
			opStatusPill(c, op.Status)
		})
		ui.Column(c).FillWidth().MinWidth(0).Padding(0, 10, 10, 10).Gap(8).Children(func() {
			a.assistOpBody(c, op)
			a.assistOpFoot(c, i, op)
		})
	})
}

func (a *App) assistOpBody(c *ui.Context, op *assistant.FileOp) {
	t := c.Theme()
	switch op.Kind {
	case assistant.OpCreate:
		opPreview(c, op.After)
	case assistant.OpEdit:
		lines := assistant.DiffHunks(assistant.DiffLines(op.Before, op.After), 1)
		ui.Column(c).FillWidth().MinWidth(0).Radius(6).Padding(4, 0).Background(colorWindow()).Children(func() {
			for n, l := range lines {
				if n == opDiffLines {
					ui.Text(c, "…以及另外 "+itoa(len(lines)-n)+" 行").FontSize(11).TextColor(colorMuted()).Padding(2, 8)
					break
				}
				row := ui.Row(c).FillWidth().MinWidth(0).Padding(1, 8).Gap(6)
				color, sign := colorMuted(), " "
				switch l.Kind {
				case '-':
					row.Background(t.Danger.Alpha(0.1))
					color, sign = t.Danger, "−"
				case '+':
					row.Background(t.Success.Alpha(0.1))
					color, sign = t.Success, "+"
				}
				row.Children(func() {
					ui.Text(c, sign).FontSize(11.5).TextColor(color).Width(10).Shrink(0)
					text := colorText()
					if l.Kind == ' ' {
						text = colorMuted()
					}
					ui.Text(c, l.Text).FontSize(11.5).TextColor(text).SingleLine().Ellipsis("…").MinWidth(0)
				})
			}
		})
	case assistant.OpRename, assistant.OpMove:
		opFromTo(c, filepath.Base(op.Path), a.assistRel(op.To))
	case assistant.OpBatch, assistant.OpDelete, assistant.OpRead:
		if op.Status == assistant.OpRunning && len(op.Items) > 0 {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1).MinWidth(0).Children(func() {
					components.Progress(c, float64(op.Done())/float64(len(op.Items)))
				})
				ui.Text(c, itoa(op.Done())+" / "+itoa(len(op.Items))).FontSize(11).TextColor(colorMuted()).Shrink(0)
			})
		}
		ui.Column(c).FillWidth().MinWidth(0).Gap(2).Children(func() {
			for n, it := range op.Items {
				if n == opListLines {
					ui.Text(c, "…以及另外 "+itoa(len(op.Items)-n)+" 项").FontSize(11).TextColor(colorMuted())
					break
				}
				a.assistOpItem(c, op, it)
			}
		})
	}
	if op.Status == assistant.OpPending && op.Danger {
		ui.Row(c).FillWidth().Padding(6, 8).Radius(6).Gap(6).AlignItems(ui.Center).
			Background(t.Danger.Alpha(0.08)).Children(func() {
			components.Icon(c, lucide.TriangleAlert).Size(13, 13).TextColor(t.Danger).Shrink(0)
			note := op.Note
			if note == "" {
				note = "这一步会改动文件，确认后才会执行。"
			}
			ui.Text(c, note).FontSize(11.5).TextColor(colorText()).Wrap().MinWidth(0)
		})
	} else if op.Note != "" {
		ui.Text(c, op.Note).FontSize(11.5).TextColor(colorMuted()).Wrap()
	}
	if op.Status == assistant.OpFailed && op.Err != "" {
		ui.Text(c, op.Err).FontSize(11.5).TextColor(t.Danger).Wrap()
	}
}

func (a *App) assistOpItem(c *ui.Context, op *assistant.FileOp, it assistant.OpItem) {
	t := c.Theme()
	ui.Row(c).FillWidth().MinWidth(0).Height(20).Gap(6).AlignItems(ui.Center).Children(func() {
		switch {
		case it.Done && op.Status == assistant.OpUndone:
			components.Icon(c, lucide.Undo2).Size(12, 12).TextColor(colorMuted()).Shrink(0)
		case it.Done:
			components.Icon(c, lucide.Check).Size(12, 12).TextColor(t.Success).Shrink(0)
		case op.Status == assistant.OpRunning:
			ui.Box(c).Size(12, 12).Shrink(0).Center().Children(func() { breathDot(c, 6) })
		default:
			components.Icon(c, lucide.Circle).Size(12, 12).TextColor(colorMuted()).Shrink(0)
		}
		ui.Text(c, filepath.Base(it.From)).FontSize(11.5).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if it.To != "" {
			components.Icon(c, lucide.ArrowRight).Size(11, 11).TextColor(colorMuted()).Shrink(0)
			ui.Text(c, a.assistRel(it.To)).FontSize(11.5).TextColor(colorAccent()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
	})
}

func opFromTo(c *ui.Context, from, to string) {
	ui.Row(c).FillWidth().MinWidth(0).Padding(6, 8).Radius(6).Gap(8).AlignItems(ui.Center).
		Background(colorWindow()).Children(func() {
		ui.Text(c, from).FontSize(12).TextColor(colorMuted()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		components.Icon(c, lucide.ArrowRight).Size(12, 12).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, to).FontSize(12).FontWeight(600).TextColor(colorAccent()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
}

// opPreview shows the first lines of a new file's content.
func opPreview(c *ui.Context, text string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	ui.Column(c).FillWidth().MinWidth(0).Radius(6).Padding(6, 8).Gap(1).Background(colorWindow()).Children(func() {
		for n, l := range lines {
			if n == opPreviewLines {
				ui.Text(c, "…以及另外 "+itoa(len(lines)-n)+" 行").FontSize(11).TextColor(colorMuted())
				break
			}
			ui.Text(c, l).FontSize(11.5).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0)
		}
	})
}

// assistOpFoot is the card's button row: 确认 / 取消 while pending, 撤销
// once done when this window still holds the undo.
func (a *App) assistOpFoot(c *ui.Context, i int, op *assistant.FileOp) {
	t := c.Theme()
	switch op.Status {
	case assistant.OpPending:
		ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
			uikitSecondaryButton(c, "取消", func() { a.assistOpDecide(i, false) })
			label := "确认执行"
			if op.Kind == assistant.OpDelete {
				label = "移到回收站"
			}
			bg := colorAccent()
			if op.Danger {
				bg = t.Danger
			}
			btn := ui.ButtonBase(c.Key("op-go")).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(4).
				Background(bg).Label(label)
			if btn.Hovered() {
				btn.Background(bg.Mix(colorText(), 0.12))
			}
			btn.Children(func() { ui.Text(c, label).FontSize(12).TextColor(colorBtnText()) })
			if btn.Clicked() {
				a.assistOpDecide(i, true)
			}
		})
	case assistant.OpDone:
		if !a.assist.canUndoOp(op.ID) {
			return
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "已记录，可撤销").FontSize(11).TextColor(colorMuted()).Grow(1)
			b := ui.ButtonBase(c.Key("op-undo")).Height(26).Padding(0, 10).Radius(4).Gap(4).
				AlignItems(ui.Center).Border(1, colorDivider()).Background(colorAddress()).Label("撤销")
			if b.Hovered() {
				b.Background(colorHover())
			}
			b.Children(func() {
				components.Icon(c, lucide.Undo2).Size(12, 12).TextColor(colorText())
				ui.Text(c, "撤销").FontSize(12).TextColor(colorText())
			})
			if b.Clicked() {
				a.assistOpUndo(i)
			}
		})
	}
}

// assistPlanCard is the numbered plan of a multi-step request.
func assistPlanCard(c *ui.Context, key string, steps []assistant.PlanStep) {
	t := c.Theme()
	done := 0
	for _, s := range steps {
		if s.Status == assistant.OpDone {
			done++
		}
	}
	ui.Column(c.Key(key)).FillWidth().MinWidth(0).Radius(opCardRadius).Border(1, colorDivider()).
		Background(colorAddress()).Padding(8, 10).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.ListTodo).Size(14, 14).TextColor(colorAccent())
			ui.Text(c, "执行计划").FontSize(12.5).FontWeight(600).TextColor(colorText()).Grow(1)
			ui.Text(c, itoa(done)+" / "+itoa(len(steps))).FontSize(11).TextColor(colorMuted())
		})
		for n, s := range steps {
			ui.Row(c).FillWidth().MinWidth(0).Height(22).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(14, 14).Shrink(0).Center().Children(func() {
					switch s.Status {
					case assistant.OpDone:
						components.Icon(c, lucide.CircleCheck).Size(14, 14).TextColor(t.Success)
					case assistant.OpRunning:
						breathDot(c, 8)
					case assistant.OpFailed:
						components.Icon(c, lucide.CircleX).Size(14, 14).TextColor(t.Danger)
					default:
						components.Icon(c, lucide.Circle).Size(14, 14).TextColor(colorMuted())
					}
				})
				color := colorMuted()
				weight := 400
				if s.Status == assistant.OpRunning {
					color, weight = colorText(), 600
				} else if s.Status == assistant.OpDone {
					color = colorText()
				}
				ui.Text(c, itoa(n+1)+". "+s.Text).FontSize(12).FontWeight(weight).TextColor(color).
					SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
	})
}

// assistClock is a turn's time label: 14:03 today, 10/22 14:03 before.
func assistClock(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	now := time.Now()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("1/2 15:04")
}

// assistCopyButton copies text and says so for a moment.
func (a *App) assistCopyButton(c *ui.Context, key, text string) {
	copied := a.assist.copiedKey == key && time.Since(a.assist.copiedAt) < 1500*time.Millisecond
	icon, label := lucide.Copy, "复制消息"
	if copied {
		icon, label = lucide.Check, "消息已复制"
	}
	b := chat.IconAction(c.Key(key), label, icon)
	if b.Clicked() {
		c.WriteClipboard(text)
		a.assist.copiedKey, a.assist.copiedAt = key, time.Now()
		c.After(1500 * time.Millisecond)
	}
}

// assistSpeaker is the assistant's row above its reply: who, when, and a
// 演示 tag while the scripted demo answers instead of a model.
func (a *App) assistSpeaker(c *ui.Context, at time.Time) {
	ui.Row(c).FillWidth().Height(22).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(20, 20).Radius(10).Shrink(0).Center().Background(colorAccent().Alpha(0.14)).Children(func() {
			components.Icon(c, lucide.Bot).Size(12, 12).TextColor(colorAccent())
		})
		ui.Text(c, "晴谷助手").FontSize(12).FontWeight(600).TextColor(colorText())
		if s := assistClock(at); s != "" {
			ui.Text(c, s).FontSize(11).TextColor(colorMuted())
		}
		if assistDemoOn() {
			ui.Text(c, "演示").FontSize(10.5).TextColor(colorMuted()).Padding(1, 6).Radius(4).Background(colorHover())
		}
	})
}

// assistContextBar names the folder the next message works in, right
// above the composer, with the item and selection counts.
func (a *App) assistContextBar(c *ui.Context) {
	if a.activeTab < 0 || a.activeTab >= len(a.tabs) {
		return
	}
	tab := a.cur()
	if tab == nil || tab.path == "" || specialPath(tab.path) {
		return
	}
	name := filepath.Base(tab.path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = tab.path
	}
	row := ui.Row(c.Key("assist-ctx")).FillWidth().MinWidth(0).Padding(0, 6).Height(22).Gap(6).AlignItems(ui.Center)
	row.Children(func() {
		components.Icon(c, lucide.Folder).Size(13, 13).TextColor(colorFolder()).Shrink(0)
		ui.Text(c, "当前目录").FontSize(11).TextColor(colorMuted()).Shrink(0)
		ui.Text(c, name).FontSize(11.5).FontWeight(600).TextColor(colorText()).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		meta := itoa(len(a.visibleEntries())) + " 个项目"
		if n := len(a.selectedEntries()); n > 0 {
			meta += " · 已选择 " + itoa(n) + " 个"
		}
		ui.Text(c, meta).FontSize(11).TextColor(colorMuted()).SingleLine().Shrink(0)
	})
	components.Tooltip(c, row, tab.path)
}

// assistOps is the window-side state of the cards: when each started (for
// its breath), the undo each finished step left behind, and a demo script
// waiting on a card's confirmation.
type assistOps struct {
	started   map[string]time.Time
	undo      map[string]func() error
	demoAfter map[string][]demoBeat
	copiedKey string
	copiedAt  time.Time
}

func (u *assistantUI) opStart(id string) {
	if u.started == nil {
		u.started = map[string]time.Time{}
	}
	u.started[id] = time.Now()
}

func (u *assistantUI) opSince(id string) time.Time { return u.started[id] }

func (u *assistantUI) setUndo(id string, fn func() error) {
	if u.undo == nil {
		u.undo = map[string]func() error{}
	}
	u.undo[id] = fn
}

func (u *assistantUI) canUndoOp(id string) bool { return u.undo[id] != nil }

// assistOpDecide answers a pending card: 确认 resumes the script that
// asked, 取消 closes the card and the run.
func (a *App) assistOpDecide(i int, yes bool) {
	if i < 0 || i >= len(a.assist.turns) || a.assist.turns[i].Op == nil {
		return
	}
	op := a.assist.turns[i].Op
	if op.Status != assistant.OpPending {
		return
	}
	rest := a.assist.demoAfter[op.ID]
	delete(a.assist.demoAfter, op.ID)
	if !yes {
		op.Status = assistant.OpCancelled
		op.Note = ""
		aiFx.endRun()
		a.assist.turns = append(a.assist.turns, aiTurn{Role: "assistant", Text: "好的，已取消，文件没有改动。", At: time.Now()})
		return
	}
	a.assistDemoPlay(rest)
}

// assistOpUndo reverses a finished card and marks it 已撤销.
func (a *App) assistOpUndo(i int) {
	if i < 0 || i >= len(a.assist.turns) || a.assist.turns[i].Op == nil {
		return
	}
	op := a.assist.turns[i].Op
	fn := a.assist.undo[op.ID]
	if fn == nil {
		return
	}
	delete(a.assist.undo, op.ID)
	if err := fn(); err != nil {
		op.Err = "部分项目没能撤销：原位置已有同名文件或文件已被移走。"
		a.reload()
		return
	}
	op.Status = assistant.OpUndone
	op.Note = "已恢复到操作前的状态。"
	a.reload()
	a.statusMsg = "已撤销：" + opLookOf(op).title
}

func (u *assistantUI) gateDemo(id string, rest []demoBeat) {
	if u.demoAfter == nil {
		u.demoAfter = map[string][]demoBeat{}
	}
	u.demoAfter[id] = rest
}

// assistantEmpty is the blank conversation: who the assistant is, what it
// can do to files, and one example per kind of file work. An example fills
// the box rather than sending, so the request is still read before 发送.
func (a *App) assistantEmpty(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(28, 4, 12, 4).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(32, 32).Radius(16).Shrink(0).Center().Background(colorAccent().Alpha(0.14)).Children(func() {
				components.Icon(c, lucide.Bot).Size(18, 18).TextColor(colorAccent())
			})
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, "晴谷助手").FontSize(18).FontWeight(600).TextColor(colorText())
				hint := "新建、修改、重命名、归类、删除文件，也能读内容做总结。"
				if assistDemoOn() {
					hint = "演示模式：离线运行，操作在临时文件夹里进行。"
				}
				ui.Text(c, hint).FontSize(12).TextColor(colorMuted()).Wrap()
			})
		})
		ui.Text(c, "试试这样问").FontSize(11).FontWeight(600).TextColor(colorMuted()).Padding(14, 0, 2, 0)
		for _, s := range assistStarters {
			card := ui.ButtonBase(c.Key("starter-"+s.title)).FillWidth().Radius(8).
				Padding(8, 10).Gap(10).AlignItems(ui.Center).Label(s.title)
			card.Border(1, colorDivider()).Background(colorAddress())
			if card.Hovered() {
				card.Background(colorHover())
			}
			card.Children(func() {
				ui.Box(c).Size(26, 26).Radius(6).Shrink(0).Center().Background(colorAccent().Alpha(0.1)).Children(func() {
					components.Icon(c, s.icon).Size(14, 14).TextColor(colorAccent())
				})
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, s.title).FontSize(12.5).FontWeight(600).TextColor(colorText()).SingleLine()
					ui.Text(c, "“"+s.fill+"”").FontSize(11).TextColor(colorMuted()).SingleLine().Ellipsis("…")
				})
				components.Icon(c, lucide.CornerDownLeft).Size(14, 14).TextColor(colorMuted()).Shrink(0)
			})
			components.Tooltip(c, card, s.hint)
			if card.Clicked() {
				a.assist.draft = s.fill
			}
		}
	})
}
