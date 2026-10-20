package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sunvale/internal/remote"
)

// remote_ext_test.go drives the manage console's shell extension surface
// over the same real-HTTP harness as remote_test.go: the ext list and run
// endpoints (remote_ext.go) against a seeded extensions registry, the
// validation paths of the terminal endpoint — a real Terminal window is
// never opened — and the global flag of /api/fs/search.

// remoteSeedExtensions writes the registry file the ext endpoints read,
// under the temporary HOME's config dir, so loadShellExtensions sees it on
// the next request. It skips when the platform's config dir does not follow
// HOME (Windows), the way the HOME guards in remote_test.go do.
func remoteSeedExtensions(t *testing.T, e *remoteTestEnv, exts []shellExt) {
	t.Helper()
	dir, err := os.UserConfigDir()
	if err != nil || !strings.HasPrefix(dir, e.home) {
		t.Skip("UserConfigDir does not follow HOME on this platform")
	}
	if err := os.MkdirAll(filepath.Join(dir, "winexplorer"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(exts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "winexplorer", "extensions.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteExtList(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Documents")
	e.writeFile("Documents/notes.txt", "N")
	remoteSeedExtensions(t, e, []shellExt{
		{Name: "列出目录", Match: []string{"dir"}, Command: "ls -la {path}"},
		{Name: "用终端打开", Match: []string{"dir"}, Command: "cd \"{path}\"", Terminal: true},
		{Name: "查看文本", Match: []string{"txt"}, Command: "cat \"{path}\""},
	})

	// guests cannot list the extensions
	rr := e.api(http.MethodPost, "/api/fs/ext/list", "", map[string]any{"path": "/Documents", "name": "Documents"})
	if rr.Code != 401 {
		t.Fatalf("guest ext/list: code %d, want 401", rr.Code)
	}

	tok := e.login()

	// a folder gets the dir-matching extensions, in registration order
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/Documents", "name": "Documents"})
	if rr.Code != 200 {
		t.Fatalf("ext/list dir: code %d (%s)", rr.Code, rr.Message)
	}
	var data remoteExtListData
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Exts) != 2 || data.Exts[0].Name != "列出目录" || data.Exts[1].Name != "用终端打开" {
		t.Fatalf("dir exts = %+v", data.Exts)
	}
	if data.Exts[0].Terminal || !data.Exts[1].Terminal {
		t.Fatalf("dir terminal flags = %+v", data.Exts)
	}

	// a file gets the txt-matching one, its extension taken from the name
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/Documents/notes.txt", "name": "notes.txt"})
	if rr.Code != 200 {
		t.Fatalf("ext/list file: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Exts) != 1 || data.Exts[0].Name != "查看文本" || data.Exts[0].Terminal {
		t.Fatalf("file exts = %+v", data.Exts)
	}

	// an empty name falls back to the disk path's base
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/Documents/notes.txt"})
	if rr.Code != 200 {
		t.Fatalf("ext/list nameless: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Exts) != 1 || data.Exts[0].Name != "查看文本" {
		t.Fatalf("nameless exts = %+v", data.Exts)
	}

	// nothing matches: an empty list, not null
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/Documents/notes.txt", "name": "other.md"})
	if rr.Code != 200 {
		t.Fatalf("ext/list unmatched: code %d (%s)", rr.Code, rr.Message)
	}
	if err := json.Unmarshal(rr.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Exts == nil || len(data.Exts) != 0 {
		t.Fatalf("unmatched exts = %#v, want []", data.Exts)
	}

	// validation: a missing path, an escaping one and a missing object
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"name": "x"})
	if rr.Code != 400 {
		t.Fatalf("ext/list without path: code %d, want 400", rr.Code)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/../evil", "name": "x"})
	if rr.Code != 400 || rr.Message != "invalid path" {
		t.Fatalf("ext/list escaping: code %d (%s), want 400 invalid path", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, map[string]any{"path": "/ghost.txt", "name": "ghost.txt"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("ext/list missing: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// a body that is not the request's shape is its own error
	rr = e.api(http.MethodPost, "/api/fs/ext/list", tok, []any{})
	if rr.Code != 400 {
		t.Fatalf("ext/list bad body: code %d, want 400", rr.Code)
	}
}

func TestRemoteExtRun(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.writeFile("Documents/notes.txt", "N")
	marker := filepath.Join(t.TempDir(), "marker")
	remoteSeedExtensions(t, e, []shellExt{
		{Name: "建标记文件", Match: []string{"*"}, Command: fmt.Sprintf("touch %q", marker)},
		{Name: "仅目录", Match: []string{"dir"}, Command: "true"},
	})
	tok := e.login()

	// guests cannot run extensions
	rr := e.api(http.MethodPost, "/api/fs/ext/run", "", map[string]any{
		"path": "/Documents/notes.txt", "name": "notes.txt", "ext_name": "建标记文件",
	})
	if rr.Code != 401 {
		t.Fatalf("guest ext/run: code %d, want 401", rr.Code)
	}

	// the run is detached: the reply is immediate, the marker lands later
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/Documents/notes.txt", "name": "notes.txt", "ext_name": "建标记文件",
	})
	if rr.Code != 200 {
		t.Fatalf("ext/run: code %d (%s)", rr.Code, rr.Message)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the marker landed before the reply, the run was not detached")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatal("ext/run never created the marker file")
		} else {
			time.Sleep(20 * time.Millisecond)
		}
	}

	// validation: no ext_name, an unknown one, one that does not match the
	// item, an escaping path and a missing object
	for _, extName := range []string{"", "   "} {
		rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
			"path": "/Documents/notes.txt", "name": "notes.txt", "ext_name": extName,
		})
		if rr.Code != 400 {
			t.Fatalf("ext/run ext_name %q: code %d, want 400", extName, rr.Code)
		}
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/Documents/notes.txt", "name": "notes.txt", "ext_name": "不存在",
	})
	if rr.Code != 500 || rr.Message != "extension not found" {
		t.Fatalf("ext/run unknown: code %d (%s), want 500 extension not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/Documents/notes.txt", "name": "notes.txt", "ext_name": "仅目录",
	})
	if rr.Code != 500 || rr.Message != "extension not found" {
		t.Fatalf("ext/run non-matching: code %d (%s), want 500 extension not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/../evil", "name": "x", "ext_name": "建标记文件",
	})
	if rr.Code != 400 || rr.Message != "invalid path" {
		t.Fatalf("ext/run escaping: code %d (%s), want 400 invalid path", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/ghost.txt", "name": "ghost.txt", "ext_name": "建标记文件",
	})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("ext/run missing: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/ext/run", tok, map[string]any{
		"path": "/Documents/notes.txt", "name": "notes.txt",
	})
	if rr.Code != 400 {
		t.Fatalf("ext/run without ext_name: code %d, want 400", rr.Code)
	}
	// and a guest never reaches any of the above
	if rr = e.api(http.MethodPost, "/api/fs/ext/run", "", map[string]any{"path": "/x", "ext_name": "x"}); rr.Code != 401 {
		t.Fatalf("guest ext/run without fields: code %d, want 401", rr.Code)
	}
}

func TestRemoteTerminalValidation(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Documents")
	e.writeFile("Documents/notes.txt", "N")

	// guests cannot open a terminal
	rr := e.api(http.MethodPost, "/api/fs/terminal", "", map[string]any{"path": "/Documents"})
	if rr.Code != 401 {
		t.Fatalf("guest terminal: code %d, want 401", rr.Code)
	}

	// the validation paths only — a real Terminal window is never opened
	tok := e.login()
	rr = e.api(http.MethodPost, "/api/fs/terminal", tok, map[string]any{})
	if rr.Code != 400 {
		t.Fatalf("terminal without path: code %d, want 400", rr.Code)
	}
	rr = e.api(http.MethodPost, "/api/fs/terminal", tok, map[string]any{"path": "/../evil"})
	if rr.Code != 400 || rr.Message != "invalid path" {
		t.Fatalf("terminal escaping: code %d (%s), want 400 invalid path", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/terminal", tok, map[string]any{"path": "/ghost"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("terminal missing: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	rr = e.api(http.MethodPost, "/api/fs/terminal", tok, map[string]any{"path": "/Documents/notes.txt"})
	if rr.Code != 400 {
		t.Fatalf("terminal on a file: code %d, want 400", rr.Code)
	}
	rr = e.api(http.MethodPost, "/api/fs/terminal", tok, []any{})
	if rr.Code != 400 {
		t.Fatalf("terminal bad body: code %d, want 400", rr.Code)
	}
}

func TestRemoteFSSearchGlobal(t *testing.T) {
	e := newRemoteTestEnv(t)
	e.mkdir("Documents/deep")
	e.writeFile("Documents/deep/global-hit-深.txt", "G")

	tok := e.login()
	search := func(global bool) map[string]remoteFSSearchItem {
		t.Helper()
		rr := e.api(http.MethodPost, "/api/fs/search", tok, map[string]any{
			"parent": "/", "keywords": "global-hit", "global": global, "scope": 0, "page": 1, "per_page": 100,
		})
		if rr.Code != 200 {
			t.Fatalf("search global %v: code %d (%s)", global, rr.Code, rr.Message)
		}
		var data remoteFSSearchData
		if err := json.Unmarshal(rr.Data, &data); err != nil {
			t.Fatal(err)
		}
		out := map[string]remoteFSSearchItem{}
		for _, it := range data.Content {
			out[it.Name] = it
		}
		return out
	}

	// global:true answers with the nested hit under the virtual root
	hits := search(true)
	if len(hits) != 1 {
		t.Fatalf("global search: %d hits, want 1 (%v)", len(hits), hits)
	}
	if it := hits["global-hit-深.txt"]; it.Parent != "/Documents/deep" || it.IsDir || it.Type != remote.FSTypeText {
		t.Fatalf("global hit = %+v", it)
	}
	// the plain search keeps working without the flag
	if hits = search(false); len(hits) != 1 {
		t.Fatalf("plain search: %d hits, want 1 (%v)", len(hits), hits)
	}
}
