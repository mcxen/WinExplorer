package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 续聊旧会话：新消息追加到该会话，旧消息保留；回复结束后键盘仍在输入框，
// 接着打字 + Enter 发送给助手，而不是落到文件列表打开选中的文件。
func TestAssistantContinueKeepsHistoryAndComposerFocus(t *testing.T) {
	a, tt, dir := keynavWindow(t, 1)
	a.assist.loaded = true
	a.assist.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	a.assist.providers = []aiProvider{{ID: "p", Name: "local", Model: "m", BaseURL: "http://127.0.0.1:9/v1", APIKey: "k"}}
	a.assist.activeID = "p"
	// The old session: a stopped half reply from this morning.
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "你好", At: time.Now().Add(-2 * time.Hour)},
		{Role: "assistant", Text: "半句", Stopped: true, At: time.Now().Add(-2 * time.Hour)},
	}
	a.assist.persistSession(dir)
	oldID := a.assist.sessionID

	var runs atomic.Int32
	old := runAssistantHeadless
	runAssistantHeadless = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		runs.Add(1)
		return assistRunResult{Text: "你好！"}, nil
	}
	t.Cleanup(func() { runAssistantHeadless = old })

	a.selectOnly(filepath.Join(dir, "f000.txt"))
	a.assistantOpen = true
	a.requestComposerFocus()
	tt.Frame()
	tt.Type("你好")
	tt.Key(0, ui.KeyEnter)
	waitAssistIdle(t, a, tt)
	if runs.Load() != 1 {
		t.Fatalf("runs = %d", runs.Load())
	}
	// No pointer move, no extra frame: the next key goes to the composer.
	tt.Type("再见")
	tt.Key(0, ui.KeyEnter)
	waitAssistIdle(t, a, tt)
	if runs.Load() != 2 {
		t.Fatalf("Enter after the reply did not reach the composer (runs = %d, draft %q)", runs.Load(), a.assist.draft)
	}

	if a.assist.sessionID != oldID {
		t.Fatalf("continuing made a new session %q, want %q", a.assist.sessionID, oldID)
	}
	data, err := os.ReadFile(a.assist.sessionPath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	var file aiSessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Turns) < 4 || file.Turns[0].Text != "你好" || file.Turns[1].Text != "半句" {
		t.Fatalf("old turns lost: %+v", file.Turns)
	}

	// A new chat with the same title is a session of its own.
	a.assist.clearSession()
	a.assist.turns = []aiTurn{{Role: "user", Text: "你好", At: time.Now()}}
	a.assist.persistSession(dir)
	idx, ok := readIndexFile(a.assist.indexPath())
	if !ok || len(idx.Sessions) != 2 {
		t.Fatalf("sessions titled alike were merged: %+v", idx.Sessions)
	}
}

func waitAssistIdle(t *testing.T, a *App, tt *ui.Tester) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	tt.Frame()
	for a.assist.running && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		tt.Frame()
	}
	if a.assist.running {
		t.Fatal("the run did not finish")
	}
}
