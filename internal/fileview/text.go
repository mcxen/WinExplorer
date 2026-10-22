package fileview

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/aiq/go-rtf"
	"github.com/lu4p/cat/odtxt"
	"github.com/mgilbir/spine/docx"
	"github.com/mgilbir/spine/pptx"
	"github.com/mgilbir/spine/xlsx"
)

// text.go is every route that ends in prose: the plain readers, the office
// package parsers, the ebook walk and the archive listing. The document
// parsers are the same pure-Go libraries the remote preview uses; keeping
// them here means the assistant and the web preview extract the same text
// from the same report.

// plainText reads the head of a text-like file. A file that turns out to be
// binary is not an error: the caller gets the sniff verdict as the body.
func plainText(path string, size int64, opt Options) (string, bool, []string) {
	const sniff = 64 << 10
	data, err := readHead(path, int64(opt.TextRunes*4))
	if err != nil {
		return "", false, []string{err.Error()}
	}
	var warns []string
	if len(data) > sniff {
		data = data[:sniff]
	}
	// A UTF-16 file is full of NULs and fails the text sniff; the BOM is
	// the file telling us it is text in another encoding.
	utf16 := bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff})
	if !utf16 && !isMostlyText(data) {
		return hexPreview(data), true, []string{"文件不是纯文本，显示十六进制开头"}
	}
	text := decodeText(data)
	truncated := size > int64(len(data))
	if truncated {
		text += fmt.Sprintf("\n…已截断，文件共 %s", formatSize(size))
	}
	body, cut := clipRunes(text, opt.TextRunes)
	return body, truncated || cut, warns
}

// binaryText is what an unknown format falls back to: enough hex to say what
// the bytes are, never the whole file.
func binaryText(path string, size int64, opt Options) (string, bool) {
	data, err := readHead(path, 512)
	if err != nil {
		return err.Error(), false
	}
	body := hexPreview(data)
	if note := magicNote(data); note != "" {
		body = note + "\n\n" + body
	}
	truncated := size > int64(len(data))
	if truncated {
		body += fmt.Sprintf("\n…已截断，文件共 %s", formatSize(size))
	}
	out, cut := clipRunes(body, opt.TextRunes)
	return out, truncated || cut
}

// decodeText normalises the encodings Windows and macOS actually produce.
// UTF-16 with a BOM is common in Windows logs and registry exports, and a
// raw byte-to-string conversion turns those into NUL-separated garbage.
func decodeText(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}), bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return decodeUTF16(data)
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		data = data[3:]
	}
	return tidyText(string(data))
}

func decodeUTF16(data []byte) string {
	big := bytes.HasPrefix(data, []byte{0xfe, 0xff})
	body := data[2:]
	if len(body)%2 != 0 {
		body = body[:len(body)-1]
	}
	units := make([]uint16, 0, len(body)/2)
	for i := 0; i+1 < len(body); i += 2 {
		lo, hi := body[i], body[i+1]
		if big {
			lo, hi = hi, lo
		}
		units = append(units, uint16(lo)|uint16(hi)<<8)
	}
	return tidyText(string(utf16.Decode(units)))
}

func tidyText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = html.UnescapeString(s)
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// isMostlyText reports whether bytes look like prose or source: no NUL,
// valid UTF-8 and mostly printable. The UTF-8 check is what separates a log
// full of Chinese from a blob of binary that happens to have no NUL bytes.
func isMostlyText(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	printable := 0
	for _, c := range b {
		if c == 0 {
			return false
		}
		if (c >= 0x20 && c < 0x7f) || c == '\n' || c == '\r' || c == '\t' || c >= 0x80 {
			printable++
		}
	}
	if printable*100 < len(b)*85 {
		return false
	}
	if utf8.Valid(b) {
		return true
	}
	// The head may cut a multi-byte rune in half; that is not binary.
	return len(b) > 3 && utf8.Valid(b[:len(b)-3])
}

// sniffKind classifies an extension-less file: text when the bytes say so,
// otherwise whatever the magic number names.
func sniffKind(path string) (Kind, bool) {
	data, err := readHead(path, 4096)
	if err != nil || len(data) == 0 {
		return "", false
	}
	if isMostlyText(data) {
		return KindText, true
	}
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG")), bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return KindImage, true
	case bytes.HasPrefix(data, []byte("%PDF")):
		return KindDoc, true
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		return KindArchive, true
	}
	return "", false
}

