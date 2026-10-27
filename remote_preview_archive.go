package main

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/mholt/archives"
)

// remote_preview_archive.go lists archive members and decodes UTF-16 sniff
// helpers for /api/fs/preview. Split from remote_preview.go (LAY1).
//
// R7 boundary: pure I/O; same move-to-internal/remote note as office half.

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
