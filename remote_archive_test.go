package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// remote_archive_test.go drives the archive half of the remote server over
// the same real-HTTP harness as remote_manage_test.go: /api/fs/archive/
// unzip and /api/fs/archive/zip, the extension list the clients read, the
// Explorer naming rules and the zip-slip refusal.

// writeZipArchive builds a ZIP at path from member name → content. Members
// ending in "/" become folders, which is how the crafted traversal cases
// get in.
func writeZipArchive(t *testing.T, path string, members map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// archiveResult decodes one archive endpoint reply.
func archiveResult(t *testing.T, rr remoteTestResp) remoteArchiveResult {
	t.Helper()
	var res remoteArchiveResult
	if err := json.Unmarshal(rr.Data, &res); err != nil {
		t.Fatalf("archive reply %s: %v", rr.Data, err)
	}
	return res
}

// readHome reads a file below the virtual root.
func readHome(t *testing.T, home string, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestRemoteArchiveUnzip(t *testing.T) {
	e := newRemoteManageEnv(t)
	tok := e.login()
	e.mkdir("arch")
	e.writeFile("arch/notes.txt", "N")
	writeZipArchive(t, filepath.Join(e.home, "arch", "pack.zip"), map[string]string{
		"a.txt":     "hello",
		"sub/b.txt": "world",
	})
	unzip := func(body map[string]any) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/archive/unzip", tok, body)
	}

	// a guest cannot extract
	if rr := e.api(http.MethodPost, "/api/fs/archive/unzip", "", map[string]any{"src_dir": "/arch", "name": "pack.zip"}); rr.Code != 401 {
		t.Fatalf("guest unzip: code %d (%s), want 401", rr.Code, rr.Message)
	}

	// dst_dir defaults to src_dir, the folder is named after the archive
	rr := unzip(map[string]any{"src_dir": "/arch", "name": "pack.zip"})
	if rr.Code != 200 {
		t.Fatalf("unzip: code %d (%s)", rr.Code, rr.Message)
	}
	res := archiveResult(t, rr)
	if len(res.Created) != 1 || res.Created[0] != "/arch/pack" {
		t.Fatalf("created = %v, want [/arch/pack]", res.Created)
	}
	if len(res.Failed) != 0 || res.Reason != "" {
		t.Fatalf("failed = %v reason = %q, want a clean run", res.Failed, res.Reason)
	}
	if got := readHome(t, e.home, "arch/pack/a.txt"); got != "hello" {
		t.Fatalf("arch/pack/a.txt = %q, want hello", got)
	}
	if got := readHome(t, e.home, "arch/pack/sub/b.txt"); got != "world" {
		t.Fatalf("arch/pack/sub/b.txt = %q, want world", got)
	}

	// a name already in use gets Explorer's 副本 suffix, not a failure
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "pack.zip"})
	if rr.Code != 200 {
		t.Fatalf("second unzip: code %d (%s)", rr.Code, rr.Message)
	}
	if res = archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/arch/pack - 副本" {
		t.Fatalf("created = %v, want [/arch/pack - 副本]", res.Created)
	}

	// dst_dir sends the folder somewhere else
	e.mkdir("out")
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "pack.zip", "dst_dir": "/out"})
	if rr.Code != 200 {
		t.Fatalf("unzip to dst_dir: code %d (%s)", rr.Code, rr.Message)
	}
	if res = archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/out/pack" {
		t.Fatalf("created = %v, want [/out/pack]", res.Created)
	}
	if got := readHome(t, e.home, "out/pack/a.txt"); got != "hello" {
		t.Fatalf("out/pack/a.txt = %q, want hello", got)
	}

	// a batch reports what it left alone instead of failing outright
	rr = unzip(map[string]any{"src_dir": "/arch", "names": []string{"pack.zip", "notes.txt"}})
	if rr.Code != 200 {
		t.Fatalf("batch unzip: code %d (%s)", rr.Code, rr.Message)
	}
	res = archiveResult(t, rr)
	if len(res.Created) != 1 || len(res.Failed) != 1 || res.Failed[0] != "notes.txt" {
		t.Fatalf("created = %v failed = %v, want one of each", res.Created, res.Failed)
	}
	if !strings.Contains(res.Reason, "notes.txt") {
		t.Fatalf("reason = %q, want it to name notes.txt", res.Reason)
	}

	// an archive that is not there fails the single-target request
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "ghost.zip"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("ghost unzip: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// a folder is not an archive
	e.mkdir("arch/folder")
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "folder"})
	if rr.Code != 500 || !strings.Contains(rr.Message, "is not an archive") {
		t.Fatalf("folder unzip: code %d (%s), want a not-an-archive error", rr.Code, rr.Message)
	}
	// nothing to do
	rr = e.api(http.MethodPost, "/api/fs/archive/unzip", tok, map[string]any{"src_dir": "/arch"})
	if rr.Code != 400 || rr.Message != "Empty file names" {
		t.Fatalf("empty unzip: code %d (%s), want 400 Empty file names", rr.Code, rr.Message)
	}
	// a destination that is not a folder
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "pack.zip", "dst_dir": "/nowhere"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("unzip to nowhere: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// sub-paths and escapes in names are skipped, never acted on
	rr = unzip(map[string]any{"src_dir": "/arch", "names": []string{"../pack.zip"}})
	if rr.Code != 400 && rr.Code != 500 {
		t.Fatalf("escaped unzip name: code %d (%s), want a refusal", rr.Code, rr.Message)
	}

	// a traversal member is refused and the half-extracted folder is gone:
	// the evil entry sits second so the first one has already landed
	writeZipArchive(t, filepath.Join(e.home, "arch", "slip.zip"), map[string]string{
		"ok.txt":      "fine",
		"../evil.txt": "bad",
	})
	rr = unzip(map[string]any{"src_dir": "/arch", "name": "slip.zip"})
	if rr.Code != 500 || !strings.Contains(rr.Message, "不安全") {
		t.Fatalf("zip-slip: code %d (%s), want the unsafe-path refusal", rr.Code, rr.Message)
	}
	if _, err := os.Lstat(filepath.Join(e.home, "evil.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a traversal member escaped the virtual root")
	}
	if _, err := os.Lstat(filepath.Join(e.home, "arch", "slip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the half-extracted folder was left behind")
	}
}

