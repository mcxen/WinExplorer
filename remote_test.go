package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// remote_test.go drives the OpenList-compatible remote server through real
// HTTP round trips against a temporary home directory: the protocol envelope,
// login in all of its forms, the fs endpoints, the signed /d and /p
// downloads and the built-in web page, plus one smoke run on a real
// listening port driven by curl.

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
	rc.PassHash = remoteHashPassword(remoteTestPass)
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

func TestRemoteFSList(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Documents")
	e.mkdir("Music")
	e.mkdir("Documents/empty")
	e.writeFile("app.log", "log")
	e.writeFile("readme.md", "hi")

	// Guest gets the 401 envelope — JSON, over HTTP 200.
	rr := e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/"})
	if rr.HTTPStatus != 200 || rr.Code != 401 {
		t.Fatalf("guest list: HTTP %d code %d, want 200/401", rr.HTTPStatus, rr.Code)
	}

	tok := e.login()
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/", "page": 1, "per_page": 0})
	if rr.Code != 200 {
		t.Fatalf("root list: code %d (%s)", rr.Code, rr.Message)
	}
	var data remoteFSListData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 4 || len(data.Content) != 4 {
		t.Fatalf("root list: total %d content %d, want 4/4", data.Total, len(data.Content))
	}
	// Folders first, then case-insensitive names.
	want := []string{"Documents", "Music", "app.log", "readme.md"}
	for i, name := range want {
		if data.Content[i].Name != name {
			t.Fatalf("root list[%d] = %q, want %q", i, data.Content[i].Name, name)
		}
	}
	// Only files carry a download sign.
	for _, o := range data.Content {
		if o.IsDir == (o.Sign != "") {
			t.Errorf("%s: is_dir %v but sign %q", o.Name, o.IsDir, o.Sign)
		}
	}

	// Pagination walks the same order.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/", "page": 2, "per_page": 2})
	if rr.Code != 200 {
		t.Fatalf("page 2: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Content) != 2 || data.Content[0].Name != "app.log" || data.Content[1].Name != "readme.md" {
		t.Fatalf("page 2 content = %+v", data.Content)
	}
	// A page past the end is the empty answer: content null.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/", "page": 3, "per_page": 2})
	if rr.Code != 200 {
		t.Fatalf("page 3: code %d (%s)", rr.Code, rr.Message)
	}
	var page struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rr.Data, &page); err != nil {
		t.Fatal(err)
	}
	if string(page.Content) != "null" {
		t.Fatalf("page past end content = %s, want null", page.Content)
	}

	// The empty folder lists content null with total 0.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/Documents/empty", "page": 1, "per_page": 0})
	if rr.Code != 200 {
		t.Fatalf("empty dir: code %d (%s)", rr.Code, rr.Message)
	}
	var empty struct {
		Content json.RawMessage `json:"content"`
		Total   int64           `json:"total"`
	}
	if err := json.Unmarshal(rr.Data, &empty); err != nil {
		t.Fatal(err)
	}
	if string(empty.Content) != "null" || empty.Total != 0 {
		t.Fatalf("empty dir: content %s total %d, want null/0", empty.Content, empty.Total)
	}

	// A missing folder is the protocol's 500 envelope, not an HTTP 500.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/nope"})
	if rr.HTTPStatus != 200 || rr.Code != 500 {
		t.Fatalf("missing folder: HTTP %d code %d, want 200/500", rr.HTTPStatus, rr.Code)
	}

	// A file is not a folder: OpenList lists a directory and says so.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/readme.md"})
	if rr.Code != 500 || rr.Message != "not a folder" {
		t.Fatalf("file list: code %d (%s), want 500 not a folder", rr.Code, rr.Message)
	}

	// A missing path is the virtual root, as JoinPath("") resolves it on
	// OpenList's side.
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"per_page": 0})
	if rr.Code != 200 || string(rr.Data) == "" {
		t.Fatalf("missing path: code %d (%s), want the root listing", rr.Code, rr.Message)
	}

	// GET binds the query string, the way gin's ShouldBind does.
	rr = e.api(http.MethodGet, "/api/fs/list?path=/Documents&page=1&per_page=0", tok, nil)
	if rr.Code != 200 {
		t.Fatalf("GET list: code %d (%s)", rr.Code, rr.Message)
	}
	var got remoteFSListData
	if err := json.Unmarshal(rr.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Content) != 1 || got.Content[0].Name != "empty" {
		t.Fatalf("GET list of /Documents = %+v", got)
	}

	// Folders answer size 0, as a Local storage without directory_size
	// does; a bare stat size (the inode on Unix) must not leak through.
	if err := json.Unmarshal(rr.Data, &got); err != nil {
		t.Fatal(err)
	}
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"})
	if err := json.Unmarshal(rr.Data, &got); err != nil {
		t.Fatal(err)
	}
	for _, o := range got.Content {
		if o.IsDir && o.Size != 0 {
			t.Fatalf("folder %s carries size %d, want 0", o.Name, o.Size)
		}
	}
}

