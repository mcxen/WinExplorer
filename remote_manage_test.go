package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// remote_manage_test.go is the manage-round harness plus /api/fs write
// endpoint tests (mkdir/rename/move/copy/remove/dirs/put). Share cases live
// in remote_share_api_test.go; sunvale admin / manage smoke / batch rename
// in remote_admin_api_test.go (LAY1 split).

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
