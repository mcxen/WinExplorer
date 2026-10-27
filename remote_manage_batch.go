package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"sunvale/internal/remote"
	"sunvale/internal/remote/driver"
)

// remote_manage_batch.go is the batch write half of the remote OpenList-compatible
// API: /api/fs/move, /api/fs/copy, /api/fs/remove, /api/fs/dirs and /api/fs/put.
// Split from remote_manage.go for maintainability (LAY1 / R7 start).
//
// R7 boundary: remoteBatchReq / remoteBatchDirs and the disk move/copy helpers in
// remote_manage.go are mostly pure FS; these (*App) handlers still need a
// Host-style interface before they can leave package main.

func (a *App) remoteHandleMove(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := a.remoteBatchReq(w, r)
	if !ok {
		return
	}
	srcDir, dstDir, ok := a.remoteBatchDirs(w, &req)
	if !ok {
		return
	}
	// Resolve drivers for both sides. Same mount → driver.Move; cross-mount
	// (local↔smb) falls back to copy-through-server, which the local path
	// already handles for same-driver cross-folder moves.
	srcDrv, srcInner, _ := a.remoteDriverFor(req.SrcDir)
	dstDrv, dstInner, _ := a.remoteDriverFor(req.DstDir)
	if srcDrv != nil && dstDrv != nil && srcDrv == dstDrv {
		// Same driver (same mount): operate on driver paths directly.
		wr, isW := srcDrv.(driver.Writer)
		if !isW {
			remote.Err(w, 403, errStorageReadOnly.Error())
			return
		}
		for _, name := range req.Names {
			if !remote.ValidBatchName(name) {
				continue
			}
			s := path.Join(srcInner, name)
			d := path.Join(dstInner, name)
			// A folder moved into its own subtree is skipped, as the old
			// batch plan did — a no-op rather than a cycle.
			if strings.HasPrefix(d, s+"/") {
				continue
			}
			if _, err := srcDrv.Get(r.Context(), s); err != nil {
				remote.Err(w, 500, "object not found")
				return
			}
			if req.Overwrite {
				_ = wr.Remove(r.Context(), d)
			} else if _, err := srcDrv.Get(r.Context(), d); err == nil {
				if req.SkipExisting {
					continue
				}
				remote.Err(w, 403, fmt.Sprintf("file [%s] exists", name))
				return
			}
			if err := wr.Move(r.Context(), s, d); err != nil {
				remote.Err(w, 500, err.Error())
				return
			}
		}
		remote.OK(w, remoteOpMsg{Message: "Move operations completed immediately"})
		return
	}
	items, code, msg := remoteBatchPlan(&req, srcDir, dstDir, false)
	if code != 0 {
		remote.Err(w, code, msg)
		return
	}
	for _, it := range items {
		if req.Overwrite {
			if _, err := os.Lstat(it.dst); err == nil {
				if err := os.RemoveAll(it.dst); err != nil {
					remote.Err(w, 500, err.Error())
					return
				}
			}
		}
		if err := remoteMoveEntry(it.src, it.dst); err != nil {
			remote.Err(w, 500, err.Error())
			return
		}
	}
	remote.OK(w, remoteOpMsg{Message: "Move operations completed immediately"})
}

// remoteHandleCopy answers POST /api/fs/move's twin /api/fs/copy: the same
// batch shape and pre-checks, plus merge — a directory copied onto an
// existing directory has its contents copied in instead of the target
// being replaced. The copy is synchronous and recursive, permissions
// carried along.
func (a *App) remoteHandleCopy(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	req, ok := a.remoteBatchReq(w, r)
	if !ok {
		return
	}
	srcDir, dstDir, ok := a.remoteBatchDirs(w, &req)
	if !ok {
		return
	}
	// Same-driver copy goes through the driver interface.
	srcDrv, srcInner, _ := a.remoteDriverFor(req.SrcDir)
	dstDrv, dstInner, _ := a.remoteDriverFor(req.DstDir)
	if srcDrv != nil && dstDrv != nil && srcDrv == dstDrv {
		wr, isW := srcDrv.(driver.Writer)
		if !isW {
			remote.Err(w, 403, errStorageReadOnly.Error())
			return
		}
		for _, name := range req.Names {
			if !remote.ValidBatchName(name) {
				continue
			}
			s := path.Join(srcInner, name)
			d := path.Join(dstInner, name)
			if req.Overwrite {
				_ = wr.Remove(r.Context(), d)
			} else if _, err := srcDrv.Get(r.Context(), d); err == nil && !req.Merge {
				remote.Err(w, 403, fmt.Sprintf("file [%s] exists", name))
				return
			}
			if err := wr.Copy(r.Context(), s, d); err != nil {
				remote.Err(w, 500, err.Error())
				return
			}
		}
		remote.OK(w, remoteOpMsg{Message: "Copy operations completed immediately"})
		return
	}
	items, code, msg := remoteBatchPlan(&req, srcDir, dstDir, true)
	if code != 0 {
		remote.Err(w, code, msg)
		return
	}
	for _, it := range items {
		if req.Overwrite && !it.merge {
			if _, err := os.Lstat(it.dst); err == nil {
				if err := os.RemoveAll(it.dst); err != nil {
					remote.Err(w, 500, err.Error())
					return
				}
			}
		}
		info, err := os.Lstat(it.src)
		if err != nil {
			remote.Err(w, 500, "object not found")
			return
		}
		if err := remoteCopyEntry(it.src, it.dst, info); err != nil {
			remote.Err(w, 500, err.Error())
			return
		}
	}
	remote.OK(w, remoteOpMsg{Message: "Copy operations completed immediately"})
}

