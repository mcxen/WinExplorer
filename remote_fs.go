package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"sunvale/internal/rename"
)

// remote_fs.go is the read side of the remote server's OpenList-compatible
// API: the /api/fs/list, /api/fs/get and /api/fs/search handlers, the
// virtual-root path mapping they share with the download routes, and the
// download sign. The virtual root "/" is the user's home directory — the
// same os.UserHomeDir() newApp caches in homePath.

// OpenList object types: 0 unknown, 1 folder, 2 video, 3 audio, 4 text,
// 5 image.
const (
	remoteFSTypeUnknown = 0
	remoteFSTypeFolder  = 1
	remoteFSTypeVideo   = 2
	remoteFSTypeAudio   = 3
	remoteFSTypeText    = 4
	remoteFSTypeImage   = 5
)

// remoteObj is one object of an OpenList listing. There is no hash backend,
// so HashInfo is OpenList's "null" string and HashInfoMap serializes as
// null.
type remoteObj struct {
	Name        string            `json:"name"`
	Size        int64             `json:"size"`
	IsDir       bool              `json:"is_dir"`
	Modified    time.Time         `json:"modified"`
	Created     time.Time         `json:"created"`
	Sign        string            `json:"sign"`
	Thumb       string            `json:"thumb"`
	Type        int               `json:"type"`
	HashInfo    string            `json:"hashinfo"`
	HashInfoMap map[string]string `json:"hash_info"`
}

// remoteFSHome mirrors newApp's homePath: the directory the virtual root
// maps onto, / when it cannot be determined.
func remoteFSHome() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/"
	}
	return home
}

// remoteFSCleanVPath normalizes a virtual path: backslashes become slashes,
// a leading / is forced and path.Clean resolves it against the root, so no
// ".." segment can survive to escape it.
func remoteFSCleanVPath(vpath string) string {
	p := strings.ReplaceAll(vpath, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// remoteDiskPath maps a virtual path to its disk path through the storage
// mounts (remote_storage.go): the mount whose path holds vpath decides
// which disk folder serves it, and ok is false when no enabled mount does
// or the path would escape it.
func (a *App) remoteDiskPath(vpath string) (string, bool) {
	p := remoteFSCleanVPath(vpath)
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	mount, inner, ok := a.remoteMountFor(p)
	if !ok {
		return "", false
	}
	return remoteDiskPathIn(mount, inner)
}

// remoteReadReq decodes one /api/fs/* request the way gin's c.ShouldBind
// does: the JSON or form-encoded body first, the query string after it as a
// fallback, so POST bodies and GET requests work alike. An empty body is
// not an error — every field keeps its zero value — and unknown fields are
// ignored, as gin ignores them.
func remoteReadReq(r *http.Request, v any) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		remoteBindQuery(r, v)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 {
		jsonBody := strings.Contains(r.Header.Get("Content-Type"), "json")
		if !jsonBody && len(body) > 0 && (body[0] == '{' || body[0] == '[') {
			jsonBody = true
		}
		if jsonBody {
			if err := json.Unmarshal(body, v); err != nil {
				return err
			}
		} else if err := remoteBindValues(body, v); err != nil {
			return err
		}
	}
	remoteBindQuery(r, v)
	return nil
}

// remoteBindQuery applies the query string to v, beside a body's values.
func remoteBindQuery(r *http.Request, v any) {
	if q := r.URL.RawQuery; q != "" {
		_ = remoteBindValues([]byte(q), v)
	}
}

// remoteBindValues fills the json-tagged fields of the struct v points to
// from a form-encoded string — application/x-www-form-urlencoded bodies and
// query strings share the syntax, values percent-decoded by the parser.
func remoteBindValues(raw []byte, v any) error {
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		key := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if key == "" || key == "-" || !values.Has(key) {
			continue
		}
		text := values.Get(key)
		field := rv.Field(i)
		switch field.Kind() {
		case reflect.String:
			field.SetString(text)
		case reflect.Bool:
			b, err := strconv.ParseBool(text)
			if err != nil {
				return err
			}
			field.SetBool(b)
		case reflect.Int, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(n)
		}
	}
	return nil
}

// remoteVPath maps a disk path under the home directory back to its virtual
// path. A path outside home surfaces as-is: a sibling of home (home+"-old")
// must not collapse into a path inside the virtual root.
func remoteVPath(diskPath string) string {
	home := remoteFSHome()
	d := filepath.Clean(diskPath)
	rel := ""
	switch {
	case d == home:
	case strings.HasPrefix(d, home+string(filepath.Separator)):
		rel = strings.TrimPrefix(d, home+string(filepath.Separator))
	default:
		rel = d
	}
	return path.Clean("/" + filepath.ToSlash(rel))
}

