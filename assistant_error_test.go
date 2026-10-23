package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestAssistantComposerUsableAfterError: a run that fails with a long,
// \r\n-laden error (as a proxy's HTML page on Windows) leaves the composer
// ready: the box is not busy, the error is one collapsible line in the
// conversation, and the next message goes out from the same pane.
func TestAssistantComposerUsableAfterError(t *testing.T) {
	a, _, dir := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assist.providers = []aiProvider{{
		ID: "p", Name: "local", Model: "m1", BaseURL: "http://127.0.0.1:9/v1", APIKey: "k",
	}}
	a.assist.activeID = "p"
	a.homePath = dir

	calls := make(chan string, 4)
	long := "502 Bad Gateway\r\n" + strings.Repeat("<html><body>upstream error ", 400) + "\r\n"
	old := runAssistantHeadless
	runAssistantHeadless = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		calls <- run.hist[len(run.hist)-1].Content
		return assistRunResult{}, errors.New(long)
	}
	t.Cleanup(func() { runAssistantHeadless = old })

	tt := ui.NewTester(func(c *ui.Context) { a.assistantPane(c) }, 560, 640)
	send := func(text string) {
		t.Helper()
		if err := tt.Click("给助手的消息"); err != nil {
			t.Fatalf("composer not clickable: %v", err)
		}
		tt.Type(text)
		tt.Key(0, ui.KeyEnter)
		select {
		case got := <-calls:
			if got != text {
				t.Fatalf("sent %q, want %q", got, text)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q never reached the runner", text)
		}
		deadline := time.Now().Add(2 * time.Second)
		for a.assist.running && time.Now().Before(deadline) {
			tt.Frame()
			time.Sleep(5 * time.Millisecond)
		}
		tt.Frame()
	}
	send("第一条")
	if a.assist.running || a.assist.stop != nil || a.assist.status != "" {
		t.Fatalf("busy after error: running=%v stop=%v status=%q", a.assist.running, a.assist.stop != nil, a.assist.status)
	}
	last := a.assist.turns[len(a.assist.turns)-1]
	if !last.Error || strings.Contains(last.Text, "\r") || len([]rune(last.Text)) > assistErrorMax+1 {
		t.Fatalf("error turn not normalized: error=%v len=%d", last.Error, len([]rune(last.Text)))
	}
	send("第二条")
	a.assist.diskPath = filepath.Join(os.TempDir(), "we-assist-err-", time.Now().Format("20060102150405"))
}

func TestAssistErrorText(t *testing.T) {
	if got := assistErrorText(errors.New(" a\r\nb\r ")); got != "a\nb" {
		t.Fatalf("got %q", got)
	}
	if got := assistErrorText(errors.New("  ")); got != "请求失败" {
		t.Fatalf("got %q", got)
	}
}
