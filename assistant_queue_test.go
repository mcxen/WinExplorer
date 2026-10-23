package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// assistant_queue_test.go covers the ZCode-style interactions around the
// composer: a folder handed to the assistant reads as a listing, a message
// sent over a running turn queues and goes out by itself, and the empty
// transcript's starter cards fill the box.

// TestReadAnyFilePeeksFolders: a folder dropped on the assistant answers
// with what is inside, not a pointer to another tool.
func TestReadAnyFilePeeksFolders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := readAnyFile(dir, nil)
	if !strings.Contains(got, "这是文件夹") || !strings.Contains(got, "note.txt") || !strings.Contains(got, "sub/") {
		t.Fatalf("folder read: %s", got)
	}
}

// queueWindow boots a window whose assistant runs a gate-held stub, so the
// test decides when the first turn ends.
func queueWindow(t *testing.T, gate chan struct{}, runs chan string) (*App, *ui.Tester) {
	t.Helper()
	a, tt, _ := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assist.providers = []aiProvider{{
		ID: "p", Name: "local", Model: "m1", BaseURL: "http://127.0.0.1:9/v1", APIKey: "k",
	}}
	a.assist.activeID = "p"
	old := runAssistantHeadless
	runAssistantHeadless = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		runs <- run.hist[len(run.hist)-1].Content
		select {
		case <-ctx.Done():
		case <-gate:
		}
		return assistRunResult{}, ctx.Err()
	}
	t.Cleanup(func() {
		runAssistantHeadless = old
		// The stubbed runs' sessions would land in the TempDir after its
		// cleanup; send any late write elsewhere.
		a.assist.diskPath = filepath.Join(os.TempDir(), "we-queue-late.json")
	})
	return a, tt
}

// TestQueuedMessageSendsItself: Enter over a running turn queues the
// message, the chip replaces it in the box, and the run's end sends it.
func TestQueuedMessageSendsItself(t *testing.T) {
	gate := make(chan struct{})
	runs := make(chan string, 2)
	a, tt := queueWindow(t, gate, runs)
	a.assistantOpen = true
	tt.Frame()

	a.assist.draft = "第一条"
	a.assistantSubmit()
	select {
	case got := <-runs:
		if got != "第一条" {
			t.Fatalf("first run got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first message never ran")
	}
	if !a.assist.running {
		t.Fatal("the gate-held run should still be running")
	}

	// The composer keeps a fresh node only across one build; drive it
	// through its own tester, as the window's pump settles Enter into a
	// newline before the pane rebuilds its tree.
	ct := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 560, 520)
	if err := ct.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	ct.Type("第二条")
	ct.Key(0, ui.KeyEnter)
	if a.assist.queued != "第二条" || a.assist.draft != "" {
		t.Fatalf("queued %q draft %q", a.assist.queued, a.assist.draft)
	}
	ct.Frame()
	if !ct.HasText("排队中：第二条") {
		t.Fatalf("no queue chip: %v", ct.Texts())
	}

	close(gate)
	select {
	case got := <-runs:
		if got != "第二条" {
			t.Fatalf("the run's follow-up got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the queued message did not go out at the run's end")
	}
	deadline := time.Now().Add(2 * time.Second)
	for a.assist.queued != "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if a.assist.queued != "" {
		t.Fatalf("the queue kept %q", a.assist.queued)
	}
	if a.assist.stop != nil {
		a.assist.stop()
	}
}

// TestQueueChipCancels: clicking the queue chip takes the message back
// into the box instead of sending it.
func TestQueueChipCancels(t *testing.T) {
	a := &App{}
	a.assist.running = true
	a.assist.queued = "等我一下"
	tt := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 560, 520)
	tt.Frame()
	if !tt.HasText("排队中：等我一下") {
		t.Fatalf("no queue chip: %v", tt.Texts())
	}
	if err := tt.Click("取消排队"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.assist.queued != "" || a.assist.draft != "等我一下" {
		t.Fatalf("queued %q draft %q", a.assist.queued, a.assist.draft)
	}
}

// TestStarterCardsFillTheBox: the empty transcript's ZCode-style starter
// cards put their request in the box rather than sending it.
func TestStarterCardsFillTheBox(t *testing.T) {
	a := &App{}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 720)
	tt.Frame()
	for _, s := range assistStarters {
		if !tt.HasText(s.title) || !tt.HasText("“"+s.fill+"”") {
			t.Fatalf("starter %q missing: %v", s.title, tt.Texts())
		}
	}
	if err := tt.Click(assistStarters[0].title); err != nil {
		t.Fatal(err)
	}
	if a.assist.draft != assistStarters[0].fill {
		t.Fatalf("draft = %q", a.assist.draft)
	}
	if a.assist.running {
		t.Fatal("a starter sent the message instead of filling the box")
	}
}
