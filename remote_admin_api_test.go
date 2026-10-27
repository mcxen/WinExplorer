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

// remote_admin_api_test.go covers /api/sunvale/* admin endpoints, the
// /@manage page, real-port manage smoke, and batch rename via the remote
// manage harness (LAY1 split from remote_manage_test.go).

func TestRemoteSunvaleStatus(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("a.txt", "A")
	tok := e.login()

	if rr := e.api(http.MethodGet, "/api/sunvale/status", "", nil); rr.Code != 401 {
		t.Fatalf("guest status: code %d, want 401", rr.Code)
	}
	rr := e.api(http.MethodGet, "/api/sunvale/status", tok, nil)
	if rr.Code != 200 {
		t.Fatalf("status: code %d (%s)", rr.Code, rr.Message)
	}
	var s remoteAdminStatus
	if err := json.Unmarshal(rr.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Enabled || s.Port != e.conf.Port || s.Username != "admin" || s.Version != remoteAdminVersion {
		t.Fatalf("status = %+v", s)
	}
	if !strings.HasPrefix(s.URL, "http://") {
		t.Fatalf("status url = %q", s.URL)
	}
	if s.Disk.Total <= 0 || s.Disk.Free <= 0 || s.Disk.Free > s.Disk.Total {
		t.Fatalf("status disk = %+v, want the home volume's capacity", s.Disk)
	}
	if s.Shares != 0 {
		t.Fatalf("status shares = %d before any share", s.Shares)
	}
	// the live share is counted
	if rr := e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": []string{"/a.txt"}}); rr.Code != 200 {
		t.Fatalf("share: code %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodGet, "/api/sunvale/status", tok, nil); rr.Code != 200 {
		t.Fatal(rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Shares != 1 {
		t.Fatalf("status shares = %d after one share, want 1", s.Shares)
	}
}

func TestRemoteSunvaleAccountAndToken(t *testing.T) {
	e := newRemoteManageEnv(t)
	tok := e.login()

	// nothing to change, and a username outside the charset
	for _, body := range []map[string]any{
		{},
		{"username": "bad name!"},
	} {
		rr := e.api(http.MethodPost, "/api/sunvale/account", e.conf.Token, body)
		if rr.Code != 400 {
			t.Fatalf("account %v: code %d (%s), want 400", body, rr.Code, rr.Message)
		}
	}

	// a password change retires the old logins, keeps the master token
	rr := e.api(http.MethodPost, "/api/sunvale/account", e.conf.Token, map[string]any{"password": "fresh horse"})
	if rr.Code != 200 {
		t.Fatalf("account: code %d (%s)", rr.Code, rr.Message)
	}
	var acc remoteAdminAccount
	if err := json.Unmarshal(rr.Data, &acc); err != nil || acc.Username != "admin" {
		t.Fatalf("account reply = %s (%v)", rr.Data, err)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"}); rr.Code != 401 {
		t.Fatalf("the old JWT survived the password change: code %d, want 401", rr.Code)
	}
	if rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": remoteTestPass}); rr.Code != 401 {
		t.Fatalf("the old password still logs in: code %d, want 401", rr.Code)
	}
	if rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": "fresh horse"}); rr.Code != 200 {
		t.Fatalf("the new password does not log in: code %d (%s)", rr.Code, rr.Message)
	}
	tok = e.token(rr.Data)
	if rr = e.api(http.MethodPost, "/api/fs/list", e.conf.Token, map[string]any{"path": "/"}); rr.Code != 200 {
		t.Fatalf("the master token died: code %d (%s)", rr.Code, rr.Message)
	}

	// a username-only change keeps the logins alive
	rr = e.api(http.MethodPost, "/api/sunvale/account", e.conf.Token, map[string]any{"username": "boss"})
	if rr.Code != 200 {
		t.Fatalf("username change: code %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"}); rr.Code != 200 {
		t.Fatalf("a username-only change retired the logins: code %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "boss", "password": "fresh horse"}); rr.Code != 200 {
		t.Fatalf("login as the new username: code %d (%s)", rr.Code, rr.Message)
	}
	tok = e.token(rr.Data)

	// reset-token: a fresh master secret, everything old is gone
	rr = e.api(http.MethodPost, "/api/sunvale/reset-token", e.conf.Token, nil)
	if rr.Code != 200 {
		t.Fatalf("reset-token: code %d (%s)", rr.Code, rr.Message)
	}
	var tk remoteAdminToken
	if err := json.Unmarshal(rr.Data, &tk); err != nil || tk.Token == "" || tk.Token == e.conf.Token {
		t.Fatalf("reset-token reply = %s (%v)", rr.Data, err)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", e.conf.Token, map[string]any{"path": "/"}); rr.Code != 401 {
		t.Fatalf("the old master token survived: code %d, want 401", rr.Code)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"}); rr.Code != 401 {
		t.Fatalf("the old JWT survived the token reset: code %d, want 401", rr.Code)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", tk.Token, map[string]any{"path": "/"}); rr.Code != 200 {
		t.Fatalf("the new master token does not work: code %d (%s)", rr.Code, rr.Message)
	}
	// a fresh login signs under the new secret
	if rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "boss", "password": "fresh horse"}); rr.Code != 200 {
		t.Fatalf("login after the reset: code %d (%s)", rr.Code, rr.Message)
	}
}