// remoteFSEncodePath percent-escapes every segment of a virtual path —
// url.PathEscape semantics, / separators kept — for the /p download URL.
func remoteFSEncodePath(vpath string) string {
	segs := strings.Split(strings.TrimPrefix(remoteFSCleanVPath(vpath), "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/" + strings.Join(segs, "/")
}

// remoteSign signs a virtual path for raw downloads, OpenList v4 style:
// URL-safe base64 (padded) of HMAC-SHA256(token, vpath+":"+expire) with the
// expire timestamp (always 0, never expiring) appended after a colon.
func (a *App) remoteSign(vpath string) string {
	expire := strconv.FormatInt(0, 10)
	mac := hmac.New(sha256.New, []byte(a.remoteConf().Token))
	mac.Write([]byte(vpath))
	mac.Write([]byte(":"))
	mac.Write([]byte(expire))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil)) + ":" + expire
}

// remoteVerifySign checks a download sign for a virtual path. The expire
// timestamp sits after the last colon: it must be 0 (never) or in the
// future, and the recomputed HMAC over vpath+":"+expire must match the
// whole sign.
func (a *App) remoteVerifySign(vpath, sign string) bool {
	sign = strings.TrimSuffix(sign, "/")
	i := strings.LastIndex(sign, ":")
	if i < 0 {
		return false
	}
	expire, err := strconv.ParseInt(sign[i+1:], 10, 64)
	if err != nil {
		return false
	}
	if expire != 0 && expire < time.Now().Unix() {
		return false
	}
	exp := strconv.FormatInt(expire, 10)
	mac := hmac.New(sha256.New, []byte(a.remoteConf().Token))
	mac.Write([]byte(vpath))
	mac.Write([]byte(":"))
	mac.Write([]byte(exp))
	want := base64.URLEncoding.EncodeToString(mac.Sum(nil)) + ":" + exp
	return subtle.ConstantTimeCompare([]byte(want), []byte(sign)) == 1
}

// remoteFSType maps a file name to its OpenList type by lowercased
// extension. The four lists are OpenList's default audio_types,
// video_types, image_types and text_types settings, checked in the order
// its utils.GetFileType checks them, so a client's preview choice agrees
// with what a real OpenList server would report for the same name.
func remoteFSType(name string, isDir bool) int {
	if isDir {
		return remoteFSTypeFolder
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "mp3", "flac", "ogg", "m4a", "wav", "opus", "wma":
		return remoteFSTypeAudio
	case "mp4", "mkv", "avi", "mov", "rmvb", "webm", "flv", "m3u8":
		return remoteFSTypeVideo
	case "jpg", "tiff", "jpeg", "png", "gif", "bmp", "svg", "ico", "swf",
		"webp", "avif":
		return remoteFSTypeImage
	case "txt", "htm", "html", "xml", "java", "properties", "sql", "js",
		"md", "json", "conf", "ini", "vue", "php", "py", "bat", "gitignore",
		"yml", "go", "sh", "c", "cpp", "h", "hpp", "tsx", "vtt", "srt", "ass",
		"rs", "lrc", "strm":
		return remoteFSTypeText
	}
	return remoteFSTypeUnknown
}

// remoteRequestBase is the scheme-and-host prefix raw_url links are built
// on, the way OpenList's common.GetApiUrl follows the request: a TLS
// connection or X-Forwarded-Proto yields https, X-Forwarded-Host replaces
// r.Host, so a reverse proxy in front of the app still hands out working
// links.
func remoteRequestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

// remoteObjName is the name an object answers with: OpenList calls the
// virtual root "root" (its local driver's root has no base name of its own),
// a folder inside it its own base name.
func remoteObjName(disk string) string {
	if filepath.Clean(disk) == filepath.Clean(remoteFSHome()) {
		return "root"
	}
	return filepath.Base(disk)
}

