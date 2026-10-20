package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// remote_share.go is the OpenList v4-compatible share system: the admin
// endpoints under /api/share/* (create, list, get, update, delete, enable,
// disable), the guest browsing of /@s/<sid> paths that remoteHandleFSList and
// remoteHandleFSGet dispatch to, and the /sd/<sid>/<path> downloads the share
// listings link to. Shares persist through the state store's shareAll and
// shareSaveAll (statestore.go), newest first in the array.

// remoteShare is one share. Files are virtual paths under the home
// directory, Expires nil for a share that never expires, and the plaintext
// Pwd round-trips through the API the way OpenList's does.
type remoteShare struct {
	ID             string     `json:"id"`
	Files          []string   `json:"files"`
	Expires        *time.Time `json:"expires"`
	Pwd            string     `json:"pwd"`
	Accessed       int        `json:"accessed"`
	MaxAccessed    int        `json:"max_accessed"`
	Disabled       bool       `json:"disabled"`
	Remark         string     `json:"remark"`
	Readme         string     `json:"readme"`
	Header         string     `json:"header"`
	OrderBy        string     `json:"order_by"`
	OrderDirection string     `json:"order_direction"`
	ExtractFolder  string     `json:"extract_folder"`
	Creator        string     `json:"creator"`
	CreatorRole    int        `json:"creator_role"`
}

// remoteShareReq is the body of share/create and share/update: every field
// of a share except the creator, plus the new_id an update may rename to.
type remoteShareReq struct {
	ID             string   `json:"id"`
	NewID          string   `json:"new_id"`
	Files          []string `json:"files"`
	Expires        *string  `json:"expires"`
	Pwd            string   `json:"pwd"`
	Accessed       int      `json:"accessed"`
	MaxAccessed    int      `json:"max_accessed"`
	Disabled       bool     `json:"disabled"`
	Remark         string   `json:"remark"`
	Readme         string   `json:"readme"`
	Header         string   `json:"header"`
	OrderBy        string   `json:"order_by"`
	OrderDirection string   `json:"order_direction"`
	ExtractFolder  string   `json:"extract_folder"`
}

// remoteShareListData is the data payload of /api/share/list.
type remoteShareListData struct {
	Content []remoteShare `json:"content"`
	Total   int           `json:"total"`
}

const (
	// remoteShareIDLen is the length of a generated share id; a collision
	// grows it by one more character, as OpenList's does.
	remoteShareIDLen = 8

	// remoteShareAccessWindow is how long one visitor IP counts as a
	// single access of a share.
	remoteShareAccessWindow = 30 * time.Minute
)

// remoteShareValidID is OpenList's custom share id charset: letters, digits,
// underscore, hyphen and CJK characters.
var remoteShareValidID = regexp.MustCompile(`^[\w\p{Han}\-]+$`)

var (
	// remoteShareMu serializes read-modify-write cycles over the share
	// list; shareAll and shareSaveAll lock the store per call only.
	remoteShareMu sync.Mutex

	// remoteShareStoreMu guards the lazy store pick below.
	remoteShareStoreMu sync.Mutex

	// remoteShareAccess deduplicates share access counting per visitor IP.
	remoteShareAccessMu sync.Mutex
	remoteShareAccess   = map[string]time.Time{}
)

// remoteShareStore returns the store shares persist in. An App built without
// one (the tests) gets a bare in-memory store, so shares still work there.
func (a *App) remoteShareStore() *persistedState {
	remoteShareStoreMu.Lock()
	defer remoteShareStoreMu.Unlock()
	if a.store == nil {
		a.store = &persistedState{}
	}
	return a.store
}

// remoteShareAll returns every share, newest first.
func (a *App) remoteShareAll() []remoteShare {
	return a.remoteShareStore().shareAll()
}

// remoteShareSaveAll replaces the whole share list.
func (a *App) remoteShareSaveAll(list []remoteShare) {
	a.remoteShareStore().shareSaveAll(list)
}

