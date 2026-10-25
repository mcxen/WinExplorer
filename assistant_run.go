package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
	officialmcp "github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/egoist/mygo"

	"sunvale/internal/applog"
	assistant "sunvale/internal/assistant"
	"sunvale/internal/rename"
)

// assistant_run.go asks Eino's chat-model agent to answer one message.
// The tools it can call are the in-process sunvale MCP server, rebound to
// the folder open in this tab before the run starts. Full autopilot is the
// same agent with a longer budget; it applies rename plans itself instead
// of waiting for 应用方案.
//
// runFileAssistant is the headless core. The desktop pane and the HTTP A2A
// endpoint both use it, so a remote run reads and writes files through the
// same local MCP tools and cannot grow a second, divergent path.

// assistantRunMu keeps two windows from rebinding the folder mid-call.
var assistantRunMu sync.Mutex

// assistRun is one send: the folder, the selection and the files are a
// snapshot, so a click during the turn cannot change what the model sees.
type assistRun struct {
	prov      aiProvider
	root      string
	hidden    bool
	book      []archivePassword
	selected  []string
	files     []string
	autopilot bool
	prefs     assistPrefs
	hist      []*schema.Message
	memories  []aiMemory
	// freqPaths is the run's snapshot of the usage ledger: it fills the
	// prompt's {frequent} section and backs the frequent_files tool.
	freqPaths []string
	// onLive paints one transcript line as it grows. onStatus is the working
	// label; onPending names a tool that has been chosen and not returned.
	// onSwitched records a model that took over after the first one was
	// rate-limited, so the next send does not walk into the same wall.
	onLive     func(id int, turn aiTurn, partial bool)
	onStatus   func(string)
	onPending  func(string)
	onSwitched func(model string)
	// runID and origin tag this run's log records (applog_glue.go);
	// origin is "pane" unless a caller (schedule, a2a, mcp) names itself.
	runID  string
	origin string
}

// assistRunResult is what a headless caller gets back. Text is the final
// assistant answer, Events keeps tool output for an A2A artifact, and
// Choice is set when the model stopped to ask the user a question.
type assistRunResult struct {
	Text   string
	Events []aiTurn
	Choice *aiChoice
	Plan   string
	Asked  bool
}

// assistantRunner is the model call behind both the pane and A2A. Tests
// replace it with a stub; production retries a rate-limited model on
// another route before it reports the failure.
type assistantRunner func(context.Context, assistRun) (assistRunResult, error)

var runAssistantHeadless assistantRunner = runWithRateFallback

