package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"sunvale/internal/remote"
)

// remote_fsapi_test.go covers OpenList-compatible /api/fs list/get/search and
// signed /d downloads via the remote_test.go HTTP harness (LAY1 split).

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
		{"movie.mp4", remote.FSTypeVideo},
		{"stream.m3u8", remote.FSTypeVideo},
		{"clip.rmvb", remote.FSTypeVideo},
		{"song.mp3", remote.FSTypeAudio},
		{"sound.m4a", remote.FSTypeAudio},
		{"photo.jpg", remote.FSTypeImage},
		{"picture.avif", remote.FSTypeImage},
		{"animation.swf", remote.FSTypeImage},
		{"notes.md", remote.FSTypeText},
		{"main.go", remote.FSTypeText},
		{"app.tsx", remote.FSTypeText},
		{"sub.srt", remote.FSTypeText},
		{"danmu.ASS", remote.FSTypeText},
		{"vars.properties", remote.FSTypeText},
		{"page.htm", remote.FSTypeText},
		{"data.bin", remote.FSTypeUnknown},
		{"archive.zip", remote.FSTypeUnknown},
	}
	for _, c := range cases {
		if got := remote.FSType(c.name, false); got != c.want {
			t.Errorf("remoteFSType(%q) = %d, want %d", c.name, got, c.want)
		}
	}
	if got := remote.FSType("folder.mp4", true); got != remote.FSTypeFolder {
		t.Fatalf("a folder named like a video = %d, want %d", got, remote.FSTypeFolder)
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
	if clip.Type != remote.FSTypeVideo {
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
	if it := hits["notes.txt"]; it.Parent != "/Documents" || it.IsDir || it.Type != remote.FSTypeText {
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