func TestRemoteSunvalePortAndEnabled(t *testing.T) {
	e := newRemoteManageEnv(t)
	master := e.conf.Token
	t.Cleanup(e.a.remoteStop)

	// the port bounds are checked before anything restarts
	for _, port := range []int{0, -1, 65536, 100000} {
		rr := e.api(http.MethodPost, "/api/sunvale/port", master, map[string]any{"port": port})
		if rr.Code != 400 || rr.Message != "invalid port" {
			t.Fatalf("port %d: code %d (%s), want 400 invalid port", port, rr.Code, rr.Message)
		}
	}

	// with the server down a port change only lands in the config
	rr := e.api(http.MethodPost, "/api/sunvale/port", master, map[string]any{"port": 23456})
	if rr.Code != 200 {
		t.Fatalf("port change: code %d (%s)", rr.Code, rr.Message)
	}
	var p remoteAdminPort
	if err := json.Unmarshal(rr.Data, &p); err != nil || p.Port != 23456 {
		t.Fatalf("port reply = %s (%v)", rr.Data, err)
	}
	rr = e.api(http.MethodGet, "/api/sunvale/status", master, nil)
	var s remoteAdminStatus
	if err := json.Unmarshal(rr.Data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Port != 23456 {
		t.Fatalf("status port = %d after the change, want 23456", s.Port)
	}

	// enabled is idempotent in both directions. Switching off retires the
	// logins the way a stop does, so these calls ride the master token.
	for range 2 {
		rr = e.api(http.MethodPost, "/api/sunvale/enabled", master, map[string]any{"enabled": false})
		if rr.Code != 200 {
			t.Fatalf("disable while down: code %d (%s)", rr.Code, rr.Message)
		}
		var en remoteAdminEnabled
		if err := json.Unmarshal(rr.Data, &en); err != nil || en.Enabled || !strings.HasPrefix(en.URL, "http://") {
			t.Fatalf("enabled reply = %s (%v)", rr.Data, err)
		}
	}

	// a login lives until the server is switched off below
	tok := e.login()

	// switching on really starts a listener on a free port
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	rr = e.api(http.MethodPost, "/api/sunvale/port", tok, map[string]any{"port": free})
	if rr.Code != 200 {
		t.Fatalf("port to the free port: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/sunvale/enabled", tok, map[string]any{"enabled": true})
	if rr.Code != 200 {
		t.Fatalf("enable: code %d (%s)", rr.Code, rr.Message)
	}
	if !remoteServing() {
		t.Fatal("enabled:true did not start the server")
	}
	rr = e.api(http.MethodPost, "/api/sunvale/enabled", tok, map[string]any{"enabled": true})
	if rr.Code != 200 || !remoteServing() {
		t.Fatalf("enable twice: code %d, serving %v", rr.Code, remoteServing())
	}

	// switching off stops it and retires the logins
	rr = e.api(http.MethodPost, "/api/sunvale/enabled", tok, map[string]any{"enabled": false})
	if rr.Code != 200 {
		t.Fatalf("disable: code %d (%s)", rr.Code, rr.Message)
	}
	if remoteServing() {
		t.Fatal("enabled:false left the server up")
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"}); rr.Code != 401 {
		t.Fatalf("the logins survived the shutdown: code %d, want 401", rr.Code)
	}
}

func TestRemoteManagePage(t *testing.T) {
	e := newRemoteManageEnv(t)

	for _, path := range []string{"/@manage", "/@manage/shares"} {
		res, body := e.do(http.MethodGet, path, nil)
		if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("%s: status %d type %q", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
		if !strings.Contains(body, "<!DOCTYPE html>") || !strings.Contains(body, "Sunvale") {
			t.Fatalf("%s: the embedded page is missing (len %d)", path, len(body))
		}
	}
}

// remoteCurl runs one curl against a real server and returns the body with
// the trailing status stripped — the same shape TestRemoteRealPortSmoke's
// own helper produces, with the command logged so -v tells the story.
func remoteCurl(t *testing.T, ctx context.Context, args ...string) (string, int) {
	t.Helper()
	args = append([]string{"-s", "--noproxy", "*", "-w", "\n%{http_code}"}, args...)
	t.Logf("$ curl %s", strings.Join(args, " "))
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

// TestRemoteManageSmoke walks the whole management story against a real
// listening port with curl, the way TestRemoteRealPortSmoke walks the read
// story: login, mkdir, upload, list, get, a ranged download, a passworded
// share browsed and downloaded as the guest, the manage page, the sunvale
// status — then a clean shutdown with the port and the temporary files gone.
func TestRemoteManageSmoke(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not available")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if hd, err := os.UserHomeDir(); err != nil || hd != home {
		t.Skip("HOME does not redirect the home dir on this platform")
	}
	remoteResetForTest()
	remoteShareAccessMu.Lock()
	remoteShareAccess = map[string]time.Time{}
	remoteShareAccessMu.Unlock()
	a := &App{store: &persistedState{}, homePath: home}
	rc := *a.remoteConf()
	rc.Username = "admin"
	rc.PassHash = remote.HashPassword(remoteTestPass)
	a.remoteSaveConf(&rc)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Skipf("cannot bind 127.0.0.1:%d: %v", port, err)
	}
	srv := &http.Server{Handler: a.remoteMux()}
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ln) }()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	curl := func(args ...string) (string, int) { return remoteCurl(t, ctx, args...) }
	decode := func(body string, out any) {
		t.Helper()
		if err := json.Unmarshal([]byte(strings.TrimSpace(body)), out); err != nil {
			t.Fatalf("decoding %q: %v", body, err)
		}
	}
	apiOK := func(body string, what string) remoteTestResp {
		t.Helper()
		var rr remoteTestResp
		decode(body, &rr)
		if rr.Code != 200 {
			t.Fatalf("%s: code %d (%s)", what, rr.Code, rr.Message)
		}
		return rr
	}

	// the login
	loginJSON, err := json.Marshal(map[string]string{"username": "admin", "password": remoteTestPass})
	if err != nil {
		t.Fatal(err)
	}
	body, code := curl("-X", "POST", "-H", "Content-Type: application/json",
		"-d", string(loginJSON), base+"/api/auth/login")
	if code != 200 {
		t.Fatalf("login: HTTP %d %s", code, body)
	}
	token := remoteTestToken(t, apiOK(body, "login").Data)

	// a folder to work in
	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"path":"/smoke"}`, base+"/api/fs/mkdir")
	if code != 200 {
		t.Fatalf("mkdir: HTTP %d %s", code, body)
	}
	apiOK(body, "mkdir")

	// an upload, the manage page's escaped File-Path form
	src := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(src, []byte(remoteTestBody), 0o644); err != nil {
		t.Fatal(err)
	}
	body, code = curl("-X", "PUT", "-H", "Authorization: "+token,
		"-H", "File-Path: %2Fsmoke%2Fdata.txt", "--data-binary", "@"+src, base+"/api/fs/put")
	if code != 200 {
		t.Fatalf("put: HTTP %d %s", code, body)
	}
	apiOK(body, "put")
	if b, err := os.ReadFile(filepath.Join(home, "smoke", "data.txt")); err != nil || string(b) != remoteTestBody {
		t.Fatalf("the upload did not land: %q %v", b, err)
	}

	// the listing sees it
	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"path":"/smoke","page":1,"per_page":0}`, base+"/api/fs/list")
	if code != 200 {
		t.Fatalf("list: HTTP %d %s", code, body)
	}
	var list struct {
		Data struct {
			Content []remoteObj `json:"content"`
			Total   int64       `json:"total"`
		} `json:"data"`
	}
	decode(body, &list)
	if list.Data.Total != 1 || len(list.Data.Content) != 1 || list.Data.Content[0].Name != "data.txt" {
		t.Fatalf("list after upload = %s", body)
	}

	// fs/get hands out the signed raw_url, and it streams — whole and ranged
	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"path":"/smoke/data.txt"}`, base+"/api/fs/get")
	if code != 200 {
		t.Fatalf("get: HTTP %d %s", code, body)
	}
	var get struct {
		Data struct {
			RawURL string `json:"raw_url"`
			Sign   string `json:"sign"`
		} `json:"data"`
	}
	decode(body, &get)
	if !strings.Contains(get.Data.RawURL, "/p/smoke/data.txt?sign=") || get.Data.Sign == "" {
		t.Fatalf("get = %s", body)
	}
	body, code = curl(get.Data.RawURL)
	if code != 200 || body != remoteTestBody {
		t.Fatalf("download: HTTP %d %q", code, body)
	}
	body, code = curl("-r", "10-", get.Data.RawURL)
	if code != 206 || body != remoteTestBody[10:] {
		t.Fatalf("ranged download: HTTP %d %q", code, body)
	}

	// a share with a password
	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-H", "Content-Type: application/json",
		"-d", `{"files":["/smoke/data.txt"],"pwd":"sekret"}`, base+"/api/share/create")
	if code != 200 {
		t.Fatalf("share create: HTTP %d %s", code, body)
	}
	var share struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(apiOK(body, "share create").Data, &share); err != nil {
		t.Fatal(err)
	}

	// the guest, unauthenticated: wrong code 403, right code the listing
	body, code = curl("-X", "POST", "-H", "Content-Type: application/json",
		"-d", `{"path":"/@s/`+share.ID+`"}`, base+"/api/fs/list")
	if code != 200 {
		t.Fatalf("guest list: HTTP %d %s", code, body)
	}
	var rr remoteTestResp
	decode(body, &rr)
	if rr.Code != 403 || rr.Message != "wrong share code" {
		t.Fatalf("guest list without the code: code %d (%s)", rr.Code, rr.Message)
	}
	body, code = curl("-X", "POST", "-H", "Content-Type: application/json",
		"-d", `{"path":"/@s/`+share.ID+`","password":"sekret"}`, base+"/api/fs/list")
	rr = remoteTestResp{}
	decode(body, &rr)
	if code != 200 || rr.Code != 200 {
		t.Fatalf("guest list with the code: HTTP %d code %d", code, rr.Code)
	}
	var guest struct {
		Data struct {
			Content []remoteObj `json:"content"`
			Total   int64       `json:"total"`
		} `json:"data"`
	}
	decode(body, &guest)
	if guest.Data.Total != 1 || guest.Data.Content[0].Name != "data.txt" || guest.Data.Content[0].Sign != "" {
		t.Fatalf("guest listing = %s", body)
	}

	// the /sd direct link carries the password in the query
	body, code = curl(base + "/sd/" + share.ID + "/data.txt?pwd=sekret")
	if code != 200 || body != remoteTestBody {
		t.Fatalf("sd download: HTTP %d %q", code, body)
	}
	body, code = curl(base + "/sd/" + share.ID + "/data.txt?pwd=wrong")
	if code != 403 || !strings.Contains(body, "wrong share code") {
		t.Fatalf("sd wrong code: HTTP %d %q", code, body)
	}

	// the manage page is the embedded HTML document
	body, code = curl(base + "/@manage")
	if code != 200 || !strings.Contains(body, "Sunvale") || !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("@manage: HTTP %d (len %d)", code, len(body))
	}

	// the status the dashboard shows counts the share
	body, code = curl("-X", "POST", "-H", "Authorization: "+token, "-d", "{}", base+"/api/sunvale/status")
	if code != 200 {
		t.Fatalf("status: HTTP %d %s", code, body)
	}
	var status struct {
		Data struct {
			Shares int `json:"shares"`
			Port   int `json:"port"`
		} `json:"data"`
	}
	decode(body, &status)
	if status.Data.Shares != 1 || status.Data.Port <= 0 {
		t.Fatalf("status = shares %d port %d, want the share counted: %s", status.Data.Shares, status.Data.Port, body)
	}

	// shutdown: the serve loop exits and the port closes
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
	if conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second); err == nil {
		conn.Close()
		t.Fatalf("port %d still accepts connections after shutdown", port)
	}
}

// TestRemoteBatchRename covers /api/fs/batch_rename and /api/fs/regex_rename:
// the endpoint the OpenList frontend calls, sharing internal/rename's engine
// with the desktop dialog.
func TestRemoteBatchRename(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("docs/a.txt", "A")
	e.writeFile("docs/b.txt", "B")
	e.writeFile("docs/taken.txt", "T")
	tok := e.login()

	batch := func(objects ...map[string]string) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/batch_rename", tok, map[string]any{
			"src_dir": "/docs", "rename_objects": objects,
		})
	}
	pair := func(from, to string) map[string]string {
		return map[string]string{"src_name": from, "new_name": to}
	}

	// A guest gets the 401 envelope; an empty list is the protocol's 400.
	if rr := e.api(http.MethodPost, "/api/fs/batch_rename", "", map[string]any{"src_dir": "/docs"}); rr.Code != 401 {
		t.Fatalf("guest batch rename: code %d, want 401", rr.Code)
	}
	if rr := e.api(http.MethodPost, "/api/fs/batch_rename", tok, map[string]any{"src_dir": "/docs"}); rr.Code != 400 {
		t.Fatalf("empty batch rename: code %d, want 400", rr.Code)
	}

	// A plain batch renames both and reports how many moved.
	rr := batch(pair("a.txt", "a2.txt"), pair("b.txt", "b2.txt"))
	if rr.Code != 200 {
		t.Fatalf("batch rename: code %d (%s)", rr.Code, rr.Message)
	}
	for _, name := range []string{"a2.txt", "b2.txt"} {
		if _, err := os.Stat(filepath.Join(e.home, "docs", name)); err != nil {
			t.Fatalf("%s not renamed: %v", name, err)
		}
	}

	// Swapping two names goes through the engine's temporary pass.
	if rr := batch(pair("a2.txt", "b2.txt"), pair("b2.txt", "a2.txt")); rr.Code != 200 {
		t.Fatalf("swap: code %d (%s)", rr.Code, rr.Message)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "docs", "b2.txt")); string(b) != "A" {
		t.Fatalf("swap lost the contents: %q", b)
	}

	// A target an unselected item holds is the protocol's 403.
	rr = batch(pair("b2.txt", "taken.txt"))
	if rr.Code != 403 || rr.Message != "file [taken.txt] exists" {
		t.Fatalf("clash: code %d (%s), want 403 file [taken.txt] exists", rr.Code, rr.Message)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "docs", "b2.txt")); string(b) != "A" {
		t.Fatal("a rejected batch still moved the file")
	}

	// A path in either name is refused: only bare names address one item.
	for _, bad := range [][2]string{{"b2.txt", "../escape.txt"}, {"sub/../b2.txt", "x.txt"}, {"b2.txt", ".."}} {
		if rr = batch(pair(bad[0], bad[1])); rr.Code != 403 {
			t.Fatalf("bad pair %v: code %d, want 403", bad, rr.Code)
		}
	}

	// A missing source is the protocol's 500.
	if rr = batch(pair("ghost.txt", "x.txt")); rr.Code != 500 {
		t.Fatalf("missing source: code %d (%s), want 500", rr.Code, rr.Message)
	}

	// Regex rename: the capture group, applied to every matching name.
	e.writeFile("pics/IMG_001.jpg", "1")
	e.writeFile("pics/IMG_002.jpg", "2")
	e.writeFile("pics/notes.txt", "n")
	rr = e.api(http.MethodPost, "/api/fs/regex_rename", tok, map[string]any{
		"src_dir": "/pics", "src_name_regex": `^IMG_(\d+)\.jpg$`, "new_name_regex": "photo-$1.jpg",
	})
	if rr.Code != 200 {
		t.Fatalf("regex rename: code %d (%s)", rr.Code, rr.Message)
	}
	for _, name := range []string{"photo-001.jpg", "photo-002.jpg", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(e.home, "pics", name)); err != nil {
			t.Fatalf("%s not renamed: %v", name, err)
		}
	}

	// An invalid regexp is a 500, not a silent no-op.
	rr = e.api(http.MethodPost, "/api/fs/regex_rename", tok, map[string]any{
		"src_dir": "/pics", "src_name_regex": `(`,
	})
	if rr.Code != 500 {
		t.Fatalf("invalid regexp: code %d, want 500", rr.Code)
	}
	if rr = e.api(http.MethodPost, "/api/fs/regex_rename", "", map[string]any{"src_dir": "/pics"}); rr.Code != 401 {
		t.Fatalf("guest regex rename: code %d, want 401", rr.Code)
	}
}
