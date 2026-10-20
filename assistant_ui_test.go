package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
		{Role: "tool", Text: "list_dir\n下载"},
		{Role: "assistant", Text: "已经看过下载"},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 640)
	tt.Frame()
	if !tt.HasText("整理下载") || !tt.HasText("已经看过下载") || !tt.HasText("调用了 1 个工具") {
		t.Fatalf("transcript: %v", tt.Texts())
	}
	if tt.HasText("list_dir") {
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
