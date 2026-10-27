package main

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sunvale/internal/remote"
)

// remote_share.go is the OpenList v4-compatible share admin API: create,
// list, get, update, delete, enable/disable under /api/share/*, plus the
// persisted share store helpers. Guest FS browsing and /sd downloads live in
// remote_share_guest.go (LAY1 split).
//
// R7 boundary: ID generation, expiry parse, and request decode are pure;
// (*App) handlers and state-store access need a Host before internal/remote.

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
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := remoteShareReadReq(r)
	if !ok {
		remote.Err(w, 400, "bad request")
		return
	}
	if !remoteShareHasFiles(req.Files) {
		remote.Err(w, 400, "must add at least 1 object")
		return
	}
	for i, f := range req.Files {
		vpath := remote.FSCleanVPath(f)
		sd, sInner, ok := a.remoteDriverFor(vpath)
		if !ok {
			remote.Err(w, 500, "file not found")
			return
		}
		if _, err := sd.Get(r.Context(), sInner); err != nil {
			remote.Err(w, 500, "file not found")
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
			remote.Err(w, 400, "invalid share id")
			return
		}
		if remoteShareIDIn(list, id) {
			remote.Err(w, 500, "sharing already exist")
			return
		}
	} else {
		id = remoteShareGenID(list)
	}
	expires, ok := remoteShareParseExpires(req.Expires)
	if !ok {
		remote.Err(w, 400, "invalid expires")
		return
	}
	share := a.remoteShareBuild(id, req, expires)
	// the head of the list is the newest share
	a.remoteShareSaveAll(append([]remoteShare{share}, list...))
	remoteShareAccessPurge(id)
	remote.OK(w, share)
}

// remoteHandleShareList implements /api/share/list, on any method: every
// share, newest first, paginated like fs/list (per_page < 1 is the whole
// list). The page numbers come from the body and the query alike.
func (a *App) remoteHandleShareList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
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
			remote.Err(w, 400, "bad request")
			return
		}
		if len(strings.TrimSpace(string(body))) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				remote.Err(w, 400, "bad request")
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
	remote.OK(w, remoteShareListData{Content: content, Total: total})
}

// remoteHandleShareGet implements GET /api/share/get?id=: one share, or the
// protocol's 404 envelope.
func (a *App) remoteHandleShareGet(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	share, ok := a.remoteShareFind(r.URL.Query().Get("id"))
	if !ok {
		remote.Err(w, 404, "sharing not found")
		return
	}
	remote.OK(w, share)
}

// remoteHandleShareUpdate implements POST /api/share/update: the full
// replacement of one share. Fields left out of the body are zeroed, the
// files must be given again, and new_id renames the share.
func (a *App) remoteHandleShareUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := remoteShareReadReq(r)
	if !ok {
		remote.Err(w, 400, "bad request")
		return
	}
	if !remoteShareHasFiles(req.Files) {
		remote.Err(w, 400, "must add at least 1 object")
		return
	}
	for i, f := range req.Files {
		req.Files[i] = remote.FSCleanVPath(f)
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
		remote.Err(w, 404, "sharing not found")
		return
	}
	id := req.ID
	if req.NewID != "" && req.NewID != req.ID {
		if !remoteShareCheckID(req.NewID) {
			remote.Err(w, 400, "invalid share id")
			return
		}
		if remoteShareIDIn(list, req.NewID) {
			remote.Err(w, 500, "sharing id already exists")
			return
		}
		id = req.NewID
	}
	expires, ok := remoteShareParseExpires(req.Expires)
	if !ok {
		remote.Err(w, 400, "invalid expires")
		return
	}
	share := a.remoteShareBuild(id, req, expires)
	list[idx] = share // keeps its place in the newest-first order
	a.remoteShareSaveAll(list)
	if id != req.ID {
		remoteShareAccessPurge(id)
	}
	remote.OK(w, share)
}

// remoteHandleShareDelete implements POST /api/share/delete?id=: the share
// leaves the list; the data is null.
func (a *App) remoteHandleShareDelete(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	a.remoteShareRemove(w, r.URL.Query().Get("id"))
}

// remoteHandleShareEnable implements POST /api/share/enable?id=: the share
// opens to guests again.
func (a *App) remoteHandleShareEnable(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	a.remoteShareSetDisabled(w, r.URL.Query().Get("id"), false)
}

// remoteHandleShareDisable implements POST /api/share/disable?id=: the share
// stops serving guests.
func (a *App) remoteHandleShareDisable(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
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
		remote.Err(w, 404, "sharing not found")
		return
	}
	a.remoteShareSaveAll(out)
	remote.OK(w, nil)
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
		remote.Err(w, 404, "sharing not found")
		return
	}
	a.remoteShareSaveAll(list)
	remote.OK(w, nil)
}

// remoteShareIsValid is OpenList's Sharing.Valid: enabled, under its visit
// cap, sharing something and not past its expiry.