func (a *App) assistantSubmit() {
	a.assist.ensure()
	if assistDemoOn() {
		a.assistDemoSubmit()
		return
	}
	a.assist.refreshConfig()
	text := strings.TrimSpace(a.assist.draft)
	files := append([]aiAttach(nil), a.assist.files...)
	if (text == "" && len(files) == 0) || a.assist.running {
		return
	}
	if a.assistOpenUIDemoSubmit() {
		return
	}
	if text == "" {
		text = "请阅读这些文件"
	}
	root := a.cur().path
	if root == "" || specialPath(root) {
		if len(files) == 0 {
			a.statusMsg = "请先打开一个文件夹，或指定要读的文件"
			return
		}
		root = filepath.Dir(files[0].Path)
	}
	prov, ok := a.assist.activeProvider()
	if !ok || prov.APIKey == "" {
		a.statusMsg = "请先在设置 → 助手中添加供应商"
		return
	}
	a.assist.draft = ""
	a.assist.files = nil
	a.assist.folder = root
	a.assist.halted = false
	a.assist.pending = ""
	a.assist.resetLive()
	a.assist.turns = append(a.assist.turns, aiTurn{Role: "user", Text: text, Files: files, At: time.Now()})
	a.assist.persistSession(root)
	a.assist.running = true
	if a.assist.autopilot {
		a.assist.status = "全托管进行中…"
	} else {
		a.assist.status = "正在思考…"
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.assist.stop = cancel
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	// The attached files are the first thing the run reads: their rows
	// breathe from the moment the message goes out.
	aiFx.markBusy(paths...)
	var selected []string
	for _, e := range a.selectedEntries() {
		if e.Path != "" {
			selected = append(selected, e.Path)
		}
	}
	run := assistRun{
		prov: prov, root: root, hidden: a.store.settings().ShowHidden,
		book: append([]archivePassword(nil), a.assist.passwords...), selected: selected,
		files: paths, autopilot: a.assist.autopilot,
		freqPaths: a.frequentTop(20),
		prefs: assistPrefs{
			Width: a.assist.width, Full: a.assist.full, Autopilot: a.assist.autopilot,
			ExtraInstruction: a.assist.extraInstruction,
			DisabledSkills:   append([]string(nil), a.assist.disabledSkills...),
		},
		hist:     a.assistHistory(),
		memories: append([]aiMemory(nil), a.assist.memories...),
	}
	run.onSwitched = func(model string) {
		id := prov.ID
		a.postAssist(func() { a.assist.useModel(id, model) })
	}
	go a.runAssistant(ctx, run)
}

func (a *App) runAssistant(ctx context.Context, run assistRun) {
	assistantRunMu.Lock()
	defer assistantRunMu.Unlock()
	var failed, asked bool
	before := map[string]bool{}
	defer func() {
		saved := sunvaleToolState.drainPending()
		undos := sunvaleToolState.drainUndos()
		nav := sunvaleToolState.drainNav()
		memAdd, memDrop := sunvaleToolState.drainMemory()
		var plans []string
		if run.autopilot && !failed && !asked && ctx.Err() == nil {
			plans = sunvaleToolState.plansSince(before)
		}
		lv := &a.assist.live
		lv.mu.Lock()
		tail := lv.pending
		lv.pending = nil
		lv.mu.Unlock()
		a.postAssist(func() {
			// The last partial of a stopped reply lands before the run
			// closes its live line, not after as a second copy.
			if tail != nil {
				a.applyAssistLive(tail)
			}
			a.assist.passwords = append(a.assist.passwords, saved...)
			if len(a.assist.passwords) > assistMaxArchivePasswords {
				a.assist.passwords = a.assist.passwords[len(a.assist.passwords)-assistMaxArchivePasswords:]
			}
			a.assist.save()
			a.assist.applyMemory(memAdd, memDrop)
			a.assist.persistSession(run.root)
			for _, op := range undos {
				a.pushUndo(op)
			}
			a.assist.running = false
			a.assist.stop = nil
			a.assist.pending = ""
			a.assist.resetLive()
			a.assist.status = ""
			a.assist.flowCool = time.Now().Add(assistFlowCool)
			aiFx.endRun()
			a.reloadAfterFx()
			for _, id := range plans {
				a.assistantApplyPlan(id)
			}
			if nav != "" {
				a.navigate(nav)
			}
			// A queued message waited out this run: it goes out now, as
			// ZCode's queue panel sends when the turn frees up.
			if a.assist.queued != "" {
				a.assist.draft = a.assist.queued
				a.assist.queued = ""
				a.assistantSubmit()
			}
		})
	}()

	if modelHist := assistModelHistory(run); len(modelHist) > 0 {
		last := modelHist[len(modelHist)-1]
		if last.Role == schema.User && len(run.files) > 0 {
			content := last.Content
			a.postAssist(func() {
				for i := len(a.assist.turns) - 1; i >= 0; i-- {
					if a.assist.turns[i].Role == "user" {
						a.assist.turns[i].Model = content
						break
					}
				}
			})
		}
	}

	before = sunvaleToolState.planIDs()
	run.onLive = func(id int, turn aiTurn, partial bool) {
		a.noteAssistLive(id, turn, partial)
	}
	run.onStatus = func(status string) {
		a.noteAssistStatus(status)
	}
	run.onPending = func(name string) {
		a.noteAssistPending(name)
	}
	result, err := runAssistantHeadless(ctx, run)
	asked = result.Asked
	if err != nil && !errors.Is(err, errAssistantStopped) {
		failed = true
		a.failAssist(err)
		return
	}
	if errors.Is(err, errAssistantStopped) {
		a.postAssist(func() {
			a.assist.halted = true
			a.assist.pending = ""
			if n := len(a.assist.turns); n > 0 {
				a.assist.turns[n-1].Stopped = true
			}
			a.assist.persistSession(run.root)
		})
	}
}

// runFileAssistant is the model loop without a window. Callers own
// assistantRunMu so the global tool bind cannot be changed underneath a
// turn. The returned events are ordinary transcript turns, which lets the
// local pane and A2A render the same tool output.
func runFileAssistant(ctx context.Context, run assistRun) (result assistRunResult, err error) {
	if run.runID == "" {
		run.runID = newRunID()
	}
	if run.origin == "" {
		run.origin = "pane"
	}
	start := time.Now()
	slog.Info("ai run start", "run", run.runID, "origin", run.origin, "provider", run.prov.Name,
		"model", run.prov.Model, "base_url", redactURL(run.prov.BaseURL), "root", run.root,
		"autopilot", run.autopilot, "history", len(run.hist), "files", len(run.files))
	defer func() {
		if p := recover(); p != nil {
			applog.LogPanic("assistant run "+run.runID, p)
			err = fmt.Errorf("助手运行异常：%v（详见日志）", p)
		}
		attrs := []any{"run", run.runID, "origin", run.origin, "model", run.prov.Model, applog.Since(start), "asked", result.Asked}
		switch {
		case errors.Is(err, errAssistantStopped):
			slog.Info("ai run stopped", attrs...)
		case err != nil:
			slog.Error("ai run failed", append(attrs, "err", applog.Trunc(err.Error(), 2000))...)
		default:
			slog.Info("ai run end", attrs...)
		}
	}()
	return runFileAssistantInner(ctx, run)
}

func runFileAssistantInner(ctx context.Context, run assistRun) (assistRunResult, error) {
	sunvaleToolState.bind(run.root, run.hidden, run.book, run.selected, run.autopilot)
	sunvaleToolState.bindWeb(run.prefs.WebSearch, assistant.SkillDisabled(run.prefs, assistant.SkillWeb))
	sunvaleToolState.bindMemory(run.memories)
	sunvaleToolState.bindFrequent(run.freqPaths)
	// The one place every run ends, windowed or headless: the lit rows'
	// breathing belongs to the run that asked for it, or a headless turn
	// (A2A, scheduled tasks) would leave its rows glowing forever.
	defer aiFx.endRun()
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  run.prov.APIKey,
		BaseURL: run.prov.BaseURL,
		Model:   run.prov.Model,
		// The logging client traces status, latency and error bodies
		// (applog_glue.go); it sets no timeout either.
		HTTPClient: aiHTTPClient(run.runID),
		// A client-wide deadline would cut a long stream off. Stop cancels
		// this context, and that is what closes the socket.
	})
	if err != nil {
		return assistRunResult{}, err
	}
	// The bag is this run's pictures. Binding it before the tools means the
	// first read_file already lands in the right place.
	bag := newMediaBag()
	sunvaleToolState.bindMedia(bag)
	vision := visionModel{inner: chatModel, bag: bag, vision: run.prov.VisionEnabled()}
	sess, err := sunvaleSession(ctx)
	if err != nil {
		return assistRunResult{}, err
	}
	tools, err := officialmcp.GetTools(ctx, &officialmcp.Config{Cli: sess, ServerName: "sunvale"})
	if err != nil {
		return assistRunResult{}, err
	}
	mode := "逐步确认"
	iters := 12
	if run.autopilot {
		mode = "全托管"
		iters = 24
	}
	memories := run.memories
	if assistant.SkillDisabled(run.prefs, assistant.SkillMemory) {
		memories = nil
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "sunvale",
		Description: "晴谷文件管理器助手",
		Instruction: assistant.EffectiveInstruction(run.prefs.ExtraInstruction),
		Model:       vision,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: iters,
		GenModelInput: assistant.GenModelInput,
	})
	if err != nil {
		return assistRunResult{}, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	iter := runner.Run(ctx, assistModelHistory(run), adk.WithSessionValues(map[string]any{
		"folder":    run.root,
		"mode":      mode,
		"selection": selectionText(run.selected),
		"memory":    memoryPrompt(memories),
		"frequent":  freqPromptText(run.freqPaths),
	}))
	var result assistRunResult
	nextID := 0
	for {
		if ctx.Err() != nil {
			return result, errAssistantStopped
		}
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			slog.Debug("ai event error", "run", run.runID, "err", ev.Err)
			if assistantStopped(ctx, ev.Err) {
				return result, errAssistantStopped
			}
			return result, ev.Err
		}
		if ev.Output == nil || ev.Output.MessageOutput == nil {
			continue
		}
		mv := ev.Output.MessageOutput
		id := nextID
		nextID++
		msg := mv.Message
		var streamErr error
		msgStart := time.Now()
		chunks := 0
		if mv.IsStreaming && mv.MessageStream != nil {
			toolSeen := map[string]bool{}
			msg, streamErr = readMessageStream(ctx, mv.MessageStream, func(full, reasoning string) {
				chunks++
				if mv.Role == schema.Tool {
					full = assistant.ToolText(full)
					turn := aiTurn{Role: "tool", Text: full, Tool: mv.ToolName, Plan: planIDFrom(full)}
					publishAssist(run, id, turn, true, &result)
					return
				}
				if reasoning != "" && strings.TrimSpace(full) == "" && run.onStatus != nil {
					run.onStatus(reasoningLabel(reasoning))
				}
				shown := streamLive(full)
				if strings.TrimSpace(shown) == "" {
					return
				}
				publishAssist(run, id, aiTurn{Role: "assistant", Text: shown}, true, &result)
			}, func(name string) {
				if toolSeen[name] || run.onPending == nil {
					return
				}
				toolSeen[name] = true
				run.onPending(name)
			})
		}
		logAssistMessage(run.runID, id, mv.Role, mv.ToolName, msg, mv.IsStreaming, chunks, msgStart, streamErr)
		if streamErr != nil && !errors.Is(streamErr, errAssistantStopped) {
			return result, streamErr
		}
		turn, has, choice := messageTurn(mv.Role, mv.ToolName, msg)
		if has {
			if errors.Is(streamErr, errAssistantStopped) {
				turn.Stopped = true
			}
			publishAssist(run, id, turn, false, &result)
			if mv.Role == schema.Tool && run.onPending != nil {
				run.onPending("")
			}
		}
		if errors.Is(streamErr, errAssistantStopped) {
			return result, errAssistantStopped
		}
		if choice != nil {
			result.Choice = choice
			result.Asked = true
			break
		}
	}
	return result, nil
}