// remoteFSObj builds the remoteObj for the file stat'd at disk, name being
// its base name and vpath its virtual path: birthtime as Created (modtime
// as the fallback, zero passed through), and, for files only, a sign.
// A folder carries size 0: OpenList's Local driver only fills it when
// directory_size is on, and a bare stat size (the inode on Unix) would read
// as a real folder total on the client.
func (a *App) remoteFSObj(name, vpath, disk string, st os.FileInfo) remoteObj {
	created := st.ModTime()
	if bt, err := rename.BirthTime(disk); err == nil {
		created = bt
	}
	size := st.Size()
	if st.IsDir() {
		size = 0
	}
	obj := remoteObj{
		Name:     name,
		Size:     size,
		IsDir:    st.IsDir(),
		Modified: st.ModTime(),
		Created:  created,
		Type:     remoteFSType(name, st.IsDir()),
		HashInfo: "null",
	}
	if !obj.IsDir {
		obj.Sign = a.remoteSign(vpath)
	}
	return obj
}

// remoteFSListData is the data payload of /api/fs/list.
type remoteFSListData struct {
	Content            []remoteObj `json:"content"`
	Total              int64       `json:"total"`
	Readme             string      `json:"readme"`
	Header             string      `json:"header"`
	Write              bool        `json:"write"`
	WriteContentBypass bool        `json:"write_content_bypass"`
	Provider           string      `json:"provider"`
}

