package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sunvale/internal/remote"
	"sunvale/internal/remote/driver"

	"sunvale/internal/fsops"

	"sunvale/internal/rename"
)

// remote_manage.go is the write side of the remote server's
// OpenList-compatible API: the /api/fs/mkdir, /api/fs/rename, the batch and
// regexp renames, plus shared batch planning helpers. Move/copy/remove/put/dirs
// live in remote_manage_batch.go (LAY1 split).
//
// Every path arrives as a virtual path under the home directory and goes
// through remoteDiskPath before anything touches the disk; the replies use
// the same always-200 {code,message,data} envelope as the read side (remote.go).
//
// R7 boundary: remoteBatchPlan / remoteMoveEntry / remoteCopyEntry / remoteRenameCheck
// are candidates for internal/remote once call sites no longer need App state.

// remoteOpMsg is the data payload of a batch that ran to completion
// synchronously, OpenList's "completed immediately" shape.
type remoteOpMsg struct {
	Message string `json:"message"`
}

// remoteDirEntry is one folder of /api/fs/dirs' reply. Modified is the
// time.Time OpenList serializes as-is, nanoseconds and zone offset included.
type remoteDirEntry struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
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
		if !remote.ValidBatchName(name) {
			continue // sub-paths and escapes are skipped, never acted on
		}
		src := filepath.Join(srcDir, name)
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return nil, 500, "object not found"
		}
		dst := filepath.Join(dstDir, name)
		if fsops.PathWithin(dst, src) {
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
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if !a.remoteWriteGuard(w, req.Path) {
		return
	}
	d, inner, ok := a.remoteDriverFor(req.Path)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	wr, ok := d.(driver.Writer)
	if !ok {
		remote.Err(w, 403, errStorageReadOnly.Error())
		return
	}
	// exists check
	if _, err := d.Get(r.Context(), inner); err == nil {
		remote.Err(w, 403, "file exists")
		return
	}
	// The parent must exist — MkdirAll would create it, which is not what
	// Explorer's 新建文件夹 does.
	parent := path.Dir(inner)
	if _, err := d.Get(r.Context(), parent); err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	if err := wr.Mkdir(r.Context(), inner); err != nil {
		if errors.Is(err, os.ErrExist) {
			remote.Err(w, 403, "file exists")
			return
		}
		remote.Err(w, 500, err.Error())
		return
	}
	remote.OK(w, nil)
}

