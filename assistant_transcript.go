package main

// assistant_transcript.go paints the assistant conversation: empty-state
// starters, exchange grouping, live paint slots, and per-turn bodies.
// Split from assistant.go for maintainability (LAY2).

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// assistStarter is one of the empty transcript's example requests: a
// ZCode-style suggested prompt card — a glyph, the request itself and a
// line about what the run then does.
type assistStarter struct {
	icon  func() *ui.SVG
	title string
	fill  string
	hint  string
}

var assistStarters = []assistStarter{
	{lucide.FilePlus, "新建文件", "新建一个周报草稿", "写好标题和小节，先预览再落盘"},
	{lucide.FilePen, "修改文件内容", "把会议纪要里的时间修改为周五下午三点", "改动以对比形式列出，可撤销"},
	{lucide.FolderInput, "重命名并归类", "把 IMG_0001.jpg 按拍摄日期重命名并归类到图片文件夹", "多步计划逐项执行"},
	{lucide.Trash, "删除临时文件", "删除所有临时文件", "删除前先确认，移到回收站后仍可撤销"},
	{lucide.Layers, "批量操作", "批量给所有 txt 笔记加上日期前缀", "显示进度，整批一次撤销"},
	{lucide.FileText, "读内容做总结", "总结一下这个文件夹里的文档", "读取文档后给出要点"},
}

func (a *App) assistantTranscript(c *ui.Context) {
	if len(a.assist.turns) == 0 {
		a.assistantEmpty(c)
		return
	}
	a.assist.open.keys = nil
	for _, ex := range assistantExchanges(a.assist.turns) {
		a.assistantExchange(c, ex)
	}
	a.assistantOpenShortcuts(c)
}

// assistExchange is one user request plus the tool steps and reply that
// followed it. User is -1 when a transcript does not start with the user.
type assistExchange struct {
	User int
	Rest []int
}

func assistantExchanges(turns []aiTurn) []assistExchange {
	var out []assistExchange
	var cur *assistExchange
	for i, t := range turns {
		if t.Role == "user" || cur == nil {
			out = append(out, assistExchange{User: -1})
			cur = &out[len(out)-1]
			if t.Role == "user" {
				cur.User = i
				continue
			}
		}
		cur.Rest = append(cur.Rest, i)
	}
	return out
}

// assistPaint keeps Onevra's fold state for one exchange. It is not saved:
// reopening a session starts folded again.
type assistPaint struct {
	turn  chat.TurnState
	group chat.GroupState
	acts  map[int]*chat.ActivityState
	md    *assistMD
}

func (u *assistantUI) paintFor(i int) *assistPaint {
	if u.paint == nil {
		u.paint = map[int]*assistPaint{}
	}
	p := u.paint[i]
	if p == nil {
		p = &assistPaint{acts: map[int]*chat.ActivityState{}}
		u.paint[i] = p
	}
	return p
}

func (u *assistantUI) resetPaint() { u.paint = nil }

// applyLive appends a transcript line, or replaces the one still streaming
// under the same id. partial keeps that slot open for the next token.
func (u *assistantUI) applyLive(id int, turn aiTurn, partial bool) {
	if turn.At.IsZero() {
		turn.At = time.Now()
	}
	if u.liveOn && u.liveID == id && u.liveAt >= 0 && u.liveAt < len(u.turns) {
		u.turns[u.liveAt] = turn
	} else {
		u.turns = append(u.turns, turn)
		u.liveAt = len(u.turns) - 1
		u.liveID = id
	}
	u.liveOn = partial
}

func (u *assistantUI) resetLive() {
	u.liveOn = false
	u.liveID = 0
	u.liveAt = 0
	u.pending = ""
}