// TestRemoteFSTypeTable pins the extension table to OpenList's default
// audio/video/image/text settings, which decide how a client previews a file.
func TestRemoteFSTypeTable(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"movie.mp4", remoteFSTypeVideo},
		{"stream.m3u8", remoteFSTypeVideo},
		{"clip.rmvb", remoteFSTypeVideo},
		{"song.mp3", remoteFSTypeAudio},
		{"sound.m4a", remoteFSTypeAudio},
		{"photo.jpg", remoteFSTypeImage},
		{"picture.avif", remoteFSTypeImage},
		{"animation.swf", remoteFSTypeImage},
		{"notes.md", remoteFSTypeText},
		{"main.go", remoteFSTypeText},
		{"app.tsx", remoteFSTypeText},
		{"sub.srt", remoteFSTypeText},
		{"danmu.ASS", remoteFSTypeText},
		{"vars.properties", remoteFSTypeText},
		{"page.htm", remoteFSTypeText},
		{"data.bin", remoteFSTypeUnknown},
		{"archive.zip", remoteFSTypeUnknown},
	}
	for _, c := range cases {
		if got := remoteFSType(c.name, false); got != c.want {
			t.Errorf("remoteFSType(%q) = %d, want %d", c.name, got, c.want)
		}
	}
	if got := remoteFSType("folder.mp4", true); got != remoteFSTypeFolder {
		t.Fatalf("a folder named like a video = %d, want %d", got, remoteFSTypeFolder)
	}
}

func TestRemoteFSGet(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.writeFile("Documents/notes.txt", remoteTestBody)
	e.writeFile("Documents/项目 #2.txt", "special")
	e.writeFile("hello world.txt", "spaced")

	tok := e.login()

	// A file: raw_url points at /p with the sign appended, and fetches.
	rr := e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": "/Documents/notes.txt"})
	if rr.Code != 200 {
		t.Fatalf("fs/get file: code %d (%s)", rr.Code, rr.Message)
	}
	var data remoteFSGetData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.IsDir || data.Sign == "" {
		t.Fatalf("fs/get file: %+v", data.remoteObj)
	}
	if !strings.Contains(data.RawURL, "/p/Documents/notes.txt?sign=") {
		t.Fatalf("raw_url = %q, want the signed /p link", data.RawURL)
	}
	res, body := e.fetch(data.RawURL)
	if res.StatusCode != 200 || body != remoteTestBody {
		t.Fatalf("raw_url fetch: status %d body %q", res.StatusCode, body)
	}

	// A name that needs escaping in the URL still round-trips through /p.
	rr = e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": "/Documents/项目 #2.txt"})
	if rr.Code != 200 {
		t.Fatalf("fs/get escaped name: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if res, body = e.fetch(data.RawURL); res.StatusCode != 200 || body != "special" {
		t.Fatalf("escaped raw_url fetch: status %d body %q", res.StatusCode, body)
	}

	// The spaced name's download keeps its spaces, not '+'.
	rr = e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": "/hello world.txt"})
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	res, _ = e.fetch(data.RawURL)
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, `filename*=utf-8''hello%20world.txt`) || strings.ContainsAny(cd, "+") {
		t.Fatalf("content-disposition = %q", cd)
	}

	// A folder carries no link and no sign.
	for _, p := range []string{"/Documents", "/"} {
		rr = e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": p})
		if rr.Code != 200 {
			t.Fatalf("fs/get %s: code %d (%s)", p, rr.Code, rr.Message)
		}
		if err := json.Unmarshal(rr.Data, &data); err != nil {
			t.Fatal(err)
		}
		if !data.IsDir || data.RawURL != "" || data.Sign != "" {
			t.Fatalf("fs/get %s: is_dir %v raw_url %q sign %q", p, data.IsDir, data.RawURL, data.Sign)
		}
	}

	// Guest: 401 envelope.
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/Documents/notes.txt"})
	if rr.Code != 401 {
		t.Fatalf("guest fs/get: code %d, want 401", rr.Code)
	}
}

