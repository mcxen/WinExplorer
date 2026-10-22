package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/fileview"
)

// assistant_files.go is the assistant's reader for every kind of file. The
// MCP read tools and the excerpts attached to a send both come through here,
// so a report reads the same way whether the model called read_files or the
// user dropped it on the composer. Pictures are pulled out as bytes and
// parked in a per-run bag: they travel to the model as real images, never as
// base64 in the tool text, which would blow up the context.

// The OS renderer is the extractor's last resort for a format Go cannot
// decode (HEIC, PDF pages, Pages documents). Wiring it here keeps fileview
// free of platform code and free of a ui dependency.
func init() { fileview.PosterFunc = sysThumbImage }

// assistReadFiles is how many paths one read_files call accepts. The number
// is a context budget: each file can carry text and pictures.
const assistReadFiles = 12

// assistMediaBag holds the pictures of one run. Tokens are the ids the tool
// text carries; the rewrite at the model boundary turns them back into
// image parts. The bag dies with the run: nothing here is persisted.
type assistMediaBag struct {
	mu    sync.Mutex
	items map[string]fileview.Frame
	next  int
}

func newMediaBag() *assistMediaBag { return &assistMediaBag{items: map[string]fileview.Frame{}} }

// add parks frames and returns their ids, in order.
func (b *assistMediaBag) add(frames []fileview.Frame) []string {
	if b == nil || len(frames) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(frames))
	for _, f := range frames {
		if len(f.Data) == 0 {
			continue
		}
		b.next++
		id := fmt.Sprintf("m%d", b.next)
		b.items[id] = f
		ids = append(ids, id)
	}
	return ids
}

// take returns the named pictures. Unknown ids are skipped: the model may
// quote a marker from an older turn.
func (b *assistMediaBag) take(ids []string) []fileview.Frame {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]fileview.Frame, 0, len(ids))
	for _, id := range ids {
		if f, ok := b.items[id]; ok {
			out = append(out, f)
		}
	}
	return out
}

// assistExtractOpts is the reading budget for one file inside a turn. The
// picture side is generous — a vision model gets more out of four clear
// frames than out of more words — while the text side stays small enough
// that a dozen files do not crowd out the question.
func assistExtractOpts() fileview.Options {
	return fileview.Options{TextRunes: 12000, MaxImages: 4, ImageSide: 1024}
}

// readAnyFile turns one path into the block a tool returns. The block is
// self-describing: what the file is, how it was read, what the text says and
// which pictures came with it.
func readAnyFile(path string, bag *assistMediaBag) string {
	var b strings.Builder
	b.WriteString(path)
	b.WriteByte('\n')
	info, err := os.Stat(path)
	if err != nil {
		b.WriteString(err.Error())
		b.WriteByte('\n')
		return b.String()
	}
	if info.IsDir() {
		fmt.Fprintf(&b, "这是文件夹，%d 项。%s\n", dirCountOrZero(path), dirPeek(path))
		return b.String()
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "" {
		ext = "无扩展名"
	}
	fmt.Fprintf(&b, "类型: %s/%s，%s，修改于 %s\n",
		fileview.KindOf(path), ext, formatBytes(info.Size()), info.ModTime().Format("2006-01-02 15:04"))
	res, err := fileview.Extract(path, assistExtractOpts())
	if err != nil {
		b.WriteString(err.Error())
		b.WriteByte('\n')
		return b.String()
	}
	fmt.Fprintf(&b, "读取方式: %s\n", res.Method)
	if res.Meta.Pages > 0 {
		fmt.Fprintf(&b, "页数: %d\n", res.Meta.Pages)
	}
	if n := utf8Len(res.Text); n > 0 {
		b.WriteString(res.Text)
		if res.Truncated {
			b.WriteString("\n（正文已截断）")
		}
		b.WriteByte('\n')
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(&b, "注意: %s\n", w)
	}
	if ids := bag.add(res.Frames); len(ids) > 0 {
		labels := make([]string, 0, len(res.Frames))
		for _, f := range res.Frames {
			labels = append(labels, f.Label)
		}
		fmt.Fprintf(&b, "附图 %d 张（%s），看图回答：%s\n",
			len(ids), strings.Join(labels, "、"), assistant.MediaMarker(ids))
	}
	return b.String()
}

// specifiedFileNote is the excerpt attached to the user message, so a model
// that forgets to call read_files still sees what was asked for. It also
// returns the pictures: for a vision model the attachment is the photo
// itself, not a paragraph about it.
func specifiedFileNote(paths []string) (string, []fileview.Frame) {
	var b strings.Builder
	b.WriteString("用户指定的文件，回答前先看这里；不够再调用 read_files：\n")
	var frames []fileview.Frame
	used := 0
	for _, raw := range paths {
		if info, err := os.Stat(raw); err == nil && info.IsDir() {
			fmt.Fprintf(&b, "%s\n这是文件夹，%d 项。%s\n---\n", raw, dirCountOrZero(raw), dirPeek(raw))
			continue
		}
		if len(frames) < 4 {
			res, err := fileview.Extract(raw, assistExtractOpts())
			if err != nil {
				fmt.Fprintf(&b, "%s\n%s\n---\n", raw, err.Error())
				continue
			}
			block := raw + "\n" + noteText(res) + "---\n"
			if used > 0 && used+len(block) > assistNoteBytes {
				fmt.Fprintf(&b, "%s\n（超出本次附带上限，请用 read_files 读取）\n", raw)
				continue
			}
			b.WriteString(block)
			used += len(block)
			for _, f := range res.Frames {
				if len(frames) >= 4 {
					break
				}
				frames = append(frames, f)
			}
			continue
		}
		fmt.Fprintf(&b, "%s\n（图片名额已满，请用 read_files 看图）\n---\n", raw)
	}
	return b.String(), frames
}

// noteText is the compact form of an extraction for the attached-files note:
// no method line, no warnings, just what the file says.
func noteText(res fileview.Result) string {
	var b strings.Builder
	b.WriteString(res.Text)
	if res.Truncated {
		b.WriteString("\n（正文已截断）")
	}
	b.WriteByte('\n')
	return b.String()
}

// assistNoteBytes is the budget of the attached-files note. The note is a
// fallback, not the reading itself, so it stays smaller than the tool path.
const assistNoteBytes = 48 * 1024

func utf8Len(s string) int { return len([]rune(s)) }

// dirPeek names the first few entries so a folder attachment still tells
// the model what is inside without walking the whole tree.
func dirPeek(path string) string {
	ents, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	names := make([]string, 0, min(len(ents), 8))
	for _, e := range ents {
		if len(names) >= 8 {
			break
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "（空文件夹）"
	}
	return "里面有：" + strings.Join(names, "、")
}

func dirCountOrZero(path string) int {
	n := 0
	if ents, err := os.ReadDir(path); err == nil {
		n = len(ents)
	}
	return n
}
