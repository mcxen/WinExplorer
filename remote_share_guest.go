package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"sunvale/internal/remote"
)

// remote_share_guest.go is the guest side of remote shares: validity checks,
// virtual-path resolve, /@s listing and get, /sd downloads, and access
// counting. Split from remote_share.go (LAY1 / R7 start).
//
// R7 boundary: remoteShareIsValid / SplitPath / Resolve / EncodePath are pure
// path helpers; FS handlers still bind App and the share store.

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
		f = remote.FSCleanVPath(f)
		if path.Base(f) == child {
			return path.Join(f, rest), true
		}
	}
	p := "/" + sub
	for _, f := range files {
		f = remote.FSCleanVPath(f)
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
	d, inner, ok := a.remoteDriverFor(vpath)
	if !ok {
		return remoteObj{}, false
	}
	o, err := d.Get(context.Background(), inner)
	if err != nil {
		return remoteObj{}, false
	}
	obj := a.remoteFSObj(path.Base(vpath), vpath, o)
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
		remote.Err(w, code, msg)
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
			remote.Err(w, 500, "object not found")
			return
		}
		sd, sInner, ok := a.remoteDriverFor(vp)
		if !ok {
			remote.Err(w, 500, "object not found")
			return
		}
		entries, err := sd.List(r.Context(), sInner)
		if err != nil {
			remote.Err(w, 500, "object not found")
			return
		}
		objs = make([]remoteObj, 0, len(entries))
		for _, o := range entries {
			name := o.Name
			obj, ok := a.remoteShareObj(path.Join(vp, name), "")
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
	remote.OK(w, remoteFSListData{
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
		remote.Err(w, code, msg)
		return
	}
	data := remoteFSGetData{
		remoteObj: remoteObj{
			Name:     sid,
			IsDir:    true,
			Type:     remote.FSTypeFolder,
			HashInfo: "null",
		},
		Readme:   share.Readme,
		Header:   share.Header,
		Provider: "unknown",
	}
	if sub != "" {
		vp, ok := remoteShareResolve(share.Files, sub)
		if !ok {
			remote.Err(w, 500, "object not found")
			return
		}
		d, inner, ok := a.remoteDriverFor(vp)
		if !ok {
			remote.Err(w, 500, "object not found")
			return
		}
		o, err := d.Get(r.Context(), inner)
		if err != nil {
			remote.Err(w, 500, "object not found")
			return
		}
		obj := a.remoteFSObj(path.Base(vp), vp, o)
		obj.Sign = ""
		data.remoteObj = obj
		if !obj.IsDir {
			link := remote.RequestBase(r) + "/sd/" + sid + remoteShareEncodePath(sub)
			if share.Pwd != "" {
				link += "?pwd=" + url.QueryEscape(share.Pwd)
			}
			data.RawURL = link
		}
	}
	a.remoteShareCountAccess(sid, r)
	remote.OK(w, data)
}

// remoteHandleShareDown implements GET/HEAD /sd/<sid>/<path>: the guest
// download of one file inside a share, the password in the pwd query
// parameter. Errors are OpenList's HTML pages.
func (a *App) remoteHandleShareDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		remote.HTMLFail(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/sd/")
	sid, sub, _ := strings.Cut(rest, "/")
	sub = strings.TrimPrefix(remote.FSCleanVPath("/"+sub), "/")
	share, code, msg := a.remoteShareLookup(sid, r.URL.Query().Get("pwd"))
	if code != 0 {
		remote.HTMLFail(w, code, msg)
		return
	}
	if sub == "" {
		remote.HTMLFail(w, http.StatusInternalServerError, "not a file")
		return
	}
	vp, ok := remoteShareResolve(share.Files, sub)
	if !ok {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	sd, sInner, ok := a.remoteDriverFor(vp)
	if !ok {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	o, err := sd.Get(r.Context(), sInner)
	if err != nil {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	if o.IsDir {
		remote.HTMLFail(w, http.StatusInternalServerError, "not a file")
		return
	}
	f, err := sd.Open(r.Context(), sInner)
	if err != nil {
		remote.HTMLFail(w, http.StatusInternalServerError, "file not found")
		return
	}
	defer f.Close()
	a.remoteShareCountAccess(sid, r)
	name := o.Name
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=utf-8''%s`,
		url.PathEscape(name), url.PathEscape(name)))
	w.Header().Set("Etag", fmt.Sprintf(`"%x-%x"`, o.ModTime.Unix(), o.Size))
	http.ServeContent(w, r, name, o.ModTime, f)
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
