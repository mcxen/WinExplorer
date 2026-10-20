package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/rename"
)

// assistant.go is the right-hand AI pane. The transcript and the composer
// are Onevra's chat views. The model is whatever OpenAI-compatible provider
// the user added. A rename plan is only a proposal until the user presses
// 应用方案, unless 全托管 is on and the run finishes the plan itself.
// The pane's width drags; 全屏 takes the room to the right of the sidebar.
// Closing the pane does not drop the transcript. The current session is
// reloaded from the user config directory the next time a window opens.
// A drop on the transcript or the composer names those files for the next
// message and leaves them where they were. A fenced OpenUI block in a reply
// is drawn with the controls in assistant_openui.go. Its buttons send the
// next message and do not change files themselves.

const (
	assistantPaneW float32 = 380
	assistantMinW  float32 = 320
)

type aiTurn = assistant.Turn
type aiAttach = assistant.Attach

// aiRenamePlan is a proposal rename_plan produced and 应用方案 runs.
type aiRenamePlan struct {
	Dir  string
	Rows []rename.Row
}

// assistantUI is one window's assistant. Providers, the password book and
// the cross-session memory are shared on disk. The transcript is one session,
// also on disk, so each window can reopen the last conversation.
type assistantUI struct {
	loaded     bool
	draft      string
	running    bool
	status     string
	turns      []aiTurn
	scroll     chat.ConversationState
	plans      map[string]aiRenamePlan
	applied    map[string]bool
	showProv   bool
	showBook   bool
	showHist   bool
	floatLay   chat.FloatingConversationState
	modelPick  chat.PickerState
	modelLists map[string][]string
	modelBusy  map[string]bool
	paint      map[int]*assistPaint
	live       assistLive
	// open is the OpenUI cards' state this window keeps: form values, the
	// source shown for a card that failed, the button awaiting a confirm,
	// and the shortcuts of the latest card.
	open assistOpenState
	// form fields for adding a provider, a password or a memory.
	pName, pURL, pModel, pKey string
	pwName, pwSecret          string
	memDraft                  string

	providers []aiProvider
	activeID  string
	passwords []archivePassword
	diskPath  string
	stop      context.CancelFunc
	// pending is a tool the model has named and not finished. halted is the
	// last turn being cut off before the stopped bit is written onto it.
	// liveOn replaces one growing line instead of appending every token.
	pending string
	halted  bool
	liveOn  bool
	liveID  int
	liveAt  int

	// sessionID is empty until the first message of a new chat is saved.
	sessionID       string
	sessionTitle    string
	created         time.Time
	folder          string
	sessions        []aiSessionMeta
	memories        []aiMemory
	memoryBroken    bool
	indexBroken     bool
	configBroken    bool
	legacyPasswords json.RawMessage
	baseProviders   []aiProvider
	baseActiveID    string
	basePasswords   []archivePassword
	basePrefs       assistPrefs
	baseConfigEpoch uint64
	baseAbsent      bool

	// width is the pane in DIPs. full hides the file list so the chat uses
	// the whole area right of the sidebar. autopilot lets the model finish
	// ordinary file work without a confirmation on each step.
	width          float32
	full           bool
	autopilot      bool
	savedFull      bool
	savedAutopilot bool
	widthDirty     bool
	widthDragging  bool
	files          []aiAttach
	picking        bool
}

func (a *App) toggleAssistant() { a.assistantOpen = !a.assistantOpen }

func (a *App) assistantPane(c *ui.Context) {
	a.assist.ensure()
	if !a.assist.full {
		a.assist.width = a.clampAssistWidth(c, a.assist.width)
	}
	pane := ui.Row(c).FillHeight().MinHeight(0)
	if a.assist.full {
		pane.Grow(1).FillWidth()
	} else {
		pane.Width(a.assist.width).Shrink(0)
	}
	pane.Background(c.Theme().Background)
	pane.Children(func() {
		ui.Box(c).Width(1).FillHeight().Shrink(0).Background(colorDivider())
		if !a.assist.full {
			a.assistantResizeHandle(c)
		} else {
			a.assist.widthDragging = false
		}
		body := ui.Column(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0)
		body.Children(func() {
			a.assistantHeader(c)
			if a.assist.showProv || a.assist.showBook || a.assist.showHist {
				ui.Scroll(c).FillWidth().Shrink(0).MaxHeight(280).Background(c.Theme().Surface).Children(func() {
					if a.assist.showProv {
						a.assistantProviders(c)
					}
					if a.assist.showBook {
						a.assistantPasswords(c)
					}
					if a.assist.showHist {
						a.assistantRecords(c)
					}
				})
				ui.Box(c).Height(1).FillWidth().Shrink(0).Background(colorDivider())
			}
			// The column, not the card, takes a drag from this window and a
			// native file list. The card only sees DroppedFiles, so a drop
			// on the composer would otherwise miss both.
			conv := ui.Column(c).Grow(1).FillWidth().MinHeight(0).MinWidth(0)
			conv.Children(func() {
				chat.FloatingConversation(c, &a.assist.floatLay, &a.assist.scroll, "回到底部",
					func(*ui.Context) { a.assistantTranscript(c) },
					func(*ui.Context) { a.assistantComposer(c) },
				)
			})
			a.bindAssistDrop(conv)
		})
	})
	a.assist.noteChrome()
}