func (a *App) assistantExchange(c *ui.Context, ex assistExchange) {
	turns := a.assist.turns
	key := ex.User
	if key < 0 && len(ex.Rest) > 0 {
		key = ex.Rest[0]
	}
	paint := a.assist.paintFor(key)
	var tools, cards, errs []int
	var answer strings.Builder
	var replies []int
	for _, i := range ex.Rest {
		t := turns[i]
		switch {
		case t.Role == "tool" && (t.Op != nil || len(t.Steps) > 0):
			// File work and plans are cards in the reply, not tool log rows.
			cards = append(cards, i)
		case t.Role == "tool":
			tools = append(tools, i)
		case t.Error:
			errs = append(errs, i)
		default:
			text := t.Text
			if t.Choice != nil && strings.TrimSpace(text) == t.Choice.Prompt {
				text = ""
			}
			// OpenUI programs stay in the text: the Markdown view draws them
			// where the model put them.
			if strings.TrimSpace(text) != "" {
				if answer.Len() > 0 {
					answer.WriteString("\n\n")
				}
				answer.WriteString(strings.TrimSpace(text))
				replies = append(replies, i)
			}
		}
	}
	final := answer.String()
	last := key == a.assist.lastExchangeKey(ex)
	status := chat.Completed
	working := ""
	stopped := false
	for _, i := range ex.Rest {
		if turns[i].Stopped {
			stopped = true
		}
	}
	if last && a.assist.halted {
		stopped = true
	}
	if last && a.assist.running {
		status = chat.Running
		working = a.assist.status
		if working == "" {
			working = "正在思考"
		}
	} else if stopped {
		status = chat.Cancelled
	}
	var prompt chat.View
	if ex.User >= 0 {
		user := turns[ex.User]
		prompt = func(c *ui.Context) {
			if user.Text != "" {
				ui.Text(c, user.Text).FontSize(13.5).TextColor(colorText()).Wrap()
			}
			if len(user.Files) > 0 {
				ui.Row(c).FillWidth().Wrap().Gap(4).Children(func() {
					for _, f := range user.Files {
						chip := ui.Box(c.Key(f.Path + "-fx"))
						if last && a.assist.running {
							a.assistChipBreath(c, chip, f.Path)
						}
						chip.Children(func() {
							chat.Attachment(c.Key(f.Path), chat.AttachmentOptions{Label: f.Name, Detail: f.Detail})
						})
					}
				})
			} // The user's side of the meta row: time and a copy of the request.
			ui.Row(c).Key("meta").FillWidth().Height(20).Gap(2).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if s := assistClock(user.At); s != "" {
					ui.Text(c, s).FontSize(11).TextColor(colorMuted()).Padding(0, 4)
				}
				a.assistCopyButton(c, "copy-u-"+itoa(ex.User), user.Text)
			})
		}
	}
	var steps []chat.Step
	if len(tools) > 0 || (last && a.assist.running && a.assist.pending != "") {
		activities := make([]chat.Activity, 0, len(tools)+1)
		names := make([]string, 0, len(tools))
		for _, i := range tools {
			t := turns[i]
			text := t.Text
			st := paint.acts[i]
			if st == nil {
				st = &chat.ActivityState{}
				paint.acts[i] = st
			}
			action := t.Tool
			if action == "" {
				action = "工具"
			}
			if t.Plan != "" {
				action = "重命名方案"
			}
			if t.Tool != "" {
				names = append(names, t.Tool)
			}
			actStatus := chat.Completed
			if last && a.assist.running && a.assist.liveOn && a.assist.liveAt == i {
				actStatus = chat.Running
			}
			detail := assistFirstLine(text)
			activities = append(activities, chat.Activity{
				ID: itoa(i), Action: action, Detail: detail,
				Status: actStatus, Mono: true, State: st, MaxHeight: 220,
				Content: func(c *ui.Context) {
					ui.Text(c, text).FontSize(12).TextColor(colorText()).Wrap()
				},
			})
		}
		if last && a.assist.running && a.assist.pending != "" {
			// An empty Detail puts the toolkit's pulse dot on the row; the
			// verb beats the raw tool name while it still says doing what.
			activities = append(activities, chat.Activity{
				ID: "pending", Action: assistToolVerb(a.assist.pending), Status: chat.Running,
			})
			names = append(names, a.assist.pending)
		}
		title := assistSummary(len(tools))
		if label := assistCallSummary(names, last && a.assist.running); label != "" {
			title = label
		}
		steps = []chat.Step{{
			ID: "tools", Title: title, Activities: activities, Group: &paint.group,
		}}
	}
	// With cards the reply owns its working line, under the cards, so the
	// dots do not sit above the work they describe.
	ownWorking := ""
	if len(cards) > 0 && status == chat.Running {
		ownWorking, status, working = working, chat.Completed, ""
	}
	var finalView chat.View
	var actions chat.View
	if final != "" || len(cards) > 0 {
		streaming := last && a.assist.running
		var at time.Time
		if len(ex.Rest) > 0 {
			at = turns[ex.Rest[0]].At
		}
		finalView = func(c *ui.Context) {
			ui.Column(c.Key("reply")).FillWidth().MinWidth(0).Gap(8).Children(func() {
				a.assistSpeaker(c, at)
				for _, i := range cards {
					if t := turns[i]; len(t.Steps) > 0 {
						assistPlanCard(c, "plan-steps-"+itoa(i), t.Steps)
					} else {
						a.assistOpCard(c, i, t.Op)
					}
				}
				if final != "" {
					ui.Column(c.Key("md")).FillWidth().MinWidth(0).Children(func() {
						a.assistMarkdown(c, paint, final, replies, streaming)
					})
				}
				if ownWorking != "" {
					ui.Row(c.Key("own-working")).Height(22).Gap(8).AlignItems(ui.Center).Children(func() {
						breathDot(c, 8)
						ui.Text(c, ownWorking).FontSize(12.5).TextColor(colorMuted())
					})
				}
			})
		}
		if final != "" && !streaming {
			actions = func(c *ui.Context) { a.assistCopyButton(c, "copy-a-"+itoa(key), final) }
		}
	}
	summary := assistSummary(len(tools))
	if len(steps) == 1 && steps[0].Title != "" {
		summary = steps[0].Title
	}
	if stopped && status == chat.Cancelled {
		summary = "已停止"
	}
	chat.Turn(c, &paint.turn, chat.TurnOptions{
		ID: itoa(key), Status: status, Summary: summary, Working: working,
		Prompt: prompt, Steps: steps, Final: finalView, Actions: actions,
		HoldOpen: !a.assist.scroll.AtLatest(),
		Footer: func(c *ui.Context) {
			if last && ex.User >= 0 {
				a.paintAssistArchiveTasks(c, turns[ex.User].At)
			}
			if stopped && status == chat.Cancelled {
				ui.Text(c, "已停止").FontSize(12).TextColor(colorMuted())
			}
			for _, i := range ex.Rest {
				t := turns[i]
				if t.Plan != "" {
					a.assistantPlanCard(c.Key("plan-"+t.Plan), t.Plan)
				}
				hide := t.Choice != nil && t.Choice.Prompt != "" && strings.Contains(final, t.Choice.Prompt)
				a.assistantChoices(c.Key("choice-"+itoa(i)), i, hide)
			}
			for _, i := range errs {
				a.assistErrorLine(c, i, turns[i].Text, turns[i].At)
			}
		},
	})
}