// publishAssist paints a line and, once it is finished, keeps it for the
// headless result. Partials only update the open slot.
func publishAssist(run assistRun, id int, turn aiTurn, partial bool, result *assistRunResult) {
	if run.onLive != nil {
		run.onLive(id, turn, partial)
	}
	if partial {
		return
	}
	if result == nil {
		return
	}
	result.Events = append(result.Events, turn)
	if turn.Text != "" {
		result.Text = turn.Text
	}
	if turn.Plan != "" {
		result.Plan = turn.Plan
	}
	if turn.Choice != nil {
		result.Choice = turn.Choice
		result.Asked = true
	}
}

// assistModelHistory appends the named-file excerpts to the last user
// message, and the pictures with them when the model can see. The caller's
// history is left alone: its messages are shared with the visible
// transcript, and the excerpts must not be written to disk.
func assistModelHistory(run assistRun) []*schema.Message {
	hist := append([]*schema.Message(nil), run.hist...)
	if len(run.files) == 0 || len(hist) == 0 {
		return hist
	}
	last := hist[len(hist)-1]
	if last == nil || last.Role != schema.User {
		return hist
	}
	note, frames := specifiedFileNote(run.files)
	body := last.Content
	if body != "" {
		body += "\n\n"
	}
	body += note
	if !run.prov.VisionEnabled() {
		frames = nil
	}
	hist[len(hist)-1] = attachFiles(body, frames)
	return hist
}

