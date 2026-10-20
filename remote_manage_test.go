package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

// remote_manage_test.go drives the management round's server surface over
// the same real-HTTP harness as remote_test.go: the fs write endpoints and
// uploads (remote_manage.go), the share system end to end — admin side and
// guest side (remote_share.go), the /api/sunvale/* endpoints
// (remote_admin.go), the /@manage page, and one curl smoke run on a real
// listening port that walks the whole story.

// newRemoteManageEnv layers this round's needs on top of remote_test.go's
// environment: a persisted state for the trash log and the shares, a
// homePath so /api/sunvale/status sees the volume, and a clean
// share-visitor dedup window — the access map is global and would otherwise
// leak between tests.
// loadStateFile reads a state file by its own path, for tests whose store
// writes beside the home folder rather than under it.
func loadStateFile(path string) *persistedState {
	st := &persistedState{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		return &persistedState{path: path}
	}
	st.path = path
	return st
}

func newRemoteManageEnv(t *testing.T) *remoteTestEnv {
	e := newRemoteTestEnv(t)
	// Shares persist through a store with a path, so the reloading test can
	// read them back. The file lives beside the home folder rather than
	// under it: loadState would create Library/Application Support inside
	// the home, and the fs tests list that home's root and would see it.
	e.a.store = &persistedState{path: filepath.Join(t.TempDir(), "state.json")}
	e.a.homePath = e.home
	remoteShareAccessMu.Lock()
	remoteShareAccess = map[string]time.Time{}
	remoteShareAccessMu.Unlock()
	return e
}

// shareOf decodes one share reply.
func (e *remoteTestEnv) shareOf(data json.RawMessage) remoteShare {
	e.t.Helper()
	var s remoteShare
	if err := json.Unmarshal(data, &s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// remoteJSEscape mirrors JavaScript's encodeURIComponent byte for byte —
// the exact encoder the manage page applies to the File-Path header before
// an upload — so the put tests prove the browser's wire form decodes back to
// the intended path. url.PathEscape would leave the "/" separators alone,
// which the page never does.
func remoteJSEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '!', c == '~', c == '*',
			c == '\'', c == '(', c == ')':
			b.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0xF])
		}
	}
	return b.String()
}

// put streams content through PUT /api/fs/put. filePath is written to the
// File-Path header exactly as given — the tests hand it the escaped or the
// raw form to pin both wire shapes — and a query string inside url reaches
// the path-parameter fallback.
func (e *remoteTestEnv) put(url, token, filePath, overwrite, content string) remoteTestResp {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodPut, e.srv.URL+url, strings.NewReader(content))
	if err != nil {
		e.t.Fatal(err)
	}
	if filePath != "" {
		req.Header.Set("File-Path", filePath)
	}
	if overwrite != "" {
		req.Header.Set("Overwrite", overwrite)
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
	var rr remoteTestResp
	rr.HTTPStatus = res.StatusCode
	if err := json.Unmarshal(raw, &rr); err != nil {
		e.t.Fatalf("PUT %s: decoding %q: %v", url, raw, err)
	}
	return rr
}