// magicNote names a binary from its first bytes so the model does not have
// to read hex to know it is holding a Word 97 file rather than a photo.
func magicNote(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.HasPrefix(data, []byte("\xd0\xcf\x11\xe0")):
		return "这是 OLE 复合文档（Word 97 / Excel 97 一类的旧格式），需要外部转换工具才能抽出正文。"
	case bytes.HasPrefix(data, []byte("Rar!")):
		return "这是 RAR 归档。"
	case bytes.HasPrefix(data, []byte("7z\xbc\xaf\x27\x1c")):
		return "这是 7z 归档。"
	case bytes.HasPrefix(data, []byte("\x1f\x8b")):
		return "这是 gzip 压缩的数据。"
	case bytes.HasPrefix(data, []byte("\x7fELF")):
		return "这是 Linux 可执行文件。"
	case bytes.HasPrefix(data, []byte("MZ")):
		return "这是 Windows 可执行文件。"
	case bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xce}), bytes.HasPrefix(data, []byte{0xfe, 0xed, 0xfa, 0xcf}):
		return "这是 macOS / Linux 可执行文件。"
	}
	return ""
}

func hexPreview(data []byte) string {
	const lines = 12
	out := make([]byte, 0, lines*80)
	out = append(out, "二进制内容，十六进制开头：\n"...)
	for off := 0; off < len(data) && off < lines*16; off += 16 {
		chunk := data[off:min(off+16, len(data))]
		out = append(out, fmt.Sprintf("%08x  %s\n", off, hex.EncodeToString(chunk))...)
	}
	return string(out)
}

// extractDoc pulls the text out of a document format. The pure-Go readers
// come first; a PDF or an old binary .doc then tries the Python route,
// because neither has a pure-Go parser in this module.
func extractDoc(path string, info os.FileInfo, opt Options, res Result) Result {
	res.Method = "内置解析"
	ext := extOf(path)
	var text string
	var err error
	switch ext {
	case "docx", "docm", "dotx", "dotm":
		text, err = docxText(path)
	case "xlsx", "xlsm", "xltx", "xltm":
		text, err = xlsxText(path)
	case "pptx", "pptm", "potx", "ppsx", "pps":
		text, err = pptxText(path)
	case "odt", "ods", "odp", "odg":
		text, err = odfText(path)
	case "rtf", "rtfd":
		text, err = rtfText(path)
	case "epub":
		text, err = epubText(path)
	case "pages", "numbers", "key":
		text, err = iworkText(path)
		if err == nil && strings.TrimSpace(text) == "" {
			err = errors.New("iWork 包里没有抽出文字")
		}
	}
	if err == nil && strings.TrimSpace(text) == "" {
		err = errors.New("文档里没有可读的文字")
	}
	if err == nil {
		res.Text, res.Truncated = clipRunes(tidyText(text), opt.TextRunes)
		return res
	}
	// No pure-Go route worked. Scripts know more formats (PDF, .doc, .mobi…)
	// and machine rendering still gives the model something to look at.
	res.Warnings = append(res.Warnings, "内置解析失败："+err.Error())
	if out, ok := runScript(path, opt, res.Kind); ok {
		res = mergeScript(res, out, opt)
		if strings.TrimSpace(res.Text) != "" && !strings.HasPrefix(res.Text, "（") {
			return res
		}
	}
	if ext == "pdf" && !opt.NoExternal {
		if text, ok := pdftotext(path); ok {
			res.Text, res.Truncated = clipRunes(tidyText(text), opt.TextRunes)
			res.Method = "pdftotext"
			res.Warnings = res.Warnings[:len(res.Warnings)-1]
			appendPageFrame(&res, path, opt)
			return res
		}
	}
	appendPageFrame(&res, path, opt)
	if len(res.Frames) > 0 {
		res.Method = "系统渲染"
		res.Text = fmt.Sprintf("没能抽出正文。上面是渲染出来的第一页，可以看图了解内容。文件 %s，%s。", path, formatSize(info.Size()))
	}
	return res
}

// appendPageFrame attaches a rendered first page when the OS can render the
// document (Quick Look on macOS). It is the last resort that still lets a
// vision model read a PDF or a Pages file.
func appendPageFrame(res *Result, path string, opt Options) {
	if len(res.Frames) >= opt.MaxImages || PosterFunc == nil {
		return
	}
	img, err := PosterFunc(path, opt.ImageSide)
	if err != nil || img == nil {
		return
	}
	if frame, ok := encodeFrame("第 1 页", img, opt.ImageSide); ok {
		res.Frames = append(res.Frames, frame)
	}
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

func rtfText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return rtf.Convert(string(b), rtf.PlainTextRules(), rtf.IgnoreList())
}

// odfText prefers cat's ODT reader and falls back to walking content.xml:
// cat only keeps text:p, so a spreadsheet or a deck would come back empty.
func odfText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if text, err := odtxt.BytesToStr(b); err == nil && strings.TrimSpace(text) != "" {
		return text, nil
	}
	raw, err := zipEntry(path, "content.xml")
	if err != nil {
		return "", err
	}
	return xmlText(raw), nil
}

// iworkText reads a Pages / Keynote / Numbers package. Apple ships them as
// zip archives whose Index/*.iwa protobuf is not readable here, but the
// preview PDF inside a Pages file and the plain-text parts are.
func iworkText(path string) (string, error) {
	names, err := zipNames(path)
	if err != nil {
		return "", err
	}
	var out []string
	for _, name := range names {
		low := strings.ToLower(name)
		if !strings.HasSuffix(low, ".txt") && !strings.HasSuffix(low, ".xml") {
			continue
		}
		raw, err := zipEntry(path, name)
		if err != nil {
			continue
		}
		if text := tidyText(xmlText(raw)); text != "" {
			out = append(out, text)
		}
		if len(out) >= 3 {
			break
		}
	}
	return strings.Join(out, "\n"), nil
}

