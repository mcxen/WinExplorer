package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sunvale/internal/remote"
)

// remote_test.go is the shared HTTP harness for the OpenList-compatible
// remote server tests, plus public/login/me/path-mapping cases. FS API
// cases live in remote_fsapi_test.go; real-port smoke in remote_smoke_test.go
// (LAY1 split).

// remoteTestPass is the password of every test account.
const remoteTestPass = "top secret 42"

// remoteTestBody is the content of the sample file the download tests
// stream; 26 bytes so the Range assertions can quote exact offsets.
const remoteTestBody = "abcdefghijklmnopqrstuvwxyz"

// remoteResetForTest clears the package-level remote state — the config
// cache, the in-memory plaintext password and the live login tokens — so
// tests never share it. Test-only; the running app never calls it.
func remoteResetForTest() {
	remoteConfMu.Lock()
	remoteConfCache = nil
	remotePassPlain = ""
	remoteConfMu.Unlock()
	remoteLiveMu.Lock()
	remoteLiveTokens = map[string]struct{}{}
	remoteLiveMu.Unlock()
}

// remoteTestEnv is one test's private server: a fresh account over a
// temporary home directory, with the mux served by httptest.
type remoteTestEnv struct {
	t    *testing.T
	home string
	a    *App
	conf *remoteConfig
	srv  *httptest.Server
}

// newRemoteTestEnv points HOME at an empty temporary directory — the virtual
// root "/" the fs endpoints see — resets the remote globals and starts the
// server with a known account.
func newRemoteTestEnv(t *testing.T) *remoteTestEnv {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if hd, err := os.UserHomeDir(); err != nil || hd != home {
		t.Skip("HOME does not redirect the home dir on this platform")
	}
	remoteResetForTest()
	a := &App{}
	rc := *a.remoteConf()
	rc.Username = "admin"
	rc.PassHash = remote.HashPassword(remoteTestPass)
	a.remoteSaveConf(&rc)
	e := &remoteTestEnv{t: t, home: home, a: a, conf: &rc}
	e.srv = httptest.NewServer(a.remoteMux())
	t.Cleanup(e.srv.Close)
	return e
}

func (e *remoteTestEnv) mkdir(rel string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Join(e.home, filepath.FromSlash(rel)), 0o755); err != nil {
		e.t.Fatal(err)
	}
}

func (e *remoteTestEnv) writeFile(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.home, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// remoteTestResp is a decoded /api/* envelope together with the HTTP status
// it arrived with.
type remoteTestResp struct {
	HTTPStatus int             `json:"-"`
	Code       int             `json:"code"`
	Message    string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

// api sends one JSON /api/* request — Authorization set when token is not
// empty — and decodes the always-JSON envelope.
func (e *remoteTestEnv) api(method, path, token string, body any) remoteTestResp {
	e.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		e.t.Fatalf("%s %s: content-type = %q, want application/json", method, path, ct)
	}
	var rr remoteTestResp
	rr.HTTPStatus = res.StatusCode
	if err := json.Unmarshal(raw, &rr); err != nil {
		e.t.Fatalf("%s %s: decoding %q: %v", method, path, raw, err)
	}
	return rr
}

// do performs a raw request — the non-JSON routes use it — and returns the
// response with its body fully read.
func (e *remoteTestEnv) do(method, path string, hdr map[string]string) (*http.Response, string) {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return res, string(raw)
}

// fetch retrieves an absolute URL, as fs/get's raw_url hands it out.
func (e *remoteTestEnv) fetch(url string) (*http.Response, string) {
	e.t.Helper()
	res, err := e.srv.Client().Get(url)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return res, string(raw)
}

// login performs the plaintext login and returns the token.
func (e *remoteTestEnv) login() string {
	e.t.Helper()
	rr := e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": remoteTestPass})
	if rr.Code != 200 {
		e.t.Fatalf("login: code %d (%s)", rr.Code, rr.Message)
	}
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rr.Data, &d); err != nil {
		e.t.Fatal(err)
	}
	if d.Token == "" {
		e.t.Fatalf("login returned no token: %s", rr.Data)
	}
	return d.Token
}

