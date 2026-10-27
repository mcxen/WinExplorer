package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

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
	// liveEpoch bumps on each submit and each Stop so a cancelled run's
	// late stream/tool callbacks cannot paint into the next turn.
	pending   string
	halted    bool
	liveOn    bool
	liveID    int
	liveAt    int
	liveEpoch uint64

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
			Running:      a.assist.running && !a.assist.halted,
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
		if comp.Stop {
			a.assistantRequestStop()
		}
		switch {
		case comp.Send && !a.assist.running:
			a.assistantSubmit()
		case comp.Send && strings.TrimSpace(a.assist.draft) != "":
			// A message sent over a running (or just-stopped) turn waits out
			// the run: the queue chip says so. Stop clears the queue; a later
			// Send while cleanup finishes queues again.
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
