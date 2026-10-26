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
	loaded   bool
	draft    string
	running  bool
	status   string
	turns    []aiTurn
	scroll   chat.ConversationState
	plans    map[string]aiRenamePlan
	applied  map[string]bool
	showProv bool
	showBook bool
	showHist bool
	// sessionRenamed keeps a user title from auto titling; histW is the
	// history column's width, histDragging/histDirty save it on release.
	sessionRenamed bool
	savedHist      bool
	histW          float32
	histDragging   bool
	histDirty      bool
	histQuery      string
	renamingID     string
	renameDraft    string
	floatLay       chat.FloatingConversationState
	// errOpen is which error lines are expanded (assistErrorLine).
	errOpen    map[int]bool
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
	width            float32
	full             bool
	autopilot        bool
	extraInstruction string
	disabledSkills   []string
	savedFull        bool
	savedAutopilot   bool
	widthDirty       bool
	widthDragging    bool
	files            []aiAttach
	// chipLit is when each attached path lit up: the composer's chips
	// breathe from then on, and the sent message's chips keep the phase
	// while the run reads them.
	chipLit map[string]time.Time
	// queued is a message sent while a run was going: it waits out the run
	// and goes out by itself when the run comes back (ZCode's queue).
	queued string
	// flowCool is when the header strip's last sweep finishes: the run is
	// over, but the light fades out rather than snapping off.
	flowCool time.Time
	picking  bool

	// slashIdx is the highlighted row in the “/” popup; slashHide keeps
	// the menu closed after Esc until the draft is no longer a slash query.
	slashIdx  int
	slashHide bool

	// ops holds the file-operation cards' undo and timing (assistant_opcard.go).
	assistOps
}

// toggleAssistant opens or closes the pane. Opening onto a blank session
// with sessions on disk brings up the history column, so 回车 or the
// toolbar's 助手 lands on the conversations there is to pick from; a pane
// reopened over a live conversation keeps whatever face it had.
func (a *App) toggleAssistant() {
	a.assistantOpen = !a.assistantOpen
	if a.assistantOpen {
		a.assist.ensure()
		if len(a.assist.turns) == 0 && len(a.assist.sessions) > 0 {
			a.assist.showHist = true
		}
	}
}

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
	if visualV2 && !a.assist.full {
		// The docked pane sits inside the opaque content card, where a
		// backdrop has nothing to blur; a material there also stalled the
		// composer's second send under test. Tint only.
		pane.Background(glassTint())
	} else {
		pane.Background(c.Theme().Background)
	}
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
			a.assistFlowStrip(c)
			// Below the header the pane splits like ZCode's chat: the history
			// column on the left while 会话 is on, the conversation filling the
			// rest. The column keeps its own scroll so a long list never
			// pushes the composer away.
			below := ui.Row(c).Grow(1).FillWidth().MinHeight(0).MinWidth(0)
			below.Children(func() {
				if a.assist.showHist {
					// Full screen gives the column its full width; a narrow pane
					// keeps the conversation's room and shrinks the list instead.
					histW := a.assist.histWidth()
					if !a.assist.full {
						histW = min(histW, max(assistHistMinW, a.assist.width-280))
					}
					hist := ui.Column(c).Width(histW).Shrink(0).FillHeight().MinHeight(0).
						Background(c.Theme().Surface)
					hist.Children(func() { a.assistantRecords(c) })
					ui.Box(c).Width(1).FillHeight().Shrink(0).Background(colorDivider()).Children(func() {
						a.assistantHistGrip(c)
					})
				}
				// The column, not the card, takes a drag from this window and a
				// native file list. The card only sees DroppedFiles, so a drop
				// on the composer would otherwise miss both.
				conv := ui.Column(c).Grow(1).FillHeight().MinHeight(0).MinWidth(0)
				conv.Children(func() {
					chat.FloatingConversation(c, &a.assist.floatLay, &a.assist.scroll, "回到底部",
						func(*ui.Context) { a.assistantTranscript(c) },
						func(*ui.Context) { a.assistantComposer(c) },
					)
				})
				a.bindAssistDrop(conv)
			})
		})
	})
	a.assist.noteChrome()
}