// remoteHandleFSList answers /api/fs/list, on any method: the objects
// directly inside a virtual folder — folders first, then names
// case-insensitively — with OpenList's pagination. An empty folder (or a
// page past the end) gives content null; total is the whole folder. A
// missing path means the virtual root, as JoinPath("") does on OpenList's
// side. Paths under /@s/ are the guest browsing of a share
// (remote_share.go), open without a login.
func (a *App) remoteHandleFSList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Password string `json:"password"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
		Refresh  bool   `json:"refresh"`
	}
	if err := remoteReadReq(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if vpath := remoteFSCleanVPath(req.Path); remoteShareVPath(vpath) {
		a.remoteShareFSList(w, r, vpath, req.Password, req.Page, req.PerPage)
		return
	}
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	disk, ok := a.remoteDiskPath(req.Path)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	info, err := os.Stat(disk)
	if err != nil {
		remoteErr(w, 500, "object not found")
		return
	}
	if !info.IsDir() {
		remoteErr(w, 500, "not a folder")
		return
	}
	entries, err := os.ReadDir(disk)
	if err != nil {
		remoteErr(w, 500, "object not found")
		return
	}
	vdir := remoteVPath(disk)
	objs := make([]remoteObj, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		diskName := filepath.Join(disk, name)
		st, err := os.Stat(diskName)
		if err != nil {
			continue // unreadable entry: leave it out, keep the listing alive
		}
		objs = append(objs, a.remoteFSObj(name, path.Join(vdir, name), diskName, st))
	}
	sort.SliceStable(objs, func(i, j int) bool {
		if objs[i].IsDir != objs[j].IsDir {
			return objs[i].IsDir
		}
		return strings.ToLower(objs[i].Name) < strings.ToLower(objs[j].Name)
	})
	page := req.Page
	if page < 1 {
		page = 1
	}
	var start int64
	if req.PerPage > 0 {
		start = int64(page-1) * int64(req.PerPage)
	}
	var content []remoteObj
	if start < int64(len(objs)) {
		end := int64(len(objs))
		if req.PerPage > 0 && start+int64(req.PerPage) < end {
			end = start + int64(req.PerPage)
		}
		content = objs[start:end]
	}
	remoteOK(w, remoteFSListData{
		Content:            content,
		Total:              int64(len(objs)),
		Readme:             "",
		Header:             "",
		Write:              true,
		WriteContentBypass: false,
		Provider:           "unknown",
	})
}

// remoteFSGetData is the data payload of /api/fs/get: the object with its
// fields flattened on top level, plus the raw link and page furniture
// OpenList adds. Related stays null for everything but video files, where
// OpenList fills it with the same-prefix siblings (subtitles and danmu the
// client's player picks up).
type remoteFSGetData struct {
	remoteObj
	RawURL   string      `json:"raw_url"`
	Readme   string      `json:"readme"`
	Header   string      `json:"header"`
	Provider string      `json:"provider"`
	Related  []remoteObj `json:"related"`
}

// remoteHandleFSGet answers /api/fs/get, on any method: one object, with a
// signed raw_url pointing at this server's /p route for files (directories
// get an empty one and no sign). A missing path is the virtual root, as on
// OpenList's side. Paths under /@s/ are the guest browsing of a share
// (remote_share.go), open without a login.
func (a *App) remoteHandleFSGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Password string `json:"password"`
	}
	if err := remoteReadReq(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if vpath := remoteFSCleanVPath(req.Path); remoteShareVPath(vpath) {
		a.remoteShareFSGet(w, r, vpath, req.Password)
		return
	}
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	disk, ok := a.remoteDiskPath(req.Path)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	st, err := os.Stat(disk)
	if err != nil {
		remoteErr(w, 500, "object not found")
		return
	}
	vpath := remoteVPath(disk)
	data := remoteFSGetData{
		remoteObj: a.remoteFSObj(remoteObjName(disk), vpath, disk, st),
		Readme:    "",
		Header:    "",
		Provider:  "Local",
	}
	if !data.IsDir {
		data.RawURL = remoteRequestBase(r) + "/p" + remoteFSEncodePath(vpath) + "?sign=" + data.Sign
		data.Related = a.remoteRelated(disk, data.Name, data.Type)
	}
	remoteOK(w, data)
}

// remoteRelated is OpenList's filterRelated: for a video, the siblings in
// the same folder whose names start with the video's name minus its
// extension, so a player finds its .srt/.ass/.vtt subtitles and the .xml
// danmu next to the file. Anything else — a folder, a non-video, a folder
// too big to list — stays nil, which serializes as null.
func (a *App) remoteRelated(disk, name string, typ int) []remoteObj {
	if typ != remoteFSTypeVideo {
		return nil
	}
	entries, err := os.ReadDir(filepath.Dir(disk))
	if err != nil {
		return nil
	}
	stem := strings.TrimSuffix(name, path.Ext(name))
	vdir := remoteVPath(filepath.Dir(disk))
	var related []remoteObj
	for _, de := range entries {
		sibling := de.Name()
		if sibling == name || !strings.HasPrefix(sibling, stem) {
			continue
		}
		diskName := filepath.Join(filepath.Dir(disk), sibling)
		st, err := os.Stat(diskName)
		if err != nil {
			continue
		}
		related = append(related, a.remoteFSObj(sibling, path.Join(vdir, sibling), diskName, st))
	}
	return related
}

// remoteFSSearchItem is one hit of /api/fs/search: the virtual folder the
// match sits in, plus the fields a listing row needs.
type remoteFSSearchItem struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
	Type   int    `json:"type"`
}

// remoteFSSearchData is the data payload of /api/fs/search.
type remoteFSSearchData struct {
	Content []remoteFSSearchItem `json:"content"`
	Total   int64                `json:"total"`
}

// remoteHandleFSSearch answers /api/fs/search, on any method: Explorer's
// walkMatches under a virtual folder — hidden entries skipped, capped at
// three seconds, or the desktop's eight-second searchTimeout when global
// asks for the whole-home 全局搜索 (the virtual root "/" is the home
// directory) — mapped to OpenList hits, filtered by scope (0 all, 1 folders
// only, 2 files only) and paginated like /api/fs/list. A missing parent is
// the virtual root, as JoinPath("") does on OpenList's side.
func (a *App) remoteHandleFSSearch(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Parent   string `json:"parent"`
		Keywords string `json:"keywords"`
		Global   bool   `json:"global"`
		Scope    int    `json:"scope"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
	}
	if err := remoteReadReq(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Page < 1 || req.PerPage < 1 {
		remoteErr(w, 400, "page and per_page must be positive")
		return
	}
	disk, ok := a.remoteDiskPath(req.Parent)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	if st, err := os.Stat(disk); err != nil || !st.IsDir() {
		remoteErr(w, 500, "object not found")
		return
	}
	deadline := 3 * time.Second
	if req.Global {
		deadline = searchTimeout
	}
	hits := walkMatches(disk, req.Keywords, false, time.Now().Add(deadline))
	items := make([]remoteFSSearchItem, 0, len(hits))
	for _, h := range hits {
		if (req.Scope == 1 && !h.IsDir) || (req.Scope == 2 && h.IsDir) {
			continue
		}
		items = append(items, remoteFSSearchItem{
			Parent: path.Dir(remoteVPath(h.Path)),
			Name:   h.Name,
			IsDir:  h.IsDir,
			Size:   h.Size,
			Type:   remoteFSType(h.Name, h.IsDir),
		})
	}
	start := int64(req.Page-1) * int64(req.PerPage)
	var content []remoteFSSearchItem
	if start < int64(len(items)) {
		end := start + int64(req.PerPage)
		if end > int64(len(items)) {
			end = int64(len(items))
		}
		content = items[start:end]
	}
	remoteOK(w, remoteFSSearchData{Content: content, Total: int64(len(items))})
}
