package main

import (
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"sunvale/internal/remote"
)

// remote_preview.go serves /api/fs/preview for files a browser cannot render
// itself: sniff/plain-text/hex. Office/OpenDocument live in
// remote_preview_office.go; zip/tar listings in remote_preview_archive.go
// (LAY1 split).
//
// R7 boundary: buildFilePreview and the pure readers below are strong
// candidates for internal/remote; the (*App) HTTP handler needs auth/path Host.

const (
	previewTextCap   = 64 << 10
	previewXMLCap    = 4 << 20
	previewOfficeCap = 40 << 20
	previewListCap   = 200
	previewHexBytes  = 256
)

// previewPayload is one /api/fs/preview body. Text is already clipped;
// entries is a zip or tar listing.
type previewPayload struct {
	Kind      string         `json:"kind"`
	Text      string         `json:"text,omitempty"`
	Truncated bool           `json:"truncated,omitempty"`
	Note      string         `json:"note,omitempty"`
	Entries   []previewEntry `json:"entries,omitempty"`
}

// previewEntry is one name inside an archive.
type previewEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Dir  bool   `json:"dir"`
}

// remoteHandleFSPreview answers /api/fs/preview. A share path is open to
// its guest, the same way /api/fs/get is; every other path needs the owner.
func (a *App) remoteHandleFSPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Password string `json:"password"`
	}
	if err := remote.ReadReq(r, &req); err != nil {
		remote.Err(w, 400, "bad request")
		return
	}
	disk, code, msg := a.remoteReadableDisk(r, req.Path, req.Password)
	if code != 0 {
		remote.Err(w, code, msg)
		return
	}
	info, err := os.Stat(disk)
	if err != nil || info.IsDir() {
		remote.Err(w, 500, "object not found")
		return
	}
	remote.OK(w, buildFilePreview(disk, info.Size()))
}

// remoteReadableDisk resolves a virtual path to a file the caller may
// read: a share guest for /@s/…, the owner otherwise.
func (a *App) remoteReadableDisk(r *http.Request, vpath, password string) (string, int, string) {
	vpath = remote.FSCleanVPath(vpath)
	if remoteShareVPath(vpath) {
		sid, sub := remoteShareSplitPath(vpath)
		share, code, msg := a.remoteShareLookup(sid, password)
		if code != 0 {
			return "", code, msg
		}
		if sub == "" {
			return "", 400, "not a file"
		}
		vp, ok := remoteShareResolve(share.Files, sub)
		if !ok {
			return "", 500, "object not found"
		}
		vpath = vp
	} else if !a.remoteIsAdmin(r) {
		return "", 401, "Guest user is disabled, login please"
	}
	disk, ok := a.remoteDiskPath(vpath)
	if !ok {
		return "", 400, "invalid path"
	}
	return disk, 0, ""
}

// buildFilePreview picks a preview for a file on disk. Office packages
// (including macro and template siblings) become their text, epub becomes
// the book text, archives become a listing, and everything else is sniffed
// so a LICENSE or a Makefile still shows up.
func buildFilePreview(path string, size int64) previewPayload {
	ext := previewExt(path)
	switch {
	case ext == "cab":
		// archives does not read cabinet files. Keep the desktop hint
		// instead of a generic "cannot read" for a format we recognize.
		return previewPayload{Kind: "archive", Note: "此压缩格式请在桌面端解压后查看"}
	case officeKind(ext) != "":
		return previewOffice(path, size)
	case ext == "rtf":
		return previewRTF(path, size)
	case ext == "epub":
		return previewEpub(path)
	case archiveContainer(ext):
		return previewArchive(path)
	default:
		return previewSniff(path, size)
	}
}

// officeKind maps the Office and OpenDocument family onto the readers
// previewOffice knows how to call.
func officeKind(ext string) string {
	switch ext {
	case "docx", "docm", "dotx", "dotm":
		return "docx"
	case "xlsx", "xlsm", "xltx", "xltm":
		return "xlsx"
	case "pptx", "pptm", "potx", "potm", "ppsx":
		return "pptx"
	case "odt", "ods", "odp", "odg":
		return "odt"
	default:
		return ""
	}
}

// archiveContainer is a package or a single compressed stream. Listing
// and decompression go through archives.Identify, which also accepts
// zip-shaped names such as apk and jar.
func archiveContainer(ext string) bool {
	switch ext {
	case "zip", "jar", "war", "ear", "apk", "aab", "ipa", "whl", "vsix",
		"xpi", "crx", "nupkg", "cbz", "pages", "numbers", "key",
		"tar", "tgz", "tar.gz", "gz", "tbz2", "tar.bz2", "bz2",
		"txz", "tar.xz", "xz", "tar.zst", "zst", "tzst", "lz4",
		"7z", "rar":
		return true
	default:
		return false
	}
}

func previewListed(entries []previewEntry, trunc bool, err error) previewPayload {
	if err != nil {
		return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
	}
	if len(entries) == 0 {
		return previewPayload{Kind: "archive", Note: "压缩包是空的"}
	}
	return previewPayload{Kind: "archive", Entries: entries, Truncated: trunc}
}

// previewExt is the archive-aware extension: "photos.tar.gz" stays tar.gz
// rather than becoming gz.
func previewExt(path string) string {
	l := strings.ToLower(filepath.Base(path))
	for _, suf := range []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"} {
		if strings.HasSuffix(l, suf) {
			return strings.TrimPrefix(suf, ".")
		}
	}
	return strings.TrimPrefix(filepath.Ext(l), ".")
}

func clipPreview(s string) (string, bool) {
	s = tidyPreviewText(s)
	r := []rune(s)
	if len(r) <= previewTextCap {
		return s, false
	}
	return string(r[:previewTextCap]) + "\n…", true
}

func tidyPreviewText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = html.UnescapeString(s)
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

func readPrefix(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, n))
}

func previewSniff(path string, size int64) previewPayload {
	f, err := os.Open(path)
	if err != nil {
		return previewPayload{Note: "无法读取文件"}
	}
	defer f.Close()
	buf := make([]byte, min64(size, 8<<10))
	n, _ := f.Read(buf)
	buf = buf[:n]
	if len(buf) == 0 {
		return previewPayload{Kind: "text", Note: "文件是空的"}
	}
	// Windows Notepad still saves "Unicode" text as UTF-16. Those files
	// contain NUL bytes, so the UTF-8 sniff would dump them as hex.
	if text, ok := decodeUTF16(buf); ok && isMostlyText([]byte(text)) {
		rest, _ := io.ReadAll(io.LimitReader(f, previewTextCap*2))
		all := append(append([]byte{}, buf...), rest...)
		if decoded, ok := decodeUTF16(all); ok {
			text = decoded
		}
		trunc := size > int64(len(buf)+len(rest))
		text, cut := clipPreview(text)
		return previewPayload{Kind: "text", Text: text, Truncated: trunc || cut}
	}
	if isMostlyText(buf) {
		rest, _ := io.ReadAll(io.LimitReader(f, previewTextCap))
		text := string(append(buf, rest...))
		trunc := size > int64(len(buf)+len(rest))
		if trunc {
			text += "\n…"
		}
		return previewPayload{Kind: "text", Text: tidyPreviewText(text), Truncated: trunc}
	}
	n = min(len(buf), previewHexBytes)
	return previewPayload{
		Kind: "hex",
		Text: "二进制文件，只显示开头\n\n" + hexDump(buf[:n]),
	}
}