func (a *App) assistantComposer(c *ui.Context) {
	var attachments chat.View
	if len(a.assist.files) > 0 {
		attachments = func(c *ui.Context) { a.assistantAttachments(c) }
	}
	slashOpen := a.assistSlashOpen()
	slashItems := a.assistSlashItems()
	if slashOpen {
		a.assistSlashClamp(len(slashItems))
	}
	ui.Column(c).FillWidth().MinWidth(0).Shrink(0).Gap(6).Children(func() {
		if slashOpen {
			a.assistantSlashMenu(c, slashItems)
		}
		if a.assist.queued != "" {
			// ZCode's queue panel: the message waits for the run, a click
			// takes it back into the box.
			q := ui.ButtonBase(c).Key("assist-queue").FillWidth().Radius(8).
				Padding(6, 10).Gap(8).AlignItems(ui.Center).Label("取消排队")
			q.Border(1, colorDivider()).Background(c.Theme().Surface)
			if q.Hovered() {
				q.Background(colorHover())
			}
			q.Children(func() {
				breathDot(c, 8)
				ui.Text(c, "排队中："+a.assist.queued).FontSize(12).TextColor(colorText()).
					SingleLine().Grow(1).MinWidth(0)
				ui.Text(c, "点击取消").FontSize(11).TextColor(colorMuted()).Shrink(0)
			})
			if q.Clicked() {
				a.assist.draft = a.assist.queued
				a.assist.queued = ""
			}
		}
		a.assistContextBar(c)
		comp := chat.GlassComposer(c, &a.assist.floatLay, &a.assist.draft, chat.ComposerOptions{
			Label:        "给助手的消息",
			Placeholder:  "让助手整理这个文件夹…",
			SendLabel:    "发送",
			StopLabel:    "停止",
			Running:      a.assist.running,
			AcceptFiles:  true,
			SendDisabled: slashOpen,
			Attachments:  attachments,
			Trailing:     a.assistantComposerTrailing,
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
		if comp.Dragging {
			// ZCode's drop overlay: hovering files over the card says what
			// letting go does, over both the in-window drag and a native one.
			hint := ui.Row(c).Key("assist-drop-hint").Absolute().Attach(ui.AnchorTop, ui.AnchorTop).
				Top(6).FillWidth().Justify(ui.Center)
			hint.Children(func() {
				pill := ui.Row(c).Padding(5, 12).Radius(13).Gap(6).AlignItems(ui.Center).
					Background(colorAccent())
				pill.Children(func() {
					components.Icon(c, lucide.Paperclip).Size(13, 13).TextColor(ui.Hex("#FFFFFF"))
					ui.Text(c, "松开以附加到对话").FontSize(12).TextColor(ui.Hex("#FFFFFF")).SingleLine()
				})
			})
		}
		if slashOpen {
			a.assistantSlashKeys(comp.Input, slashItems)
		}
		// FileDrop on the card itself. Native and in-window drops land on the
		// conversation column (bindAssistDrop), so this is not a second copy.
		if len(comp.Dropped) > 0 {
			a.attachDroppedPaths(comp.Dropped)
		}
		if comp.Stop && a.assist.stop != nil {
			a.assist.queued = ""
			a.assist.stop()
			a.assist.status = "正在停止…"
		}
		switch {
		case comp.Send && !a.assist.running:
			a.assistantSubmit()
		case comp.Send && strings.TrimSpace(a.assist.draft) != "":
			// A message sent over a running turn waits out the run instead
			// of interrupting it: the queue chip above the card says so,
			// and cancels on a click. One message waits; a new one takes
			// its place.
			a.assist.queued = strings.TrimSpace(a.assist.draft)
			a.assist.draft = ""
		}
	})
}

// litChip notes a freshly attached path: its chip breathes from now.
func (u *assistantUI) litChip(path string) {
	if u.chipLit == nil {
		u.chipLit = map[string]time.Time{}
	}
	if len(u.chipLit) > 128 {
		u.chipLit = map[string]time.Time{}
	}
	u.chipLit[aiFxKey(path)] = time.Now()
}

// assistChipBreath makes one attachment chip breathe: the composer's
// chips from the moment the files are dropped or picked, the sent
// message's chips while the run is reading them. The phase comes from
// chipLit; a chip without an entry breathes from an arbitrary phase.
func (a *App) assistChipBreath(c *ui.Context, chip ui.Element, path string) {
	breathOverlay(c, chip, a.assist.chipLit[aiFxKey(path)])
}

func (a *App) assistantAttachments(c *ui.Context) {
	drop := -1
	for i, f := range a.assist.files {
		// A chip breathes from the moment the file joins: the drag's answer
		// before anything is sent.
		chip := ui.Box(c.Key(f.Path + "-fx"))
		a.assistChipBreath(c, chip, f.Path)
		var res chat.AttachmentResult
		chip.Children(func() {
			res = chat.Attachment(c.Key(f.Path), chat.AttachmentOptions{
				Label: f.Name, Detail: f.Detail, RemoveLabel: "移除 " + f.Name,
			})
		})
		if res.Remove {
			drop = i
		}
	}
	if drop >= 0 {
		delete(a.assist.chipLit, aiFxKey(a.assist.files[drop].Path))
		a.assist.files = append(a.assist.files[:drop], a.assist.files[drop+1:]...)
	}
}

func (a *App) assistantHeader(c *ui.Context) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 8).Gap(2).AlignItems(ui.Center).Children(func() {
		components.Icon(c, lucide.Bot).Size(16, 16).TextColor(colorText()).Shrink(0)
		ui.Text(c, a.assist.sessionLabel()).FontSize(13).FontWeight(600).TextColor(colorText()).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Margin(0, 0, 0, 6)
		a.assistantHeaderIcon(c, lucide.MessagesSquare(), "会话", a.assist.showHist, func() { a.assist.togglePanel("hist") })
		if components.IconButton(c, lucide.Settings2(), "助手设置", components.IconButtonPlain).Clicked() {
			a.openAssistantSettings()
		}
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

func (a *App) assistantPickFiles() {
	win := a.win
	dir := assistDialogDir(a.cur().path)
	a.runAssistPick(func() ([]string, error) {
		return mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      win,
			Title:       "指定要读取的文件",
			DefaultPath: dir,
			ButtonLabel: "添加",
			Message:     "可以多选。助手会读这些文件后再回答。",
			Multiple:    true,
		})
	}, func(paths []string, err error) {
		if err != nil {
			a.statusMsg = err.Error()
			return
		}
		n := a.assist.addFiles(paths)
		if n > 0 {
			a.noteUsedPaths(2, paths...)
			a.statusMsg = "已加入 " + itoa(n) + " 个项目"
		} else if len(paths) > 0 {
			a.statusMsg = "没有新的文件"
		}
	})
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
	a.noteUsedPaths(2, paths...)
	a.statusMsg = "已加入 " + itoa(n) + " 个项目"
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
		a.statusMsg = "已加入 " + itoa(n) + " 个项目"
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
		a.statusMsg = "已加入 " + itoa(n) + " 个项目"
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
		u.litChip(abs)
		added++
	}
	if added > 0 && strings.TrimSpace(u.draft) == "" {
		u.draft = "请阅读这些文件"
	}
	return added
}

