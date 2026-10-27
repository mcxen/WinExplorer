package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/aiq/go-rtf"
	"github.com/lu4p/cat/odtxt"
	"github.com/mgilbir/spine/docx"
	"github.com/mgilbir/spine/pptx"
	"github.com/mgilbir/spine/xlsx"
	"github.com/mholt/archives"
	nethtml "golang.org/x/net/html"
)

// remote_preview_office.go extracts text previews from Office, OpenDocument,
// RTF and EPUB for /api/fs/preview. Split from remote_preview.go (LAY1).
//
// R7 boundary: all functions here are pure file readers — safe to move into
// internal/remote once previewPayload is exported or shared.

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
