package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sunvale/internal/remote"
)

// remote_mcp_e2e_test.go exercises both loopback protocols through real SDK
// clients and sockets. It stays short-safe: live model access is separately
// gated by SUNVALE_LIVE.
func TestRemoteOpenListEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback end-to-end test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	remoteResetForTest()
	a := &App{}
	conf := *a.remoteConf()
	conf.Username, conf.PassHash = "admin", remote.HashPassword(remoteTestPass)
	a.remoteSaveConf(&conf)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: a.remoteMux()}
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())
	base := "http://" + ln.Addr().String()

	api := func(method, path, token string, body any) remoteTestResp {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = strings.NewReader(string(data))
		}
		req, err := http.NewRequest(method, base+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out remoteTestResp
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	login := api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": remoteTestPass})
	token := remoteTestToken(t, login.Data)
	put, err := http.NewRequest(http.MethodPut, base+"/api/fs/put", strings.NewReader("roundtrip"))
	if err != nil {
		t.Fatal(err)
	}
	put.Header.Set("Authorization", token)
	put.Header.Set("File-Path", url.PathEscape("/upload.txt"))
	res, err := http.DefaultClient.Do(put)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("upload HTTP %d", res.StatusCode)
	}
	if rr := api(http.MethodPost, "/api/fs/list", token, map[string]any{"path": "/"}); rr.Code != 200 || !strings.Contains(string(rr.Data), "upload.txt") {
		t.Fatalf("list: %+v", rr)
	}
	get := api(http.MethodPost, "/api/fs/get", token, map[string]string{"path": "/upload.txt"})
	var info struct {
		RawURL string `json:"raw_url"`
	}
	if err := json.Unmarshal(get.Data, &info); err != nil {
		t.Fatal(err)
	}
	download, err := http.Get(info.RawURL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(download.Body)
	download.Body.Close()
	if string(data) != "roundtrip" {
		t.Fatalf("download = %q", data)
	}
	if rr := api(http.MethodPost, "/api/fs/rename", token, map[string]string{"path": "/upload.txt", "name": "renamed.txt"}); rr.Code != 200 {
		t.Fatalf("rename: %+v", rr)
	}
	if rr := api(http.MethodPost, "/api/fs/remove", token, map[string]any{"dir": "/", "names": []string{"renamed.txt"}}); rr.Code != 200 {
		t.Fatalf("remove: %+v", rr)
	}
	if _, err := os.Stat(filepath.Join(home, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatalf("removed file remains: %v", err)
	}
}

func TestAssistantMCPStreamableEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("loopback end-to-end test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	remoteResetForTest()
	resetAssistantHTTPForTest()
	a := &App{}
	conf := a.remoteConf()
	srv := httptest.NewServer(a.remoteMux())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "sunvale-e2e", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: mcpBearerTransport{
			token: conf.Token, next: http.DefaultTransport,
		}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 7 {
		t.Fatalf("ListTools: %d %v", len(listed.Tools), err)
	}
	call := func(name string, args map[string]any) {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || result.IsError {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
	}
	call("assistant_config_list", map[string]any{})
	call("assistant_config_save", map[string]any{
		"id": "e2e", "name": "e2e", "baseURL": "http://127.0.0.1:1/v1",
		"model": "e2e", "apiKey": "test-only", "active": true,
	})
	call("assistant_config_activate", map[string]any{"id": "e2e"})
	call("assistant_status", map[string]any{})
	call("a2a_info", map[string]any{})
	oldRun := runAssistantHeadless
	runAssistantHeadless = func(context.Context, assistRun) (assistRunResult, error) {
		return assistRunResult{Text: "e2e"}, nil
	}
	call("assistant_run", map[string]any{"prompt": "列出目录"})
	runAssistantHeadless = oldRun
	call("assistant_config_delete", map[string]any{"id": "e2e"})
}

func TestAssistantLiveToolCall(t *testing.T) {
	if os.Getenv("SUNVALE_LIVE") != "1" {
		t.Skip("set SUNVALE_LIVE=1 for the configured real endpoint")
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := activeAssistantProvider(disk)
	if !ok || provider.APIKey == "" {
		t.Skip("no active configured provider")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live.txt"), []byte("sunvale-live-marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := make(chan string, 8)
	sunvaleToolState.setToolCallHook(func(name string) { called <- name })
	defer sunvaleToolState.setToolCallHook(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err = runAssistantHeadless(ctx, assistRun{prov: provider, root: dir, prefs: disk.Prefs,
		hist: []*schema.Message{schema.UserMessage("请先调用 list_dir 查看当前目录，再简短回答文件名。")}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-ctx.Done():
		t.Fatal("助手未调用工具")
	}
}