func TestRemoteArchiveZip(t *testing.T) {
	e := newRemoteManageEnv(t)
	tok := e.login()
	e.mkdir("zip/folder")
	e.writeFile("zip/a.txt", "A")
	e.writeFile("zip/folder/b.txt", "B")
	zipIt := func(body map[string]any) remoteTestResp {
		return e.api(http.MethodPost, "/api/fs/archive/zip", tok, body)
	}

	if rr := e.api(http.MethodPost, "/api/fs/archive/zip", "", map[string]any{"src_dir": "/zip", "names": []string{"a.txt"}}); rr.Code != 401 {
		t.Fatalf("guest zip: code %d (%s), want 401", rr.Code, rr.Message)
	}

	// a single folder packs under its own name, recursively
	rr := zipIt(map[string]any{"src_dir": "/zip", "names": []string{"folder"}})
	if rr.Code != 200 {
		t.Fatalf("zip folder: code %d (%s)", rr.Code, rr.Message)
	}
	if res := archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/zip/folder.zip" {
		t.Fatalf("created = %v, want [/zip/folder.zip]", res.Created)
	}
	zr, err := zip.OpenReader(filepath.Join(e.home, "zip", "folder.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]string{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, _ := rc.Read(buf)
		rc.Close()
		got[f.Name] = string(buf[:n])
	}
	if len(got) != 1 || got["folder/b.txt"] != "B" {
		t.Fatalf("zip members = %v, want folder/b.txt=B", got)
	}

	// a batch is named 压缩文件, as Explorer does
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"a.txt", "folder"}})
	if rr.Code != 200 {
		t.Fatalf("zip batch: code %d (%s)", rr.Code, rr.Message)
	}
	if res := archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/zip/压缩文件.zip" {
		t.Fatalf("created = %v, want [/zip/压缩文件.zip]", res.Created)
	}

	// a single file is named after itself; a second one gets 副本
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"a.txt"}})
	if rr.Code != 200 {
		t.Fatalf("zip file: code %d (%s)", rr.Code, rr.Message)
	}
	if res := archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/zip/a.zip" {
		t.Fatalf("created = %v, want [/zip/a.zip]", res.Created)
	}
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"a.txt"}})
	if res := archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/zip/a - 副本.zip" {
		t.Fatalf("created = %v, want [/zip/a - 副本.zip]", res.Created)
	}

	// dst_dir places the archive elsewhere
	e.mkdir("zout")
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"a.txt"}, "dst_dir": "/zout"})
	if rr.Code != 200 {
		t.Fatalf("zip to dst_dir: code %d (%s)", rr.Code, rr.Message)
	}
	if res := archiveResult(t, rr); len(res.Created) != 1 || res.Created[0] != "/zout/a.zip" {
		t.Fatalf("created = %v, want [/zout/a.zip]", res.Created)
	}

	// an empty batch and a missing item are the two protocol errors
	rr = e.api(http.MethodPost, "/api/fs/archive/zip", tok, map[string]any{"src_dir": "/zip"})
	if rr.Code != 400 || rr.Message != "Empty file names" {
		t.Fatalf("empty zip: code %d (%s), want 400 Empty file names", rr.Code, rr.Message)
	}
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"ghost"}})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("ghost zip: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
	// a destination that is not a folder
	rr = zipIt(map[string]any{"src_dir": "/zip", "names": []string{"a.txt"}, "dst_dir": "/nowhere"})
	if rr.Code != 500 || rr.Message != "object not found" {
		t.Fatalf("zip to nowhere: code %d (%s), want 500 object not found", rr.Code, rr.Message)
	}
}