// assistEventTurn converts one Eino event into a transcript line. A
// choice ends the turn: the next user message is the selection, not a
// prompt for the model to answer itself.
func assistEventTurn(ev *adk.AgentEvent) (aiTurn, bool, *aiChoice) {
	if ev == nil || ev.Output == nil || ev.Output.MessageOutput == nil {
		return aiTurn{}, false, nil
	}
	mv := ev.Output.MessageOutput
	return messageTurn(mv.Role, mv.ToolName, mv.Message)
}

// messageTurn is the visible form of one finished model or tool message.
// A tool line is kept even when its text is empty, so the call itself shows.
func messageTurn(role schema.RoleType, tool string, msg *schema.Message) (aiTurn, bool, *aiChoice) {
	if msg == nil {
		return aiTurn{}, false, nil
	}
	if tool == "" {
		tool = msg.ToolName
	}
	content := msg.Content
	if role == schema.Tool {
		content = assistant.ToolText(content)
	}
	// Tool output can quote fences from a file. Only assistant prose hides
	// an unfinished choice or OpenUI block.
	if role != schema.Tool {
		content = streamShown(content)
	}
	display, choice := parseChoices(content)
	if choice != nil && strings.TrimSpace(display) == "" {
		display = choice.Prompt
	}
	if role == schema.Tool {
		if strings.TrimSpace(display) == "" && tool == "" && choice == nil {
			return aiTurn{}, false, nil
		}
		return aiTurn{Role: "tool", Text: display, Tool: tool, Plan: planIDFrom(display), Choice: choice}, true, choice
	}
	if strings.TrimSpace(display) == "" && choice == nil {
		return aiTurn{}, false, nil
	}
	return aiTurn{Role: "assistant", Text: display, Choice: choice}, true, choice
}