func TestRemoteFSMkdir(t *testing.T) {
	e := newRemoteManageEnv(t)
	tok := e.login()

	if rr := e.api(http.MethodPost, "/api/fs/mkdir", "", map[string]any{"path": "/docs"}); rr.Code != 401 {
		t.Fatalf("guest mkdir: code %d, want 401", rr.Code)
	}

	rr := e.api(http.MethodPost, "/api/fs/mkdir", tok, map[string]any{"path": "/docs"})
	if rr.HTTPStatus != 200 || rr.Code != 200 {
		t.Fatalf("mkdir: HTTP %d code %d (%s)", rr.HTTPStatus, rr.Code, rr.Message)
	}
	if info, err := os.Stat(filepath.Join(e.home, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("mkdir did not create the folder: %v", err)
	}

	// an existing target is the protocol's "file exists"
	rr = e.api(http.MethodPost, "/api/fs/mkdir", tok, map[string]any{"path": "/docs"})
	if rr.Code != 403 || rr.Message != "file exists" {
		t.Fatalf("mkdir twice: code %d (%s), want 403 file exists", rr.Code, rr.Message)
	}
	// only the last level is created; the parent must exist
	rr = e.api(http.MethodPost, "/api/fs/mkdir", tok, map[string]any{"path": "/missing/child"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("mkdir without parent: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// dot-dot stays inside the virtual root and works like any name
	rr = e.api(http.MethodPost, "/api/fs/mkdir", tok, map[string]any{"path": "/../escape"})
	if rr.Code != 200 {
		t.Fatalf("mkdir with dot-dot: code %d (%s)", rr.Code, rr.Message)
	}
	if info, err := os.Stat(filepath.Join(e.home, "escape")); err != nil || !info.IsDir() {
		t.Fatalf("dot-dot did not resolve against the root: %v", err)
	}
	// a body that is not the request's shape is its own error
	rr = e.api(http.MethodPost, "/api/fs/mkdir", tok, []any{})
	if rr.Code != 400 {
		t.Fatalf("bad body: code %d, want 400", rr.Code)
	}
}

func TestRemoteFSRename(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("docs/a.txt", "A")
	e.writeFile("docs/b.txt", "B")
	tok := e.login()

	rr := e.api(http.MethodPost, "/api/fs/rename", tok, map[string]any{"path": "/docs/a.txt", "name": "renamed.txt"})
	if rr.Code != 200 {
		t.Fatalf("rename: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "docs", "a.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rename left the old name behind")
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "docs", "renamed.txt")); err != nil || string(b) != "A" {
		t.Fatalf("renamed file: %q %v", b, err)
	}

	// a bare name only
	for _, name := range []string{"", ".", "..", "sub/dir", `sub\dir`} {
		rr = e.api(http.MethodPost, "/api/fs/rename", tok, map[string]any{"path": "/docs/b.txt", "name": name})
		if rr.Code != 403 || rr.Message != "invalid name" {
			t.Fatalf("rename to %q: code %d (%s), want 403 invalid name", name, rr.Code, rr.Message)
		}
	}

	// a conflict says which file exists, and leaves both sides alone
	rr = e.api(http.MethodPost, "/api/fs/rename", tok, map[string]any{"path": "/docs/renamed.txt", "name": "b.txt"})
	if rr.Code != 403 || rr.Message != "file [b.txt] exists" {
		t.Fatalf("rename conflict: code %d (%s), want 403 naming the file", rr.Code, rr.Message)
	}
	for _, name := range []string{"renamed.txt", "b.txt"} {
		if _, err := os.Lstat(filepath.Join(e.home, "docs", name)); err != nil {
			t.Fatalf("%s lost after the refused rename: %v", name, err)
		}
	}

	// the virtual root is not renamable, a missing source is not there
	rr = e.api(http.MethodPost, "/api/fs/rename", tok, map[string]any{"path": "/", "name": "root"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("rename root: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/rename", tok, map[string]any{"path": "/docs/ghost.txt", "name": "x.txt"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("rename missing: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
}

func TestRemoteFSMove(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.mkdir("src/self")
	e.mkdir("dst")
	e.writeFile("src/file.txt", "F")
	e.writeFile("src/keep.txt", "K")
	e.writeFile("dst/file.txt", "D")
	tok := e.login()
	move := func(body map[string]any) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/move", tok, body)
	}

	// a conflict that is neither skipped nor overwritten fails the whole
	// batch before anything is touched
	rr := move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"file.txt"}})
	if rr.Code != 403 || rr.Message != "file [file.txt] exists" {
		t.Fatalf("move conflict: code %d (%s), want 403 naming the file", rr.Code, rr.Message)
	}
	for p, want := range map[string]string{"src/file.txt": "F", "dst/file.txt": "D"} {
		if b, err := os.ReadFile(filepath.Join(e.home, filepath.FromSlash(p))); err != nil || string(b) != want {
			t.Fatalf("%s moved despite the 403: %q %v", p, b, err)
		}
	}

	// skip_existing leaves both copies
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"file.txt"}, "skip_existing": true})
	if rr.Code != 200 {
		t.Fatalf("move skip_existing: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "src", "file.txt")); err != nil {
		t.Fatalf("skipped move lost the source: %v", err)
	}

	// overwrite replaces the destination
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"file.txt"}, "overwrite": true})
	if rr.Code != 200 {
		t.Fatalf("move overwrite: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "dst", "file.txt")); err != nil || string(b) != "F" {
		t.Fatalf("overwritten destination: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "src", "file.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the moved file stayed in the source")
	}

	// a folder moved into its own subtree is skipped, its batchmates are not
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/src/self", "names": []string{"self", "keep.txt"}})
	if rr.Code != 200 {
		t.Fatalf("move into itself: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "src", "self", "self")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the folder was moved into itself")
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "src", "self", "keep.txt")); err != nil || string(b) != "K" {
		t.Fatalf("keep.txt did not move with the batch: %q %v", b, err)
	}

	// sub-paths and escapes in names are skipped, never acted on
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"self/..", "../evil.txt"}})
	if rr.Code != 200 {
		t.Fatalf("move with bad names: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "evil.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an escaped name was created outside the batch")
	}

	// a missing source fails the batch
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"ghost.txt"}})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("move missing source: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// an empty batch has nothing to do
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{}})
	if rr.Code != 400 || rr.Message != "Empty file names" {
		t.Fatalf("move empty: code %d (%s), want 400 Empty file names", rr.Code, rr.Message)
	}
	// a missing endpoint folder is an error, not a move
	rr = move(map[string]any{"src_dir": "/src", "dst_dir": "/nowhere", "names": []string{"keep.txt"}})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("move to missing dir: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
}