// me asks /api/me for the caller's identity.
func (e *remoteTestEnv) me(token string) remoteMeUser {
	e.t.Helper()
	rr := e.api(http.MethodGet, "/api/me", token, nil)
	if rr.HTTPStatus != 200 || rr.Code != 200 {
		e.t.Fatalf("/api/me: HTTP %d code %d (%s)", rr.HTTPStatus, rr.Code, rr.Message)
	}
	var u remoteMeUser
	if err := json.Unmarshal(rr.Data, &u); err != nil {
		e.t.Fatal(err)
	}
	return u
}

func TestRemotePublicEndpoints(t *testing.T) {
	e := newRemoteTestEnv(t)

	rr := e.api(http.MethodGet, "/api/public/settings", "", nil)
	if rr.HTTPStatus != 200 || rr.Code != 200 {
		t.Fatalf("settings: HTTP %d code %d (%s)", rr.HTTPStatus, rr.Code, rr.Message)
	}
	var settings map[string]any
	if err := json.Unmarshal(rr.Data, &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings) < 10 {
		t.Fatalf("settings has %d entries, want a full public map", len(settings))
	}
	for k, v := range settings {
		if _, ok := v.(string); !ok {
			t.Errorf("settings[%q] = %#v, want a string value", k, v)
		}
	}
	if settings["site_title"] != "Sunvale" {
		t.Errorf("site_title = %v, want Sunvale", settings["site_title"])
	}

	rr = e.api(http.MethodGet, "/api/public/archive_extensions", "", nil)
	var exts []string
	if rr.Code != 200 || json.Unmarshal(rr.Data, &exts) != nil {
		t.Fatalf("archive_extensions: code %d data %s, want an array of extensions", rr.Code, rr.Data)
	}
	for _, want := range []string{".zip", ".tar.gz", ".7z"} {
		found := false
		for _, ext := range exts {
			if ext == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("archive_extensions = %v, missing %s", exts, want)
		}
	}

	res, body := e.do(http.MethodGet, "/", nil)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("GET /: status %d type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}

	res, body = e.do(http.MethodGet, "/ping", nil)
	if res.StatusCode != 200 || body != "pong" {
		t.Fatalf("GET /ping: status %d body %q", res.StatusCode, body)
	}

	// CORS, as OpenList's default config sends it: every reply carries the
	// wildcard headers and the preflight is answered without a handler.
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	res, _ = e.do(http.MethodOptions, "/api/fs/list", map[string]string{
		"Origin": "http://example.test", "Access-Control-Request-Method": "POST",
	})
	if res.StatusCode != http.StatusNoContent || res.Header.Get("Access-Control-Allow-Headers") != "*" {
		t.Fatalf("preflight: status %d headers %v", res.StatusCode, res.Header)
	}
}

func TestRemoteLogin(t *testing.T) {
	e := newRemoteTestEnv(t)

	// A wrong password is an error envelope in the body, HTTP still 200.
	rr := e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": "nope"})
	if rr.HTTPStatus != 200 || rr.Code != 401 {
		t.Fatalf("wrong password: HTTP %d code %d, want 200/401", rr.HTTPStatus, rr.Code)
	}

	// Plaintext login.
	rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "admin", "password": remoteTestPass})
	if rr.Code != 200 {
		t.Fatalf("plaintext login: code %d (%s)", rr.Code, rr.Message)
	}
	plain := e.token(rr.Data)

	// Hash login, with the StaticHash computed here from the OpenList salt
	// itself — not through remoteHashPassword — so the algorithm is pinned:
	// hex(sha256(password + "-https://github.com/alist-org/alist")).
	sum := sha256.Sum256([]byte(remoteTestPass + "-https://github.com/alist-org/alist"))
	rr = e.api(http.MethodPost, "/api/auth/login/hash", "", map[string]string{"username": "admin", "password": hex.EncodeToString(sum[:])})
	if rr.Code != 200 {
		t.Fatalf("hash login: code %d (%s)", rr.Code, rr.Message)
	}
	hashed := e.token(rr.Data)

	// Both tokens act as the administrator.
	for _, tok := range []string{plain, hashed} {
		if me := e.me(tok); me.Role != 2 || me.Disabled {
			t.Fatalf("token did not authenticate as admin: %+v", me)
		}
	}

	// A wrong username is rejected as well.
	rr = e.api(http.MethodPost, "/api/auth/login", "", map[string]string{"username": "root", "password": remoteTestPass})
	if rr.Code != 401 {
		t.Fatalf("wrong username: code %d, want 401", rr.Code)
	}
}

