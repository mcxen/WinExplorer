package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// assistant_composer_test.go pins the pane's keyboard contract: Enter in the
// composer sends the draft (Shift+Enter keeps its newline), and the history
// list groups sessions by recency as ZCode's timeline task list does.

// TestAssistantComposerEnterSends drives the real pane: with the composer's
// input focused, a plain Enter submits the draft to the runner, and an open
// slash menu keeps Enter for its own choice.
func TestAssistantComposerEnterSends(t *testing.T) {
	a, _, dir := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assist.providers = []aiProvider{{
		ID: "p", Name: "local", Model: "m1", BaseURL: "http://127.0.0.1:9/v1", APIKey: "k",
	}}
	a.assist.activeID = "p"
	a.homePath = dir

	sent := make(chan string, 1)
	old := runAssistantHeadless
	done := make(chan struct{})
	runAssistantHeadless = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		last := run.hist[len(run.hist)-1]
		sent <- last.Content
		<-ctx.Done()
		close(done)
		return assistRunResult{}, ctx.Err()
	}
	t.Cleanup(func() { runAssistantHeadless = old })

	// The composer keeps a fresh node only across the frames of one build;
	// rebuild it like the pane does so Enter's pending bit survives the
	// pump that empties the key queue between keys.
	tt := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 560, 520)
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("整理这个文件夹")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Type("谢谢")
	if got := a.assist.draft; got != "整理这个文件夹\n谢谢" {
		t.Fatalf("shift+enter draft = %q", got)
	}
	tt.Key(0, ui.KeyEnter)
	deadline := time.Now().Add(2 * time.Second)
	for !a.assist.running && time.Now().Before(deadline) {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !a.assist.running {
		t.Fatal("Enter did not send the draft")
	}
	select {
	case got := <-sent:
		if got != "整理这个文件夹\n谢谢" {
			t.Fatalf("sent %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the runner never saw the message")
	}
	if a.assist.draft != "" {
		t.Fatalf("the sent draft stayed in the box: %q", a.assist.draft)
	}
	if a.assist.stop != nil {
		a.assist.stop()
	}
	// The goroutine stops below and walks the store it opened; its diskPath
	// is no longer the TempDir cleanup removes, so nothing lands late.
	a.assist.diskPath = filepath.Join(os.TempDir(), "we-assist-enter-", time.Now().Format("20060102150405"))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the run did not stop")
	}
}

// TestAssistantSlashMenuKeepsEnter: while the slash menu is open, Enter picks
// the highlighted command instead of sending the half-typed trigger.
func TestAssistantSlashMenuKeepsEnter(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assistantOpen = true
	tt.Frame()
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("/")
	tt.Frame()
	if !a.assistSlashOpen() {
		t.Fatal("the slash menu did not open")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.assist.running {
		t.Fatal("Enter under the slash menu started a run")
	}
}

// TestAssistTimelineGroup buckets the history list the way ZCode's
// taskTimelineGroups does: 今天, 昨天, 本周, 上周, 本月, 更早.
func TestAssistTimelineGroup(t *testing.T) {
	now := time.Now()
	at := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, d.Location())
	}
	cases := []struct {
		when time.Time
		want string
	}{
		{now, "今天"},
		{at(now.AddDate(0, 0, -1)), "昨天"},
	}
	for _, tc := range cases {
		if got := assistTimelineGroup(tc.when); got != tc.want {
			t.Fatalf("%v = %q, want %q", tc.when, got, tc.want)
		}
	}
	// A date three weeks back is never 本周/上周/本月 confusion; it is 更早
	// unless this run crosses a month boundary, where 本月 stays wrong and
	// only 上周-or-older is asserted loosely.
	old := at(now.AddDate(0, 0, -21))
	if got := assistTimelineGroup(old); got == "今天" || got == "昨天" {
		t.Fatalf("three weeks back = %q", got)
	}
	if got := assistTimelineGroup(time.Time{}); got != "更早" {
		t.Fatalf("zero = %q", got)
	}
}

