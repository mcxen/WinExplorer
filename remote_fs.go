package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"sunvale/internal/remote"
	"sunvale/internal/searchquery"

	"sunvale/internal/rename"
)

// remote_fs.go is the read side of the remote server's OpenList-compatible
// API: the /api/fs/list, /api/fs/get and /api/fs/search handlers, the
// virtual-root path mapping they share with the download routes, and the
// download sign. The virtual root "/" is the user's home directory — the
// same os.UserHomeDir() newApp caches in homePath.

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
	// Path is set only on the homepage's storage rows, where the label is
	// not the virtual path the row opens.
	Path string `json:"path,omitempty"`
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

// remoteDiskPath maps a virtual path to its disk path through the storage
// mounts (remote_storage.go): the mount whose path holds vpath decides
// which disk folder serves it, and ok is false when no enabled mount does
// or the path would escape it.
func (a *App) remoteDiskPath(vpath string) (string, bool) {
	p := remote.FSCleanVPath(vpath)
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
		Type:     remote.FSType(name, st.IsDir()),
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
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if vpath := remote.FSCleanVPath(req.Path); remoteShareVPath(vpath) {
		a.remoteShareFSList(w, r, vpath, req.Password, req.Page, req.PerPage)
		return
	}
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	disk, ok := a.remoteDiskPath(req.Path)
	if !ok {
		remote.Err(w, 400, "invalid path")
		return
	}
	info, err := os.Stat(disk)
	if err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	if !info.IsDir() {
		remote.Err(w, 500, "not a folder")
		return
	}
	entries, err := os.ReadDir(disk)
	if err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	hidden := a.remoteShowHidden()
	vdir := remoteVPath(disk)
	objs := make([]remoteObj, 0, len(entries))
	for _, de := range entries {
		name := de.Name()
		// Dotfiles stay out unless the owner asked the remote pages to show
		// them. The filter lives here, not in each page, so the homepage and
		// the management file page cannot disagree.
		if !hidden && strings.HasPrefix(name, ".") {
			continue
		}
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
	remote.OK(w, remoteFSListData{
		Content:            content,
		Total:              int64(len(objs)),
		Readme:             "",
		Header:             "",
		Write:              true,
		WriteContentBypass: false,
		Provider:           "unknown",
	})
}

// remoteShowHidden reports whether the remote pages list dotfiles. It is
// the remote server's own switch, not the desktop's 查看 setting: a phone
// browsing the homepage should not inherit what the desktop window hides.
func (a *App) remoteShowHidden() bool {
	if a.store == nil {
		return false
	}
	return a.store.remoteShowHidden()
}

// remoteHandleHidden is GET/POST /api/fs/hidden: the owner reads the
// switch, and a POST flips it. Both pages ask here so the homepage and the
// management file page stay on the same answer.
func (a *App) remoteHandleHidden(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Show bool `json:"show"`
		}
		if err := remote.ReadReq(r, &req); err != nil {
			remote.Err(w, 400, "bad request")
			return
		}
		if a.store != nil {
			a.store.setRemoteShowHidden(req.Show)
		}
	}
	remote.OK(w, map[string]any{"show": a.remoteShowHidden()})
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
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if vpath := remote.FSCleanVPath(req.Path); remoteShareVPath(vpath) {
		a.remoteShareFSGet(w, r, vpath, req.Password)
		return
	}
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	disk, ok := a.remoteDiskPath(req.Path)
	if !ok {
		remote.Err(w, 400, "invalid path")
		return
	}
	st, err := os.Stat(disk)
	if err != nil {
		remote.Err(w, 500, "object not found")
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
		data.RawURL = remote.RequestBase(r) + "/p" + remote.FSEncodePath(vpath) + "?sign=" + data.Sign
		data.Related = a.remoteRelated(disk, data.Name, data.Type)
	}
	remote.OK(w, data)
}

// remoteRelated is OpenList's filterRelated: for a video, the siblings in
// the same folder whose names start with the video's name minus its
// extension, so a player finds its .srt/.ass/.vtt subtitles and the .xml
// danmu next to the file. Anything else — a folder, a non-video, a folder
// too big to list — stays nil, which serializes as null.
func (a *App) remoteRelated(disk, name string, typ int) []remoteObj {
	if typ != remote.FSTypeVideo {
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
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Parent   string `json:"parent"`
		Keywords string `json:"keywords"`
		Global   bool   `json:"global"`
		Scope    int    `json:"scope"`
		Page     int    `json:"page"`
		PerPage  int    `json:"per_page"`
		// Near is the folder on screen. Hits there lead the page; an empty
		// near uses Parent, so a search of the current folder still lists
		// its own files before files further down.
		Near string `json:"near"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if req.Page < 1 || req.PerPage < 1 {
		remote.Err(w, 400, "page and per_page must be positive")
		return
	}
	disk, ok := a.remoteDiskPath(req.Parent)
	if !ok {
		remote.Err(w, 400, "invalid path")
		return
	}
	if st, err := os.Stat(disk); err != nil || !st.IsDir() {
		remote.Err(w, 500, "object not found")
		return
	}
	deadline := 3 * time.Second
	if req.Global {
		deadline = searchTimeout
	}
	hits := walkMatches(disk, req.Keywords, false, time.Now().Add(deadline))
	kept := make([]Entry, 0, len(hits))
	for _, h := range hits {
		if (req.Scope == 1 && !h.IsDir) || (req.Scope == 2 && h.IsDir) {
			continue
		}
		kept = append(kept, h)
	}
	nearDisk := disk
	if req.Near != "" {
		if d, ok := a.remoteDiskPath(req.Near); ok {
			nearDisk = d
		}
	}
	rankRemoteHits(kept, nearDisk, searchquery.Parse(req.Keywords))
	items := make([]remoteFSSearchItem, 0, len(kept))
	for _, h := range kept {
		items = append(items, remoteFSSearchItem{
			Parent: path.Dir(remoteVPath(h.Path)),
			Name:   h.Name,
			IsDir:  h.IsDir,
			Size:   h.Size,
			Type:   remote.FSType(h.Name, h.IsDir),
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
	remote.OK(w, remoteFSSearchData{Content: content, Total: int64(len(items))})
}

// rankRemoteHits puts the folder on screen first, then its subtree, then
// the rest. Relevance still orders hits inside each band.
func rankRemoteHits(hits []Entry, near string, q searchquery.Query) {
	now := time.Now()
	sort.SliceStable(hits, func(i, j int) bool {
		si, _, _ := q.Score(hits[i].Name, hits[i].Path, hits[i].IsDir, hits[i].ModTime, now, "", near)
		sj, _, _ := q.Score(hits[j].Name, hits[j].Path, hits[j].IsDir, hits[j].ModTime, now, "", near)
		if si != sj {
			return si > sj
		}
		if c := strings.Compare(strings.ToLower(hits[i].Name), strings.ToLower(hits[j].Name)); c != 0 {
			return c < 0
		}
		return hits[i].Path < hits[j].Path
	})
}
