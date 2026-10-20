package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// assistant_choice_paint.go draws the questions a reply asks (offer_choices)
// and the rename plans it proposes. A single choice is a list of radio
// buttons that sends on click; several choices are check boxes and a send
// button; either takes an answer of the user's own. A plan is a confirm card
// listing old and new names; nothing is renamed until 应用方案.

// assistantChoices draws the choice under turn i. hidePrompt drops the
// question when the reply above already says it.
func (a *App) assistantChoices(c *ui.Context, i int, hidePrompt bool) {
	if i < 0 || i >= len(a.assist.turns) {
		return
	}
	ch := a.assist.turns[i].Choice
	if ch == nil {
		return
	}
	ui.Column(c).FillWidth().Padding(10).Gap(6).Radius(8).Border(1, colorDivider()).
		Background(colorHover()).Children(func() {
		if ch.Prompt != "" && !hidePrompt {
			ui.Text(c, ch.Prompt).FontSize(13).TextColor(colorText()).Wrap().Selectable()
		}
		if ch.Sent {
			openChosenNote(c, choiceAnswer(ch))
			return
		}
		busy := a.assist.running
		var keys []openShortcut
		if ch.Multi {
			ui.Text(c, "可多选").FontSize(11).TextColor(colorMuted())
			for j := range ch.Options {
				j := j
				box := ui.Checkbox(c.Key("opt-"+itoa(j)), &ch.On[j], ch.Options[j])
				if busy {
					box.Disabled(true)
				}
				keys = append(keys, openShortcut{label: ch.Options[j], run: func() { ch.On[j] = !ch.On[j] }})
			}
			send := components.Button(c, "发送所选", components.ButtonPrimary)
			if busy {
				send.Disabled(true)
			} else if send.Clicked() {
				a.assistantSendChoices(i)
			}
			keys = append(keys, openShortcut{label: "发送所选", primary: true, run: func() { a.assistantSendChoices(i) }})
		} else {
			picked := -1
			ui.RadioGroup(c, func() {
				for j := range ch.Options {
					j := j
					r := ui.Radio(c.Key("opt-"+itoa(j)), &picked, j, ch.Options[j])
					if busy {
						r.Disabled(true)
					} else if r.Clicked() {
						a.assistantPickChoice(i, j)
					}
					keys = append(keys, openShortcut{label: ch.Options[j], run: func() { a.assistantPickChoice(i, j) }})
				}
			})
		}
		a.assistantChoiceCustom(c, i, busy)
		if !busy {
			a.assist.open.keys = keys
		}
	})
}

// assistantChoiceCustom is the 其他 field: an answer the options did not
// offer, sent as it is.
func (a *App) assistantChoiceCustom(c *ui.Context, i int, busy bool) {
	st := &a.assist.open
	if st.custom == nil {
		st.custom = map[int]*string{}
	}
	if st.custom[i] == nil {
		st.custom[i] = new(string)
	}
	val := st.custom[i]
	ui.Row(c.Key("custom")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		components.TextBox(c, val, components.TextBoxOptions{ReadOnly: busy}).Grow(1).MinWidth(0).
			Label("其他回答").Placeholder("其他，自己填写")
		btn := components.Button(c, "发送", components.ButtonSecondary)
		if busy || strings.TrimSpace(*val) == "" {
			btn.Disabled(true)
		} else if btn.Clicked() {
			a.assistantSendCustom(i, strings.TrimSpace(*val))
		}
	})
}

// choiceAnswer is what a sent choice answered.
func choiceAnswer(ch *aiChoice) string {
	if ch.Custom != "" {
		return ch.Custom
	}
	var picked []string
	for j, on := range ch.On {
		if on && j < len(ch.Options) {
			picked = append(picked, ch.Options[j])
		}
	}
	return strings.Join(picked, "；")
}

// assistantPickChoice answers a single choice with option j at once.
func (a *App) assistantPickChoice(i, j int) {
	if a.assist.running || i < 0 || i >= len(a.assist.turns) {
		return
	}
	ch := a.assist.turns[i].Choice
	if ch == nil || ch.Sent || j < 0 || j >= len(ch.Options) {
		return
	}
	for k := range ch.On {
		ch.On[k] = k == j
	}
	a.assistantSendChoices(i)
}