func selectionText(paths []string) string {
	if len(paths) == 0 {
		return "无"
	}
	more := 0
	if len(paths) > 30 {
		more = len(paths) - 30
		paths = paths[:30]
	}
	text := strings.Join(paths, "\n")
	if more > 0 {
		text += fmt.Sprintf("\n还有 %d 个", more)
	}
	return text
}

// assistHistory is the tail of this session. Older turns stay on screen and
// on disk, but the model only sees the recent part so one long chat cannot
// crowd out the current folder.
func (a *App) assistHistory() []*schema.Message {
	return modelHistory(a.assist.turns)
}

// showAssistEvent reports whether the reply is a choice the user has to
// answer before the agent should keep calling tools.
func (a *App) showAssistEvent(ev *adk.AgentEvent) bool {
	t, ok, choice := assistEventTurn(ev)
	if !ok {
		return false
	}
	a.postAssist(func() {
		a.assist.turns = append(a.assist.turns, t)
		a.assist.scroll.JumpToLatest()
		a.assist.persistSession(a.assist.folder)
	})
	return choice != nil
}

func planIDFrom(text string) string {
	const mark = "plan_id:"
	i := strings.Index(text, mark)
	if i < 0 {
		return ""
	}
	id := strings.TrimSpace(text[i+len(mark):])
	if n := strings.IndexAny(id, "\n "); n >= 0 {
		id = id[:n]
	}
	return id
}

func (a *App) failAssist(err error) {
	a.postAssist(func() {
		a.assist.turns = append(a.assist.turns, aiTurn{Role: "assistant", Text: assistErrorText(err), Error: true, At: time.Now()})
		// The composer must take input again right away: the run's own
		// cleanup follows, but an error never leaves the box busy.
		a.assist.running = false
		a.assist.stop = nil
		a.assist.pending = ""
		a.assist.status = ""
		a.assist.persistSession(a.assist.folder)
	})
}

// assistLiveEvery is how often a growing reply repaints: a model sends
// tokens far faster than a person reads, and each repaint is a hop to the
// main thread and a frame.
const assistLiveEvery = 33 * time.Millisecond

// assistLive coalesces the growing line of a reply: the latest partial
// waits up to assistLiveEvery and replaces any before it. seq orders every
// update, so a partial landing after the finished line it led to is
// dropped instead of reopening it.
type assistLive struct {
	mu      sync.Mutex
	pending *assistLiveUpdate
	armed   bool
	seq     uint64
	applied uint64
}