func (e *remoteTestEnv) token(data json.RawMessage) string {
	e.t.Helper()
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		e.t.Fatal(err)
	}
	if d.Token == "" {
		e.t.Fatalf("login returned no token: %s", data)
	}
	return d.Token
}

func TestRemoteMe(t *testing.T) {
	e := newRemoteTestEnv(t)

	// No token: the disabled guest, which makes clients show the login page.
	if me := e.me(""); me.Role != 1 || !me.Disabled || me.Username != "guest" {
		t.Fatalf("guest /api/me = %+v", me)
	}

	// The raw config token in Authorization is the administrator.
	if me := e.me(e.conf.Token); me.Role != 2 || me.Disabled || me.Username != "admin" {
		t.Fatalf("master token /api/me = %+v", me)
	}

	// A login token is the administrator, and dies with logout.
	tok := e.login()
	if me := e.me(tok); me.Role != 2 || me.Username != "admin" {
		t.Fatalf("login token /api/me = %+v", me)
	}
	rr := e.api(http.MethodGet, "/api/auth/logout", tok, nil)
	if rr.Code != 200 {
		t.Fatalf("logout: code %d (%s)", rr.Code, rr.Message)
	}
	if me := e.me(tok); me.Role != 1 || !me.Disabled {
		t.Fatalf("after logout /api/me = %+v, want the guest back", me)
	}

	// The master token is not a login JWT and survives the logout.
	if me := e.me(e.conf.Token); me.Role != 2 {
		t.Fatalf("master token after logout = %+v", me)
	}
}

func TestRemotePathMapping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if hd, err := os.UserHomeDir(); err != nil || hd != home {
		t.Skip("HOME does not redirect the home dir on this platform")
	}
	a := newStoreApp(t, home)

	if p, ok := a.remoteDiskPath("/Documents/notes.txt"); !ok || p != filepath.Join(home, "Documents", "notes.txt") {
		t.Fatalf("remoteDiskPath(/Documents/notes.txt) = %q %v", p, ok)
	}
	// Dot-dot is resolved against the virtual root, never past it.
	for _, v := range []string{"/..", "/../etc/passwd", "/a/../../etc"} {
		if p, ok := a.remoteDiskPath(v); !ok || !strings.HasPrefix(p, home) {
			t.Fatalf("remoteDiskPath(%q) = %q %v, want it contained in %q", v, p, ok, home)
		}
	}
	if v := remoteVPath(filepath.Join(home, "Documents", "notes.txt")); v != "/Documents/notes.txt" {
		t.Fatalf("remoteVPath = %q, want /Documents/notes.txt", v)
	}
	// A sibling of home must not collapse into the virtual root.
	if v := remoteVPath(home + "-old"); v != home+"-old" {
		t.Fatalf("remoteVPath(home-old) = %q, want it left absolute", v)
	}
}

func remoteTestToken(t *testing.T, data json.RawMessage) string {
	t.Helper()
	var d struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &d); err != nil || d.Token == "" {
		t.Fatalf("login returned no token: %s (%v)", data, err)
	}
	return d.Token
}