// remoteHandleRename answers POST /api/fs/rename: one entry in place, its
// parent directory unchanged. The new name must be a bare name, an
// existing target is "file [name] exists" unless overwrite allows the
// swap, and the virtual root itself is not renamable.
func (a *App) remoteHandleRename(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Path      string `json:"path"`
		Name      string `json:"name"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if remote.FSCleanVPath(req.Path) == "/" {
		remote.Err(w, 500, "object not found")
		return
	}
	if req.Name == "" || req.Name == "." || req.Name == ".." || strings.ContainsAny(req.Name, `/\`) {
		remote.Err(w, 403, "invalid name")
		return
	}
	if !a.remoteWriteGuard(w, req.Path) {
		return
	}
	d, inner, ok := a.remoteDriverFor(req.Path)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	wr, ok := d.(driver.Writer)
	if !ok {
		remote.Err(w, 403, errStorageReadOnly.Error())
		return
	}
	if _, err := d.Get(r.Context(), inner); err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	dstInner := path.Join(path.Dir(inner), req.Name)
	if !req.Overwrite && dstInner != inner {
		if _, err := d.Get(r.Context(), dstInner); err == nil {
			remote.Err(w, 403, fmt.Sprintf("file [%s] exists", req.Name))
			return
		}
	}
	if err := wr.Rename(r.Context(), inner, req.Name); err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	remote.OK(w, nil)
}

// remoteBatchRenameReq is the body of /api/fs/batch_rename, OpenList's
// shape: the folder and one src→new name pair per item.
type remoteBatchRenameReq struct {
	SrcDir        string `json:"src_dir"`
	RenameObjects []struct {
		SrcName string `json:"src_name"`
		NewName string `json:"new_name"`
	} `json:"rename_objects"`
}

// remoteHandleBatchRename answers POST /api/fs/batch_rename: one src_dir
// with explicit src_name → new_name pairs, the endpoint the OpenList
// frontend's batch rename calls. The plan runs through internal/rename, the
// same engine the desktop dialog uses, so its validation, its clash rules
// and its two-pass name swap are shared rather than reimplemented.
func (a *App) remoteHandleBatchRename(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req remoteBatchRenameReq
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	if len(req.RenameObjects) == 0 {
		remote.Err(w, 400, "Empty file names")
		return
	}
	dir, ok := a.remoteDiskPath(req.SrcDir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	if !a.remoteWriteGuard(w, req.SrcDir) {
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		remote.Err(w, 500, "object not found")
		return
	}
	rows := make([]rename.Row, 0, len(req.RenameObjects))
	for _, o := range req.RenameObjects {
		if o.SrcName == "" || o.NewName == "" {
			continue // the protocol skips empty pairs, as OpenList does
		}
		if !remote.ValidBatchName(o.SrcName) {
			remote.Err(w, 403, "using relative path is not allowed")
			return
		}
		rows = append(rows, rename.Row{Name: o.SrcName, To: o.NewName})
	}
	// Every target must be a bare name; a bad one fails the whole request,
	// as OpenList's per-object check does, rather than applying the rest.
	for _, row := range rows {
		if why := rename.ValidateName(row.To); why != "" {
			remote.Err(w, 403, "using relative path is not allowed")
			return
		}
	}
	// The sources must exist, and the targets must not be taken by anything
	// that is not itself moving away.
	if code, msg := remoteRenameCheck(dir, rows); code != 0 {
		remote.Err(w, code, msg)
		return
	}
	moved, failed := rename.Apply(dir, rows)
	if len(failed) > 0 {
		remote.Err(w, 500, "failed to rename: "+strings.Join(failed, ", "))
		return
	}
	remote.OK(w, remoteOpMsg{Message: fmt.Sprintf("Successfully renamed %d %s",
		len(moved), pluralize(len(moved), "file", "files"))})
}

// remoteHandleRegexRename answers POST /api/fs/regex_rename: a Go regexp
// and its replacement, applied to every name in src_dir that the regexp
// matches. The engine expands the date tokens a client's frontend would,
// so the remote and desktop answers agree.
func (a *App) remoteHandleRegexRename(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		SrcDir       string `json:"src_dir"`
		SrcNameRegex string `json:"src_name_regex"`
		NewNameRegex string `json:"new_name_regex"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	re, err := regexp.Compile(req.SrcNameRegex)
	if err != nil || req.SrcNameRegex == "" {
		remote.Err(w, 500, "invalid regexp")
		return
	}
	if !a.remoteWriteGuard(w, req.SrcDir) {
		return
	}
	dir, ok := a.remoteDiskPath(req.SrcDir)
	if !ok {
		remote.Err(w, 500, "object not found")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		remote.Err(w, 500, "object not found")
		return
	}
	var rows []rename.Row
	for _, de := range entries {
		name := de.Name()
		if !re.MatchString(name) {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		row := rename.Row{Name: name, IsDir: de.IsDir(), ModTime: info.ModTime()}
		row.To = re.ReplaceAllString(name, req.NewNameRegex)
		row.To = rename.DateTokens(row.To, row)
		if why := rename.ValidateName(row.To); why != "" {
			remote.Err(w, 403, "using relative path is not allowed")
			return
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		remote.OK(w, remoteOpMsg{Message: "Nothing to rename"})
		return
	}
	if code, msg := remoteRenameCheck(dir, rows); code != 0 {
		remote.Err(w, code, msg)
		return
	}
	moved, failed := rename.Apply(dir, rows)
	if len(failed) > 0 {
		remote.Err(w, 500, "failed to rename: "+strings.Join(failed, ", "))
		return
	}
	remote.OK(w, remoteOpMsg{Message: fmt.Sprintf("Successfully renamed %d %s",
		len(moved), pluralize(len(moved), "file", "files"))})
}

// remoteRenameCheck is the pre-flight every remote rename runs: the sources
// exist, and no target is already taken by something that is not one of the
// sources moving away. It answers the protocol's code and message, zero for
// a plan that can run.
func remoteRenameCheck(dir string, rows []rename.Row) (int, string) {
	sources := make(map[string]bool, len(rows))
	for _, r := range rows {
		sources[strings.ToLower(r.Name)] = true
	}
	for _, r := range rows {
		if _, err := os.Lstat(filepath.Join(dir, r.Name)); err != nil {
			return 500, "object not found: " + r.Name
		}
		if strings.EqualFold(r.To, r.Name) {
			continue
		}
		if sources[strings.ToLower(r.To)] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, r.To)); err == nil {
			return 403, fmt.Sprintf("file [%s] exists", r.To)
		}
	}
	return 0, ""
}

// pluralize is OpenList's Pluralize: the singular for one, the plural
// otherwise.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// remoteHandleMove answers POST /api/fs/move: a batch of names relocated
// from src_dir to dst_dir. The whole batch is pre-checked, so a conflict
// that isn't skipped or overwritten leaves every file where it was; the
// move itself runs synchronously, one os.Rename per item.