func (a *App) assistantComposer(c *ui.Context) {
	var attachments chat.View
	if len(a.assist.files) > 0 {
		attachments = func(c *ui.Context) { a.assistantAttachments(c) }
	}
	comp := chat.GlassComposer(c, &a.assist.floatLay, &a.assist.draft, chat.ComposerOptions{
		Label:       "给助手的消息",
		Placeholder: "让助手整理这个文件夹…",
		SendLabel:   "发送",
		StopLabel:   "停止",
		Running:     a.assist.running,
		AcceptFiles: true,
		Attachments: attachments,
		Accessory: func(c *ui.Context) {
			a.assistantModelPicker(c)
			chat.SwitchChip(c, &a.assist.autopilot, "全托管")
			if chat.IconAction(c, "指定文件", lucide.Paperclip).Clicked() {
				a.assistantPickFiles()
			}
			if chat.IconAction(c, "带上选中", lucide.ListChecks).Clicked() {
				a.assistantAttachSelection()
			}
		},
		OnPaste: func(text string) bool {
			a.attachPastedPaths(text)
			return false
		},
	})
	comp.Root.FillWidth().Shrink(0)
	// FileDrop on the card itself. Native and in-window drops land on the
	// conversation column (bindAssistDrop), so this is not a second copy.
	if len(comp.Dropped) > 0 {
		a.attachDroppedPaths(comp.Dropped)
	}
	if comp.Stop && a.assist.stop != nil {
		a.assist.stop()
		a.assist.status = "正在停止…"
	}
	if comp.Send && !a.assist.running {
		a.assistantSubmit()
	}
}

func (a *App) assistantAttachments(c *ui.Context) {
	drop := -1
	for i, f := range a.assist.files {
		res := chat.Attachment(c.Key(f.Path), chat.AttachmentOptions{
			Label: f.Name, Detail: f.Detail, RemoveLabel: "移除 " + f.Name,
		})
		if res.Remove {
			drop = i
		}
	}
	if drop >= 0 {
		a.assist.files = append(a.assist.files[:drop], a.assist.files[drop+1:]...)
	}
}

func (a *App) assistantHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		components.Icon(c, lucide.Bot).Size(16, 16).TextColor(colorText()).Shrink(0)
		ui.Text(c, a.assist.sessionLabel()).FontSize(13).FontWeight(600).TextColor(colorText()).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Margin(0, 0, 0, 6)
		a.assistantHeaderIcon(c, lucide.MessagesSquare(), "会话", a.assist.showHist, func() { a.assist.togglePanel("hist") })
		a.assistantHeaderIcon(c, lucide.KeyRound(), "供应商", a.assist.showProv, func() { a.assist.togglePanel("prov") })
		a.assistantHeaderIcon(c, lucide.Lock(), "解压密码", a.assist.showBook, func() { a.assist.togglePanel("book") })
		fullIcon, fullLabel := lucide.Maximize2(), "全屏"
		if a.assist.full {
			fullIcon, fullLabel = lucide.Minimize2(), "还原"
		}
		if components.IconButton(c, fullIcon, fullLabel, components.IconButtonPlain).Clicked() {
			a.assist.full = !a.assist.full
		}
		if components.IconButton(c, lucide.X(), "关闭助手", components.IconButtonPlain).Clicked() {
			a.assistantOpen = false
		}
	})
}

func (a *App) assistantHeaderIcon(c *ui.Context, icon *ui.SVG, label string, on bool, act func()) {
	style := components.IconButtonPlain
	if on {
		style = components.IconButtonToolbar
	}
	if components.IconButton(c, icon, label, style).Clicked() {
		act()
	}
}

