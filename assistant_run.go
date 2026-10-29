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
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"sunvale/internal/applog"
	assistant "sunvale/internal/assistant"
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
	a.assist.liveEpoch++
	a.assist.turns = append(a.assist.turns, aiTurn{Role: "user", Text: text, Files: files, At: time.Now()})
	// Sending is a request to see the answer: the transcript follows the new
	// turn even when it had been scrolled up or paused by opening a fold.
	a.assist.scroll.JumpToLatest()
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
	epoch := a.assist.liveEpoch
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
		stopped := ctx.Err() != nil
		a.postAssist(func() {
			mine := a.assist.liveEpoch == epoch
			// A stopped run's last partial lands only if this epoch still owns
			// the pane; a newer Stop/submit must not be painted over.
			if mine && tail != nil {
				if stopped {
					tail.turn.Stopped = true
					tail.partial = false
				}
				a.applyAssistLive(tail)
			}
			if mine {
				a.assist.passwords = append(a.assist.passwords, saved...)
				if len(a.assist.passwords) > assistMaxArchivePasswords {
					a.assist.passwords = a.assist.passwords[len(a.assist.passwords)-assistMaxArchivePasswords:]
				}
				a.assist.save()
				a.assist.applyMemory(memAdd, memDrop)
				a.assist.persistSession(run.root)
			}
			for _, op := range undos {
				a.pushUndo(op)
			}
			if mine {
				a.assist.running = false
				a.assist.stop = nil
				a.assist.pending = ""
				a.assist.resetLive()
				a.assist.status = ""
				if stopped {
					a.assist.halted = true
				}
				a.assist.flowCool = time.Now().Add(assistFlowCool)
				a.requestComposerFocus()
			}
			aiFx.endRun()
			if mine {
				a.reloadAfterFx()
				for _, id := range plans {
					a.assistantApplyPlan(id)
				}
				if nav != "" {
					a.navigate(nav)
				}
				// Queued Send after Stop waits here. Stop itself clears the
				// queue so an accidental double-Stop does not resurrect text.
				if a.assist.queued != "" {
					a.assist.draft = a.assist.queued
					a.assist.queued = ""
					a.assistantSubmit()
				}
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
		a.noteAssistLive(epoch, id, turn, partial)
	}
	run.onStatus = func(status string) {
		a.noteAssistStatus(epoch, status)
	}
	run.onPending = func(name string) {
		a.noteAssistPending(epoch, name)
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
	mode := "逐步确认"
	iters := 12
	if run.autopilot {
		mode = "全托管"
		iters = 24
	}
	// budget is the iteration guard: it injects the wrap-up instruction on
	// the last model call and lets the loop below finish the turn with an
	// answer instead of ErrExceedMaxIterations when the model still calls
	// tools then.
	budget := newBudgetModel(vision, iters)
	sess, err := sunvaleSession(ctx)
	if err != nil {
		return assistRunResult{}, err
	}
	tools, err := sunvaleRunTools(ctx, sess)
	if err != nil {
		return assistRunResult{}, err
	}
	memories := run.memories
	if assistant.SkillDisabled(run.prefs, assistant.SkillMemory) {
		memories = nil
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "sunvale",
		Description: "晴谷文件管理器助手",
		Instruction: assistant.EffectiveInstruction(run.prefs.ExtraInstruction),
		Model:       budget,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
				// File tools share one bind; parallel writes would race.
				ExecuteSequentially: true,
				// A hallucinated name must come back as text the model can
				// correct, not as an error that kills the whole turn.
				UnknownToolsHandler: unknownAssistTool,
			},
		},
		MaxIterations: iters,
		GenModelInput: assistant.GenModelInput,
		// Transient network / 5xx only. 429 stays with runWithRateFallback
		// so the two retry layers do not amplify each other.
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries:  2,
			IsRetryAble: func(_ context.Context, err error) bool { return assistant.TransientModelError(err) },
		},
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
			if errors.Is(ev.Err, adk.ErrExceedMaxIterations) {
				// The model kept calling tools into the last budgeted call.
				// The turn already has real work in it; one direct model call
				// with the wrap-up note turns that work into an answer rather
				// than an error the user cannot act on.
				if finErr := budgetFinishTurn(ctx, run, chatModel, nextID, &result); finErr == nil {
					nextID++
					return result, nil
				} else {
					slog.Warn("ai budget finish failed", "run", run.runID, "err", finErr)
				}
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

// unknownAssistTool answers a hallucinated tool name with text the model can
// act on. Returning an error here would abort the whole ReAct loop.
func unknownAssistTool(_ context.Context, name, _ string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "?"
	}
	return fmt.Sprintf("没有名为 %q 的工具。请改用系统提示里列出的工具名重试，不要编造工具。", name), nil
}

// budgetFinishTurn salvages a turn the iteration budget cut off. The
// transcript turns already published carry the model's work (assistant text,
// tool calls and results); replaying them with the wrap-up note and no tools
// asks for the answer those results imply, in one call that cannot loop.
func budgetFinishTurn(ctx context.Context, run assistRun, chatModel model.BaseChatModel, id int, result *assistRunResult) error {
	msgs := []*schema.Message{schema.UserMessage(budgetFinishAsk(result))}
	out, err := chatModel.Generate(ctx, msgs)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(streamShown(out.Content))
	if text == "" {
		return errors.New("收尾生成没有返回文字")
	}
	publishAssist(run, id, aiTurn{Role: "assistant", Text: text}, false, result)
	return nil
}

// budgetFinishAsk is the single user message of the salvage call: a compact
// transcript of the cut-off turn plus the demand for an answer now.
// budgetFinishAskMax is the rune ceiling for the salvage prompt. The wrap-up
// call has no tools and must stay inside ordinary context windows.
const budgetFinishAskMax = 12000

func budgetFinishAsk(result *assistRunResult) string {
	var b strings.Builder
	b.WriteString("刚才的一轮对话在工具预算用尽时被截断，以下是已发生的轮次：\n\n")
	for _, ev := range result.Events {
		switch ev.Role {
		case "tool":
			text := strings.TrimSpace(ev.Text)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "[工具 %s] %s\n\n", ev.Tool, assistant.LimitRunes(assistant.ToolText(text), 2000))
		case "assistant":
			if t := strings.TrimSpace(ev.Text); t != "" {
				fmt.Fprintf(&b, "[助手] %s\n\n", assistant.LimitRunes(t, 2000))
			}
		}
		if len([]rune(b.String())) > budgetFinishAskMax {
			break
		}
	}
	body := assistant.LimitRunes(b.String(), budgetFinishAskMax)
	return body + "请只根据上面的内容直接回答用户最初的问题：给出结论与必要的路径、数字；未完成的部分明确说明做到了哪一步、下一步该做什么。不要调用工具，不要再描述计划。"
}