type assistLiveUpdate struct {
	seq     uint64
	id      int
	turn    aiTurn
	partial bool
}

func (a *App) noteAssistLive(id int, turn aiTurn, partial bool) {
	lv := &a.assist.live
	lv.mu.Lock()
	lv.seq++
	up := &assistLiveUpdate{seq: lv.seq, id: id, turn: turn, partial: partial}
	if partial && mygo.App.IsReady() {
		lv.pending = up
		if !lv.armed {
			lv.armed = true
			time.AfterFunc(assistLiveEvery, a.flushAssistLive)
		}
		lv.mu.Unlock()
		return
	}
	// A finished line replaces whatever partial of it is waiting; another
	// line's partial goes first, so the transcript keeps its order.
	prev := lv.pending
	lv.pending = nil
	lv.mu.Unlock()
	if prev != nil && prev.id == id {
		prev = nil
	}
	a.postAssist(func() {
		if prev != nil {
			a.applyAssistLive(prev)
		}
		a.applyAssistLive(up)
	})
}

func (a *App) flushAssistLive() {
	lv := &a.assist.live
	lv.mu.Lock()
	up := lv.pending
	lv.pending = nil
	lv.armed = false
	lv.mu.Unlock()
	if up != nil {
		a.postAssist(func() { a.applyAssistLive(up) })
	}
}

// applyAssistLive runs on the main thread.
func (a *App) applyAssistLive(up *assistLiveUpdate) {
	lv := &a.assist.live
	lv.mu.Lock()
	stale := up.seq <= lv.applied
	if !stale {
		lv.applied = up.seq
	}
	lv.mu.Unlock()
	if stale {
		return
	}
	follow := a.assist.scroll.AtLatest()
	a.assist.applyLive(up.id, up.turn, up.partial)
	if up.turn.Role == "tool" && !up.partial {
		a.assist.pending = ""
	}
	if !up.partial {
		a.assist.persistSession(a.assist.folder)
	}
	if follow {
		a.assist.scroll.JumpToLatest()
	}
}

func (a *App) noteAssistStatus(status string) {
	if status == "" {
		return
	}
	a.postAssist(func() { a.assist.status = status })
}

func (a *App) noteAssistPending(name string) {
	a.postAssist(func() {
		a.assist.pending = name
		if name != "" {
			a.assist.status = "正在调用 " + name
		}
	})
}

func (a *App) postAssist(fn func()) {
	run := func() {
		fn()
		if a.win != nil && !a.win.IsDestroyed() {
			a.win.Invalidate()
		}
	}
	if !mygo.App.IsReady() {
		fn()
		return
	}
	mygo.RunOnMain(run)
}

func (a *App) assistantApplyPlan(id string) {
	plan, ok := sunvaleToolState.takePlan(id)
	if !ok {
		a.statusMsg = "方案已失效"
		return
	}
	moved, failed := rename.Apply(plan.Dir, plan.Rows)
	for _, m := range moved {
		aiFx.markRenamed(m.From, m.To)
	}
	if a.assist.applied == nil {
		a.assist.applied = map[string]bool{}
	}
	a.assist.applied[id] = true
	if len(moved) > 0 {
		op := undoOp{Desc: "重命名"}
		for _, m := range moved {
			op.Moved = append(op.Moved, undoMove{From: m.From, To: m.To})
		}
		a.pushUndo(op)
	}
	if len(failed) > 0 {
		a.statusMsg = "部分重命名失败：" + failed[0]
	} else {
		a.statusMsg = "已应用重命名方案"
	}
	a.reload()
}

// reloadAfterFx is the run's closing reload. Rows the assistant moved away
// are still sliding out (aifx.go); the reload waits for the last of them,
// so none of them vanishes mid-slide.
func (a *App) reloadAfterFx() {
	d := aiFx.pendingLeave()
	if d <= 0 {
		a.reload()
		return
	}
	time.AfterFunc(d+30*time.Millisecond, func() {
		a.postAssist(a.reload)
	})
}