// assistantResizeHandle is the left edge. Dragging left widens the pane
// toward the file list; a double click is the same as 全屏.
func (a *App) assistantResizeHandle(c *ui.Context) {
	grip := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-3).Width(7).
		Cursor(ui.CursorResizeEW)
	a.assist.widthDragging = grip.Dragging()
	if dx, _, ok := grip.Dragged(); ok && dx != 0 {
		a.assist.width = a.clampAssistWidth(c, a.assist.width-dx)
		a.assist.widthDirty = true
	}
	if grip.DoubleClicked() {
		a.assist.full = !a.assist.full
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		if !grip.Hovered() && !grip.Dragging() {
			return
		}
		p.Fill(ui.Rect{X: r.X + (r.W-1)/2, Y: r.Y, W: 1, H: r.H}, colorAccent(), 0)
	})
}

// clampAssistWidth keeps a sliver of the file list unless the user asked
// for 全屏. The window size is what Context knows; the sidebar and the
// preview, when it is open, are not part of the chat's room.
func (a *App) clampAssistWidth(c *ui.Context, w float32) float32 {
	if w < assistantMinW {
		w = assistantMinW
	}
	winW, _ := c.Size()
	if winW <= 0 {
		return w
	}
	room := winW - a.navWidth - 16
	if a.previewOpen {
		room -= a.previewW
	}
	maxW := room - 160
	if maxW < assistantMinW {
		maxW = assistantMinW
	}
	if w > maxW {
		w = maxW
	}
	return w
}

// assistStarters are the empty transcript's example requests.
var assistStarters = []string{
	"按拍摄日期重命名照片",
	"把安装包和压缩包归档到“归档”文件夹",
	"找出重复或临时文件",
}

