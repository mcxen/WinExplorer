package main

import (
	"context"
	"fmt"
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
	hist      []*schema.Message
	memories  []aiMemory
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
// replace it with a stub; production uses runFileAssistant.
type assistantRunner func(context.Context, assistRun) (assistRunResult, error)

var runAssistantHeadless assistantRunner = runFileAssistant

func (a *App) assistantSubmit() {
	a.assist.ensure()
	a.assist.refreshConfig()
	text := strings.TrimSpace(a.assist.draft)
	files := append([]aiAttach(nil), a.assist.files...)
	if (text == "" && len(files) == 0) || a.assist.running {
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
		a.statusMsg = "请先在助手里添加供应商"
		return
	}
	a.assist.draft = ""
	a.assist.files = nil
	a.assist.folder = root
	a.assist.turns = append(a.assist.turns, aiTurn{Role: "user", Text: text, Files: files})
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
	var selected []string
	for _, e := range a.selectedEntries() {
		if e.Path != "" {
			selected = append(selected, e.Path)
		}
	}
	run := assistRun{
		prov: prov, root: root, hidden: a.store.settings().ShowHidden,
		book: append([]archivePassword(nil), a.assist.passwords...), selected: selected,
		files: paths, autopilot: a.assist.autopilot, hist: a.assistHistory(),
		memories: append([]aiMemory(nil), a.assist.memories...),
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
		a.postAssist(func() {
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
			if a.assist.status == "正在思考…" || a.assist.status == "全托管进行中…" {
				a.assist.status = ""
			}
			a.reload()
			for _, id := range plans {
				a.assistantApplyPlan(id)
			}
			if nav != "" {
				a.navigate(nav)
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
	result, err := runFileAssistant(ctx, run)
	if err != nil {
		failed = true
		a.failAssist(err)
		return
	}
	asked = result.Asked
	for _, t := range result.Events {
		turn := t
		a.postAssist(func() {
			a.assist.turns = append(a.assist.turns, turn)
			a.assist.scroll.JumpToLatest()
		})
	}
}

// runFileAssistant is the model loop without a window. Callers own
// assistantRunMu so the global tool bind cannot be changed underneath a
// turn. The returned events are ordinary transcript turns, which lets the
// local pane and A2A render the same tool output.
func runFileAssistant(ctx context.Context, run assistRun) (assistRunResult, error) {
	sunvaleToolState.bind(run.root, run.hidden, run.book, run.selected, run.autopilot)
	sunvaleToolState.bindMemory(run.memories)
	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:  run.prov.APIKey,
		BaseURL: run.prov.BaseURL,
		Model:   run.prov.Model,
		Timeout: 90 * time.Second,
	})
	if err != nil {
		return assistRunResult{}, err
	}
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
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "sunvale",
		Description: "晴谷文件管理器助手",
		Instruction: assistantInstruction,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: iters,
	})
	if err != nil {
		return assistRunResult{}, err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	iter := runner.Run(ctx, assistModelHistory(run), adk.WithSessionValues(map[string]any{
		"folder":    sessionPlain(run.root),
		"mode":      mode,
		"selection": sessionPlain(selectionText(run.selected)),
		"memory":    memoryPrompt(run.memories),
	}))
	var result assistRunResult
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			return result, ev.Err
		}
		if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.IsStreaming {
			msg, err := ev.Output.MessageOutput.GetMessage()
			if err != nil {
				return result, err
			}
			evCopy := *ev
			output := *ev.Output
			variant := *ev.Output.MessageOutput
			variant.Message = msg
			variant.MessageStream = nil
			variant.IsStreaming = false
			output.MessageOutput = &variant
			evCopy.Output = &output
			ev = &evCopy
		}
		turn, has, choice := assistEventTurn(ev)
		if !has {
			continue
		}
		result.Events = append(result.Events, turn)
		result.Text = turn.Text
		result.Plan = turn.Plan
		if choice != nil {
			result.Choice = choice
			result.Asked = true
			break
		}
	}
	return result, nil
}

// assistModelHistory appends the named-file excerpts to the last user
// message. The caller's history is left alone: its messages are shared with
// the visible transcript, and the excerpts must not be written to disk.
func assistModelHistory(run assistRun) []*schema.Message {
	hist := append([]*schema.Message(nil), run.hist...)
	if len(run.files) == 0 || len(hist) == 0 {
		return hist
	}
	last := hist[len(hist)-1]
	if last == nil || last.Role != schema.User {
		return hist
	}
	copyLast := *last
	copyLast.Content += "\n\n" + specifiedFileNote(run.files)
	hist[len(hist)-1] = &copyLast
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
	if mv.Message == nil || mv.Message.Content == "" {
		return aiTurn{}, false, nil
	}
	text := mv.Message.Content
	display, choice := parseChoices(text)
	if choice != nil && strings.TrimSpace(display) == "" {
		display = choice.Prompt
	}
	if strings.TrimSpace(display) == "" && choice == nil {
		return aiTurn{}, false, nil
	}
	role := "assistant"
	plan := ""
	if mv.Role == schema.Tool {
		role = "tool"
		plan = planIDFrom(display)
	}
	return aiTurn{Role: role, Text: display, Plan: plan, Choice: choice}, true, choice
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

// sessionPlain keeps path text out of the f-string braces. The instruction
// is the template; a folder named with a brace would otherwise be a hole.
func sessionPlain(s string) string {
	s = strings.ReplaceAll(s, "{", "(")
	s = strings.ReplaceAll(s, "}", ")")
	return s
}

// assistHistory is the tail of this session. Older turns stay on screen and
// on disk, but the model only sees the recent part so one long chat cannot
// crowd out the current folder.
func (a *App) assistHistory() []*schema.Message {
	return modelHistory(a.assist.turns)
}

func modelHistory(turns []aiTurn) []*schema.Message {
	var msgs []*schema.Message
	for _, t := range turns {
		switch t.Role {
		case "user":
			body := t.Text
			if t.Model != "" {
				body = t.Model
			}
			msgs = append(msgs, schema.UserMessage(limitRunes(body, assistHistRunes)))
		case "assistant":
			if t.Text != "" {
				msgs = append(msgs, schema.AssistantMessage(limitRunes(t.Text, assistHistRunes), nil))
			}
		}
	}
	if len(msgs) > assistHistMsgs {
		msgs = msgs[len(msgs)-assistHistMsgs:]
	}
	return msgs
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
		a.assist.turns = append(a.assist.turns, aiTurn{Role: "assistant", Text: err.Error()})
		a.assist.status = ""
		a.assist.persistSession(a.assist.folder)
	})
}

func (a *App) postAssist(fn func()) {
	if !mygo.App.IsReady() {
		fn()
		return
	}
	mygo.RunOnMain(fn)
}

func (a *App) assistantApplyPlan(id string) {
	plan, ok := sunvaleToolState.takePlan(id)
	if !ok {
		a.statusMsg = "方案已失效"
		return
	}
	moved, failed := rename.Apply(plan.Dir, plan.Rows)
	if a.assist.applied == nil {
		a.assist.applied = map[string]bool{}
	}
	a.assist.applied[id] = true
	if len(moved) > 0 {
		op := undoOp{desc: "重命名"}
		for _, m := range moved {
			op.moved = append(op.moved, undoMove{From: m.From, To: m.To})
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
