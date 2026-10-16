package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// previewState is the loaded result shown in the preview pane. Only one
// body field is normally filled; the rest stay zero.
type previewState struct {
	// kind drives how the pane renders the body.
	kind previewKind
	// gen tags the load; the pane only swaps in a state whose gen matches
	// the selection it asked for, so a slow load cannot replace a newer
	// selection.
	gen  uint64
	path string

	// bitmap is the decoded still: an image, or a video/PDF poster frame.
	bitmap *ui.Bitmap
	// text is the (possibly truncated) contents for text/code preview.
	text string
	// textFull is the raw byte length, for the "约 N KB" hint.
	textFull int
	// meta is the property list shown above or in place of the body.
	meta []propRow
	// err is a short message when loading failed but a file is selected.
	err string

	// media holds audio player / video poster metadata.
	media mediaInfo
}

type previewKind int

const (
	previewNone previewKind = iota // nothing or a directory selected
	previewImage
	previewText
	previewAudio
	previewVideo
	previewDoc  // PDF/other document, poster only
	previewMeta // any file we only have properties for
)

type propRow struct {
	Key, Val string
}

type mediaInfo struct {
	// duration of audio/video in seconds; 0 if unknown.
	duration float64
	// player is the MyGo audio player, set for WAV/MP3/Vorbis audio.
	player *audioPlayer
	// poster is a frame for video, shown as a still with a play affordance.
	poster *ui.Bitmap
}

// preview pane width bounds.
const (
	previewMinW float32 = 220
	previewMaxW float32 = 460
)

// singleSelected returns the single selected file, or "" when zero or many
// are selected (the preview pane follows Explorer's one-file rule).
func (a *App) singleSelected() Entry {
	sel := a.selectedEntries()
	if len(sel) != 1 || sel[0].IsDir {
		return Entry{}
	}
	return sel[0]
}

// requestPreview kicks off a background load for the current single
// selection, or clears the pane when nothing qualifies. Safe from any
// goroutine; the load walks the filesystem off the main thread and swaps
// the result in via swapPreview. A repeated request for the same file is
// a no-op, so clicking or re-selecting does not re-run decode work.
func (a *App) requestPreview() {
	e := a.singleSelected()
	path := e.Path
	if cur := a.preview.Load(); cur != nil && cur.path == path && (path == "" || cur.kind != previewNone) {
		return // unchanged
	}
	gen := atomic.AddUint64(&a.previewGen, 1)
	if path == "" {
		a.swapPreview(&previewState{kind: previewNone, gen: gen}, gen)
		return
	}
	go a.loadPreview(path, gen)
}

// loadPreview reads path and builds a previewState, then stores it if the
// selection it was asked for is still current.
func (a *App) loadPreview(path string, gen uint64) {
	st := a.buildPreview(path)
	st.gen = gen
	a.swapPreview(st, gen)
}

func (a *App) swapPreview(st *previewState, gen uint64) {
	if cur := a.preview.Load(); cur != nil && cur.gen > gen {
		return // a newer request already superseded this one
	}
	// Stop any prior media player before replacing the pane.
	if cur := a.preview.Swap(st); cur != nil && cur.media.player != nil {
		cur.media.player.stop()
	}
	repaint()
}

// repaint asks the window to redraw so the pane shows the new state.
func repaint() {
	mygo.RunOnMain(func() {
		if win := mygo.Windows(); len(win) > 0 {
			win[0].Invalidate()
		}
	})
}

// buildPreview does the actual decoding/inspection. It must not touch the
// UI; it returns a self-contained previewState.
func (a *App) buildPreview(path string) *previewState {
	info, err := os.Lstat(path)
	if err != nil {
		return &previewState{kind: previewMeta, path: path, err: "无法读取文件"}
	}
	kind := kindByExt(info.IsDir(), strings.ToLower(strings.TrimPrefix(filepath.Ext(path), ".")))
	st := &previewState{kind: kind, path: path}
	st.meta = a.fileProps(path, info, kind)

	switch kind {
	case previewImage:
		if b, e := decodeImage(path); e == nil {
			st.bitmap = b
		} else {
			st.err = "无法预览图片：" + e.Error()
			st.kind = previewMeta
		}
	case previewText:
		txt, full, e := readTextPreview(path, 64*1024)
		if e == nil {
			st.text = txt
			st.textFull = full
		} else {
			st.err = "无法读取文本：" + e.Error()
			st.kind = previewMeta
		}
	case previewAudio:
		st.media = loadAudio(path)
		if st.media.player == nil {
			st.kind = previewMeta
		}
	case previewVideo:
		st.media = loadVideo(path)
		st.kind = previewDoc // render as a poster with a play affordance
		if st.media.poster != nil {
			st.bitmap = st.media.poster
		}
	case previewDoc:
		if b, e := pdfPoster(path); e == nil {
			st.bitmap = b
		} else if b, e := quickLookThumb(path, 512); e == nil {
			st.bitmap = b
		}
	}
	return st
}

// fileProps builds the property list Explorer's details pane shows.
func (a *App) fileProps(path string, info os.FileInfo, kind previewKind) []propRow {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	rows := []propRow{
		{Key: "名称", Val: info.Name()},
		{Key: "类型", Val: typeLabel(Entry{IsDir: info.IsDir(), Ext: ext})},
	}
	size := formatBytes(info.Size())
	if info.IsDir() {
		if n, e := dirCount(path); e == nil {
			size = fmt.Sprintf("%d 个项目", n)
		}
	}
	rows = append(rows, propRow{Key: "大小", Val: size})
	rows = append(rows,
		propRow{Key: "修改日期", Val: formatFullTime(info.ModTime())},
		propRow{Key: "创建日期", Val: createdTime(path)},
		propRow{Key: "位置", Val: filepath.Dir(path)},
	)
	for _, r := range formatProps(path, kind) {
		rows = append(rows, r)
	}
	return rows
}