// TestRemoteFSGetRootAndRelated covers the object shape the official client
// reads: the root is called root, related is null except for videos, whose
// subtitles travel beside them, and a missing path means the root.
func TestRemoteFSGetRootAndRelated(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.writeFile("media/clip.mp4", "v")
	e.writeFile("media/clip.srt", "sub")
	e.writeFile("media/clip.zh.ass", "sub")
	e.writeFile("media/clip.xml", "danmu")
	e.writeFile("media/other.mp4", "v")
	e.writeFile("notes.txt", "n")
	tok := e.login()

	get := func(path string) remoteFSGetData {
		t.Helper()
		rr := e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": path})
		if rr.Code != 200 {
			t.Fatalf("fs/get %s: code %d (%s)", path, rr.Code, rr.Message)
		}
		var data remoteFSGetData
		if err := json.Unmarshal(rr.Data, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}

	// The virtual root answers OpenList's name for it.
	root := get("/")
	if !root.IsDir || root.Name != "root" || root.Size != 0 {
		t.Fatalf("root = %+v, want name root, a folder, size 0", root)
	}
	// A missing path is the same root.
	if rr := e.api(http.MethodGet, "/api/fs/get", tok, nil); rr.Code != 200 {
		t.Fatalf("GET fs/get: code %d (%s)", rr.Code, rr.Message)
	}

	// A video brings its same-prefix siblings; another stem stays out.
	clip := get("/media/clip.mp4")
	if clip.Type != remoteFSTypeVideo {
		t.Fatalf("clip.mp4 type = %d, want video", clip.Type)
	}
	names := make([]string, 0, len(clip.Related))
	for _, o := range clip.Related {
		names = append(names, o.Name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "clip.srt,clip.xml,clip.zh.ass" {
		t.Fatalf("related = %v, want the clip.* siblings only", names)
	}

	// Everything else answers JSON null, the field still present.
	for _, p := range []string{"/media", "/notes.txt", "/media/clip.srt"} {
		raw, err := json.Marshal(get(p))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), `"related":null`) {
			t.Fatalf("fs/get %s: %s, want related null", p, raw)
		}
	}
}

func TestRemoteDownload(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.writeFile("Documents/notes.txt", remoteTestBody)
	sign := e.a.remoteSign("/Documents/notes.txt")

	for _, prefix := range []string{"/d", "/p"} {
		u := prefix + "/Documents/notes.txt?sign=" + sign

		// The whole file.
		res, body := e.do(http.MethodGet, u, nil)
		if res.StatusCode != 200 || body != remoteTestBody {
			t.Fatalf("%s full: status %d body %q", prefix, res.StatusCode, body)
		}

		// A range request: 206 with the tail and a precise Content-Range.
		res, body = e.do(http.MethodGet, u, map[string]string{"Range": "bytes=10-"})
		if res.StatusCode != 206 || body != remoteTestBody[10:] {
			t.Fatalf("%s range: status %d body %q", prefix, res.StatusCode, body)
		}
		if cr := res.Header.Get("Content-Range"); cr != "bytes 10-25/26" {
			t.Fatalf("%s range: content-range %q", prefix, cr)
		}

		// HEAD: the length, never a body.
		res, body = e.do(http.MethodHead, u, nil)
		if res.StatusCode != 200 || res.ContentLength != int64(len(remoteTestBody)) || body != "" {
			t.Fatalf("%s head: status %d len %d body %q", prefix, res.StatusCode, res.ContentLength, body)
		}
	}

	// No sign: OpenList's HTML error page.
	res, body := e.do(http.MethodGet, "/p/Documents/notes.txt", nil)
	if res.StatusCode != 401 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "sign invalid") {
		t.Fatalf("unsigned: status %d type %q body %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}

	// A well-formed sign that does not match, ditto.
	res, body = e.do(http.MethodGet, "/p/Documents/notes.txt?sign=deadbeef:0", nil)
	if res.StatusCode != 401 || !strings.Contains(body, "sign invalid") {
		t.Fatalf("bad sign: status %d body %q", res.StatusCode, body)
	}

	// A sign whose expiry has passed says so, as OpenList's wording does.
	past := time.Now().Add(-time.Hour).Unix()
	expire := strconv.FormatInt(past, 10)
	mac := hmac.New(sha256.New, []byte(e.conf.Token))
	mac.Write([]byte("/Documents/notes.txt:" + expire))
	expired := base64.URLEncoding.EncodeToString(mac.Sum(nil)) + ":" + expire
	res, body = e.do(http.MethodGet, "/p/Documents/notes.txt?sign="+expired, nil)
	if res.StatusCode != 401 || !strings.Contains(body, "sign expired") {
		t.Fatalf("expired sign: status %d body %q", res.StatusCode, body)
	}

	// A signed folder is still not a file.
	dirSign := e.a.remoteSign("/Documents")
	res, body = e.do(http.MethodGet, "/p/Documents?sign="+dirSign, nil)
	if res.StatusCode != 500 || !strings.Contains(body, "not a file") {
		t.Fatalf("folder download: status %d body %q", res.StatusCode, body)
	}

	// The root of /d is the home folder: the same refusal.
	res, body = e.do(http.MethodGet, "/d/", nil)
	if res.StatusCode != 500 || !strings.Contains(body, "not a file") {
		t.Fatalf("root download: status %d body %q", res.StatusCode, body)
	}
}

func TestRemoteFSSearch(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Documents")
	e.writeFile("Documents/notes.txt", "n")
	e.mkdir("Music/playlists")
	e.writeFile("Music/playlists.txt", "p")

	tok := e.login()
	search := func(keywords string, scope int) map[string]remoteFSSearchItem {
		t.Helper()
		rr := e.api(http.MethodPost, "/api/fs/search", tok, map[string]any{
			"parent": "/", "keywords": keywords, "scope": scope, "page": 1, "per_page": 100,
		})
		if rr.Code != 200 {
			t.Fatalf("search %q scope %d: code %d (%s)", keywords, scope, rr.Code, rr.Message)
		}
		var data remoteFSSearchData
		if err := json.Unmarshal(rr.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Total != int64(len(data.Content)) {
			t.Fatalf("search %q scope %d: total %d but %d hits", keywords, scope, data.Total, len(data.Content))
		}
		out := map[string]remoteFSSearchItem{}
		for _, it := range data.Content {
			out[it.Name] = it
		}
		return out
	}

	// A keyword hits the file wherever it sits.
	hits := search("notes", 0)
	if len(hits) != 1 {
		t.Fatalf("notes: %d hits, want 1 (%v)", len(hits), hits)
	}
	if it := hits["notes.txt"]; it.Parent != "/Documents" || it.IsDir || it.Type != remoteFSTypeText {
		t.Fatalf("notes.txt hit = %+v", it)
	}

	// A keyword matching a folder and a file: scope filters.
	if hits = search("playlist", 0); len(hits) != 2 {
		t.Fatalf("playlist scope 0: %d hits, want 2 (%v)", len(hits), hits)
	}
	if hits = search("playlist", 1); len(hits) != 1 || !hits["playlists"].IsDir {
		t.Fatalf("playlist scope 1: %v", hits)
	}
	if hits = search("playlist", 2); len(hits) != 1 || hits["playlists.txt"].IsDir || hits["playlists.txt"].Parent != "/Music" {
		t.Fatalf("playlist scope 2: %v", hits)
	}

	// Guest: 401 envelope.
	rr := e.api(http.MethodPost, "/api/fs/search", "", map[string]any{"parent": "/", "keywords": "x", "scope": 0, "page": 1, "per_page": 10})
	if rr.Code != 401 {
		t.Fatalf("guest search: code %d, want 401", rr.Code)
	}
}

// TestRemoteRealPortSmoke runs the server on a real listening port, as
// remoteStart would, and walks the full client flow with curl: ping, login,
// list, get, a ranged download, logout — then shuts down and proves the
// serve loop exits and the port closes.
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
	rc.PassHash = remoteHashPassword(remoteTestPass)
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