func (a *App) assistantTranscript(c *ui.Context) {
	if len(a.assist.turns) == 0 {
		ui.Column(c).FillWidth().Padding(48, 8, 12, 8).Gap(8).AlignItems(ui.Center).Children(func() {
			components.Icon(c, lucide.Bot).Size(28, 28).TextColor(colorMuted())
			ui.Text(c, "晴谷助手").FontSize(18).FontWeight(600).TextColor(colorText())
			ui.Text(c, "整理、重命名、归档当前文件夹里的文件。").
				FontSize(13).TextColor(colorMuted()).Wrap().TextAlign(ui.Center)
			// Starters put a request in the box rather than sending it: the
			// user still reads it, picks the model and presses 发送.
			ui.Column(c).FillWidth().Gap(6).Padding(12, 0, 0, 0).Children(func() {
				for _, s := range assistStarters {
					if components.Button(c, s, components.ButtonSecondary).FillWidth().Clicked() {
						a.assist.draft = s
					}
				}
			})
		})
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
	var tools []int
	var answer strings.Builder
	var replies []int
	for _, i := range ex.Rest {
		t := turns[i]
		switch t.Role {
		case "tool":
			tools = append(tools, i)
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
						chat.Attachment(c.Key(f.Path), chat.AttachmentOptions{Label: f.Name, Detail: f.Detail})
					}
				})
			}
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
			if detail == "" && actStatus == chat.Running {
				detail = "进行中"
			}
			activities = append(activities, chat.Activity{
				ID: itoa(i), Action: action, Detail: detail,
				Status: actStatus, Mono: true, State: st, MaxHeight: 220,
				Content: func(c *ui.Context) {
					ui.Text(c, text).FontSize(12).TextColor(colorText()).Wrap()
				},
			})
		}
		if last && a.assist.running && a.assist.pending != "" {
			activities = append(activities, chat.Activity{
				ID: "pending", Action: a.assist.pending, Detail: "进行中", Status: chat.Running,
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
	var finalView chat.View
	if final != "" {
		streaming := last && a.assist.running
		finalView = func(c *ui.Context) {
			ui.Column(c.Key("md")).FillWidth().MinWidth(0).Children(func() {
				a.assistMarkdown(c, paint, final, replies, streaming)
			})
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
		Prompt: prompt, Steps: steps, Final: finalView,
		HoldOpen: !a.assist.scroll.AtLatest(),
		Footer: func(c *ui.Context) {
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

func (a *App) assistantPickFiles() {
	if a.assist.picking {
		return
	}
	a.assist.picking = true
	win := a.win
	dir := a.cur().path
	if dir == "" || specialPath(dir) {
		dir, _ = os.UserHomeDir()
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "指定要读取的文件",
			DefaultPath: dir,
			ButtonLabel: "添加",
			Message:     "可以多选。助手会读这些文件后再回答。",
			Multiple:    true,
		})
		a.postAssist(func() {
			a.assist.picking = false
			if err != nil {
				a.statusMsg = err.Error()
				return
			}
			n := a.assist.addFiles(paths)
			if n > 0 {
				a.statusMsg = "已加入 " + itoa(n) + " 个文件"
			} else if len(paths) > 0 {
				a.statusMsg = "没有新的文件"
			}
		})
	}()
}

func (a *App) assistantAttachSelection() {
	var paths []string
	for _, e := range a.selectedEntries() {
		if e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	if len(paths) == 0 {
		a.statusMsg = "先在列表里选中文件"
		return
	}
	n := a.assist.addFiles(paths)
	if n == 0 {
		a.statusMsg = "没有新的文件"
		return
	}
	a.statusMsg = "已加入 " + itoa(n) + " 个文件"
}

func (a *App) attachPastedPaths(text string) {
	if strings.Count(text, "\n") > 20 {
		return
	}
	var paths []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r'
	}) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := os.Stat(strings.Trim(line, "\"'")); err == nil {
			paths = append(paths, line)
		}
	}
	if len(paths) == 0 {
		return
	}
	n := a.assist.addFiles(paths)
	if n > 0 {
		a.statusMsg = "已加入 " + itoa(n) + " 个文件"
	}
}

// bindAssistDrop makes the transcript and the composer one target. A drop
// names the paths for the next message (addFiles). It does not move or
// copy: the conversation is not a folder. The card keeps AcceptFiles for
// the FileDrop path; registering DropOptions there too would take the
// native drop away from this column and skip dropTookOwnDrag.
func (a *App) bindAssistDrop(el ui.Element) {
	if over, ok := ui.DragOver[dragPayload](el); ok && len(over.paths) > 0 {
		el.Background(colorSelected().Alpha(0.75))
	}
	a.bindNativeDrop(el, func(paths []string) {
		a.attachDroppedPaths(paths)
	})
	if el.FileDragOver() {
		el.Background(colorSelected().Alpha(0.75))
	}
	if p, ok := ui.Drop[dragPayload](el); ok && len(p.paths) > 0 {
		a.attachDroppedPaths(p.paths)
		a.dropTookOwnDrag = true
	}
	if files := el.DroppedFiles(); len(files) > 0 {
		a.attachDroppedPaths(files)
	}
}

// attachDroppedPaths hangs local paths on the assistant. A file URL from
// another application is a path; a remote link is not, and is said so.
func (a *App) attachDroppedPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	local := make([]string, 0, len(paths))
	rejected := false
	for _, p := range paths {
		path, ok := localFilePath(p)
		if !ok {
			rejected = true
			continue
		}
		local = append(local, path)
	}
	n := 0
	if len(local) > 0 {
		n = a.assist.addFiles(local)
	}
	switch {
	case n > 0:
		a.statusMsg = "已加入 " + itoa(n) + " 个文件"
	case len(local) > 0:
		a.statusMsg = "没有新的文件"
	case rejected:
		a.statusMsg = "只能指定本地文件或文件夹"
	}
}

func (u *assistantUI) addFiles(paths []string) int {
	seen := map[string]bool{}
	for _, f := range u.files {
		seen[f.Path] = true
	}
	added := 0
	for _, raw := range paths {
		raw = strings.Trim(strings.TrimSpace(raw), "\"'")
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if seen[abs] || len(u.files) >= assistReadFiles {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			continue
		}
		detail := "文件夹"
		if !info.IsDir() {
			detail = formatBytes(info.Size())
		}
		u.files = append(u.files, aiAttach{Path: abs, Name: filepath.Base(abs), Detail: detail})
		seen[abs] = true
		added++
	}
	if added > 0 && strings.TrimSpace(u.draft) == "" {
		u.draft = "请阅读这些文件"
	}
	return added
}

func (a *App) assistantProviders(c *ui.Context) {
	a.assist.refreshConfig()
	ui.Column(c).FillWidth().Padding(4, 8).Gap(4).Children(func() {
		for _, p := range a.assist.providers {
			id := p.ID
			row := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center)
			row.Children(func() {
				label := p.Name
				if p.ID == a.assist.activeID {
					label = "● " + label
				}
				if components.Button(c, label, components.ButtonGhost).Clicked() {
					a.assist.refreshConfig()
					a.assist.activeID = id
					a.assist.save()
				}
				if components.Button(c, "删除", components.ButtonGhost).Clicked() {
					a.assist.deleteProvider(id)
				}
			})
		}
		a.assistantField(c, "名称", &a.assist.pName, "OpenAI")
		a.assistantField(c, "接口", &a.assist.pURL, "https://api.openai.com/v1")
		a.assistantField(c, "模型", &a.assist.pModel, "gpt-4o-mini")
		a.assistantField(c, "密钥", &a.assist.pKey, "sk-…")
		if components.Button(c, "添加供应商", components.ButtonSecondary).Clicked() {
			a.assist.addProvider()
		}
		ui.Text(c, "密钥只写在本机配置里，不会出现在回复中。").FontSize(11).TextColor(colorMuted()).Wrap()
	})
}

