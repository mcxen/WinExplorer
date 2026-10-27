package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sunvale/internal/remote"
)

// remote_smoke_test.go runs one real listening-port curl smoke against the
// remote server harness (LAY1 split from remote_test.go).

func TestRemoteRealPortSmoke(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if hd, err := os.UserHomeDir(); err != nil || hd != home {
		t.Skip("HOME does not redirect the home dir on this platform")
	}
	remoteResetForTest()
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "smoke.txt"), []byte(remoteTestBody), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	rc := *a.remoteConf()
	rc.Username = "admin"
	rc.PassHash = remote.HashPassword(remoteTestPass)
	a.remoteSaveConf(&rc)

	ln, err := net.Listen("tcp", "127.0.0.1:15244")
	if err != nil {
		t.Skipf("cannot bind 127.0.0.1:15244: %v", err)
	}
	srv := &http.Server{Handler: a.remoteMux()}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()
	base := "http://127.0.0.1:15244"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// curl reports the body followed by a newline and the status code.
	curl := func(args ...string) (string, int) {
		t.Helper()
		args = append([]string{"-s", "--noproxy", "*", "-w", "\n%{http_code}"}, args...)
		out, err := exec.CommandContext(ctx, "curl", args...).Output()
		if err != nil {
			t.Fatalf("curl %v: %v", args, err)
		}
		s := string(out)
		i := strings.LastIndexByte(s, '\n')
		if i < 0 {
			t.Fatalf("curl %v: no status in %q", args, s)
		}
		code, err := strconv.Atoi(s[i+1:])
		if err != nil {
			t.Fatalf("curl %v: bad status %q", args, s[i+1:])
		}
		return s[:i], code
	}
	decode := func(body string, out any) {
		t.Helper()
		if err := json.Unmarshal([]byte(strings.TrimSpace(body)), out); err != nil {
			t.Fatalf("decoding %q: %v", body, err)
		}
	}

	body, code := curl(base + "/ping")
	if code != 200 || body != "pong" {
		t.Fatalf("ping: %d %q", code, body)
	}

	loginJSON, err := json.Marshal(map[string]string{"username": "admin", "password": remoteTestPass})
	if err != nil {
		t.Fatal(err)
	}
	body, code = curl("-X", "POST", "-H", "Content-Type: application/json", "-d", string(loginJSON), base+"/api/auth/login")
	if code != 200 {
		t.Fatalf("login: %d %q", code, body)
	}
	var login remoteTestResp
	decode(body, &login)
	if login.Code != 200 {
		t.Fatalf("login: code %d (%s)", login.Code, login.Message)
	}
	token := remoteTestToken(t, login.Data)

	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"path":"/","page":1,"per_page":0}`, base+"/api/fs/list")
	if code != 200 {
		t.Fatalf("list: %d %q", code, body)
	}
	var list remoteTestResp
	decode(body, &list)
	if list.Code != 200 {
		t.Fatalf("list: code %d (%s)", list.Code, list.Message)
	}
	var listData struct {
		Content []remoteObj `json:"content"`
		Total   int64       `json:"total"`
	}
	decode(string(list.Data), &listData)
	if listData.Total != 2 { // Documents + smoke.txt
		t.Fatalf("list total %d, want 2: %s", listData.Total, body)
	}
	var fileSign string
	for _, o := range listData.Content {
		if o.Name == "smoke.txt" {
			fileSign = o.Sign
		}
	}
	if fileSign == "" {
		t.Fatalf("smoke.txt missing its sign in the listing: %s", body)
	}

	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"path":"/smoke.txt"}`, base+"/api/fs/get")
	if code != 200 {
		t.Fatalf("get: %d %q", code, body)
	}
	var get remoteTestResp
	decode(body, &get)
	if get.Code != 200 {
		t.Fatalf("get: code %d (%s)", get.Code, get.Message)
	}
	var getData struct {
		RawURL string `json:"raw_url"`
		Sign   string `json:"sign"`
	}
	decode(string(get.Data), &getData)
	if !strings.Contains(getData.RawURL, "/p/smoke.txt?sign=") || getData.Sign != fileSign {
		t.Fatalf("get: raw_url %q sign %q (list sign %q)", getData.RawURL, getData.Sign, fileSign)
	}

	// The whole file through the signed link.
	body, code = curl(getData.RawURL)
	if code != 200 || body != remoteTestBody {
		t.Fatalf("download: %d %q", code, body)
	}
	// A range from the middle.
	body, code = curl("-r", "10-", getData.RawURL)
	if code != 206 || body != remoteTestBody[10:] {
		t.Fatalf("ranged download: %d %q", code, body)
	}

	// Logout invalidates the token.
	body, code = curl("-H", "Authorization: "+token, base+"/api/auth/logout")
	if code != 200 {
		t.Fatalf("logout: %d %q", code, body)
	}
	body, code = curl("-H", "Authorization: "+token, base+"/api/me")
	if code != 200 {
		t.Fatalf("me after logout: %d %q", code, body)
	}
	var me remoteTestResp
	decode(body, &me)
	var meUser remoteMeUser
	decode(string(me.Data), &meUser)
	if meUser.Role != 1 || !meUser.Disabled {
		t.Fatalf("me after logout = %+v, want the guest", meUser)
	}

	// Shutdown: the serve loop exits and the port closes.
	shutdownCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve returned %v, want ErrServerClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the serve loop did not exit after shutdown")
	}
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:15244", time.Second); err == nil {
		conn.Close()
		t.Fatal("the port still accepts connections after shutdown")
	}
}

// remoteTestToken pulls the token out of a login reply; the smoke test has
// no remoteTestEnv to ask.
