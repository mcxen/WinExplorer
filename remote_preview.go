package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/aiq/go-rtf"
	"github.com/lu4p/cat/odtxt"
	"github.com/mgilbir/spine/docx"
	"github.com/mgilbir/spine/pptx"
	"github.com/mgilbir/spine/xlsx"
	"github.com/mholt/archives"
	nethtml "golang.org/x/net/html"

	"sunvale/internal/remote"
)

// remote_preview.go serves /api/fs/preview, the text the management page
// and the guest page show for files a browser cannot render itself:
// Office and OpenDocument packages, zip/tar listings, and a sniff of
// anything else (plain text, or a short hex dump when the bytes are not).
//
// The package readers are existing pure-Go libraries. A hand-rolled XML
// walk misses shared strings, slide order and real OOXML packages, and
// it cannot open 7z, rar or xz. cab still has no pure-Go reader here.

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

func previewOffice(path string, size int64) previewPayload {
	if size > previewOfficeCap {
		return previewPayload{Kind: "document", Note: "文件太大，无法在浏览器里抽出正文"}
	}
	var (
		text string
		err  error
	)
	switch officeKind(previewExt(path)) {
	case "docx":
		text, err = docxText(path)
	case "xlsx":
		text, err = xlsxText(path)
	case "pptx":
		text, err = pptxText(path)
	default:
		text, err = odfText(path)
	}
	if err != nil || strings.TrimSpace(text) == "" {
		return previewPayload{Kind: "document", Note: "没能从文档里抽出文字，可用「下载」保存"}
	}
	text, trunc := clipPreview(text)
	return previewPayload{Kind: "document", Text: text, Truncated: trunc}
}

func docxText(path string) (string, error) {
	d, err := docx.Open(path)
	if err != nil {
		return "", err
	}
	defer d.Close()
	return d.Text(), nil
}

func xlsxText(path string) (string, error) {
	w, err := xlsx.Open(path)
	if err != nil {
		return "", err
	}
	defer w.Close()
	return w.Text(), nil
}

func pptxText(path string) (string, error) {
	p, err := pptx.Open(path)
	if err != nil {
		return "", err
	}
	defer p.Close()
	return p.Text(), nil
}

// odfText prefers cat's ODT reader, then falls back to the character data
// in content.xml. cat only keeps text:p, so a spreadsheet or a slide
// would otherwise come back empty.
func odfText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if text, err := odtxt.BytesToStr(b); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	raw := odfContentXML(b)
	if len(raw) == 0 {
		return "", os.ErrNotExist
	}
	return odfXMLText(raw), nil
}

func odfContentXML(b []byte) []byte {
	var raw []byte
	err := archives.Zip{}.Extract(context.Background(), bytes.NewReader(b), func(_ context.Context, info archives.FileInfo) error {
		name := strings.ToLower(strings.ReplaceAll(info.NameInArchive, "\\", "/"))
		if path.Base(name) != "content.xml" || info.Open == nil {
			return nil
		}
		rc, err := info.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		raw, err = io.ReadAll(io.LimitReader(rc, previewXMLCap))
		if err != nil {
			return err
		}
		return fs.SkipAll
	})
	if err != nil && !errors.Is(err, fs.SkipAll) && len(raw) == 0 {
		return nil
	}
	return raw
}

