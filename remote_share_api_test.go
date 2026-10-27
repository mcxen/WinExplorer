package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// remote_share_api_test.go covers /api/share admin CRUD and guest browse/
// download limits via the remote manage harness (LAY1 split from
// remote_manage_test.go).

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