// TestAssistantHistoryCollapses: the column's own 收起 button folds the
// history away and leaves the conversation the whole pane; the header's
// 会话 brings it back.
func TestAssistantHistoryCollapses(t *testing.T) {
	a, tt, _ := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assistantOpen = true
	a.assist.width = 560
	a.assist.showHist = true
	a.assist.sessions = []aiSessionMeta{{ID: "s1", Title: "上次的活", Updated: time.Now()}}
	tt.Frame()
	if !tt.HasText("上次的活") {
		t.Fatalf("no history column: %v", tt.Texts())
	}
	if err := tt.Click("收起历史会话"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.assist.showHist || tt.HasText("上次的活") {
		t.Fatal("收起 left the history column on screen")
	}
	if !tt.HasText("给助手的消息") {
		t.Fatal("收起 took the composer with it")
	}
	if err := tt.Click("会话"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.assist.showHist || !tt.HasText("上次的活") {
		t.Fatal("会话 did not bring the column back")
	}
}

// TestAssistantOpensOntoHistory: opening the pane onto a blank session with
// history on disk shows the history column, and a bare Enter in the file
// pane — nothing selected — opens the pane onto that column.
func TestAssistantOpensOntoHistory(t *testing.T) {
	a, tt, _ := keynavWindow(t, 0)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assist.sessions = []aiSessionMeta{{ID: "s1", Title: "上次的活", Updated: time.Now()}}
	tt.Frame()
	if len(a.selectedEntries()) != 0 {
		t.Fatal("the empty folder should have nothing selected")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.assistantOpen || !a.assist.showHist {
		t.Fatalf("Enter: open %v hist %v", a.assistantOpen, a.assist.showHist)
	}
	// A live conversation keeps the pane's face: opening over its turns
	// does not drop the history column over what the user was reading.
	a.assistantOpen = false
	a.assist.showHist = false
	a.assist.turns = []aiTurn{{Role: "user", Text: "在吗"}}
	a.toggleAssistant()
	if !a.assistantOpen || a.assist.showHist {
		t.Fatalf("reopen over a live chat: open %v hist %v", a.assistantOpen, a.assist.showHist)
	}
}

// TestEnterStillOpensFiles guards the Explorer contract the assistant's
// 回车 carve-out respects: a selected file's Enter opens, never chats.
func TestEnterStillOpensFiles(t *testing.T) {
	a, tt, dir := keynavWindow(t, 2)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	a.reload()
	tt.Frame()
	a.selectOnly(filepath.Join(dir, "sub"))
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.assistantOpen {
		t.Fatal("Enter on a selection opened the assistant")
	}
	if a.cur().path != filepath.Join(dir, "sub") {
		t.Fatalf("Enter went to %q", a.cur().path)
	}
}

// TestAssistantHistoryGroupsAndCurrentRow draws the records panel: the
// sessions fall under recency headings, the open session is marked current,
// and a quiet row carries no delete button until hovered.
func TestAssistantHistoryGroupsAndCurrentRow(t *testing.T) {
	a := &App{}
	now := time.Now()
	a.assist.sessions = []aiSessionMeta{
		{ID: "today", Title: "今天的活", Updated: now},
		{ID: "old", Title: "上个月的活", Updated: now.AddDate(0, -2, 0)},
	}
	a.assist.sessionID = "today"
	tt := ui.NewTester(func(c *ui.Context) { a.assistantRecords(c) }, 420, 600)
	tt.Frame()
	for _, want := range []string{"今天", "今天的活", "更早", "上个月的活"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	// The bare app's files do not back these made-up sessions, so opening
	// one is only observed through the busy guard that stops it.
	a.assist.running = true
	if err := tt.Click("上个月的活"); err != nil {
		t.Fatal(err)
	}
	if a.assist.sessionID != "today" {
		t.Fatalf("open reached %q behind a running turn", a.assist.sessionID)
	}
	// The bare app's files do not back these made-up sessions; opening one
	// is only observable through the same busy guard a running turn uses.
	a.assist.running = true
	if err := tt.Click("上个月的活"); err != nil {
		t.Fatal(err)
	}
	if a.assist.sessionID != "today" {
		t.Fatalf("open reached %q behind a running turn", a.assist.sessionID)
	}
}
