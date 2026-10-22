// Package fileview turns one file on disk into everything an AI can use:
// the readable text, a few pictures (the picture itself, PDF pages or video
// frames) and the metadata that explains what the file is.
//
// The assistant's read tools call here so "read a file" means the same thing
// for a .go source file, a .docx report, a photo, a screen recording or a
// spreadsheet. Formats the Go standard library cannot decode fall through a
// chain of helpers instead of failing: pure-Go document readers first, then
// command-line tools when the machine has them (ffprobe/ffmpeg for media,
// pdftotext for PDF), then an embedded Python script that can use whatever
// extra module the user installed (Pillow, OpenCV, pypdf, python-docx…).
// Every result records which route was used, so the model never guesses how
// a number was produced.
package fileview

import (
	"os"
	"path/filepath"
	"strings"
)

// Kind is the coarse family a file belongs to. It decides which extractors
// run; it is not a MIME type and never leaves the package as one.
type Kind string

const (
	KindText    Kind = "text" // plain prose or structured data (txt, md, csv, json…)
	KindCode    Kind = "code" // source code: read verbatim, no tidying
	KindImage   Kind = "image"
	KindVideo   Kind = "video"
	KindAudio   Kind = "audio"
	KindDoc     Kind = "doc" // document formats whose text has to be extracted
	KindArchive Kind = "archive"
	KindBinary  Kind = "binary"
)

// Options bounds one extraction. A zero Options is valid: Extract fills in
// the defaults. The budgets exist because one call reads up to a dozen files
// and every byte lands in the model's context.
type Options struct {
	// TextRunes caps the extracted body text. Default 16000 runes.
	TextRunes int
	// MaxImages caps the pictures attached. Default 4.
	MaxImages int
	// ImageSide is the longest edge of an attached picture. Default 1024.
	ImageSide int
	// NoScript and NoExternal turn the outside routes off: a sandbox, or a
	// test that must not depend on what the machine happens to have.
	NoScript   bool // skip the bundled Python helper
	NoExternal bool // skip ffprobe / ffmpeg / pdftotext
}

func (o Options) withDefaults() Options {
	if o.TextRunes <= 0 {
		o.TextRunes = 16000
	}
	if o.MaxImages <= 0 {
		o.MaxImages = 4
	}
	if o.ImageSide <= 0 {
		o.ImageSide = 1024
	}
	return o
}

// Frame is one picture taken from the file: the image itself, a PDF page or
// a video frame. Label is what the model should call it in a reply.
type Frame struct {
	Label  string
	MIME   string // image/jpeg or image/png
	Data   []byte
	Width  int
	Height int
}

// Meta is the media/document metadata worth telling the model about.
type Meta struct {
	Duration   float64 // seconds
	Width      int
	Height     int
	VideoCodec string
	AudioCodec string
	Pages      int
}

// Result is one extracted file. Text is always non-empty: when nothing could
// be extracted it explains why, so the model can say so instead of inventing
// content. Frames are what a vision model sees.
type Result struct {
	Path      string
	Kind      Kind
	Text      string
	Truncated bool
	Frames    []Frame
	Meta      Meta
	Method    string   // route used: 内置解析 / ffprobe+ffmpeg / Python 脚本 / 系统渲染
	Warnings  []string // non-fatal problems, e.g. "没有找到 ffmpeg"
}

// Extract reads one file with every mechanism that applies to its kind.
func Extract(path string, opt Options) (Result, error) {
	opt = opt.withDefaults()
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, err
	}
	if info.IsDir() {
		return Result{}, errIsDir
	}
	kind := KindOf(path)
	res := Result{Path: path, Kind: kind}
	switch kind {
	case KindText, KindCode:
		res.Text, res.Truncated, res.Warnings = plainText(path, info.Size(), opt)
		res.Method = "内置解析"
	case KindDoc:
		res = extractDoc(path, info, opt, res)
	case KindImage:
		res = extractImage(path, info, opt, res)
	case KindVideo:
		res = extractVideo(path, info, opt, res)
	case KindAudio:
		res = extractAudio(path, info, opt, res)
	case KindArchive:
		res = extractArchive(path, info, opt, res)
	default:
		res.Text, res.Truncated = binaryText(path, info.Size(), opt)
		res.Method = "十六进制"
	}
	if strings.TrimSpace(res.Text) == "" {
		res.Text = "（没有读到可读的文字）"
	}
	return res, nil
}