func TestRemoteFSCopy(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.mkdir("src/inner")
	e.mkdir("dst2")
	e.mkdir("dst/inner")
	e.writeFile("src/one.txt", "1")
	e.writeFile("src/inner/two.txt", "2")
	e.writeFile("dst/inner/old.txt", "O")
	tok := e.login()
	copyReq := func(body map[string]any) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/copy", tok, body)
	}

	// a plain file copy leaves the source in place
	rr := copyReq(map[string]any{"src_dir": "/src", "dst_dir": "/dst2", "names": []string{"one.txt"}})
	if rr.Code != 200 {
		t.Fatalf("copy file: code %d (%s)", rr.Code, rr.Message)
	}
	for p, want := range map[string]string{"src/one.txt": "1", "dst2/one.txt": "1"} {
		if b, err := os.ReadFile(filepath.Join(e.home, filepath.FromSlash(p))); err != nil || string(b) != want {
			t.Fatalf("%s after copy: %q %v", p, b, err)
		}
	}

	// a directory copies recursively into a fresh place
	rr = copyReq(map[string]any{"src_dir": "/src", "dst_dir": "/dst2", "names": []string{"inner"}})
	if rr.Code != 200 {
		t.Fatalf("copy dir: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "dst2", "inner", "two.txt")); err != nil || string(b) != "2" {
		t.Fatalf("recursive copy: %q %v", b, err)
	}

	// onto an existing directory without merge: the conflict is named
	rr = copyReq(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"inner"}})
	if rr.Code != 403 || rr.Message != "file [inner] exists" {
		t.Fatalf("copy conflict: code %d (%s), want 403 naming the folder", rr.Code, rr.Message)
	}

	// merge combines the trees, keeping what was already there
	rr = copyReq(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"inner"}, "merge": true})
	if rr.Code != 200 {
		t.Fatalf("copy merge: code %d (%s)", rr.Code, rr.Message)
	}
	for p, want := range map[string]string{"dst/inner/old.txt": "O", "dst/inner/two.txt": "2", "src/inner/two.txt": "2"} {
		if b, err := os.ReadFile(filepath.Join(e.home, filepath.FromSlash(p))); err != nil || string(b) != want {
			t.Fatalf("%s after the merge: %q %v", p, b, err)
		}
	}

	// overwrite replaces the destination directory
	rr = copyReq(map[string]any{"src_dir": "/src", "dst_dir": "/dst", "names": []string{"inner"}, "overwrite": true})
	if rr.Code != 200 {
		t.Fatalf("copy overwrite: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "dst", "inner", "old.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("overwrite copy kept the replaced folder's contents")
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "dst", "inner", "two.txt")); err != nil || string(b) != "2" {
		t.Fatalf("overwritten copy: %q %v", b, err)
	}
}