func (u *assistantUI) lastExchangeKey(ex assistExchange) int {
	exchanges := assistantExchanges(u.turns)
	if len(exchanges) == 0 {
		return -1
	}
	last := exchanges[len(exchanges)-1]
	if last.User >= 0 {
		return last.User
	}
	if len(last.Rest) > 0 {
		return last.Rest[0]
	}
	return -1
}

func assistSummary(tools int) string {
	if tools == 0 {
		return "已回复"
	}
	return "调用了 " + itoa(tools) + " 个工具"
}

// assistCallSummary is the fold line once tool names are known. While the
// turn is still running it says what is happening instead of a past count.
func assistCallSummary(names []string, running bool) string {
	seen := map[string]bool{}
	var uniq []string
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		uniq = append(uniq, name)
	}
	if len(uniq) == 0 {
		return ""
	}
	shown := uniq
	if len(shown) > 3 {
		shown = shown[:3]
	}
	label := strings.Join(shown, "、")
	if running {
		return "正在调用 " + label
	}
	if len(uniq) > len(shown) {
		return "调用了 " + label + " 等"
	}
	return "调用了 " + label
}

func assistFirstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if len([]rune(text)) > 48 {
		text = string([]rune(text)[:48]) + "…"
	}
	return text
}

func (a *App) assistantTurn(c *ui.Context, i int) {
	t := &a.assist.turns[i]
	// The choice card already shows its prompt. Repeating it as the reply
	// makes the question appear twice.
	text := t.Text
	if t.Choice != nil && strings.TrimSpace(text) == t.Choice.Prompt {
		text = ""
	}
	switch t.Role {
	case "user":
		chat.Message(c, true, func(*ui.Context) {
			chat.Bubble(c, func(*ui.Context) {
				if t.Text != "" {
					ui.Text(c, t.Text).FontSize(13).TextColor(colorText()).Wrap()
				}
				if len(t.Files) > 0 {
					ui.Row(c).FillWidth().Wrap().Gap(4).Children(func() {
						for _, f := range t.Files {
							chat.Attachment(c.Key(f.Path), chat.AttachmentOptions{Label: f.Name, Detail: f.Detail})
						}
					})
				}
			})
		})
	case "tool":
		if text != "" {
			ui.Text(c.Key("tool-"+itoa(i)), text).FontSize(11).TextColor(colorMuted()).Wrap()
		}
		if t.Plan != "" {
			a.assistantPlanCard(c.Key("plan-"+t.Plan), t.Plan)
		}
		a.assistantChoices(c, i, false)
	default:
		prose, blocks := splitOpenUI(text)
		if prose != "" {
			ui.Text(c.Key("ai-"+itoa(i)), prose).FontSize(13).TextColor(colorText()).Wrap()
		}
		for n, block := range blocks {
			a.paintOpenBlock(c.Key("openui-"+itoa(i)+"-"+itoa(n)), block, i)
		}
		a.assistantChoices(c, i, false)
	}
}