// remoteShareFind returns the share with the given id.
func (a *App) remoteShareFind(id string) (remoteShare, bool) {
	for _, s := range a.remoteShareAll() {
		if s.ID == id {
			return s, true
		}
	}
	return remoteShare{}, false
}

// remoteShareIDIn reports whether id is taken in a share list.
func remoteShareIDIn(list []remoteShare, id string) bool {
	for i := range list {
		if list[i].ID == id {
			return true
		}
	}
	return false
}

// remoteShareCheckID applies OpenList's custom share id rules: at most 64
// runes of letters, digits, underscore, hyphen and CJK characters.
func remoteShareCheckID(id string) bool {
	return len([]rune(id)) <= 64 && remoteShareValidID.MatchString(id)
}

// remoteShareRandID is a random [A-Za-z0-9] share id of n characters, with
// rejection keeping every character as likely.
func remoteShareRandID(n int) string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	limit := 256 - 256%len(chars)
	out := make([]byte, 0, n)
	buf := make([]byte, n+8)
	for len(out) < n {
		rand.Read(buf)
		for _, c := range buf {
			if int(c) < limit {
				out = append(out, chars[int(c)%len(chars)])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out)
}

// remoteShareGenID picks the id of a new share: eight random characters,
// grown by one more on every collision with a share in the list.
func remoteShareGenID(list []remoteShare) string {
	id := remoteShareRandID(remoteShareIDLen)
	for remoteShareIDIn(list, id) {
		id += remoteShareRandID(1)
	}
	return id
}

// remoteShareParseExpires reads the expires field: null or absent for a
// share that never expires, an RFC3339 time otherwise.
func remoteShareParseExpires(v *string) (*time.Time, bool) {
	if v == nil {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil, false
	}
	return &t, true
}

// remoteShareBuild fills a share from a request body, the id and expiry
// already resolved.
func (a *App) remoteShareBuild(id string, req remoteShareReq, expires *time.Time) remoteShare {
	return remoteShare{
		ID:             id,
		Files:          req.Files,
		Expires:        expires,
		Pwd:            req.Pwd,
		Accessed:       req.Accessed,
		MaxAccessed:    req.MaxAccessed,
		Disabled:       req.Disabled,
		Remark:         req.Remark,
		Readme:         req.Readme,
		Header:         req.Header,
		OrderBy:        req.OrderBy,
		OrderDirection: req.OrderDirection,
		ExtractFolder:  req.ExtractFolder,
		Creator:        a.remoteConf().Username,
		CreatorRole:    1,
	}
}

// remoteShareReadReq decodes a share request body.
func remoteShareReadReq(r *http.Request) (remoteShareReq, bool) {
	var req remoteShareReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		return req, false
	}
	return req, true
}

// remoteShareHasFiles reports whether a request shares at least one object.
func remoteShareHasFiles(files []string) bool {
	return !(len(files) == 0 || (len(files) == 1 && files[0] == ""))
}

// remoteHandleShareCreate implements POST /api/share/create: a new share of
// the posted files, which must exist under the virtual root. A custom id
// follows OpenList's charset rules; without one, eight random characters are
// generated. The reply carries the share, password included.
func (a *App) remoteHandleShareCreate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := remoteShareReadReq(r)
	if !ok {
		remoteErr(w, 400, "bad request")
		return
	}
	if !remoteShareHasFiles(req.Files) {
		remoteErr(w, 400, "must add at least 1 object")
		return
	}
	for i, f := range req.Files {
		vpath := remoteFSCleanVPath(f)
		disk, ok := a.remoteDiskPath(vpath)
		if !ok {
			remoteErr(w, 500, "file not found")
			return
		}
		if _, err := os.Stat(disk); err != nil {
			remoteErr(w, 500, "file not found")
			return
		}
		req.Files[i] = vpath
	}
	remoteShareMu.Lock()
	defer remoteShareMu.Unlock()
	list := a.remoteShareAll()
	id := req.ID
	if id != "" {
		if !remoteShareCheckID(id) {
			remoteErr(w, 400, "invalid share id")
			return
		}
		if remoteShareIDIn(list, id) {
			remoteErr(w, 500, "sharing already exist")
			return
		}
	} else {
		id = remoteShareGenID(list)
	}
	expires, ok := remoteShareParseExpires(req.Expires)
	if !ok {
		remoteErr(w, 400, "invalid expires")
		return
	}
	share := a.remoteShareBuild(id, req, expires)
	// the head of the list is the newest share
	a.remoteShareSaveAll(append([]remoteShare{share}, list...))
	remoteShareAccessPurge(id)
	remoteOK(w, share)
}

