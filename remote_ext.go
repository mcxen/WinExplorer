package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// remote_ext.go is the manage console's shell extension surface: the
// context-menu commands the user registers in
// os.UserConfigDir()/winexplorer/extensions.json (extensions.go, Explorer's
// registry verb equivalent) listed and run for one item over the LAN, plus
// the 在此处打开终端 endpoint. Every handler is the administrator's alone —
// guests get the login envelope — and answers the same always-200
// {code,message,data} envelope as the rest of the API. The registry is read
// per request, so edits to extensions.json land on the next call.

// remoteExtItem is one shell extension of /api/fs/ext/list's reply: the
// menu name and whether running it opens a terminal window.
type remoteExtItem struct {
	Name     string `json:"name"`
	Terminal bool   `json:"terminal"`
}

// remoteExtListData is the data payload of /api/fs/ext/list.
type remoteExtListData struct {
	Exts []remoteExtItem `json:"exts"`
}

// remoteExtDiskPath maps an ext request's virtual path onto the disk, like
// remoteDiskPath, but refuses any ".." segment outright: the fs endpoints
// let path.Clean absorb a climb at the virtual root, while the ext verbs
// hand the resolved path to shell commands, so an escape attempt is worth a
// 400 rather than a silently resolved name.
func remoteExtDiskPath(vpath string) (string, bool) {
	for _, seg := range strings.Split(strings.ReplaceAll(vpath, "\\", "/"), "/") {
		if seg == ".." {
			return "", false
		}
	}
	return remoteDiskPath(vpath)
}

// remoteExtEntry builds the pseudo-Entry the shell extensions match and run
// against: the request's name over the disk path, is_dir from the stat and
// the extension taken from the name.
func remoteExtEntry(name, disk string, st os.FileInfo) Entry {
	if name == "" {
		name = filepath.Base(disk)
	}
	return Entry{
		Name:  name,
		Path:  disk,
		IsDir: st.IsDir(),
		Ext:   strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")),
	}
}

// remoteExtTarget resolves an ext request's item: the virtual path onto the
// disk (remoteExtDiskPath's ".." refusal included), the pseudo-Entry the
// exts match against and a fresh read of the registry. The protocol errors
// are answered here; ok is false once the reply is written.
func remoteExtTarget(w http.ResponseWriter, vpath, name string) (Entry, []shellExt, bool) {
	disk, ok := remoteExtDiskPath(vpath)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return Entry{}, nil, false
	}
	st, err := os.Stat(disk)
	if err != nil {
		remoteErr(w, 500, "object not found")
		return Entry{}, nil, false
	}
	exts, _, err := loadShellExtensions()
	if err != nil {
		remoteErr(w, 500, err.Error())
		return Entry{}, nil, false
	}
	return remoteExtEntry(name, disk, st), exts, true
}

// remoteHandleExtList answers POST /api/fs/ext/list: the shell extensions
// that match one item, in registration order, as the manage console's
// right-click menu wants them.
func (a *App) remoteHandleExtList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Path == "" {
		remoteErr(w, 400, "path is required")
		return
	}
	e, exts, ok := remoteExtTarget(w, req.Path, req.Name)
	if !ok {
		return
	}
	out := make([]remoteExtItem, 0, len(exts))
	for _, x := range exts {
		if x.Disabled {
			continue
		}
		if x.matches(e) {
			out = append(out, remoteExtItem{Name: x.Name, Terminal: x.Terminal})
		}
	}
	remoteOK(w, remoteExtListData{Exts: out})
}

// remoteHandleExtRun answers POST /api/fs/ext/run: one shell extension run
// server-side against one item. x.run is already detached — sh -c with a
// reaping goroutine, or a new terminal window for terminal:true — so it
// starts in a goroutine and the reply never waits for a slow tool.
func (a *App) remoteHandleExtRun(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path    string `json:"path"`
		Name    string `json:"name"`
		ExtName string `json:"ext_name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Path == "" {
		remoteErr(w, 400, "path is required")
		return
	}
	if strings.TrimSpace(req.ExtName) == "" {
		remoteErr(w, 400, "ext_name is required")
		return
	}
	e, exts, ok := remoteExtTarget(w, req.Path, req.Name)
	if !ok {
		return
	}
	var chosen *shellExt
	for i := range exts {
		if exts[i].Name == req.ExtName && !exts[i].Disabled && exts[i].matches(e) {
			chosen = &exts[i]
			break
		}
	}
	if chosen == nil {
		remoteErr(w, 500, "extension not found")
		return
	}
	x := *chosen
	go func() { _ = x.run(e) }()
	remoteOK(w, nil)
}

// remoteHandleTerminal answers POST /api/fs/terminal: the system terminal
// opened at a folder, 在此处打开终端 over the LAN. Folders only, and
// sysTerminal can take a moment, so it starts in a goroutine and the reply
// does not wait for the window.
func (a *App) remoteHandleTerminal(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Path == "" {
		remoteErr(w, 400, "path is required")
		return
	}
	disk, ok := remoteExtDiskPath(req.Path)
	if !ok {
		remoteErr(w, 400, "invalid path")
		return
	}
	st, err := os.Stat(disk)
	if err != nil {
		remoteErr(w, 500, "object not found")
		return
	}
	if !st.IsDir() {
		remoteErr(w, 400, "not a folder")
		return
	}
	go func() { _ = sysTerminal(disk) }()
	remoteOK(w, nil)
}