// remoteBatchReq decodes the shared move/copy body, answering the protocol
// errors itself on failure.
func (a *App) remoteBatchReq(w http.ResponseWriter, r *http.Request) (remoteBatchReq, bool) {
	var req remoteBatchReq
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return req, false
	}
	if len(req.Names) == 0 {
		remote.Err(w, 400, "Empty file names")
		return req, false
	}
	return req, true
}

// remoteBatchDirs maps a move/copy request's src_dir and dst_dir onto the
// disk and requires both to be folders, so the batch runs against real
// directories. ok is false once the protocol error has been written.
func (a *App) remoteBatchDirs(w http.ResponseWriter, req *remoteBatchReq) (src, dst string, ok bool) {
	srcDir, ok := a.remoteDiskPath(req.SrcDir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return "", "", false
	}
	dstDir, ok := a.remoteDiskPath(req.DstDir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return "", "", false
	}
	for _, dir := range []string{srcDir, dstDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			remote.Err(w, 500, "object not found")
			return "", "", false
		}
	}
	// Either side on a read-only mount kills the operation: moving out of
	// an SMB share writes to its source, copying into one writes to its
	// destination.
	if !a.remoteWriteGuard(w, req.SrcDir, req.DstDir) {
		return "", "", false
	}
	return srcDir, dstDir, true
}

// remoteHandleRemove answers POST /api/fs/remove: a batch of names deleted
// the way Explorer deletes — moved into ~/.Trash with a restore record, so
// the 回收站 page can put them back. Already-gone entries are skipped, as
// OpenList's best-effort remove does.
func (a *App) remoteHandleRemove(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Dir   string   `json:"dir"`
		Names []string `json:"names"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if len(req.Names) == 0 {
		remote.Err(w, 400, "Empty file names")
		return
	}
	dirDisk, ok := a.remoteDiskPath(req.Dir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	if !a.remoteWriteGuard(w, req.Dir) {
		return
	}
	d, inner, ok := a.remoteDriverFor(req.Dir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	wr, isWriter := d.(driver.Writer)
	for _, name := range req.Names {
		if !remote.ValidBatchName(name) {
			continue
		}
		itemInner := path.Join(inner, name)
		if _, err := d.Get(r.Context(), itemInner); err != nil {
			continue // already gone: best effort, like OpenList
		}
		// Local mounts trash through the desktop 回收站 for undo; network
		// mounts remove directly, since there is no bin on the share.
		if isWriter && d.Config().ID == "smb" {
			if err := wr.Remove(r.Context(), itemInner); err != nil {
				remote.Err(w, 500, err.Error())
				return
			}
			continue
		}
		disk := filepath.Join(dirDisk, name)
		dst, key, err := sysTrashMove(disk)
		if err != nil {
			remote.Err(w, 500, err.Error())
			return
		}
		if a.store != nil && key != "" {
			a.store.logTrash(disk, key)
		}
		_ = dst
	}
	remote.OK(w, nil)
}

// remoteHandleDirs answers /api/fs/dirs, on any method: the folders directly
// inside a virtual path, one level deep, as the flat array the file pickers
// want. A missing path is the virtual root; force_root changes nothing —
// the virtual root is the only root there is.
func (a *App) remoteHandleDirs(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Password  string `json:"password"`
		ForceRoot bool   `json:"force_root"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	d, inner, ok := a.remoteDriverFor(req.Path)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	entries, err := d.List(r.Context(), inner)
	if err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	dirs := make([]remoteDirEntry, 0, len(entries))
	hidden := a.remoteShowHidden()
	for _, o := range entries {
		if !hidden && strings.HasPrefix(o.Name, ".") {
			continue
		}
		if !o.IsDir {
			continue
		}
		dirs = append(dirs, remoteDirEntry{Name: o.Name, Modified: o.ModTime})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	remote.OK(w, dirs)
}

// remoteHandlePut answers PUT /api/fs/put: the request body is one file's
// raw bytes, its destination the URL-escaped File-Path header (the path
// query parameter as a fallback). The stream lands in a temporary file
// next to its destination and arrives by one atomic rename, so a target
// never shows a half-written upload. Overwrite defaults to on; As-Task is
// honored by doing the work synchronously.
func (a *App) remoteHandlePut(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	vpath := r.Header.Get("File-Path")
	if vpath != "" {
		unescaped, err := url.PathUnescape(vpath)
		if err != nil {
			remote.Err(w, 400, "bad request")
			return
		}
		vpath = unescaped
	}
	if vpath == "" {
		vpath = r.URL.Query().Get("path")
	}
	if vpath == "" {
		remote.Err(w, 400, "bad request")
		return
	}
	if !a.remoteWriteGuard(w, vpath) {
		return
	}
	d, inner, ok := a.remoteDriverFor(vpath)
	if !ok || remote.FSCleanVPath(vpath) == "/" {
		remote.Err(w, 500, "object not found")
		return
	}
	wr, ok := d.(driver.Writer)
	if !ok {
		remote.Err(w, 403, errStorageReadOnly.Error())
		return
	}
	overwrite := r.Header.Get("Overwrite") != "false"
	if !overwrite {
		if _, err := d.Get(r.Context(), inner); err == nil {
			remote.Err(w, 403, "file exists")
			return
		}
	}
	if err := wr.Put(r.Context(), inner, r.Body); err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	if ms, err := strconv.ParseInt(r.Header.Get("Last-Modified"), 10, 64); err == nil && ms > 0 {
		// mtime is best-effort; the local driver could Chtimes but the
		// interface does not expose it, and the upload's own mtime is
		// rarely wrong enough to matter over a network mount.
		_ = ms
	}
	remote.OK(w, nil)
}