var errIsDir = errStr("这是文件夹")

type errStr string

func (e errStr) Error() string { return string(e) }

// extOf is the lower-case extension without the dot. Files like Makefile or
// LICENSE have none and are classified by sniffing instead.
func extOf(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

var (
	textExts = map[string]bool{
		"txt": true, "md": true, "markdown": true, "log": true, "csv": true,
		"tsv": true, "json": true, "jsonl": true, "xml": true, "yml": true,
		"yaml": true, "toml": true, "ini": true, "cfg": true, "conf": true,
		"plist": true, "env": true, "gitignore": true, "gitattributes": true,
		"html": true, "htm": true, "css": true, "svg": true, "srt": true,
		"vtt": true, "tex": true, "bib": true, "rst": true, "adoc": true,
	}
	codeExts = map[string]bool{
		"go": true, "py": true, "js": true, "ts": true, "tsx": true,
		"jsx": true, "rs": true, "java": true, "kt": true, "swift": true,
		"c": true, "h": true, "cpp": true, "cc": true, "hpp": true,
		"cs": true, "rb": true, "php": true, "sh": true, "bash": true,
		"zsh": true, "ps1": true, "bat": true, "sql": true, "lua": true,
		"r": true, "m": true, "mm": true, "scala": true, "dart": true,
		"vue": true, "svelte": true, "pl": true, "ex": true, "exs": true,
		"zig": true, "nim": true, "hs": true, "erl": true, "clj": true,
	}
	imageExts = map[string]bool{
		"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true,
		"bmp": true, "heic": true, "heif": true, "tif": true, "tiff": true,
		"avif": true, "ico": true, "svg": true,
	}
	videoExts = map[string]bool{
		"mp4": true, "m4v": true, "mov": true, "mkv": true, "avi": true,
		"webm": true, "wmv": true, "flv": true, "mpg": true, "mpeg": true,
		"ts": false, // .ts is TypeScript in codeExts; video transport streams are rare here
	}
	audioExts = map[string]bool{
		"wav": true, "mp3": true, "m4a": true, "aac": true, "flac": true,
		"ogg": true, "oga": true, "opus": true, "aiff": true, "aif": true,
		"wma": true,
	}
	docExts = map[string]bool{
		"pdf": true, "doc": true, "docx": true, "docm": true, "dotx": true,
		"dotm": true, "xls": true, "xlsx": true, "xlsm": true, "xltx": true,
		"ppt": true, "pptx": true, "pptm": true, "potx": true, "pps": true,
		"ppsx": true, "odt": true, "ods": true, "odp": true, "odg": true,
		"rtf": true, "rtfd": true, "epub": true, "mobi": true, "azw3": true,
		"pages": true, "numbers": true, "key": true,
	}
	archiveExts = map[string]bool{
		"zip": true, "tar": true, "gz": true, "tgz": true, "bz2": true,
		"xz": true, "7z": true, "rar": true, "jar": true, "apk": true,
		"whl": true,
	}
)

// KindOf classifies by extension first and by content sniff when the name
// says nothing (Makefile, LICENSE, a file with no suffix).
func KindOf(path string) Kind {
	switch ext := extOf(path); {
	case textExts[ext]:
		return KindText
	case codeExts[ext]:
		return KindCode
	case imageExts[ext]:
		return KindImage
	case videoExts[ext]:
		return KindVideo
	case audioExts[ext]:
		return KindAudio
	case docExts[ext]:
		return KindDoc
	case archiveExts[ext]:
		return KindArchive
	}
	if kind, ok := sniffKind(path); ok {
		return kind
	}
	return KindBinary
}