// remoteHandleShareList implements /api/share/list, on any method: every
// share, newest first, paginated like fs/list (per_page < 1 is the whole
// list). The page numbers come from the body and the query alike.
func (a *App) remoteHandleShareList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	page, perPage := 1, 0
	if r.Body != nil {
		var req struct {
			Page    int `json:"page"`
			PerPage int `json:"per_page"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			remoteErr(w, 400, "bad request")
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				remoteErr(w, 400, "bad request")
				return
			}
			page, perPage = req.Page, req.PerPage
		}
	}
	q := r.URL.Query()
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			perPage = n
		}
	}
	if page < 1 {
		page = 1
	}
	list := a.remoteShareAll()
	total := len(list)
	var content []remoteShare
	if perPage > 0 {
		start := (page - 1) * perPage
		if start < total {
			end := start + perPage
			if end > total {
				end = total
			}
			content = list[start:end]
		}
	} else if total > 0 {
		content = list
	}
	remoteOK(w, remoteShareListData{Content: content, Total: total})
}

// remoteHandleShareGet implements GET /api/share/get?id=: one share, or the
// protocol's 404 envelope.
func (a *App) remoteHandleShareGet(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	share, ok := a.remoteShareFind(r.URL.Query().Get("id"))
	if !ok {
		remoteErr(w, 404, "sharing not found")
		return
	}
	remoteOK(w, share)
}

// remoteHandleShareUpdate implements POST /api/share/update: the full
// replacement of one share. Fields left out of the body are zeroed, the
// files must be given again, and new_id renames the share.
func (a *App) remoteHandleShareUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := remoteShareReadReq(r)
	if !ok {
		remoteErr(w, 400, "bad request")
		return
	}
	if !remoteShareHasFiles(req.Files) {
		remoteErr(w, 400, "must add at least 1 object")
		return
	}
	for i, f := range req.Files {
		req.Files[i] = remoteFSCleanVPath(f)
	}
	remoteShareMu.Lock()
	defer remoteShareMu.Unlock()
	list := a.remoteShareAll()
	idx := -1
	for i := range list {
		if list[i].ID == req.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		remoteErr(w, 404, "sharing not found")
		return
	}
	id := req.ID
	if req.NewID != "" && req.NewID != req.ID {
		if !remoteShareCheckID(req.NewID) {
			remoteErr(w, 400, "invalid share id")
			return
		}
		if remoteShareIDIn(list, req.NewID) {
			remoteErr(w, 500, "sharing id already exists")
			return
		}
		id = req.NewID
	}
	expires, ok := remoteShareParseExpires(req.Expires)
	if !ok {
		remoteErr(w, 400, "invalid expires")
		return
	}
	share := a.remoteShareBuild(id, req, expires)
	list[idx] = share // keeps its place in the newest-first order
	a.remoteShareSaveAll(list)
	if id != req.ID {
		remoteShareAccessPurge(id)
	}
	remoteOK(w, share)
}

// remoteHandleShareDelete implements POST /api/share/delete?id=: the share
// leaves the list; the data is null.
func (a *App) remoteHandleShareDelete(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	a.remoteShareRemove(w, r.URL.Query().Get("id"))
}

// remoteHandleShareEnable implements POST /api/share/enable?id=: the share
// opens to guests again.
func (a *App) remoteHandleShareEnable(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	a.remoteShareSetDisabled(w, r.URL.Query().Get("id"), false)
}

// remoteHandleShareDisable implements POST /api/share/disable?id=: the share
// stops serving guests.
func (a *App) remoteHandleShareDisable(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	a.remoteShareSetDisabled(w, r.URL.Query().Get("id"), true)
}

// remoteShareRemove drops a share from the list.
func (a *App) remoteShareRemove(w http.ResponseWriter, sid string) {
	remoteShareMu.Lock()
	defer remoteShareMu.Unlock()
	list := a.remoteShareAll()
	out := make([]remoteShare, 0, len(list))
	found := false
	for _, s := range list {
		if s.ID == sid {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		remoteErr(w, 404, "sharing not found")
		return
	}
	a.remoteShareSaveAll(out)
	remoteOK(w, nil)
}

// remoteShareSetDisabled opens or closes a share to guests.
func (a *App) remoteShareSetDisabled(w http.ResponseWriter, sid string, disabled bool) {
	remoteShareMu.Lock()
	defer remoteShareMu.Unlock()
	list := a.remoteShareAll()
	found := false
	for i := range list {
		if list[i].ID == sid {
			list[i].Disabled = disabled
			found = true
			break
		}
	}
	if !found {
		remoteErr(w, 404, "sharing not found")
		return
	}
	a.remoteShareSaveAll(list)
	remoteOK(w, nil)
}

// remoteShareIsValid is OpenList's Sharing.Valid: enabled, under its visit
// cap, sharing something and not past its expiry.
func remoteShareIsValid(s remoteShare) bool {
	if s.Disabled {
		return false
	}
	if s.MaxAccessed > 0 && s.Accessed >= s.MaxAccessed {
		return false
	}
	if len(s.Files) == 0 {
		return false
	}
	if s.Expires != nil && !s.Expires.IsZero() && s.Expires.Before(time.Now()) {
		return false
	}
	return true
}

// remoteShareLookup finds the share a guest path names and checks it: the
// id must exist, the share must still be valid and, when it carries one,
// the password must match. The messages are OpenList's.
func (a *App) remoteShareLookup(sid, pwd string) (remoteShare, int, string) {
	if sid == "" {
		return remoteShare{}, 400, "invalid share id"
	}
	share, ok := a.remoteShareFind(sid)
	if !ok {
		return remoteShare{}, 500, "the share does not exist"
	}
	if !remoteShareIsValid(share) {
		return remoteShare{}, 500, "the share has expired or is no longer valid"
	}
	if share.Pwd != "" && share.Pwd != pwd {
		return remoteShare{}, 403, "wrong share code"
	}
	return share, 0, ""
}

// remoteShareVPath reports whether a cleaned virtual path belongs to the
// guest share surface: /@s itself or a path below /@s/. A home folder whose
// name merely starts with "@s" stays an ordinary folder.
func remoteShareVPath(vpath string) bool {
	return vpath == "/@s" || strings.HasPrefix(vpath, "/@s/")
}

// remoteShareSplitPath splits a cleaned /@s/<sid>/<sub...> path into the
// share id and the sub path below the share root; "/@s" alone leaves the id
// empty, the invalid share id of the protocol.
func remoteShareSplitPath(vpath string) (sid, sub string) {
	rest := strings.TrimPrefix(vpath, "/@s")
	sid, sub, _ = strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	return sid, sub
}

// remoteShareResolve maps a sub path below the share root onto the virtual
// path it names. The first segment may name a shared object by its base
// name — OpenList's rule — or the whole sub path may be a path under one of
// the shared virtual paths, which keeps links shaped like the real ones
// working.
func remoteShareResolve(files []string, sub string) (string, bool) {
	if sub == "" {
		return "", true
	}
	child, rest, _ := strings.Cut(sub, "/")
	for _, f := range files {
		f = remoteFSCleanVPath(f)
		if path.Base(f) == child {
			return path.Join(f, rest), true
		}
	}
	p := "/" + sub
	for _, f := range files {
		f = remoteFSCleanVPath(f)
		if f == "/" || p == f || strings.HasPrefix(p, f+"/") {
			return p, true
		}
	}
	return "", false
}

// remoteShareEncodePath percent-escapes every segment of a share sub path —
// url.PathEscape semantics, / separators kept — for the /sd download URL.
func remoteShareEncodePath(sub string) string {
	segs := strings.Split(strings.TrimPrefix(sub, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/" + strings.Join(segs, "/")
}

// remoteShareObj builds the remoteObj of one object inside a share: the fs
// object shape, but never a download sign — share downloads go through /sd
// links, not signed /p ones.
func (a *App) remoteShareObj(vpath, disk string) (remoteObj, bool) {
	st, err := os.Stat(disk)
	if err != nil {
		return remoteObj{}, false
	}
	obj := a.remoteFSObj(path.Base(vpath), vpath, disk, st)
	obj.Sign = ""
	return obj, true
}

// remoteShareSortObjs orders a share listing the way fs/list orders one:
// folders first, then names case-insensitively.
func remoteShareSortObjs(objs []remoteObj) {
	sort.SliceStable(objs, func(i, j int) bool {
		if objs[i].IsDir != objs[j].IsDir {
			return objs[i].IsDir
		}
		return strings.ToLower(objs[i].Name) < strings.ToLower(objs[j].Name)
	})
}

// remoteShareFSList answers /api/fs/list for a /@s/<sid> path: the guest
// browsing of a share. The share root lists the shared entries themselves
// (a single-file share lists that one file), a sub path lists the directory
// it resolves to, and everything shares fs/list's pagination and shape with
// the share's readme and header, write off and provider unknown.
func (a *App) remoteShareFSList(w http.ResponseWriter, r *http.Request, vpath, password string, page, perPage int) {
	sid, sub := remoteShareSplitPath(vpath)
	share, code, msg := a.remoteShareLookup(sid, password)
	if code != 0 {
		remoteErr(w, code, msg)
		return
	}
	var objs []remoteObj
	if sub == "" {
		objs = make([]remoteObj, 0, len(share.Files))
		for _, f := range share.Files {
			if f == "/" {
				continue // the virtual root itself is not an entry
			}
			disk, ok := a.remoteDiskPath(f)
			if !ok {
				continue
			}
			obj, ok := a.remoteShareObj(f, disk)
			if !ok {
				continue // a shared file gone from disk is left out
			}
			objs = append(objs, obj)
		}
		remoteShareSortObjs(objs)
	} else {
		vp, ok := remoteShareResolve(share.Files, sub)
		if !ok {
			remoteErr(w, 500, "object not found")
			return
		}
		disk, ok := a.remoteDiskPath(vp)
		if !ok {
			remoteErr(w, 500, "object not found")
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
		objs = make([]remoteObj, 0, len(entries))
		for _, de := range entries {
			name := de.Name()
			diskName := filepath.Join(disk, name)
			obj, ok := a.remoteShareObj(path.Join(vp, name), diskName)
			if !ok {
				continue // unreadable entry: leave it out, keep the listing alive
			}
			objs = append(objs, obj)
		}
		remoteShareSortObjs(objs)
	}
	a.remoteShareCountAccess(sid, r)
	if page < 1 {
		page = 1
	}
	var start int
	if perPage > 0 {
		start = (page - 1) * perPage
	}
	var content []remoteObj
	if start < len(objs) {
		end := len(objs)
		if perPage > 0 && start+perPage < end {
			end = start + perPage
		}
		content = objs[start:end]
	}
	remoteOK(w, remoteFSListData{
		Content:  content,
		Total:    int64(len(objs)),
		Readme:   share.Readme,
		Header:   share.Header,
		Write:    false,
		Provider: "unknown",
	})
}

// remoteShareFSGet answers /api/fs/get for a /@s/<sid> path. The share root
// is a virtual folder named after the share; an object below it is the real
// object, files carrying a /sd raw_url with the share password when there
// is one.
func (a *App) remoteShareFSGet(w http.ResponseWriter, r *http.Request, vpath, password string) {
	sid, sub := remoteShareSplitPath(vpath)
	share, code, msg := a.remoteShareLookup(sid, password)
	if code != 0 {
		remoteErr(w, code, msg)
		return
	}
	data := remoteFSGetData{
		remoteObj: remoteObj{
			Name:     sid,
			IsDir:    true,
			Type:     remoteFSTypeFolder,
			HashInfo: "null",
		},
		Readme:   share.Readme,
		Header:   share.Header,
		Provider: "unknown",
	}
	if sub != "" {
		vp, ok := remoteShareResolve(share.Files, sub)
		if !ok {
			remoteErr(w, 500, "object not found")
			return
		}
		disk, ok := a.remoteDiskPath(vp)
		if !ok {
			remoteErr(w, 500, "object not found")
			return
		}
		st, err := os.Stat(disk)
		if err != nil {
			remoteErr(w, 500, "object not found")
			return
		}
		obj := a.remoteFSObj(path.Base(vp), vp, disk, st)
		obj.Sign = ""
		data.remoteObj = obj
		if !obj.IsDir {
			link := remoteRequestBase(r) + "/sd/" + sid + remoteShareEncodePath(sub)
			if share.Pwd != "" {
				link += "?pwd=" + url.QueryEscape(share.Pwd)
			}
			data.RawURL = link
		}
	}
	a.remoteShareCountAccess(sid, r)
	remoteOK(w, data)
}

// remoteHandleShareDown implements GET/HEAD /sd/<sid>/<path>: the guest
// download of one file inside a share, the password in the pwd query
// parameter. Errors are OpenList's HTML pages.
func (a *App) remoteHandleShareDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		remoteHTMLFail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/sd/")
	sid, sub, _ := strings.Cut(rest, "/")
	sub = strings.TrimPrefix(remoteFSCleanVPath("/"+sub), "/")
	share, code, msg := a.remoteShareLookup(sid, r.URL.Query().Get("pwd"))
	if code != 0 {
		remoteHTMLFail(w, code, msg)
		return
	}
	if sub == "" {
		remoteHTMLFail(w, http.StatusInternalServerError, "not a file")
		return
	}
	vp, ok := remoteShareResolve(share.Files, sub)
	if !ok {
		remoteHTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	disk, ok := a.remoteDiskPath(vp)
	if !ok {
		remoteHTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	info, err := os.Stat(disk)
	if err != nil {
		remoteHTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	if info.IsDir() {
		remoteHTMLFail(w, http.StatusInternalServerError, "not a file")
		return
	}
	f, err := os.Open(disk)
	if err != nil {
		remoteHTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	defer f.Close()
	a.remoteShareCountAccess(sid, r)
	name := filepath.Base(disk)
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=utf-8''%s`,
		url.PathEscape(name), url.PathEscape(name)))
	w.Header().Set("Etag", fmt.Sprintf(`"%x-%x"`, info.ModTime().Unix(), info.Size()))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// remoteShareAccessPurge drops the access dedup entries of a share id, so a
