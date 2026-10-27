package main

// assistant_run_stream.go handles streaming publish, model history shaping,
// stop/fail, liveEpoch UI updates, and apply-plan follow-up.
// Split from assistant_run.go for maintainability (LAY2). Stability
// invariants (stop / halted / liveEpoch) are moved as-is; do not change
// behavior here without updating docs/specs/14-ai-stability.md.

import (
	"fmt"
	"strings"
	assistant "sunvale/internal/assistant"
	"sunvale/internal/rename"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/egoist/mygo"
)

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

// assistantRequestStop is the Stop button: cancel the run ctx immediately,
// free the composer (halted clears Running), drop the queue, and discard
// coalesced live paints. liveEpoch is NOT bumped here — the run's defer
// still owns cleanup (running/stop); halted alone drops late paints.
func (a *App) assistantRequestStop() {
	if !a.assist.running && a.assist.stop == nil {
		return
	}
	a.assist.queued = ""
	a.assist.halted = true
	a.assist.pending = ""
	a.assist.status = ""
	lv := &a.assist.live
	lv.mu.Lock()
	lv.pending = nil
	lv.armed = false
	lv.mu.Unlock()
	if n := len(a.assist.turns); n > 0 {
		a.assist.turns[n-1].Stopped = true
	}
	if a.assist.stop != nil {
		a.assist.stop()
		a.assist.stop = nil
	}
	a.assist.persistSession(a.assist.folder)
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

func (a *App) noteAssistLive(epoch uint64, id int, turn aiTurn, partial bool) {
	lv := &a.assist.live
	lv.mu.Lock()
	if a.assist.liveEpoch != epoch || a.assist.halted {
		lv.mu.Unlock()
		return
	}
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
		if a.assist.liveEpoch != epoch || a.assist.halted {
			return
		}
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
	halted := a.assist.halted
	lv.mu.Unlock()
	if up != nil && !halted {
		a.postAssist(func() {
			if a.assist.halted {
				return
			}
			a.applyAssistLive(up)
		})
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

func (a *App) noteAssistStatus(epoch uint64, status string) {
	if status == "" {
		return
	}
	a.postAssist(func() {
		if a.assist.liveEpoch != epoch || a.assist.halted {
			return
		}
		a.assist.status = status
	})
}

func (a *App) noteAssistPending(epoch uint64, name string) {
	a.postAssist(func() {
		if a.assist.liveEpoch != epoch || a.assist.halted {
			return
		}
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