func TestRemoteFSRemove(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.mkdir("docs")
	e.writeFile("docs/gone.txt", "G")
	tok := e.login()

	// a name that is already gone is skipped, not an error
	rr := e.api(http.MethodPost, "/api/fs/remove", tok, map[string]any{"dir": "/docs", "names": []string{"gone.txt", "ghost.txt"}})
	if rr.Code != 200 {
		t.Fatalf("remove: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "docs", "gone.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("remove left the file in place")
	}

	// it landed in ~/.Trash, listed the way the 回收站 page lists it
	entries := trashEntries()
	var hit *Entry
	for i := range entries {
		if entries[i].Name == "gone.txt" {
			hit = &entries[i]
		}
	}
	if hit == nil {
		t.Fatalf("gone.txt is not in the trash: %+v", entries)
	}
	if b, err := os.ReadFile(hit.Path); err != nil || string(b) != "G" {
		t.Fatalf("the trashed file: %q %v", b, err)
	}
	// and the restore log maps it back to where it came from
	if original, ok := e.a.store.originalForTrash("gone.txt"); !ok || original != filepath.Join(e.home, "docs", "gone.txt") {
		t.Fatalf("restore log = %q %v, want the original path", original, ok)
	}

	if rr := e.api(http.MethodPost, "/api/fs/remove", tok, map[string]any{"dir": "/docs", "names": []string{}}); rr.Code != 400 {
		t.Fatalf("remove empty: code %d, want 400", rr.Code)
	}
	if rr := e.api(http.MethodPost, "/api/fs/remove", "", map[string]any{"dir": "/docs", "names": []string{"x"}}); rr.Code != 401 {
		t.Fatalf("guest remove: code %d, want 401", rr.Code)
	}
}

func TestRemoteFSDirs(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.mkdir("alpha")
	e.mkdir("beta/gamma")
	e.mkdir("empty")
	e.writeFile("file.txt", "x")
	e.writeFile("alpha/nested.txt", "n")
	tok := e.login()

	rr := e.api(http.MethodPost, "/api/fs/dirs", tok, map[string]any{"path": "/"})
	if rr.Code != 200 {
		t.Fatalf("dirs: code %d (%s)", rr.Code, rr.Message)
	}
	var dirs []remoteDirEntry
	if err := json.Unmarshal(rr.Data, &dirs); err != nil {
		t.Fatal(err)
	}
	// one level, folders only, sorted
	if len(dirs) != 3 || dirs[0].Name != "alpha" || dirs[1].Name != "beta" || dirs[2].Name != "empty" {
		t.Fatalf("dirs = %+v, want alpha beta empty", dirs)
	}
	if dirs[0].Modified.IsZero() {
		t.Fatal("dirs entries carry no modified time")
	}

	// an empty folder answers an empty JSON array, not null
	rr = e.api(http.MethodPost, "/api/fs/dirs", tok, map[string]any{"path": "/empty"})
	if rr.Code != 200 || string(rr.Data) != "[]" {
		t.Fatalf("empty dirs: code %d data %s, want 200 []", rr.Code, rr.Data)
	}

	// a file is not a folder of folders
	rr = e.api(http.MethodPost, "/api/fs/dirs", tok, map[string]any{"path": "/file.txt"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("dirs on a file: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	if rr := e.api(http.MethodPost, "/api/fs/dirs", "", map[string]any{"path": "/"}); rr.Code != 401 {
		t.Fatalf("guest dirs: code %d, want 401", rr.Code)
	}
}

func TestRemoteFSPut(t *testing.T) {
	e := newRemoteManageEnv(t)
	tok := e.login()

	// guests cannot upload
	rr := e.put("/api/fs/put", "", "/up/file.txt", "", "no")
	if rr.Code != 401 {
		t.Fatalf("guest put: code %d, want 401", rr.Code)
	}

	// the manage page's wire form: encodeURIComponent in File-Path
	rr = e.put("/api/fs/put", tok, remoteJSEscape("/up/file.txt"), "", "hello")
	if rr.Code != 200 {
		t.Fatalf("put: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "up", "file.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("uploaded file: %q %v", b, err)
	}

	// Overwrite:"false" keeps the existing file
	rr = e.put("/api/fs/put", tok, remoteJSEscape("/up/file.txt"), "false", "other")
	if rr.Code != 403 || rr.Message != "file exists" {
		t.Fatalf("put conflict: code %d (%s), want 403 file exists", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "up", "file.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("conflicted put overwrote: %q %v", b, err)
	}

	// the default is overwrite
	rr = e.put("/api/fs/put", tok, remoteJSEscape("/up/file.txt"), "", "replaced")
	if rr.Code != 200 {
		t.Fatalf("put overwrite: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "up", "file.txt")); err != nil || string(b) != "replaced" {
		t.Fatalf("overwriting put: %q %v", b, err)
	}

	// a Chinese path with spaces round-trips, parents created on the way
	vpath := "/上传 文件/中文 文档.txt"
	rr = e.put("/api/fs/put", tok, remoteJSEscape(vpath), "", "中文")
	if rr.Code != 200 {
		t.Fatalf("put Chinese path: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "上传 文件", "中文 文档.txt")); err != nil || string(b) != "中文" {
		t.Fatalf("Chinese upload: %q %v", b, err)
	}
	// and the raw UTF-8 header a curl user would send decodes too
	rr = e.put("/api/fs/put", tok, "/直接.txt", "", "raw")
	if rr.Code != 200 {
		t.Fatalf("put raw path: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "直接.txt")); err != nil || string(b) != "raw" {
		t.Fatalf("raw upload: %q %v", b, err)
	}

	// the path query parameter is the fallback
	rr = e.put("/api/fs/put?path=/up/q.txt", tok, "", "", "query")
	if rr.Code != 200 {
		t.Fatalf("put via query: code %d (%s)", rr.Code, rr.Message)
	}
	if b, err := os.ReadFile(filepath.Join(e.home, "up", "q.txt")); err != nil || string(b) != "query" {
		t.Fatalf("query upload: %q %v", b, err)
	}

	// no path at all, and the virtual root, are refused
	rr = e.put("/api/fs/put", tok, "", "", "x")
	if rr.Code != 400 {
		t.Fatalf("put without a path: code %d, want 400", rr.Code)
	}
	rr = e.put("/api/fs/put", tok, "/", "", "x")
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("put at the root: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
}

func TestRemoteShareCreate(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("docs/a.txt", "A")
	e.mkdir("docs/folder")
	tok := e.login()

	// at least one object is mandatory
	for _, files := range [][]string{{}, {""}} {
		rr := e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": files})
		if rr.Code != 400 || rr.Message != "must add at least 1 object" {
			t.Fatalf("create files %v: code %d (%s), want 400 must add at least 1 object", files, rr.Code, rr.Message)
		}
	}
	// the shared objects must exist
	rr := e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": []string{"/missing.txt"}})
	if rr.Code != 500 || rr.Message != "file not found" {
		t.Fatalf("create missing file: code %d (%s), want 500 file not found", rr.Code, rr.Message)
	}
	if rr := e.api(http.MethodPost, "/api/share/create", "", map[string]any{"files": []string{"/docs"}}); rr.Code != 401 {
		t.Fatalf("guest create: code %d, want 401", rr.Code)
	}

	// a generated id, the password and the cleaned files echo back
	rr = e.api(http.MethodPost, "/api/share/create", tok, map[string]any{
		"files": []string{"docs/a.txt"}, "pwd": "s3cret", "remark": "round trip",
	})
	if rr.Code != 200 {
		t.Fatalf("create: code %d (%s)", rr.Code, rr.Message)
	}
	s := e.shareOf(rr.Data)
	if len(s.ID) != 8 {
		t.Fatalf("generated id %q, want 8 characters", s.ID)
	}
	if s.Pwd != "s3cret" || s.Remark != "round trip" {
		t.Fatalf("echo = pwd %q remark %q", s.Pwd, s.Remark)
	}
	if len(s.Files) != 1 || s.Files[0] != "/docs/a.txt" {
		t.Fatalf("files = %v, want the cleaned virtual path", s.Files)
	}
	if s.Expires != nil || s.Disabled || s.Creator != "admin" || s.CreatorRole != 1 {
		t.Fatalf("fresh share = %+v", s)
	}

	// a custom id is kept, CJK included; invalid or taken ids are not
	rr = e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"id": "自定义-1", "files": []string{"/docs"}})
	if rr.Code != 200 || e.shareOf(rr.Data).ID != "自定义-1" {
		t.Fatalf("custom id: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"id": "bad id!", "files": []string{"/docs"}})
	if rr.Code != 400 || rr.Message != "invalid share id" {
		t.Fatalf("invalid id: code %d (%s), want 400 invalid share id", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"id": "自定义-1", "files": []string{"/docs"}})
	if rr.Code != 500 || rr.Message != "sharing already exist" {
		t.Fatalf("duplicate id: code %d (%s), want 500 sharing already exist", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": []string{"/docs"}, "expires": "not-a-time"})
	if rr.Code != 400 || rr.Message != "invalid expires" {
		t.Fatalf("bad expires: code %d (%s), want 400 invalid expires", rr.Code, rr.Message)
	}

	// the share persists: the state file the store writes reloads with the
	// share intact
	reloaded := loadStateFile(e.a.store.path)
	if len(reloaded.Shares) != 2 || reloaded.Shares[0].ID != "自定义-1" || reloaded.Shares[1].ID != s.ID {
		t.Fatalf("reloaded shares = %+v", reloaded.Shares)
	}
}

func TestRemoteShareListUpdateDelete(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("a.txt", "A")
	e.writeFile("b.txt", "B")
	e.writeFile("c.txt", "C")
	tok := e.login()
	share := func(file string) string {
		rr := e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": []string{file}})
		if rr.Code != 200 {
			t.Fatalf("share %s: code %d (%s)", file, rr.Code, rr.Message)
		}
		return e.shareOf(rr.Data).ID
	}
	id1, id2, id3 := share("/a.txt"), share("/b.txt"), share("/c.txt")

	// the list is newest first
	rr := e.api(http.MethodPost, "/api/share/list", tok, map[string]any{"page": 1, "per_page": 0})
	if rr.Code != 200 {
		t.Fatalf("list: code %d (%s)", rr.Code, rr.Message)
	}
	var data remoteShareListData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 3 || len(data.Content) != 3 ||
		data.Content[0].ID != id3 || data.Content[1].ID != id2 || data.Content[2].ID != id1 {
		t.Fatalf("list = %+v, want %s %s %s newest first", data, id3, id2, id1)
	}

	// pagination over the same order, from the body and the query alike
	rr = e.api(http.MethodPost, "/api/share/list", tok, map[string]any{"page": 1, "per_page": 2})
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 3 || len(data.Content) != 2 || data.Content[0].ID != id3 || data.Content[1].ID != id2 {
		t.Fatalf("page 1 = %+v", data)
	}
	rr = e.api(http.MethodPost, "/api/share/list", tok, map[string]any{"page": 2, "per_page": 2})
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Content) != 1 || data.Content[0].ID != id1 {
		t.Fatalf("page 2 = %+v", data)
	}
	rr = e.api(http.MethodPost, "/api/share/list?page=1&per_page=1", tok, nil)
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Content) != 1 || data.Content[0].ID != id3 {
		t.Fatalf("query pagination = %+v", data)
	}
	rr = e.api(http.MethodPost, "/api/share/list", tok, map[string]any{"page": 9, "per_page": 2})
	var page struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rr.Data, &page); err != nil {
		t.Fatal(err)
	}
	if string(page.Content) != "null" {
		t.Fatalf("page past the end: content %s, want null", page.Content)
	}
	if rr := e.api(http.MethodPost, "/api/share/list", "", nil); rr.Code != 401 {
		t.Fatalf("guest list: code %d, want 401", rr.Code)
	}

	// get: one share, or the protocol's 404
	rr = e.api(http.MethodGet, "/api/share/get?id="+id2, tok, nil)
	if rr.Code != 200 || e.shareOf(rr.Data).ID != id2 {
		t.Fatalf("get: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodGet, "/api/share/get?id=nope", tok, nil)
	if rr.Code != 404 || rr.Message != "sharing not found" {
		t.Fatalf("get missing: code %d (%s), want 404 sharing not found", rr.Code, rr.Message)
	}

	// update replaces wholesale and renames on new_id, keeping the slot
	rr = e.api(http.MethodPost, "/api/share/update", tok, map[string]any{"id": id1})
	if rr.Code != 400 || rr.Message != "must add at least 1 object" {
		t.Fatalf("update without files: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/update", tok, map[string]any{"id": "nope", "files": []string{"/a.txt"}})
	if rr.Code != 404 || rr.Message != "sharing not found" {
		t.Fatalf("update missing: code %d (%s), want 404 sharing not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/update", tok, map[string]any{"id": id2, "new_id": "renamed", "files": []string{"/b.txt"}})
	if rr.Code != 200 || e.shareOf(rr.Data).ID != "renamed" {
		t.Fatalf("update rename: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodGet, "/api/share/get?id=renamed", tok, nil)
	if rr.Code != 200 || e.shareOf(rr.Data).ID != "renamed" {
		t.Fatalf("the renamed share is missing: %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodGet, "/api/share/get?id="+id2, tok, nil); rr.Code != 404 {
		t.Fatalf("the old id survived the rename: %d", rr.Code)
	}
	rr = e.api(http.MethodPost, "/api/share/update", tok, map[string]any{"id": "renamed", "new_id": id3, "files": []string{"/b.txt"}})
	if rr.Code != 500 || rr.Message != "sharing id already exists" {
		t.Fatalf("rename onto a taken id: code %d (%s)", rr.Code, rr.Message)
	}

	// enable/disable gate the guest side
	rr = e.api(http.MethodPost, "/api/share/disable?id="+id3, tok, nil)
	if rr.Code != 200 {
		t.Fatalf("disable: code %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + id3}); rr.Code != 500 {
		t.Fatalf("disabled share browse: code %d, want 500", rr.Code)
	}
	rr = e.api(http.MethodPost, "/api/share/enable?id="+id3, tok, nil)
	if rr.Code != 200 {
		t.Fatalf("enable: code %d (%s)", rr.Code, rr.Message)
	}
	if rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + id3}); rr.Code != 200 {
		t.Fatalf("re-enabled share browse: code %d (%s)", rr.Code, rr.Message)
	}

	// delete drops the share; gone ids are 404s
	rr = e.api(http.MethodPost, "/api/share/delete?id="+id1, tok, nil)
	if rr.Code != 200 {
		t.Fatalf("delete: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/share/list", tok, nil)
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 2 {
		t.Fatalf("total after delete = %d, want 2", data.Total)
	}
	for _, call := range []string{"/api/share/delete?id=" + id1, "/api/share/disable?id=ghost", "/api/share/enable?id=ghost"} {
		if rr = e.api(http.MethodPost, call, tok, nil); rr.Code != 404 || rr.Message != "sharing not found" {
			t.Fatalf("%s: code %d (%s), want 404 sharing not found", call, rr.Code, rr.Message)
		}
	}
}

func TestRemoteShareGuestBrowse(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("docs/a.txt", "A")
	e.writeFile("docs/b.txt", "B")
	e.mkdir("docs/sub")
	e.writeFile("docs/sub/inner.txt", "I")
	e.writeFile("docs/solitary.txt", "S")
	e.mkdir("@settings") // a real folder whose name starts with "@s"
	e.writeFile("@settings/real.txt", "R")
	tok := e.login()
	share := func(files ...string) string {
		rr := e.api(http.MethodPost, "/api/share/create", tok, map[string]any{"files": files})
		if rr.Code != 200 {
			t.Fatalf("share %v: code %d (%s)", files, rr.Code, rr.Message)
		}
		return e.shareOf(rr.Data).ID
	}
	multi := share("/docs/a.txt", "/docs/b.txt", "/docs/sub")
	single := share("/docs/solitary.txt")

	// no login at all browses the share root: the shared entries themselves,
	// folders first, and never a download sign
	rr := e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + multi, "page": 1, "per_page": 0})
	if rr.HTTPStatus != 200 || rr.Code != 200 {
		t.Fatalf("guest list: HTTP %d code %d (%s)", rr.HTTPStatus, rr.Code, rr.Message)
	}
	var data remoteFSListData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 3 || len(data.Content) != 3 {
		t.Fatalf("share root = total %d %+v", data.Total, data.Content)
	}
	if !data.Content[0].IsDir || data.Content[0].Name != "sub" ||
		data.Content[1].Name != "a.txt" || data.Content[2].Name != "b.txt" {
		t.Fatalf("share root order = %+v", data.Content)
	}
	for _, o := range data.Content {
		if o.Sign != "" {
			t.Fatalf("%s carries a sign %q inside a share", o.Name, o.Sign)
		}
	}
	if data.Write || data.Provider != "unknown" {
		t.Fatalf("share list is writable or has a provider: %+v", data)
	}

	// the share root is a virtual folder named after the share
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + multi})
	if rr.Code != 200 {
		t.Fatalf("share root get: code %d (%s)", rr.Code, rr.Message)
	}
	var get remoteFSGetData
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if !get.IsDir || get.Name != multi || get.Sign != "" || get.RawURL != "" {
		t.Fatalf("share root get = %+v", get)
	}

	// a file get answers the /sd raw_url, signed never
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + multi + "/a.txt"})
	if rr.Code != 200 {
		t.Fatalf("share file get: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if get.IsDir || get.Sign != "" {
		t.Fatalf("share file get = %+v", get)
	}
	if want := "/sd/" + multi + "/a.txt"; !strings.Contains(get.RawURL, want) {
		t.Fatalf("raw_url = %q, want it pointing at %s", get.RawURL, want)
	}

	// a shared folder lists its contents below the share
	rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + multi + "/sub"})
	if rr.Code != 200 {
		t.Fatalf("share sub list: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 1 || len(data.Content) != 1 || data.Content[0].Name != "inner.txt" {
		t.Fatalf("share sub list = %+v", data)
	}

	// the whole-sub-path form — links shaped like the real ones — resolves too
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + multi + "/docs/sub/inner.txt"})
	if rr.Code != 200 {
		t.Fatalf("full-subpath get: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if get.IsDir || !strings.Contains(get.RawURL, "/sd/"+multi+"/docs/sub/inner.txt") {
		t.Fatalf("full-subpath get = %+v", get)
	}

	// a single-file share: the root lists the file, the root get is the
	// virtual folder
	rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + single})
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 1 || data.Content[0].Name != "solitary.txt" || data.Content[0].IsDir {
		t.Fatalf("single-file root list = %+v", data)
	}
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + single})
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if !get.IsDir || get.Name != single {
		t.Fatalf("single-file root get = %+v, want the virtual folder", get)
	}
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + single + "/solitary.txt"})
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if get.IsDir || !strings.Contains(get.RawURL, "/sd/"+single+"/solitary.txt") {
		t.Fatalf("single-file get = %+v", get)
	}

	// unknown ids, uncovered paths and the bare /@s are the protocol's errors
	for path, want := range map[string]struct {
		code int
		msg  string
	}{
		"/@s/ghost":                       {500, "the share does not exist"},
		"/@s/" + multi + "/uncovered.txt": {500, "object not found"},
		"/@s":                             {400, "invalid share id"},
	} {
		rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": path})
		if rr.Code != want.code || rr.Message != want.msg {
			t.Fatalf("guest list %s: code %d (%s), want %d %s", path, rr.Code, rr.Message, want.code, want.msg)
		}
	}

	// the prefix trap: an ordinary folder named @settings stays browsable by
	// the admin, with its ordinary signed downloads
	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/@settings"})
	if rr.Code != 200 {
		t.Fatalf("list @settings: code %d (%s), want the ordinary folder", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/get", tok, map[string]any{"path": "/@settings/real.txt"})
	if rr.Code != 200 {
		t.Fatalf("get @settings/real.txt: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if get.Sign == "" || !strings.Contains(get.RawURL, "/p/") {
		t.Fatalf("@settings/real.txt get = %+v, want the ordinary signed /p link", get)
	}
	// and it is not the guests' folder
	if rr = e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@settings"}); rr.Code != 401 {
		t.Fatalf("guest list @settings: code %d, want 401", rr.Code)
	}
}

func TestRemoteSharePasswordAndLimits(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("secret.txt", "S")
	e.writeFile("open.txt", "O")
	tok := e.login()
	share := func(body map[string]any) string {
		rr := e.api(http.MethodPost, "/api/share/create", tok, body)
		if rr.Code != 200 {
			t.Fatalf("share %v: code %d (%s)", body, rr.Code, rr.Message)
		}
		return e.shareOf(rr.Data).ID
	}
	list := func(sid, password string) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/list", "", map[string]any{"path": "/@s/" + sid, "password": password})
	}

	locked := share(map[string]any{"files": []string{"/secret.txt"}, "pwd": "s3cret"})
	if rr := list(locked, ""); rr.Code != 403 || rr.Message != "wrong share code" {
		t.Fatalf("locked without code: code %d (%s), want 403 wrong share code", rr.Code, rr.Message)
	}
	if rr := list(locked, "wrong"); rr.Code != 403 || rr.Message != "wrong share code" {
		t.Fatalf("locked with a wrong code: code %d (%s), want 403 wrong share code", rr.Code, rr.Message)
	}
	if rr := list(locked, "s3cret"); rr.Code != 200 {
		t.Fatalf("locked with the code: code %d (%s)", rr.Code, rr.Message)
	}

	// fs/get shares the same check and links the password into /sd
	rr := e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + locked + "/secret.txt"})
	if rr.Code != 403 || rr.Message != "wrong share code" {
		t.Fatalf("locked get without code: code %d (%s)", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + locked + "/secret.txt", "password": "s3cret"})
	if rr.Code != 200 {
		t.Fatalf("locked get with the code: code %d (%s)", rr.Code, rr.Message)
	}
	var lockedGet remoteFSGetData
	if err := json.Unmarshal(rr.Data, &lockedGet); err != nil {
		t.Fatal(err)
	}
	if want := "/sd/" + locked + "/secret.txt?pwd=s3cret"; !strings.Contains(lockedGet.RawURL, want) {
		t.Fatalf("raw_url = %q, want %s inside", lockedGet.RawURL, want)
	}

	// an expired share is no longer valid
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	expired := share(map[string]any{"files": []string{"/open.txt"}, "expires": past})
	if rr := list(expired, ""); rr.Code != 500 || rr.Message != "the share has expired or is no longer valid" {
		t.Fatalf("expired: code %d (%s), want 500", rr.Code, rr.Message)
	}

	// a disabled share, same answer
	disabled := share(map[string]any{"files": []string{"/open.txt"}})
	if rr := e.api(http.MethodPost, "/api/share/disable?id="+disabled, tok, nil); rr.Code != 200 {
		t.Fatalf("disable: code %d (%s)", rr.Code, rr.Message)
	}
	if rr := list(disabled, ""); rr.Code != 500 || rr.Message != "the share has expired or is no longer valid" {
		t.Fatalf("disabled: code %d (%s), want 500", rr.Code, rr.Message)
	}

	// the visit cap lets one guest visit through
	capped := share(map[string]any{"files": []string{"/open.txt"}, "max_accessed": 1})
	if rr := list(capped, ""); rr.Code != 200 {
		t.Fatalf("capped first visit: code %d (%s)", rr.Code, rr.Message)
	}
	if rr := list(capped, ""); rr.Code != 500 || rr.Message != "the share has expired or is no longer valid" {
		t.Fatalf("capped second visit: code %d (%s), want 500", rr.Code, rr.Message)
	}

	// the visitor counter runs once per IP per half hour
	plain := share(map[string]any{"files": []string{"/open.txt"}})
	for range 4 {
		if rr := list(plain, ""); rr.Code != 200 {
			t.Fatalf("plain visits: code %d (%s)", rr.Code, rr.Message)
		}
	}
	rr = e.api(http.MethodGet, "/api/share/get?id="+capped, tok, nil)
	if s := e.shareOf(rr.Data); s.Accessed != 1 {
		t.Fatalf("capped accessed = %d, want 1", s.Accessed)
	}
	rr = e.api(http.MethodGet, "/api/share/get?id="+plain, tok, nil)
	if s := e.shareOf(rr.Data); s.Accessed != 1 {
		t.Fatalf("plain accessed = %d after four visits, want 1 per IP per half hour", s.Accessed)
	}
}

func TestRemoteShareDownload(t *testing.T) {
	e := newRemoteManageEnv(t)
	e.writeFile("docs/dl.txt", remoteTestBody)
	tok := e.login()
	share := func(body map[string]any) string {
		rr := e.api(http.MethodPost, "/api/share/create", tok, body)
		if rr.Code != 200 {
			t.Fatalf("share %v: code %d (%s)", body, rr.Code, rr.Message)
		}
		return e.shareOf(rr.Data).ID
	}
	free := share(map[string]any{"files": []string{"/docs/dl.txt"}})
	locked := share(map[string]any{"files": []string{"/docs/dl.txt"}, "pwd": "pass"})
	folder := share(map[string]any{"files": []string{"/docs"}})

	// the whole file, no sign, no login
	res, body := e.do(http.MethodGet, "/sd/"+free+"/dl.txt", nil)
	if res.StatusCode != 200 || body != remoteTestBody {
		t.Fatalf("sd download: status %d body %q", res.StatusCode, body)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") ||
		!strings.Contains(res.Header.Get("Content-Disposition"), "dl.txt") {
		t.Fatalf("sd headers: %q %q", res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"))
	}

	// a range request: 206 with the tail and the precise Content-Range
	res, body = e.do(http.MethodGet, "/sd/"+free+"/dl.txt", map[string]string{"Range": "bytes=10-"})
	if res.StatusCode != 206 || body != remoteTestBody[10:] {
		t.Fatalf("sd range: status %d body %q", res.StatusCode, body)
	}
	if cr := res.Header.Get("Content-Range"); cr != "bytes 10-25/26" {
		t.Fatalf("sd range: content-range %q", cr)
	}

	// HEAD answers the length, never a body
	res, body = e.do(http.MethodHead, "/sd/"+free+"/dl.txt", nil)
	if res.StatusCode != 200 || res.ContentLength != int64(len(remoteTestBody)) || body != "" {
		t.Fatalf("sd head: status %d len %d body %q", res.StatusCode, res.ContentLength, body)
	}

	// the share root and a shared folder are not files
	for _, p := range []string{"/sd/" + free, "/sd/" + free + "/", "/sd/" + folder + "/docs"} {
		res, body = e.do(http.MethodGet, p, nil)
		if res.StatusCode != 500 || !strings.Contains(body, "not a file") {
			t.Fatalf("%s: status %d body %q, want the 500 not a file page", p, res.StatusCode, body)
		}
	}

	// a wrong password is OpenList's HTML error page with the protocol code
	res, body = e.do(http.MethodGet, "/sd/"+locked+"/dl.txt", nil)
	if res.StatusCode != 403 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "wrong share code") {
		t.Fatalf("locked without the code: status %d type %q body %q", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	// the password opens it, in the query parameter the links carry
	res, body = e.do(http.MethodGet, "/sd/"+locked+"/dl.txt?pwd=pass", nil)
	if res.StatusCode != 200 || body != remoteTestBody {
		t.Fatalf("locked download: status %d body %q", res.StatusCode, body)
	}
	// and the raw_url fs/get builds fetches as the guest
	rr := e.api(http.MethodPost, "/api/fs/get", "", map[string]any{"path": "/@s/" + locked + "/dl.txt", "password": "pass"})
	var get remoteFSGetData
	if err := json.Unmarshal(rr.Data, &get); err != nil {
		t.Fatal(err)
	}
	if res, body = e.fetch(get.RawURL); res.StatusCode != 200 || body != remoteTestBody {
		t.Fatalf("raw_url download: status %d body %q", res.StatusCode, body)
	}

	// an unknown share and an uncovered path are error pages
	res, body = e.do(http.MethodGet, "/sd/ghost/x.txt", nil)
	if res.StatusCode != 500 || !strings.Contains(body, "the share does not exist") {
		t.Fatalf("unknown share: status %d body %q", res.StatusCode, body)
	}
	res, body = e.do(http.MethodGet, "/sd/"+free+"/other.txt", nil)
	if res.StatusCode != 500 || !strings.Contains(body, "file not found") {
		t.Fatalf("uncovered path: status %d body %q", res.StatusCode, body)
	}
}

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