// odfXMLText keeps character data and breaks on paragraph and row ends.
// Local names ignore the office/text/table prefixes real files use.
func odfXMLText(raw []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			if t.Name.Local == "br" || t.Name.Local == "line-break" {
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p", "h", "br", "row", "table-row", "line-break":
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func previewRTF(path string, size int64) previewPayload {
	b, err := readPrefix(path, previewXMLCap)
	if err != nil {
		return previewPayload{Kind: "document", Note: "无法读取文档"}
	}
	// go-rtf decodes \uN (and skips the fallback character after it) and
	// \'hh. Word writes Chinese that way.
	text, err := rtf.Convert(string(b), rtf.PlainTextRules(), rtf.IgnoreList())
	if err != nil || strings.TrimSpace(text) == "" {
		return previewPayload{Kind: "document", Note: "没能从文档里抽出文字，可用「下载」保存"}
	}
	text, trunc := clipPreview(text)
	return previewPayload{Kind: "document", Text: text, Truncated: trunc || int64(len(b)) < size}
}

// previewEpub pulls chapter text out of an EPUB (a zip of XHTML). Script
// and style are dropped. When the book has no readable markup, the zip
// listing is the fallback.
func previewEpub(disk string) previewPayload {
	f, err := os.Open(disk)
	if err != nil {
		return previewPayload{Kind: "document", Note: "没能从电子书里抽出文字，可用「下载」保存"}
	}
	defer f.Close()
	type chapter struct {
		name string
		text string
	}
	var chaps []chapter
	total := 0
	stopped := false
	err = archives.Zip{}.Extract(context.Background(), f, func(_ context.Context, info archives.FileInfo) error {
		low := strings.ToLower(strings.ReplaceAll(info.NameInArchive, "\\", "/"))
		if !strings.HasSuffix(low, ".xhtml") && !strings.HasSuffix(low, ".html") && !strings.HasSuffix(low, ".htm") {
			return nil
		}
		if info.Open == nil || (info.FileInfo != nil && info.IsDir()) {
			return nil
		}
		if len(chaps) >= previewListCap || total >= previewTextCap*4 {
			stopped = true
			return fs.SkipAll
		}
		rc, err := info.Open()
		if err != nil {
			return nil
		}
		raw, _ := io.ReadAll(io.LimitReader(rc, previewXMLCap))
		rc.Close()
		text := htmlPlain(raw)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		chaps = append(chaps, chapter{name: low, text: text})
		total += len(text)
		return nil
	})
	if err != nil && !errors.Is(err, fs.SkipAll) && len(chaps) == 0 {
		return previewPayload{Kind: "document", Note: "没能从电子书里抽出文字，可用「下载」保存"}
	}
	sort.Slice(chaps, func(i, j int) bool { return chaps[i].name < chaps[j].name })
	var b strings.Builder
	for _, c := range chaps {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(c.text)
	}
	if strings.TrimSpace(b.String()) == "" {
		listed := previewArchive(disk)
		if len(listed.Entries) > 0 {
			return listed
		}
		return previewPayload{Kind: "document", Note: "没能从电子书里抽出文字，可用「下载」保存"}
	}
	text, trunc := clipPreview(b.String())
	return previewPayload{Kind: "document", Text: text, Truncated: trunc || stopped}
}

// htmlPlain keeps visible text and skips script and style. The tokenizer
// drops tags, so a chapter does not show its markup.
func htmlPlain(raw []byte) string {
	z := nethtml.NewTokenizer(bytes.NewReader(raw))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		switch tt {
		case nethtml.ErrorToken:
			return b.String()
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tt == nethtml.StartTagToken && (tag == "script" || tag == "style") {
				skip++
				continue
			}
			if tag == "br" && skip == 0 {
				b.WriteByte('\n')
			}
		case nethtml.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			if tag == "script" || tag == "style" {
				if skip > 0 {
					skip--
				}
				continue
			}
			if skip == 0 && htmlBlock(tag) {
				b.WriteByte('\n')
			}
		case nethtml.TextToken:
			if skip == 0 {
				b.WriteString(string(z.Text()))
			}
		}
	}
}

func htmlBlock(tag string) bool {
	switch tag {
	case "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "br":
		return true
	default:
		return false
	}
}

// previewArchive lists an archive, or sniffs a lone compressed stream
// (a .gz that is not a tar) once it has been decompressed. Names only:
// file bodies stay unread so a listing cannot expand a zip bomb.
func previewArchive(disk string) previewPayload {
	f, err := os.Open(disk)
	if err != nil {
		return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
	}
	defer f.Close()
	ctx := context.Background()
	format, stream, err := archives.Identify(ctx, filepath.Base(disk), f)
	if err != nil || format == nil {
		return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
	}
	if ex, ok := format.(archives.Extractor); ok {
		entries, trunc, err := listArchive(ctx, ex, stream)
		return previewListed(entries, trunc, err)
	}
	if dec, ok := format.(archives.Decompressor); ok {
		rc, err := dec.OpenReader(stream)
		if err != nil {
			return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
		}
		defer rc.Close()
		return previewDecompressed(rc)
	}
	return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
}

func listArchive(ctx context.Context, ex archives.Extractor, r io.Reader) ([]previewEntry, bool, error) {
	var out []previewEntry
	trunc := false
	err := ex.Extract(ctx, r, func(_ context.Context, info archives.FileInfo) error {
		if len(out) >= previewListCap {
			trunc = true
			return fs.SkipAll
		}
		entry, ok := archiveEntry(info)
		if !ok {
			return nil
		}
		out = append(out, entry)
		return nil
	})
	if errors.Is(err, fs.SkipAll) {
		err = nil
		trunc = true
	}
	return out, trunc, err
}

func archiveEntry(info archives.FileInfo) (previewEntry, bool) {
	raw := strings.ReplaceAll(info.NameInArchive, "\\", "/")
	raw = strings.TrimPrefix(raw, "./")
	dir := strings.HasSuffix(raw, "/")
	var size int64
	if info.FileInfo != nil {
		dir = dir || info.IsDir()
		size = info.Size()
	}
	name := strings.Trim(raw, "/")
	if name == "" || name == "." {
		return previewEntry{}, false
	}
	return previewEntry{Name: name, Size: size, Dir: dir}, true
}

func previewDecompressed(r io.Reader) previewPayload {
	buf, err := io.ReadAll(io.LimitReader(r, previewXMLCap+1))
	if err != nil && len(buf) == 0 {
		return previewPayload{Kind: "archive", Note: "无法读取压缩包内容"}
	}
	more := len(buf) > previewXMLCap
	if more {
		buf = buf[:previewXMLCap]
	}
	if len(buf) == 0 {
		return previewPayload{Kind: "text", Note: "文件是空的"}
	}
	if text, ok := decodeUTF16(buf); ok && isMostlyText([]byte(text)) {
		text, cut := clipPreview(text)
		return previewPayload{Kind: "text", Text: text, Truncated: more || cut}
	}
	if isMostlyText(buf) {
		text := string(buf)
		if more {
			text += "\n…"
		}
		return previewPayload{Kind: "text", Text: tidyPreviewText(text), Truncated: more}
	}
	n := min(len(buf), previewHexBytes)
	return previewPayload{
		Kind: "hex",
		Text: "二进制文件，只显示开头\n\n" + hexDump(buf[:n]),
	}
}

// decodeUTF16 turns a UTF-16 blob into a Go string. A BOM decides the
// endianness. Without one, only the ASCII-in-UTF-16 shape is accepted:
// CJK text is not half NULs, and guessing its endianness swaps every
// character.
func decodeUTF16(b []byte) (string, bool) {
	if len(b) < 4 {
		return "", false
	}
	le := false
	skip := 0
	bom := false
	switch {
	case b[0] == 0xFF && b[1] == 0xFE:
		le, skip, bom = true, 2, true
	case b[0] == 0xFE && b[1] == 0xFF:
		le, skip, bom = false, 2, true
	default:
		switch utf16Layout(b) {
		case 1:
			le = true
		case -1:
			le = false
		default:
			return "", false
		}
	}
	s := utf16ToString(b[skip:], le)
	if s == "" {
		return "", false
	}
	if !bom {
		ascii := 0
		runes := 0
		for _, r := range s {
			runes++
			if r == '\t' || r == '\n' || r == '\r' || (r >= 0x20 && r < 0x7f) {
				ascii++
			}
		}
		if runes == 0 || ascii*10 < runes*8 {
			return "", false
		}
	}
	return s, true
}

// utf16Layout is 1 for UTF-16LE, -1 for UTF-16BE, 0 when the bytes are
// not a NUL-padded encoding.
func utf16Layout(b []byte) int {
	n := len(b) - len(b)%2
	if n < 8 {
		return 0
	}
	if n > 512 {
		n = 512
	}
	evenZero, oddZero := 0, 0
	pairs := n / 2
	for i := 0; i < n; i += 2 {
		if b[i] == 0 {
			evenZero++
		}
		if b[i+1] == 0 {
			oddZero++
		}
	}
	if oddZero*10 >= pairs*8 && evenZero*10 < pairs*2 {
		return 1
	}
	if evenZero*10 >= pairs*8 && oddZero*10 < pairs*2 {
		return -1
	}
	return 0
}

func utf16ToString(b []byte, le bool) string {
	if len(b)%2 == 1 {
		b = b[:len(b)-1]
	}
	if len(b) < 2 {
		return ""
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		if le {
			u[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
		} else {
			u[i] = uint16(b[i*2])<<8 | uint16(b[i*2+1])
		}
	}
	return string(utf16.Decode(u))
}