// kindByExt maps a directory flag + extension to a preview kind.
func kindByExt(isDir bool, ext string) previewKind {
	if isDir {
		return previewNone
	}
	switch ext {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "tif", "tiff", "svg", "avif", "ico":
		return previewImage
	case "txt", "md", "log", "csv", "rtf", "go", "py", "js", "ts", "tsx", "jsx", "rs", "java",
		"c", "h", "cpp", "cc", "cs", "rb", "php", "swift", "kt", "json", "xml", "yml", "yaml",
		"html", "css", "sh", "toml", "ini", "plist", "env", "gitignore":
		return previewText
	case "wav", "aiff", "aif", "mp3", "ogg", "oga", "m4a", "flac", "opus":
		return previewAudio
	case "mp4", "m4v", "mov", "mkv", "avi", "webm", "wmv", "flv":
		return previewVideo
	case "pdf", "pages", "key", "numbers", "doc", "docx", "ppt", "pptx", "xls", "xlsx", "rtfd":
		return previewDoc
	default:
		return previewMeta
	}
}

// decodeImage reads an image file and returns a ui.Bitmap. MyGo's
// DecodeBitmap covers PNG/JPEG/GIF/WebP/BMP (+ EXIF); for others (HEIC,
// AVIF, TIFF, SVG) we shell out to Quick Look, which renders them to PNG.
func decodeImage(path string) (*ui.Bitmap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if b, err := ui.DecodeBitmap(data); err == nil {
		return b, nil
	}
	return quickLookThumb(path, 1024)
}

// quickLookThumb asks macOS Quick Look for a thumbnail of path and decodes
// the PNG it writes. No cgo involved.
func quickLookThumb(path string, size int) (*ui.Bitmap, error) {
	dir, err := os.MkdirTemp("", "we-ql")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command("qlmanage", "-t", "-s", fmt.Sprint(size), "-o", dir, path)
	if b, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("%s: %v", string(b), e)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("没有生成缩略图")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

// pdfPoster renders the first page of a PDF to an image via Quick Look.
func pdfPoster(path string) (*ui.Bitmap, error) {
	return quickLookThumb(path, 768)
}

// readTextPreview reads up to max bytes of a text file, returning the text
// and the full byte length (for the "约 N KB" hint).
func readTextPreview(path string, max int) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, _ := f.Read(buf)
	full := n
	if fi, e := os.Stat(path); e == nil {
		full = int(fi.Size())
	}
	return string(buf[:n]), full, nil
}

// formatProps appends format-specific metadata from mdls / ffprobe.
func formatProps(path string, kind previewKind) []propRow {
	switch kind {
	case previewVideo, previewAudio:
		if m, ok := ffprobeMeta(path, kind == previewVideo); ok {
			return m
		}
	case previewImage, previewDoc:
		if m, ok := mdlsMeta(path); ok {
			return m
		}
	}
	return nil
}

// mdlsMeta parses a few useful attributes from `mdls`.
func mdlsMeta(path string) ([]propRow, bool) {
	out, err := exec.Command("mdls", "-name", "kMDItemPixelWidth",
		"-name", "kMDItemPixelHeight", "-name", "kMDItemNumberOfPages",
		"-name", "kMDItemFSOwnerName", path).Output()
	if err != nil {
		return nil, false
	}
	vals := parseMdls(string(out))
	var rows []propRow
	if w, ok := vals["kMDItemPixelWidth"]; ok && w != "(null)" {
		h := vals["kMDItemPixelHeight"]
		rows = append(rows, propRow{Key: "尺寸", Val: w + " × " + h + " 像素"})
	}
	if p, ok := vals["kMDItemNumberOfPages"]; ok && p != "(null)" {
		rows = append(rows, propRow{Key: "页数", Val: p})
	}
	return rows, len(rows) > 0
}

func parseMdls(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		i := strings.Index(line, " = ")
		if i < 0 {
			continue
		}
		m[line[:i]] = strings.Trim(strings.TrimSpace(line[i+3:]), "\"")
	}
	return m
}

// ffprobeMeta returns duration / resolution / codec for media files.
func ffprobeMeta(path string, video bool) ([]propRow, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return nil, false
	}
	var v struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return nil, false
	}
	var rows []propRow
	if v.Format.Duration != "" {
		if d, e := time.ParseDuration(v.Format.Duration + "s"); e == nil {
			rows = append(rows, propRow{Key: "时长", Val: formatDuration(d)})
		}
	}
	if video {
		for _, s := range v.Streams {
			if s.CodecType == "video" && s.Width > 0 {
				rows = append(rows, propRow{Key: "分辨率", Val: fmt.Sprintf("%d × %d", s.Width, s.Height)})
				rows = append(rows, propRow{Key: "视频编码", Val: strings.ToUpper(s.CodecName)})
				break
			}
		}
	}
	for _, s := range v.Streams {
		if s.CodecType == "audio" {
			rows = append(rows, propRow{Key: "音频编码", Val: strings.ToUpper(s.CodecName)})
			break
		}
	}
	return rows, len(rows) > 0
}

func dirCount(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

func formatFullTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006/1/2 15:04:05")
}

func createdTime(path string) string {
	if t, err := birthTime(path); err == nil && !t.IsZero() {
		return formatFullTime(t)
	}
	return ""
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int(d / time.Second)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