func (a *App) assistantProviders(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(4, 8).Gap(4).Children(func() {
		ui.Text(c, "供应商已迁到系统设置。").FontSize(12).TextColor(colorText()).Wrap()
		if components.Button(c, "打开助手设置", components.ButtonSecondary).Clicked() {
			a.openAssistantSettings()
		}
	})
}

func (a *App) assistantPasswords(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(4, 8).Gap(4).Children(func() {
		ui.Text(c, "解压密码本请在设置中管理。").FontSize(12).TextColor(colorText()).Wrap()
		if components.Button(c, "打开助手设置", components.ButtonSecondary).Clicked() {
			a.openAssistantSettings()
		}
	})
}

func (u *assistantUI) togglePanel(which string) {
	if which == "hist" {
		u.showHist = !u.showHist
		return
	}
	u.showHist = false
}

// noteChrome writes width, fullscreen and autopilot once they settle.
// Dragging saves on release so a resize does not rewrite the config per frame.
func (u *assistantUI) noteChrome() {
	if !u.loaded || u.diskPath == "" {
		return
	}
	widthReady := (u.widthDirty && !u.widthDragging) || (u.histDirty && !u.histDragging)
	chrome := u.full != u.savedFull || u.autopilot != u.savedAutopilot || u.showHist != u.savedHist
	if !widthReady && !chrome {
		return
	}
	u.widthDirty, u.histDirty = false, false
	u.savedHist = u.showHist
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

// assistTimelineGroup is the history list's recency bucket, as ZCode's
// taskTimelineGroups: 今天, 昨天, 本周, 上周, 本月, then 更早.
func assistTimelineGroup(t time.Time) string {
	if t.IsZero() {
		return "更早"
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	switch diff := int(today.Sub(day).Hours() / 24); {
	case diff <= 0:
		return "今天"
	case diff == 1:
		return "昨天"
	}
	// The week starts on Monday, as the rest of the app's Chinese chrome.
	weekday := int(now.Weekday()+6) % 7
	thisWeek := today.AddDate(0, 0, -weekday)
	switch {
	case !day.Before(thisWeek):
		return "本周"
	case !day.Before(thisWeek.AddDate(0, 0, -7)):
		return "上周"
	case day.Year() == now.Year() && day.Month() == now.Month():
		return "本月"
	default:
		return "更早"
	}
}
