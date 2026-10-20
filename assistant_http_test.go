package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// assistant_http_test.go drives A2A and HTTP MCP over real HTTP with a
// stubbed model runner. It never calls a provider. Hidden provider keys are
// asserted absent from every response.

func newAssistantHTTPTestEnv(t *testing.T) *remoteTestEnv {
	t.Helper()
	remoteResetForTest()
	resetAssistantHTTPForTest()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	cfg, err := os.UserConfigDir()
	if err != nil {
		t.Skip("UserConfigDir does not follow HOME here")
	}
	if err := os.MkdirAll(filepath.Join(cfg, stateDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	remoteResetForTest()
	a := &App{}
	rc := *a.remoteConf()
	rc.Username = "admin"
	rc.PassHash = remoteHashPassword(remoteTestPass)
	a.remoteSaveConf(&rc)
	e := &remoteTestEnv{t: t, home: home, a: a, conf: &rc}
	e.srv = httptest.NewServer(a.remoteMux())
	t.Cleanup(e.srv.Close)
	writeTestAssistantProvider(t, "sk-test-secret", true)
	return e
}

func writeTestAssistantProvider(t *testing.T, key string, active bool) {
	t.Helper()
	path := assistantConfigPath()
	disk := assistantDisk{
		Providers: []aiProvider{{
			ID: "provider-1", Name: "local", BaseURL: "http://127.0.0.1:3425/v1",
			Model: "workbuddy/deepseek-v4.1-flash", APIKey: key,
		}},
		Prefs: assistPrefs{Width: 420, Autopilot: true},
	}
	if active {
		disk.ActiveID = "provider-1"
	}
	if err := writeAssistantDisk(path, disk); err != nil {
		t.Fatal(err)
	}
}

func (e *remoteTestEnv) a2a(method string, id any, params any, auth string) (int, map[string]any, string) {
	e.t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+"/a2a", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	var out map[string]any
	if res.StatusCode == http.StatusUnauthorized {
		return res.StatusCode, nil, string(data)
	}
	if err := json.Unmarshal(data, &out); err != nil {
		e.t.Fatalf("A2A %s: %q: %v", method, data, err)
	}
	return res.StatusCode, out, string(data)
}

func TestA2AAuthAndCard(t *testing.T) {
	e := newAssistantHTTPTestEnv(t)

	res, body := e.do(http.MethodGet, "/.well-known/agent-card.json", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"protocolVersion"`) ||
		!strings.Contains(body, `"streaming":false`) {
		t.Fatalf("card: %d %s", res.StatusCode, body)
	}
	if strings.Contains(body, "sk-test-secret") {
		t.Fatal("card leaked an API key")
	}
	code, _, raw := e.a2a("tasks/get", 1, map[string]any{"id": "none"}, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth A2A status %d", code)
	}
	if strings.Contains(raw, "sk-test-secret") {
		t.Fatal("unauth response leaked a key")
	}
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	mres, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	mres.Body.Close()
	if mres.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth MCP status %d", mres.StatusCode)
	}
}

func TestA2AMessageGetCancelAndFolderMapping(t *testing.T) {
	e := newAssistantHTTPTestEnv(t)
	e.mkdir("work")
	e.writeFile("work/a.txt", "hello")
	var (
		got    assistRun
		mu     sync.Mutex
		finish = make(chan struct{})
	)
	old := runAssistantHeadless
	runAssistantHeadless = func(ctx context.Context, run assistRun) (assistRunResult, error) {
		mu.Lock()
		got = run
		mu.Unlock()
		select {
		case <-ctx.Done():
			return assistRunResult{}, ctx.Err()
		case <-finish:
		}
		return assistRunResult{Text: "看到 hello", Events: []aiTurn{{Role: "tool", Text: "read_file"}}}, nil
	}
	t.Cleanup(func() { runAssistantHeadless = old })

	status, out, raw := e.a2a("message/send", 7, map[string]any{
		"message": map[string]any{
			"role": "user", "messageId": "m1",
			"parts": []any{map[string]any{"kind": "text", "text": "读 a.txt"}},
		},
		"metadata": map[string]any{"folder": "/work", "files": []string{"a.txt"}},
	}, "Bearer "+e.conf.Token)
	if status != 200 || out["error"] != nil {
		t.Fatalf("send: %d %s", status, raw)
	}
	res := out["result"].(map[string]any)
	id := res["id"].(string)
	if id == "" || res["status"].(map[string]any)["state"] != "working" {
		t.Fatalf("task: %s", raw)
	}
	_, out, _ = e.a2a("tasks/cancel", 10, map[string]any{"id": id}, e.conf.Token)
	if out["result"].(map[string]any)["status"].(map[string]any)["state"] != "canceled" {
		t.Fatalf("working task did not cancel: %s", fmtJSON(out))
	}
	time.Sleep(10 * time.Millisecond)
	_, out, _ = e.a2a("tasks/get", 11, map[string]any{"id": id}, e.conf.Token)
	if got := out["result"].(map[string]any)["status"].(map[string]any)["state"]; got != "canceled" {
		t.Fatalf("canceled task became %v", got)
	}

	// A second task is allowed to finish, proving cancel did not stop the
	// shared runner or leave assistantRunMu locked.
	close(finish)
	status, out, raw = e.a2a("message/send", 12, map[string]any{
		"message": map[string]any{
			"role": "user", "messageId": "m3",
			"parts": []any{map[string]any{"kind": "text", "text": "再读 a.txt"}},
		},
		"metadata": map[string]any{"folder": "/work", "files": []string{"a.txt"}},
	}, "Bearer "+e.conf.Token)
	if status != 200 || out["error"] != nil {
		t.Fatalf("second send: %d %s", status, raw)
	}
	res = out["result"].(map[string]any)
	id = res["id"].(string)
	deadline := time.Now().Add(time.Second)
	for {
		_, out, _ = e.a2a("tasks/get", 13, map[string]any{"id": id}, e.conf.Token)
		res = out["result"].(map[string]any)
		if res["status"].(map[string]any)["state"] == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task did not complete: %+v", res)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(strings.Join([]string{raw, fmtJSON(res)}, " "), "看到 hello") {
		t.Fatalf("answer missing: %s", fmtJSON(res))
	}
	mu.Lock()
	if got.root != filepath.Join(e.home, "work") || len(got.files) != 1 || got.files[0] != "a.txt" {
		t.Fatalf("runner input %+v", got)
	}
	mu.Unlock()
	if strings.Contains(fmtJSON(res), "sk-test-secret") {
		t.Fatal("task leaked a key")
	}

	_, out, _ = e.a2a("message/send", 9, map[string]any{
		"message": map[string]any{"role": "user", "messageId": "m2",
			"parts": []any{map[string]any{"kind": "text", "text": "x"}}},
		"metadata": map[string]any{"folder": "/../outside"},
	}, e.conf.Token)
	if out["error"] == nil {
		t.Fatalf("path escape accepted: %s", fmtJSON(out))
	}
}

func TestA2AFileURIAndNamedFiles(t *testing.T) {
	text, files, err := a2aTextAndFiles([]a2aPart{
		{Kind: "text", Text: "看看"},
		{Kind: "file", File: &a2aFilePart{URI: "file:///tmp/note.txt"}},
		{Kind: "data", Data: json.RawMessage(`{"paths":["/tmp/other.txt"]}`)},
	})
	if err != nil || text != "看看" || len(files) != 2 ||
		files[0] != filepath.Clean("/tmp/note.txt") || files[1] != "/tmp/other.txt" {
		t.Fatalf("text %q files %v err %v", text, files, err)
	}
	if _, _, err := a2aTextAndFiles([]a2aPart{{Kind: "file", File: &a2aFilePart{URI: "https://example.test/a"}}}); err == nil {
		t.Fatal("accepted a remote file URI")
	}
}

func TestMCPControlToolsAndKeyPreservation(t *testing.T) {
	e := newAssistantHTTPTestEnv(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             e.srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: e.conf.Token, next: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}
	sess, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"assistant_config_list", "assistant_config_save", "assistant_config_activate", "assistant_config_delete", "assistant_status", "a2a_info", "assistant_run"} {
		if !names[want] {
			t.Errorf("missing MCP tool %s", want)
		}
	}
	list, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "assistant_config_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := firstMCPText(t, list)
	if strings.Contains(text, "sk-test-secret") || !strings.Contains(text, `"keyConfigured": true`) {
		t.Fatalf("config list: %s", text)
	}
	saved, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "assistant_config_save",
		Arguments: map[string]any{
			"id": "provider-1", "name": "local-renamed",
			"baseURL": "http://127.0.0.1:3425/v1", "model": "workbuddy/deepseek-v4-pro",
		},
	})
	if err != nil || saved.IsError {
		t.Fatalf("save: %+v %v", saved, err)
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Providers) != 1 || disk.Providers[0].APIKey != "sk-test-secret" ||
		disk.Providers[0].Model != "workbuddy/deepseek-v4-pro" {
		t.Fatalf("provider after save: %+v", disk.Providers)
	}
	old := runAssistantHeadless
	runAssistantHeadless = func(_ context.Context, run assistRun) (assistRunResult, error) {
		if len(run.files) != 1 || run.files[0] != "a.txt" {
			t.Errorf("files = %v", run.files)
		}
		return assistRunResult{Text: "ok"}, nil
	}
	t.Cleanup(func() { runAssistantHeadless = old })
	run, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "assistant_run",
		Arguments: map[string]any{
			"prompt": "读文件", "folder": "/", "files": []string{"a.txt"},
		},
	})
	if err != nil || run.IsError {
		t.Fatalf("run: %+v %v", run, err)
	}
	if out := firstMCPText(t, run); !strings.Contains(out, `"text": "ok"`) {
		t.Fatalf("run output: %s", out)
	}
}

func TestPaneSaveMergesHeadlessProviderChange(t *testing.T) {
	_ = newAssistantHTTPTestEnv(t)
	u := &assistantUI{loaded: true, diskPath: assistantConfigPath()}
	u.readConfig()
	if len(u.providers) != 1 || u.providers[0].Name != "local" {
		t.Fatalf("initial providers: %+v", u.providers)
	}

	if _, err := saveAssistantProvider(assistantConfigInput{
		ID: "provider-1", Name: "headless", BaseURL: "http://127.0.0.1:3425/v1",
		Model: "workbuddy/deepseek-v4-pro",
	}); err != nil {
		t.Fatal(err)
	}
	u.width = 460
	u.save()

	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Providers) != 1 || disk.Providers[0].Name != "headless" ||
		disk.Providers[0].APIKey != "sk-test-secret" || disk.Prefs.Width != 460 {
		t.Fatalf("window save overwrote headless config: %+v", disk)
	}
	if len(u.providers) != 1 || u.providers[0].Name != "headless" {
		t.Fatalf("open pane did not refresh: %+v", u.providers)
	}
}

func TestPaneMutationMergesHeadlessProviderChange(t *testing.T) {
	_ = newAssistantHTTPTestEnv(t)
	u := &assistantUI{loaded: true, diskPath: assistantConfigPath()}
	u.readConfig()
	if _, err := saveAssistantProvider(assistantConfigInput{
		ID: "provider-1", Name: "headless", BaseURL: "http://127.0.0.1:3425/v1",
		Model: "workbuddy/deepseek-v4-pro",
	}); err != nil {
		t.Fatal(err)
	}
	u.pName, u.pURL, u.pModel, u.pKey = "second", "https://example.test/v1", "model-2", "sk-second"
	u.addProvider()

	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Providers) != 2 || disk.Providers[0].Name != "headless" ||
		disk.Providers[1].Name != "second" || disk.Providers[0].APIKey != "sk-test-secret" {
		t.Fatalf("window mutation lost headless provider: %+v", disk.Providers)
	}
}

func TestPaneSaveKeepsLocalAndHeadlessPasswordChanges(t *testing.T) {
	_ = newAssistantHTTPTestEnv(t)
	u := &assistantUI{loaded: true, diskPath: assistantConfigPath()}
	u.readConfig()
	u.pwName, u.pwSecret = "local", "local-secret"
	u.addArchivePassword()

	a := &App{}
	a.assistantApplyHeadlessSideEffects(
		[]archivePassword{{ID: "remote", Label: "remote", Secret: "remote-secret"}},
		nil, nil,
	)

	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.ArchivePasswords) != 2 ||
		disk.ArchivePasswords[0].Secret != "local-secret" ||
		disk.ArchivePasswords[1].Secret != "remote-secret" {
		t.Fatalf("password changes lost: %+v", disk.ArchivePasswords)
	}
}

func TestTwoPanesConsumeSameHeadlessChange(t *testing.T) {
	_ = newAssistantHTTPTestEnv(t)
	first := &assistantUI{loaded: true, diskPath: assistantConfigPath()}
	second := &assistantUI{loaded: true, diskPath: assistantConfigPath()}
	first.readConfig()
	second.readConfig()
	if _, err := saveAssistantProvider(assistantConfigInput{
		ID: "provider-1", Name: "headless", BaseURL: "http://127.0.0.1:3425/v1",
		Model: "workbuddy/deepseek-v4-pro",
	}); err != nil {
		t.Fatal(err)
	}
	for i, u := range []*assistantUI{first, second} {
		u.width = float32(430 + i*10)
		u.save()
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Providers) != 1 || disk.Providers[0].Name != "headless" ||
		disk.Providers[0].APIKey != "sk-test-secret" {
		t.Fatalf("second pane lost headless change: %+v", disk)
	}
	if disk.Prefs.Width != 440 {
		t.Fatalf("last pane's width lost: %+v", disk.Prefs)
	}
}

type bearerTransport struct {
	token string
	next  http.RoundTripper
}

func TestA2ALiveProviderReadsNamedFile(t *testing.T) {
	if os.Getenv("SUNVALE_LIVE_A2A") != "1" {
		t.Skip("set SUNVALE_LIVE_A2A=1 to call the configured provider")
	}
	base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	model := strings.TrimSpace(os.Getenv("SUNVALE_LIVE_MODEL"))
	if base == "" || key == "" || model == "" {
		t.Skip("OPENAI_BASE_URL, OPENAI_API_KEY and SUNVALE_LIVE_MODEL are required")
	}
	e := newAssistantHTTPTestEnv(t)
	path := filepath.Join(e.home, "live-a2a.txt")
	if err := os.WriteFile(path, []byte("晴谷 A2A 文件读取成功。"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	disk.Providers[0].BaseURL = base
	disk.Providers[0].Model = model
	disk.Providers[0].APIKey = key
	if err := writeAssistantDisk(assistantConfigPath(), disk); err != nil {
		t.Fatal(err)
	}

	_, out, raw := e.a2a("message/send", 1, map[string]any{
		"message": map[string]any{
			"role": "user", "messageId": "live",
			"parts": []any{map[string]any{
				"kind": "text", "text": "读取指定文件，只原样回复文件正文。",
			}},
		},
		"metadata": map[string]any{"folder": "/", "files": []string{"live-a2a.txt"}},
	}, e.conf.Token)
	if out["error"] != nil {
		t.Fatalf("send: %s", raw)
	}
	id := out["result"].(map[string]any)["id"].(string)
	deadline := time.Now().Add(120 * time.Second)
	for {
		_, out, _ = e.a2a("tasks/get", 2, map[string]any{"id": id}, e.conf.Token)
		res := out["result"].(map[string]any)
		state := res["status"].(map[string]any)["state"]
		if state == "completed" || state == "failed" {
			if state != "completed" || !strings.Contains(fmtJSON(res), "晴谷 A2A 文件读取成功。") {
				t.Fatalf("live A2A result: %s", fmtJSON(res))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("live A2A timed out: %s", fmtJSON(res))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if strings.Contains(fmtJSON(out), key) {
		t.Fatal("live response leaked the API key")
	}
}

func TestMCPLiveProviderReadsNamedFile(t *testing.T) {
	if os.Getenv("SUNVALE_LIVE_MCP") != "1" {
		t.Skip("set SUNVALE_LIVE_MCP=1 to call the configured provider")
	}
	base := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	model := strings.TrimSpace(os.Getenv("SUNVALE_LIVE_MODEL"))
	if base == "" || key == "" || model == "" {
		t.Skip("OPENAI_BASE_URL, OPENAI_API_KEY and SUNVALE_LIVE_MODEL are required")
	}
	e := newAssistantHTTPTestEnv(t)
	if err := os.WriteFile(filepath.Join(e.home, "live-mcp.txt"), []byte("晴谷 MCP 文件读取成功。"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	disk.Providers[0].BaseURL = base
	disk.Providers[0].Model = model
	disk.Providers[0].APIKey = key
	if err := writeAssistantDisk(assistantConfigPath(), disk); err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "live", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             e.srv.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{token: e.conf.Token, next: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}
	sess, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "assistant_run",
		Arguments: map[string]any{
			"prompt": "读取指定文件，只原样回复文件正文。",
			"folder": "/", "files": []string{"live-mcp.txt"},
		},
	})
	if err != nil || res.IsError {
		t.Fatalf("assistant_run: %+v %v", res, err)
	}
	out := firstMCPText(t, res)
	if !strings.Contains(out, "晴谷 MCP 文件读取成功。") {
		t.Fatalf("live MCP result: %s", out)
	}
	if strings.Contains(out, key) {
		t.Fatal("live MCP response leaked the API key")
	}
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func firstMCPText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("empty MCP result: %+v", res)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content %T", res.Content[0])
	}
	return tc.Text
}

func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
