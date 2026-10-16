package main

import (
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
	"sort"
	"strconv"
	"strings"
	"time"
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

// remoteDiskPath maps a virtual path to its disk path under the home
// directory. ok is false when the path would escape the virtual root.
func remoteDiskPath(vpath string) (string, bool) {
	p := remoteFSCleanVPath(vpath)
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	rel := strings.TrimPrefix(p, "/")
	if rel == "" {
		return remoteFSHome(), true
	}
	return filepath.Join(remoteFSHome(), filepath.FromSlash(rel)), true
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
// extension.
func remoteFSType(name string, isDir bool) int {
	if isDir {
		return remoteFSTypeFolder
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "mp4", "mkv", "mov", "avi", "webm", "m4v", "flv", "ts", "wmv":
		return remoteFSTypeVideo
	case "mp3", "flac", "wav", "aac", "m4a", "ogg", "opus", "wma":
		return remoteFSTypeAudio
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "heic", "tiff", "ico", "avif":
		return remoteFSTypeImage
	case "txt", "md", "log", "csv", "json", "xml", "yml", "yaml", "html",
		"css", "js", "go", "py", "rs", "java", "c", "cpp", "h", "sh", "ini",
		"toml":
		return remoteFSTypeText
	}
	return remoteFSTypeUnknown
}

// remoteFSObj builds the remoteObj for the file stat'd at disk, name being
// its base name and vpath its virtual path: birthtime as Created (modtime
// as the fallback, zero passed through), and, for files only, a sign.
func (a *App) remoteFSObj(name, vpath, disk string, st os.FileInfo) remoteObj {
	created := st.ModTime()
	if bt, err := birthTime(disk); err == nil {
		created = bt
	}
	obj := remoteObj{
		Name:     name,
		Size:     st.Size(),
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

// remoteHandleFSList answers POST /api/fs/list: the objects directly inside
// a virtual folder — folders first, then names case-insensitively — with
// OpenList's pagination. An empty folder (or a page past the end) gives
// content null; total is the whole folder. Paths under /@s/ are the guest
// browsing of a share (remote_share.go), open without a login.
func (a *App) remoteHandleFSList(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Password string `json:"password"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
		Refresh  bool   `json:"refresh"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
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
	if req.Path == "" {
		remoteErr(w, 400, "path is required")
		return
	}
	disk, ok := remoteDiskPath(req.Path)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	info, err := os.Stat(disk)
	if err != nil || !info.IsDir() {
		remoteErr(w, 500, "object not found")
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
// OpenList adds. Related stays null, as it does on OpenList's side.
type remoteFSGetData struct {
	remoteObj
	RawURL   string `json:"raw_url"`
	Readme   string `json:"readme"`
	Header   string `json:"header"`
	Provider string `json:"provider"`
	Related  any    `json:"related"`
}

// remoteHandleFSGet answers POST /api/fs/get: one object, with a signed
// raw_url pointing at this server's /p route for files (directories get an
// empty one and no sign). Paths under /@s/ are the guest browsing of a
// share (remote_share.go), open without a login.
func (a *App) remoteHandleFSGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
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
	disk, ok := remoteDiskPath(req.Path)
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
		remoteObj: a.remoteFSObj(filepath.Base(disk), vpath, disk, st),
		Readme:    "",
		Header:    "",
		Provider:  "Local",
	}
	if !data.IsDir {
		data.RawURL = "http://" + r.Host + "/p" + remoteFSEncodePath(vpath) + "?sign=" + data.Sign
	}
	remoteOK(w, data)
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

// remoteHandleFSSearch answers POST /api/fs/search: Explorer's walkMatches
// under a virtual folder — hidden entries skipped, capped at three seconds
// — mapped to OpenList hits, filtered by scope (0 all, 1 folders only,
// 2 files only) and paginated like /api/fs/list.
func (a *App) remoteHandleFSSearch(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Parent   string `json:"parent"`
		Keywords string `json:"keywords"`
		Scope    int    `json:"scope"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Page < 1 || req.PerPage < 1 {
		remoteErr(w, 400, "page and per_page must be positive")
		return
	}
	disk, ok := remoteDiskPath(req.Parent)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	if st, err := os.Stat(disk); err != nil || !st.IsDir() {
		remoteErr(w, 500, "object not found")
		return
	}
	hits := walkMatches(disk, req.Keywords, false, time.Now().Add(3*time.Second))
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