func (a *App) assistantPasswords(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(4, 8).Gap(4).Children(func() {
		ui.Text(c, "解压密码").FontSize(12).FontWeight(600).TextColor(colorText())
		ui.Text(c, "选中归档执行「全部解压」时，按这里从上到下试；密码不会发给模型。").
			FontSize(11).TextColor(colorMuted()).Wrap()
		if len(a.assist.passwords) == 0 {
			ui.Text(c, "还没有解压密码。").FontSize(11).TextColor(colorMuted()).Wrap()
		}
		for i := range a.assist.passwords {
			pw := a.assist.passwords[i]
			row := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center)
			row.Children(func() {
				label := pw.Label
				if label == "" {
					label = "解压密码 " + itoa(i+1)
				}
				ui.Text(c, label).FontSize(12).TextColor(colorText()).
					SingleLine().Grow(1).MinWidth(0)
				if components.Button(c, "删除", components.ButtonGhost).Clicked() {
					a.assist.deleteArchivePassword(pw.ID)
				}
			})
		}
		a.assistantField(c, "名称", &a.assist.pwName, "工作压缩包")
		secret := components.TextBox(c, &a.assist.pwSecret, components.TextBoxOptions{}).
			Grow(1).MinWidth(0).Height(28).FontSize(12).Placeholder("解压密码").
			Label("解压密码").Password()
		_ = secret
		if components.Button(c, "保存解压密码", components.ButtonSecondary).Clicked() {
			a.assist.addArchivePassword()
			a.statusMsg = "已保存解压密码"
		}
	})
}

func (u *assistantUI) togglePanel(which string) {
	on := false
	switch which {
	case "prov":
		on = !u.showProv
	case "book":
		on = !u.showBook
	case "hist":
		on = !u.showHist
	}
	u.showProv = which == "prov" && on
	u.showBook = which == "book" && on
	u.showHist = which == "hist" && on
}

// noteChrome writes width, fullscreen and autopilot once they settle.
// Dragging saves on release so a resize does not rewrite the config per frame.
func (u *assistantUI) noteChrome() {
	if !u.loaded || u.diskPath == "" {
		return
	}
	widthReady := u.widthDirty && !u.widthDragging
	chrome := u.full != u.savedFull || u.autopilot != u.savedAutopilot
	if !widthReady && !chrome {
		return
	}
	u.widthDirty = false
	u.savedFull = u.full
	u.savedAutopilot = u.autopilot
	u.save()
}

func (a *App) assistantBusy() bool {
	if !a.assist.running {
		return false
	}
	a.statusMsg = "请先停止当前回答"
	return true
}

func (a *App) assistantNewSession() {
	if a.assistantBusy() {
		return
	}
	if len(a.assist.turns) == 0 {
		a.statusMsg = "当前已经是新会话"
		return
	}
	a.assist.persistSession(a.assist.folder)
	a.assist.clearSession()
	a.assist.resetPaint()
	a.assist.scroll = chat.ConversationState{}
	a.statusMsg = "已新开会话"
}

func (a *App) assistantOpenSession(id string) {
	if id == "" || id == a.assist.sessionID {
		a.assist.showHist = false
		return
	}
	if a.assistantBusy() {
		return
	}
	a.assist.persistSession(a.assist.folder)
	if err := a.assist.loadSession(id); err != nil {
		a.statusMsg = "打不开这段会话"
		return
	}
	a.assist.touchActive()
	a.assist.resetPaint()
	a.assist.scroll = chat.ConversationState{}
	a.assist.showHist = false
}

