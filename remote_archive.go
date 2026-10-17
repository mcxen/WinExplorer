package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// remote_archive.go is the archive side of the remote server: the
// OpenList-compatible /api/fs/archive/unzip and /api/fs/archive/zip
// handlers, plus the extension list /api/public/archive_extensions hands
// the clients. The work itself is archive.go's — the same extractArchive
// and makeZip the desktop uses, so the zip-slip defenses, the tar family
// and the 7zz/unar fallbacks are shared, not reimplemented.
//
// Both endpoints run synchronously, like every other write here: the reply
// means the folder is already on disk, or the request failed. No task
// queue, no polling.

// remoteArchiveExts is what /api/public/archive_extensions answers with:
// the suffixes this server can extract, so a client knows when to offer
// 解压. It stays the same shape the route always had — an array of
// strings — with a leading dot so a client can test a name's tail.
func remoteArchiveExts() []string {
	return []string{
		".zip", ".tar", ".tar.gz", ".tgz", ".tar.bz2", ".tbz2", ".tar.xz",
		".txz", ".7z", ".rar",
	}
}

// remoteHandleArchiveExt implements GET /api/public/archive_extensions,
// which the web UI requests at startup and breaks without.
func (a *App) remoteHandleArchiveExt(w http.ResponseWriter, r *http.Request) {
	remoteOK(w, remoteArchiveExts())
}

// remoteArchiveReq is the body of both archive endpoints. A batch sends
// names; a single item sends name, and names wins when both arrive. dst_dir
// defaults to src_dir, encoding is accepted and ignored — archives here are
// always read with the platform's own name encoding.
type remoteArchiveReq struct {
	SrcDir   string   `json:"src_dir"`
	DstDir   string   `json:"dst_dir"`
	Name     string   `json:"name"`
	Names    []string `json:"names"`
	Encoding string   `json:"encoding"`
}

// targets is the request's item set: names when a batch was sent, otherwise
// the single name.
func (req *remoteArchiveReq) targets() []string {
	if len(req.Names) > 0 {
		return req.Names
	}
	if req.Name != "" {
		return []string{req.Name}
	}
	return nil
}

// remoteArchiveResult is the data payload of a finished archive request:
// the virtual paths just written, the names that were left alone and why.
type remoteArchiveResult struct {
	Message string   `json:"message"`
	Created []string `json:"created"`
	Failed  []string `json:"failed"`
	Reason  string   `json:"reason"`
}

// remoteArchiveDirs decodes the request and maps src_dir and dst_dir onto
// the disk, both of them folders that must already exist. dst_dir defaults
// to src_dir. ok is false once the protocol error has been written.
func (a *App) remoteArchiveDirs(w http.ResponseWriter, r *http.Request, req *remoteArchiveReq) (src, dst string, ok bool) {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(req); err != nil {
		remoteErr(w, 400, "bad request")
		return "", "", false
	}
	if len(req.targets()) == 0 {
		remoteErr(w, 400, "Empty file names")
		return "", "", false
	}
	srcDir, ok := remoteDiskPath(req.SrcDir)
	if !ok {
		remoteErr(w, 500, "object not found")
		return "", "", false
	}
	dstDir := srcDir
	if req.DstDir != "" {
		if dstDir, ok = remoteDiskPath(req.DstDir); !ok {
			remoteErr(w, 500, "object not found")
			return "", "", false
		}
	}
	for _, dir := range []string{srcDir, dstDir} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			remoteErr(w, 500, "object not found")
			return "", "", false
		}
	}
	return srcDir, dstDir, true
}

// remoteHandleArchiveUnzip answers POST /api/fs/archive/unzip: each named
// archive is extracted into its own folder, named after the archive the way
// Explorer does it, inside dst_dir (src_dir by default). A name already in
// use gets Explorer's " - 副本" treatment instead of a failure, an archive
// that cannot be read is reported in reason, and a batch where nothing was
// written fails outright so the caller sees why.
func (a *App) remoteHandleArchiveUnzip(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req remoteArchiveReq
	srcDir, dstDir, ok := a.remoteArchiveDirs(w, r, &req)
	if !ok {
		return
	}
	var created, failed []string
	reason := ""
	for _, name := range req.targets() {
		if !remoteValidBatchName(name) {
			continue // sub-paths and escapes are skipped, never acted on
		}
		disk := filepath.Join(srcDir, name)
		st, err := os.Lstat(disk)
		if err != nil {
			failed = append(failed, name)
			if reason == "" {
				reason = "object not found"
			}
			continue
		}
		if st.IsDir() || !isArchive(name) {
			failed = append(failed, name)
			if reason == "" {
				reason = fmt.Sprintf("%s is not an archive", name)
			}
			continue
		}
		base, err := uniqueFreeName(dstDir, archiveExtName(name), true)
		if err != nil {
			failed = append(failed, name)
			if reason == "" {
				reason = err.Error()
			}
			continue
		}
		target := filepath.Join(dstDir, base)
		if err := extractArchive(disk, target); err != nil {
			_ = os.RemoveAll(target) // never leave a half-extracted folder
			failed = append(failed, name)
			if reason == "" {
				reason = fmt.Sprintf("%s: %v", name, err)
			}
			continue
		}
		created = append(created, remoteVPath(target))
	}
	if len(created) == 0 {
		if reason == "" {
			reason = "object not found"
		}
		remoteErr(w, 500, reason)
		return
	}
	remoteOK(w, remoteArchiveResult{
		Message: "Unzip operations completed immediately",
		Created: created,
		Failed:  failed,
		Reason:  reason,
	})
}

// remoteHandleArchiveZip answers POST /api/fs/archive/zip: the named items
// are packed into one ZIP inside dst_dir (src_dir by default), named after
// the single item or "压缩文件" for a batch, with a free name when that
// one is taken — 压缩为 ZIP 文件 as Explorer spells it. Folders go in
// recursively under their own name; symlinks are skipped.
func (a *App) remoteHandleArchiveZip(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req remoteArchiveReq
	srcDir, dstDir, ok := a.remoteArchiveDirs(w, r, &req)
	if !ok {
		return
	}
	names := req.targets()
	stem := "压缩文件"
	if len(names) == 1 {
		stem = strings.TrimSuffix(names[0], filepath.Ext(names[0]))
	}
	entries := make([]Entry, 0, len(names))
	for _, name := range names {
		if !remoteValidBatchName(name) {
			continue
		}
		disk := filepath.Join(srcDir, name)
		st, err := os.Lstat(disk)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{Name: filepath.Base(disk), Path: disk, IsDir: st.IsDir()})
	}
	if len(entries) == 0 {
		remoteErr(w, 500, "object not found")
		return
	}
	name, err := uniqueFreeName(dstDir, stem+".zip", false)
	if err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	dst := filepath.Join(dstDir, name)
	if err := makeZip(dst, entries); err != nil {
		_ = os.Remove(dst)
		remoteErr(w, 500, err.Error())
		return
	}
	remoteOK(w, remoteArchiveResult{
		Message: "Zip operations completed immediately",
		Created: []string{remoteVPath(dst)},
	})
}
