package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// remote_manage.go is the write side of the remote server's
// OpenList-compatible API: the /api/fs/mkdir, /api/fs/rename, /api/fs/move,
// /api/fs/copy, /api/fs/remove, /api/fs/put (upload) and /api/fs/dirs
// handlers. Every path arrives as a virtual path under the home directory
// and goes through remoteDiskPath before anything touches the disk; the
// replies use the same always-200 {code,message,data} envelope as the read
// side (remote.go).

// remoteOpMsg is the data payload of a batch that ran to completion
// synchronously, OpenList's "completed immediately" shape.
type remoteOpMsg struct {
	Message string `json:"message"`
}

// remoteDirEntry is one folder of /api/fs/dirs' reply.
type remoteDirEntry struct {
	Name     string `json:"name"`
	Modified string `json:"modified"`
}

// remoteValidBatchName reports whether a batch name (the names arrays of
// move, copy and remove) addresses one entry of its directory: not empty,
// not "." or "..", and free of separators, so it can neither escape its
// directory nor carry a sub-path.
func remoteValidBatchName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsAny(name, `/\`)
}

// remoteBatchReq is the body of both /api/fs/move and /api/fs/copy.
type remoteBatchReq struct {
	SrcDir       string   `json:"src_dir"`
	DstDir       string   `json:"dst_dir"`
	Names        []string `json:"names"`
	Overwrite    bool     `json:"overwrite"`
	SkipExisting bool     `json:"skip_existing"`
	Merge        bool     `json:"merge"`
}

// remoteBatchItem is one name that survived a batch's pre-checks: the
// source path on disk and the destination path it should land at. merge is
// set when copy should copy into an existing destination directory instead
// of replacing it.
type remoteBatchItem struct {
	src   string
	dst   string
	merge bool
}

// remoteBatchPlan pre-checks a whole move or copy before anything is
// touched: names are cleaned (unusable ones are skipped), every source must
// exist, a directory cannot move into itself or its subtree, and a
// destination conflict either skips the item, merges directories (copy
// only), or fails the request outright — leaving the filesystem untouched.
// A non-zero code is the protocol error to answer with.
func remoteBatchPlan(req *remoteBatchReq, srcDir, dstDir string, allowMerge bool) (items []remoteBatchItem, code int, msg string) {
	for _, name := range req.Names {
		if !remoteValidBatchName(name) {
			continue // sub-paths and escapes are skipped, never acted on
		}
		src := filepath.Join(srcDir, name)
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return nil, 500, "object not found"
		}
		dst := filepath.Join(dstDir, name)
		if pathWithin(dst, src) {
			continue // into itself or its own subtree: a no-op or a cycle
		}
		dstInfo, err := os.Lstat(dst)
		if err == nil {
			if allowMerge && req.Merge && srcInfo.IsDir() && dstInfo.IsDir() {
				items = append(items, remoteBatchItem{src: src, dst: dst, merge: true})
				continue
			}
			if !req.Overwrite {
				if !req.SkipExisting {
					return nil, 403, fmt.Sprintf("file [%s] exists", name)
				}
				continue
			}
		}
		items = append(items, remoteBatchItem{src: src, dst: dst})
	}
	return items, 0, ""
}

// remoteMoveEntry moves one file or directory. os.Rename does it in one
// atomic step whenever the two ends share a volume; when it cannot (a
// cross-device move, typically), the entry is copied and the source
// dropped instead.
func remoteMoveEntry(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := remoteCopyEntry(src, dst, info); err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(src)
	}
	return os.Remove(src)
}

// remoteCopyEntry copies one entry from src to dst, recursively for
// directories, merging into a destination directory that already exists.
// io.Copy carries the bytes, the permission bits and the timestamps travel
// along behind it.
func remoteCopyEntry(src, dst string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			s := filepath.Join(src, e.Name())
			fi, err := os.Lstat(s)
			if err != nil {
				return err
			}
			if err := remoteCopyEntry(s, filepath.Join(dst, e.Name()), fi); err != nil {
				return err
			}
		}
		return os.Chmod(dst, info.Mode())
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(dst, info.Mode()); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// remoteHandleMkdir answers POST /api/fs/mkdir: one new folder. Only the
// last path level is created — the parent must exist — and an existing
// target is "file exists", as the protocol prescribes.
func (a *App) remoteHandleMkdir(w http.ResponseWriter, r *http.Request) {
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
	disk, ok := remoteDiskPath(req.Path)
	if !ok {
		remoteErr(w, 500, "object not found")
		return
	}
	if parent, err := os.Stat(filepath.Dir(disk)); err != nil || !parent.IsDir() {
		remoteErr(w, 500, "object not found")
		return
	}
	if _, err := os.Lstat(disk); err == nil {
		remoteErr(w, 403, "file exists")
		return
	}
	if err := os.Mkdir(disk, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			remoteErr(w, 403, "file exists")
			return
		}
		remoteErr(w, 500, err.Error())
		return
	}
	remoteOK(w, nil)
}

// remoteHandleRename answers POST /api/fs/rename: one entry in place, its
// parent directory unchanged. The new name must be a bare name, an
// existing target is "file [name] exists" unless overwrite allows the
// swap, and the virtual root itself is not renamable.
func (a *App) remoteHandleRename(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Name      string `json:"name"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if remoteFSCleanVPath(req.Path) == "/" {
		remoteErr(w, 500, "object not found")
		return
	}
	if req.Name == "" || req.Name == "." || req.Name == ".." || strings.ContainsAny(req.Name, `/\`) {
		remoteErr(w, 403, "invalid name")
		return
	}
	disk, ok := remoteDiskPath(req.Path)
	if !ok {
		remoteErr(w, 500, "object not found")
		return
	}
	if _, err := os.Lstat(disk); err != nil {
		remoteErr(w, 500, "object not found")
		return
	}
	dst := filepath.Join(filepath.Dir(disk), req.Name)
	if !req.Overwrite && dst != disk {
		if _, err := os.Lstat(dst); err == nil {
			remoteErr(w, 403, fmt.Sprintf("file [%s] exists", req.Name))
			return
		}
	}
	if err := os.Rename(disk, dst); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	remoteOK(w, nil)
}

// remoteHandleMove answers POST /api/fs/move: a batch of names relocated
// from src_dir to dst_dir. The whole batch is pre-checked, so a conflict
// that isn't skipped or overwritten leaves every file where it was; the
// move itself runs synchronously, one os.Rename per item.
func (a *App) remoteHandleMove(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
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
	items, code, msg := remoteBatchPlan(&req, srcDir, dstDir, false)
	if code != 0 {
		remoteErr(w, code, msg)
		return
	}
	for _, it := range items {
		if req.Overwrite {
			if _, err := os.Lstat(it.dst); err == nil {
				if err := os.RemoveAll(it.dst); err != nil {
					remoteErr(w, 500, err.Error())
					return
				}
			}
		}
		if err := remoteMoveEntry(it.src, it.dst); err != nil {
			remoteErr(w, 500, err.Error())
			return
		}
	}
	remoteOK(w, remoteOpMsg{Message: "Move operations completed immediately"})
}

// remoteHandleCopy answers POST /api/fs/move's twin /api/fs/copy: the same
// batch shape and pre-checks, plus merge — a directory copied onto an
// existing directory has its contents copied in instead of the target
// being replaced. The copy is synchronous and recursive, permissions
// carried along.
func (a *App) remoteHandleCopy(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
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
	items, code, msg := remoteBatchPlan(&req, srcDir, dstDir, true)
	if code != 0 {
		remoteErr(w, code, msg)
		return
	}
	for _, it := range items {
		if req.Overwrite && !it.merge {
			if _, err := os.Lstat(it.dst); err == nil {
				if err := os.RemoveAll(it.dst); err != nil {
					remoteErr(w, 500, err.Error())
					return
				}
			}
		}
		info, err := os.Lstat(it.src)
		if err != nil {
			remoteErr(w, 500, "object not found")
			return
		}
		if err := remoteCopyEntry(it.src, it.dst, info); err != nil {
			remoteErr(w, 500, err.Error())
			return
		}
	}
	remoteOK(w, remoteOpMsg{Message: "Copy operations completed immediately"})
}

// remoteBatchReq decodes the shared move/copy body, answering the protocol
// errors itself on failure.
func (a *App) remoteBatchReq(w http.ResponseWriter, r *http.Request) (remoteBatchReq, bool) {
	var req remoteBatchReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return req, false
	}
	if len(req.Names) == 0 {
		remoteErr(w, 400, "Empty file names")
		return req, false
	}
	return req, true
}

// remoteBatchDirs maps a move/copy request's src_dir and dst_dir onto the
// disk and requires both to be folders, so the batch runs against real
// directories. ok is false once the protocol error has been written.
func (a *App) remoteBatchDirs(w http.ResponseWriter, req *remoteBatchReq) (src, dst string, ok bool) {
	srcDir, ok := remoteDiskPath(req.SrcDir)
	if !ok {
		remoteErr(w, 500, "object not found")
		return "", "", false
	}
	dstDir, ok := remoteDiskPath(req.DstDir)
	if !ok {
		remoteErr(w, 500, "object not found")
		return "", "", false
	}
	for _, dir := range []string{srcDir, dstDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			remoteErr(w, 500, "object not found")
			return "", "", false
		}
	}
	return srcDir, dstDir, true
}

// remoteHandleRemove answers POST /api/fs/remove: a batch of names deleted
// the way Explorer deletes — moved into ~/.Trash with a restore record, so
// the 回收站 page can put them back. Already-gone entries are skipped, as
// OpenList's best-effort remove does.
func (a *App) remoteHandleRemove(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Dir   string   `json:"dir"`
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if len(req.Names) == 0 {
		remoteErr(w, 400, "Empty file names")
		return
	}
	dirDisk, ok := remoteDiskPath(req.Dir)
	if !ok {
		remoteErr(w, 500, "object not found")
		return
	}
	trash, err := trashDir()
	if err != nil {
		// ~/.Trash may be missing on a fresh account; the Finder creates it
		// on demand, so do the same and look once more.
		if home, herr := os.UserHomeDir(); herr == nil && home != "" {
			if mkErr := os.MkdirAll(filepath.Join(home, ".Trash"), 0o700); mkErr == nil {
				trash, err = trashDir()
			}
		}
		if err != nil {
			remoteErr(w, 500, err.Error())
			return
		}
	}
	for _, name := range req.Names {
		if !remoteValidBatchName(name) {
			continue
		}
		disk := filepath.Join(dirDisk, name)
		if _, err := os.Lstat(disk); err != nil {
			continue // already gone: best effort, like OpenList
		}
		dst, err := moveToTrash(disk, trash)
		if err != nil {
			remoteErr(w, 500, err.Error())
			return
		}
		if a.store != nil {
			a.store.logTrash(disk, filepath.Base(dst))
		}
	}
	remoteOK(w, nil)
}

// remoteHandleDirs answers POST /api/fs/dirs: the folders directly inside
// a virtual path, one level deep, as the flat array the file pickers want.
// force_root changes nothing — the virtual root is the only root there is.
func (a *App) remoteHandleDirs(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Password  string `json:"password"`
		ForceRoot bool   `json:"force_root"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	disk, ok := remoteDiskPath(req.Path)
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
	dirs := make([]remoteDirEntry, 0, len(entries))
	for _, de := range entries {
		// os.Stat follows symlinks, so a link to a folder lists as one, the
		// same way /api/fs/list shows it.
		st, err := os.Stat(filepath.Join(disk, de.Name()))
		if err != nil || !st.IsDir() {
			continue
		}
		dirs = append(dirs, remoteDirEntry{Name: de.Name(), Modified: st.ModTime().Format(time.RFC3339)})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	remoteOK(w, dirs)
}

// remoteHandlePut answers PUT /api/fs/put: the request body is one file's
// raw bytes, its destination the URL-escaped File-Path header (the path
// query parameter as a fallback). The stream lands in a temporary file
// next to its destination and arrives by one atomic rename, so a target
// never shows a half-written upload. Overwrite defaults to on; As-Task is
// honored by doing the work synchronously.
func (a *App) remoteHandlePut(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	vpath := r.Header.Get("File-Path")
	if vpath != "" {
		unescaped, err := url.PathUnescape(vpath)
		if err != nil {
			remoteErr(w, 400, "bad request")
			return
		}
		vpath = unescaped
	}
	if vpath == "" {
		vpath = r.URL.Query().Get("path")
	}
	if vpath == "" {
		remoteErr(w, 400, "bad request")
		return
	}
	disk, ok := remoteDiskPath(vpath)
	if !ok || remoteFSCleanVPath(vpath) == "/" {
		remoteErr(w, 500, "object not found")
		return
	}
	overwrite := r.Header.Get("Overwrite") != "false"
	if !overwrite {
		if _, err := os.Lstat(disk); err == nil {
			remoteErr(w, 403, "file exists")
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(disk), 0o755); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(disk), ".sunvale-upload-*")
	if err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // a no-op once the rename below succeeded
	}()
	if _, err := io.Copy(tmp, r.Body); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if err := os.Rename(tmpName, disk); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if ms, err := strconv.ParseInt(r.Header.Get("Last-Modified"), 10, 64); err == nil && ms > 0 {
		t := time.UnixMilli(ms)
		_ = os.Chtimes(disk, t, t)
	}
	remoteOK(w, nil)
}