func (a *App) assistantDeleteSession(id string) {
	if id == a.assist.sessionID && a.assistantBusy() {
		return
	}
	a.assist.deleteSession(id)
	if id != a.assist.sessionID {
		return
	}
	if len(a.assist.sessions) > 0 {
		if err := a.assist.loadSession(a.assist.sessions[0].ID); err != nil {
			a.assist.clearSession()
		}
	} else {
		a.assist.clearSession()
	}
	a.assist.resetPaint()
	a.assist.scroll = chat.ConversationState{}
	a.statusMsg = "已删除会话"
}

func (a *App) assistantRecords(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(4, 8).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "历史会话").FontSize(12).FontWeight(600).TextColor(colorText()).Grow(1)
			if components.Button(c, "新会话", components.ButtonSecondary).Clicked() {
				a.assistantNewSession()
			}
		})
		switch {
		case a.assist.diskPath == "" && len(a.assist.turns) > 0:
			ui.Text(c, "没有用户配置目录，这次对话还不能写入磁盘。").FontSize(11).TextColor(colorMuted()).Wrap()
		case len(a.assist.sessions) == 0:
			ui.Text(c, "还没有历史会话。发出第一条消息后会留在本机。").FontSize(11).TextColor(colorMuted()).Wrap()
		default:
			metas := append([]aiSessionMeta(nil), a.assist.sessions...)
			list := ui.Scroll(c).FillWidth().MaxHeight(220).Gap(2)
			list.Children(func() {
				for _, meta := range metas {
					a.assistantSessionRow(c, meta)
				}
			})
		}
		ui.Text(c, "记住的事").FontSize(12).FontWeight(600).TextColor(colorText())
		a.assistantField(c, "记忆", &a.assist.memDraft, "下载按月份归档")
		if components.Button(c, "记住", components.ButtonSecondary).Clicked() {
			msg, err := a.assist.addMemory(a.assist.memDraft)
			if err != nil {
				a.statusMsg = err.Error()
			} else {
				a.assist.memDraft = ""
				a.statusMsg = msg
			}
		}
		if len(a.assist.memories) == 0 {
			ui.Text(c, "说「记住……」，以后的会话也会知道。").FontSize(11).TextColor(colorMuted()).Wrap()
		} else {
			items := append([]aiMemory(nil), a.assist.memories...)
			mem := ui.Scroll(c).FillWidth().MaxHeight(140).Gap(2)
			mem.Children(func() {
				for _, m := range items {
					a.assistantMemoryRow(c, m)
				}
			})
		}
		ui.Text(c, a.assist.historyWhere()).FontSize(11).TextColor(colorMuted()).Wrap()
	})
}

func (a *App) assistantSessionRow(c *ui.Context, meta aiSessionMeta) {
	row := ui.Row(c.Key(meta.ID)).FillWidth().Gap(4).AlignItems(ui.Center)
	row.Children(func() {
		label := meta.Title
		if label == "" {
			label = "新会话"
		}
		if meta.ID == a.assist.sessionID {
			label = "● " + label
		}
		btn := ui.ButtonBase(c).Grow(1).MinWidth(0).Padding(2, 4).Radius(4)
		if btn.Hovered() {
			btn.Background(colorHover())
		}
		btn.Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(colorText()).SingleLine()
				detail := assistWhen(meta.Updated)
				if base := filepath.Base(meta.Folder); base != "" && base != "." && base != string(filepath.Separator) {
					if detail != "" {
						detail += " · "
					}
					detail += base
				}
				if meta.Preview != "" {
					if detail != "" {
						detail += " · "
					}
					detail += meta.Preview
				}
				if detail != "" {
					ui.Text(c, detail).FontSize(11).TextColor(colorMuted()).SingleLine()
				}
			})
		})
		if btn.Clicked() {
			a.assistantOpenSession(meta.ID)
		}
		if components.Button(c, "删除", components.ButtonGhost).Clicked() {
			a.assistantDeleteSession(meta.ID)
		}
	})
}

func (a *App) assistantMemoryRow(c *ui.Context, m aiMemory) {
	row := ui.Row(c.Key(m.ID)).FillWidth().Gap(6).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, m.Text).FontSize(12).TextColor(colorText()).SingleLine().Grow(1).MinWidth(0)
		if components.Button(c, "删除", components.ButtonGhost).Clicked() {
			a.assist.deleteMemory(m.ID)
			a.statusMsg = "已忘掉"
		}
	})
}

func (a *App) assistantField(c *ui.Context, label string, value *string, hint string) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
		components.TextBox(c, value, components.TextBoxOptions{}).Grow(1).MinWidth(0).
			Height(28).FontSize(12).Placeholder(hint)
	})
}