// xmlText keeps character data and breaks on paragraph and row ends. Local
// names ignore the office/text/table prefixes real files use.
func xmlText(raw []byte) string {
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

// epubText walks the chapters of an EPUB in reading order. An ebook is a zip
// of XHTML; script and style markup is dropped along the way.
func epubText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		low := strings.ToLower(f.Name)
		if strings.HasSuffix(low, ".xhtml") || strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm") {
			names = append(names, f.Name)
		}
	}
	sortStrings(names)
	var out []string
	total := 0
	for _, name := range names {
		if len(out) >= 40 {
			break
		}
		raw, err := zipEntry(path, name)
		if err != nil {
			continue
		}
		text := tidyText(htmlText(raw))
		if text == "" {
			continue
		}
		out = append(out, text)
		total += len(text)
		if total > 200_000 {
			break
		}
	}
	return strings.Join(out, "\n"), nil
}

// htmlText strips markup and keeps prose, so a chapter comes back as words
// rather than angle brackets.
func htmlText(raw []byte) string {
	var b strings.Builder
	dec := xml.NewDecoder(bytes.NewReader(raw))
	skip := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "script", "style", "head", "title":
				skip++
			case "br", "p", "div", "h1", "h2", "h3", "h4", "li", "tr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "script", "style", "head", "title":
				skip--
			case "p", "div", "h1", "h2", "h3", "h4", "li", "tr":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if skip <= 0 {
				b.Write(t)
			}
		}
	}
	return b.String()
}

// extractArchive lists what is inside without unpacking it. A listing tells
// the model whether the archive is worth unzipping and what to expect.
func extractArchive(path string, info os.FileInfo, opt Options, res Result) Result {
	res.Method = "内置解析"
	entries, err := archiveListing(path)
	if err != nil {
		res.Warnings = append(res.Warnings, err.Error())
		res.Text = fmt.Sprintf("这是压缩包 %s（%s），没能列出内容：%s", path, formatSize(info.Size()), err.Error())
		return res
	}
	var b strings.Builder
	fmt.Fprintf(&b, "压缩包 %s，%s，共 %d 项：\n", path, formatSize(info.Size()), len(entries))
	limit := min(len(entries), 100)
	for i := 0; i < limit; i++ {
		b.WriteString(entries[i])
		b.WriteByte('\n')
	}
	if len(entries) > limit {
		fmt.Fprintf(&b, "…还有 %d 项\n", len(entries)-limit)
	}
	res.Text, res.Truncated = clipRunes(b.String(), opt.TextRunes)
	if len(entries) > limit {
		res.Truncated = true
	}
	return res
}

// archiveListing reads a zip or tar family archive's names. The formats
// without a pure-Go reader here (rar, 7z) are named as such by the caller.
func archiveListing(path string) ([]string, error) {
	if zr, err := zip.OpenReader(path); err == nil {
		defer zr.Close()
		names := make([]string, 0, len(zr.File))
		for _, f := range zr.File {
			name := f.Name
			if f.FileInfo().IsDir() {
				name += "/"
			}
			names = append(names, name)
		}
		sortStrings(names)
		return names, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	switch {
	case strings.HasSuffix(strings.ToLower(path), ".gz"), strings.HasSuffix(strings.ToLower(path), ".tgz"):
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	case strings.HasSuffix(strings.ToLower(path), ".tar"):
	default:
		return nil, errors.New("这个压缩格式没有内置的列表支持")
	}
	tr := tar.NewReader(r)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return names, err
		}
		if hdr.Typeflag == tar.TypeDir {
			names = append(names, hdr.Name+"/")
		} else {
			names = append(names, hdr.Name)
		}
		if len(names) >= 2000 {
			break
		}
	}
	sortStrings(names)
	return names, nil
}

func zipNames(path string) ([]string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names, nil
}

// zipEntry returns one entry's bytes, capped so a hostile package cannot
// flood memory.
func zipEntry(path, name string) ([]byte, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	low := strings.ReplaceAll(strings.ToLower(name), "\\", "/")
	for _, f := range zr.File {
		if strings.ReplaceAll(strings.ToLower(f.Name), "\\", "/") != low {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, 8<<20))
	}
	return nil, errors.New("包里没有这一项")
}

func readHead(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// clipRunes trims a body to the budget and says so, because a silently cut
// excerpt reads like a complete file to the model.
func clipRunes(s string, max int) (string, bool) {
	r := []rune(s)
	if len(r) <= max {
		return s, false
	}
	return string(r[:max]) + "\n…已截断", true
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func sortStrings(s []string) { sort.Strings(s) }
