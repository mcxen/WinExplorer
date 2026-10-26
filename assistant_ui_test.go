package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/egoist/mygo/ui"
)

func TestAssistantExchangesGroupARequest(t *testing.T) {
	turns := []aiTurn{
		{Role: "tool", Text: "earlier"},
		{Role: "user", Text: "整理"},
		{Role: "tool", Text: "list_dir"},
		{Role: "assistant", Text: "好了"},
		{Role: "user", Text: "再看"},
	}
	got := assistantExchanges(turns)
	if len(got) != 3 || got[0].User != -1 || len(got[0].Rest) != 1 || got[0].Rest[0] != 0 {
		t.Fatalf("leading tool: %+v", got)
	}
	if got[1].User != 1 || len(got[1].Rest) != 2 || got[2].User != 4 || len(got[2].Rest) != 0 {
		t.Fatalf("groups: %+v", got)
	}
	if assistSummary(2) != "调用了 2 个工具" || assistFirstLine("list_dir\nbody") != "list_dir" {
		t.Fatal("summary or first line")
	}
}

func TestAssistantTranscriptFoldsTools(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "整理下载"},
		{Role: "tool", Text: "list_dir\nreport.pdf"},
		{Role: "assistant", Text: "已经看过下载"},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 640)
	tt.Frame()
	// The tool call is one collapsed row inside the reply, before the text
	// that followed it, rather than a folded group on top.
	if !tt.HasText("整理下载") || !tt.HasText("已经看过下载") || !tt.HasText("list_dir") {
		t.Fatalf("transcript: %v", tt.Texts())
	}
	if tt.HasText("report.pdf") {
		t.Fatal("tool output was expanded in a finished turn")
	}
}

func TestUseModelSwitchesProvider(t *testing.T) {
	dir := t.TempDir()
	u := &assistantUI{loaded: true, diskPath: filepath.Join(dir, "assistant.json"), width: 420}
	u.providers = []aiProvider{
		{ID: "p", Name: "local", BaseURL: "http://127.0.0.1:3425/v1", Model: "m1", APIKey: "sk-secret"},
		{ID: "q", Name: "other", BaseURL: "https://example.test/v1", Model: "m2", APIKey: "sk-other"},
	}
	u.activeID = "p"
	u.save()
	u.readConfig()
	u.useModel("q", "m2")
	data := string(mustRead(t, u.diskPath))
	if !strings.Contains(data, `"activeID": "q"`) || !strings.Contains(data, "sk-secret") || strings.Contains(data, "sk-other") == false {
		t.Fatalf("saved model switch: %s", data)
	}
	if u.activeID != "q" || u.providers[1].Model != "m2" || u.providers[0].APIKey != "sk-secret" {
		t.Fatalf("memory %+v active %s", u.providers, u.activeID)
	}
}

func TestFetchProviderModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer magpie" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"workbuddy/deepseek-v4.1-flash"},{"id":"workbuddy/deepseek-v4-pro"},{"id":"workbuddy/deepseek-v4.1-flash"}]}`))
	}))
	defer srv.Close()
	got, err := fetchProviderModels(context.Background(), aiProvider{BaseURL: srv.URL + "/v1", APIKey: "magpie"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "workbuddy/deepseek-v4-pro" {
		t.Fatalf("models %v", got)
	}
}

func TestModelPickerSelectsModel(t *testing.T) {
	a := &App{}
	a.assist.providers = []aiProvider{{
		ID: "p", Name: "local", Model: "m1", BaseURL: "http://127.0.0.1:9/v1", APIKey: "k",
	}}
	a.assist.activeID = "p"
	a.assist.modelLists = map[string][]string{"p": {"m1", "m2"}}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantModelPicker(c) }, 480, 520)
	tt.Frame()
	if err := tt.Click("m1"); err != nil || !a.assist.modelPick.Open {
		t.Fatalf("open: %v %v", err, a.assist.modelPick.Open)
	}
	if err := tt.Click("m2"); err != nil {
		t.Fatal(err)
	}
	if a.assist.providers[0].Model != "m2" || a.assist.modelPick.Open {
		t.Fatalf("model %s open %v", a.assist.providers[0].Model, a.assist.modelPick.Open)
	}
}

func TestReadMessageStreamStopsAndKeepsPartial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var seen []string
	var tools []string
	msg, err := readMessageStream(ctx, schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "你", ReasoningContent: "先看目录"},
		{Role: schema.Assistant, Content: "好", ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "list_dir"}}}},
		{Role: schema.Assistant, Content: "呀"},
	}), func(full, reasoning string) {
		seen = append(seen, full)
		if full == "你好" {
			cancel()
		}
		if reasoning != "" && !strings.Contains(reasoningLabel(reasoning), "先看目录") {
			t.Fatalf("reasoning label %q", reasoningLabel(reasoning))
		}
	}, func(name string) { tools = append(tools, name) })
	if !errors.Is(err, errAssistantStopped) {
		t.Fatalf("stop: %v", err)
	}
	if msg == nil || msg.Content != "你好" || len(seen) < 2 || seen[1] != "你好" {
		t.Fatalf("partial %+v %v", msg, seen)
	}
	if len(tools) != 1 || tools[0] != "list_dir" {
		t.Fatalf("tools %v", tools)
	}
	if streamShown("前文\n<<<choices\n- a") != "前文" {
		t.Fatal("choice marker leaked")
	}
}

func TestApplyLiveReplacesTheOpenLine(t *testing.T) {
	var u assistantUI
	u.applyLive(1, aiTurn{Role: "assistant", Text: "你"}, true)
	u.applyLive(1, aiTurn{Role: "assistant", Text: "你好"}, true)
	u.applyLive(1, aiTurn{Role: "assistant", Text: "你好"}, false)
	u.applyLive(2, aiTurn{Role: "tool", Text: "a.txt", Tool: "list_dir"}, false)
	if len(u.turns) != 2 || u.turns[0].Text != "你好" || u.turns[1].Tool != "list_dir" || u.liveOn {
		t.Fatalf("%+v live %v", u.turns, u.liveOn)
	}
}

func TestTranscriptShowsLiveToolCall(t *testing.T) {
	a := &App{}
	a.assist.running = true
	a.assist.pending = "read_file"
	a.assist.status = "正在调用 read_file"
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "整理下载"},
		{Role: "tool", Text: "a.txt\n更多", Tool: "list_dir"},
		{Role: "assistant", Text: "正在看"},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 720)
	tt.Frame()
	for _, want := range []string{"整理下载", "list_dir", "a.txt", "正在看", "read_file", "正在调用"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %s in %v", want, tt.Texts())
		}
	}
}

func TestStoppedTurnKeepsPartialText(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "继续"},
		{Role: "assistant", Text: "半句", Stopped: true},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 640)
	tt.Frame()
	if !tt.HasText("半句") || !tt.HasText("已停止") {
		t.Fatalf("stopped: %v", tt.Texts())
	}
}