func (a *App) assistantSendChoices(i int) {
	if a.assist.running || i < 0 || i >= len(a.assist.turns) {
		return
	}
	ch := a.assist.turns[i].Choice
	if ch == nil || ch.Sent {
		return
	}
	picked := choiceAnswer(ch)
	if picked == "" {
		a.statusMsg = "先勾选至少一项"
		return
	}
	a.assistantAnswerChoice(i, "我选择："+picked)
}

func (a *App) assistantSendCustom(i int, text string) {
	if a.assist.running || i < 0 || i >= len(a.assist.turns) {
		return
	}
	ch := a.assist.turns[i].Choice
	if ch == nil || ch.Sent {
		return
	}
	if a.assistantAnswerChoice(i, text) {
		a.assist.turns[i].Choice.Custom = text
		a.assist.persistSession(a.assist.folder)
	}
}

func (a *App) assistantAnswerChoice(i int, text string) bool {
	a.assist.draft = text
	before := len(a.assist.turns)
	a.assistantSubmit()
	if len(a.assist.turns) > before && i < len(a.assist.turns) && a.assist.turns[i].Choice != nil {
		a.assist.turns[i].Choice.Sent = true
		a.assist.persistSession(a.assist.folder)
		return true
	}
	return false
}

// planCardRows is how many renames the confirm card lists before it says
// how many more there are.
const planCardRows = 12

// assistantPlanCard is a rename plan as a confirm card: the names before and
// after, the rows that cannot be renamed, and 应用方案 / 不应用.
func (a *App) assistantPlanCard(c *ui.Context, id string) {
	plan, ok := sunvaleToolState.takePlan(id)
	st := &a.assist.open
	applied, dropped := a.assist.applied[id], st.planNo[id]
	ui.Column(c).FillWidth().MinWidth(0).Padding(10).Gap(8).Radius(8).Border(1, colorDivider()).
		Background(colorHover()).Children(func() {
		var todo, bad int
		for _, r := range plan.Rows {
			if r.Err != "" {
				bad++
			} else if r.To != "" && r.To != r.Name {
				todo++
			}
		}
		sub := "共 " + itoa(todo) + " 项，应用前不会改动文件"
		if bad > 0 {
			sub += "；" + itoa(bad) + " 项有冲突，会跳过"
		}
		if !ok {
			sub = "方案已失效，请重新生成"
		}
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, "确认重命名").FontSize(14).FontWeight(600).TextColor(colorText())
			ui.Text(c, sub).FontSize(12).TextColor(colorMuted()).Wrap()
		})
		if ok && len(plan.Rows) > 0 {
			cols := []ui.TableColumn{{Title: "原名称", ID: "from", MinWidth: 80}, {Title: "新名称", ID: "to", MinWidth: 80}}
			shown := plan.Rows
			if len(shown) > planCardRows {
				shown = shown[:planCardRows]
			}
			components.Table(c, nil, cols, func() {
				for r, row := range shown {
					to := row.To
					if row.Err != "" {
						to = "（" + row.Err + "）"
					}
					components.TableRow(c, r, row.Name, false, func(ci int) {
						text, color := row.Name, colorText()
						if ci == 1 {
							text = to
							if row.Err != "" {
								color = colorMuted()
							}
						}
						ui.Text(c.Key(ci), text).FontSize(12).TextColor(color).SingleLine()
					})
				}
			})
			if more := len(plan.Rows) - len(shown); more > 0 {
				ui.Text(c, "还有 "+itoa(more)+" 项未列出，位于 "+filepath.Base(plan.Dir)).FontSize(11).TextColor(colorMuted())
			}
		}
		switch {
		case applied:
			openChosenNote(c, "应用方案")
		case dropped:
			openChosenNote(c, "不应用")
		case ok:
			ui.Row(c).Gap(6).Children(func() {
				if components.Button(c, "应用方案", components.ButtonPrimary).Clicked() {
					a.assistantApplyPlan(id)
				}
				if components.Button(c, "不应用", components.ButtonSecondary).Clicked() {
					if st.planNo == nil {
						st.planNo = map[string]bool{}
					}
					st.planNo[id] = true
				}
			})
		}
	})
}