// recreated or renamed share counts its visitors afresh.
func remoteShareAccessPurge(sid string) {
	if sid == "" {
		return
	}
	remoteShareAccessMu.Lock()
	for k := range remoteShareAccess {
		if strings.HasPrefix(k, sid+"|") {
			delete(remoteShareAccess, k)
		}
	}
	remoteShareAccessMu.Unlock()
}

// remoteShareCountAccess bumps a share's visit counter, once per visitor IP
// per half hour, and persists it.
func (a *App) remoteShareCountAccess(sid string, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	key := sid + "|" + ip
	now := time.Now()
	remoteShareAccessMu.Lock()
	if t, ok := remoteShareAccess[key]; ok && now.Sub(t) < remoteShareAccessWindow {
		remoteShareAccessMu.Unlock()
		return
	}
	remoteShareAccess[key] = now
	for k, t := range remoteShareAccess {
		if now.Sub(t) >= remoteShareAccessWindow {
			delete(remoteShareAccess, k)
		}
	}
	remoteShareAccessMu.Unlock()

	remoteShareMu.Lock()
	defer remoteShareMu.Unlock()
	list := a.remoteShareAll()
	for i := range list {
		if list[i].ID == sid {
			list[i].Accessed++
			a.remoteShareSaveAll(list)
			return
		}
	}
}
