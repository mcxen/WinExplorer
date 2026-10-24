package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopfiles.go reads Linux's .desktop application entries — the
// MimeType patterns that name what an application opens, and the Exec
// line that runs it. Pure parsing, so it builds and tests everywhere.

// appDesktopDirs are the folders the 打开方式 registry reads .desktop
// files from, system copies first so a distro's defaults win; the user's
// ~/.local/share/applications comes last and holds locally installed and
// user-edited launchers.
var appDesktopDirs = []string{
	"/usr/share/applications",
	"/usr/local/share/applications",
	os.Getenv("HOME") + "/.local/share/applications",
}

// desktopMimeExts reads a .desktop file's MimeType line and returns the
// extensions its patterns name: both *.png glob patterns and the MIME
// types real launchers write — image/png and the rest of the common
// types, via mimeExts; an unknown type falls back to its subtype
// (application/x-foo → foo) when that reads as an extension.
func desktopMimeExts(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(ext string) {
		if ext != "" && !seen[ext] {
			seen[ext] = true
			out = append(out, ext)
		}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MimeType=") {
			continue
		}
		for _, mime := range strings.Split(strings.TrimPrefix(line, "MimeType="), ";") {
			if mime == "" {
				continue
			}
			if rest, ok := strings.CutPrefix(mime, "*."); ok {
				add(strings.ToLower(rest))
				continue
			}
			for _, ext := range mimeExts(mime) {
				add(ext)
			}
		}
	}
	return out
}

// mimeExts maps a MIME type to the extensions it covers, the way
// mailcap and the desktop environments' shared-mime-info database do for
// the common types; the registry only needs the ones files actually
// carry. An unknown type falls back to its subtype when that reads as an
// extension, so x-keepass → keepass still matches.
func mimeExts(mime string) []string {
	if exts, ok := mimeExtsTable[mime]; ok {
		return exts
	}
	_, sub, ok := strings.Cut(mime, "/")
	if !ok {
		return nil
	}
	sub = strings.TrimPrefix(strings.ToLower(sub), "x-")
	if sub == "" || len(sub) > 8 || !isAlphaNum(sub) {
		return nil
	}
	return []string{sub}
}

// mimeExtsTable is the shared-mime-info core, flattened to the extensions
// that matter in a file menu.
var mimeExtsTable = map[string][]string{
	// Images
	"image/png":                 {"png"},
	"image/jpeg":                {"jpg", "jpeg"},
	"image/gif":                 {"gif"},
	"image/tiff":                {"tif", "tiff"},
	"image/bmp":                 {"bmp"},
	"image/webp":                {"webp"},
	"image/x-icon":              {"ico"},
	"image/svg+xml":             {"svg"},
	"image/heic":                {"heic"},
	"image/heif":                {"heif"},
	"image/avif":                {"avif"},
	"image/x-xcf":               {"xcf"},
	"image/vnd.adobe.photoshop": {"psd"},
	// Text and code
	"text/plain":                {"txt", "log", "conf"},
	"text/html":                 {"html", "htm"},
	"text/css":                  {"css"},
	"text/csv":                  {"csv"},
	"text/markdown":             {"md", "markdown"},
	"text/x-python":             {"py"},
	"text/x-shellscript":        {"sh"},
	"text/x-csrc":               {"c"},
	"text/x-chdr":               {"h"},
	"text/x-c++src":             {"cpp", "cc", "cxx"},
	"text/x-java":               {"java"},
	"text/x-go":                 {"go"},
	"text/x-rust":               {"rs"},
	"text/x-ruby":               {"rb"},
	"text/javascript":           {"js", "mjs"},
	"application/json":          {"json"},
	"application/xml":           {"xml"},
	"application/yaml":          {"yaml", "yml"},
	"application/x-yaml":        {"yaml", "yml"},
	"application/toml":          {"toml"},
	"application/rtf":           {"rtf"},
	"application/x-desktop":     {"desktop"},
	"application/x-shellscript": {"sh"},
	// Documents
	"application/pdf":                                                           {"pdf"},
	"application/vnd.oasis.opendocument.text":                                   {"odt"},
	"application/vnd.oasis.opendocument.spreadsheet":                            {"ods"},
	"application/vnd.oasis.opendocument.presentation":                           {"odp"},
	"application/msword":                                                        {"doc"},
	"application/vnd.ms-excel":                                                  {"xls"},
	"application/vnd.ms-powerpoint":                                             {"ppt"},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   {"docx"},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         {"xlsx"},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {"pptx"},
	"application/epub+zip":                                                      {"epub"},
	// Audio and video
	"audio/mpeg":         {"mp3"},
	"audio/mp4":          {"m4a"},
	"audio/ogg":          {"ogg", "opus"},
	"audio/x-vorbis+ogg": {"ogg"},
	"audio/x-opus+ogg":   {"opus"},
	"audio/flac":         {"flac"},
	"audio/x-flac":       {"flac"},
	"audio/x-wav":        {"wav"},
	"audio/wav":          {"wav"},
	"audio/aiff":         {"aiff", "aif"},
	"audio/midi":         {"mid", "midi"},
	"video/mp4":          {"mp4", "m4v"},
	"video/x-msvideo":    {"avi"},
	"video/mpeg":         {"mpg", "mpeg"},
	"video/quicktime":    {"mov"},
	"video/x-matroska":   {"mkv"},
	"video/x-flv":        {"flv"},
	"video/webm":         {"webm"},
	"video/ogg":          {"ogv"},
	// Archives and disks
	"application/zip":                       {"zip"},
	"application/x-tar":                     {"tar"},
	"application/gzip":                      {"gz"},
	"application/x-gzip":                    {"gz"},
	"application/x-bzip2":                   {"bz2"},
	"application/x-xz":                      {"xz"},
	"application/x-7z-compressed":           {"7z"},
	"application/vnd.rar":                   {"rar"},
	"application/x-rar-compressed":          {"rar"},
	"application/zstd":                      {"zst"},
	"application/x-iso9660-image":           {"iso"},
	"application/x-apple-diskimage":         {"dmg"},
	"application/x-deb":                     {"deb"},
	"application/vnd.debian.binary-package": {"deb"},
	"application/x-rpm":                     {"rpm"},
	"application/x-appimage":                {"appimage"},
	"application/x-executable":              {"bin", "run"},
	// Fonts
	"font/ttf":               {"ttf"},
	"font/otf":               {"otf"},
	"application/x-font-ttf": {"ttf"},
	"application/x-font-otf": {"otf"},
	"font/woff":              {"woff"},
	"font/woff2":             {"woff2"},
}

// desktopAppsIn maps .desktop file ids to their Name= entries, so the
// menu shows LibreOffice instead of libreoffice-writer.
func desktopAppsIn(dirs []string) map[string]string {
	out := map[string]string{}
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".desktop") {
				continue
			}
			id := strings.TrimSuffix(f.Name(), ".desktop")
			if _, ok := out[id]; ok {
				continue
			}
			if name, ok := desktopName(filepath.Join(dir, f.Name())); ok {
				out[id] = name
			}
		}
	}
	return out
}

// desktopName reads a .desktop file's localized Name=, falling back to the
// first generic one; ok is false when the file names nothing.
func desktopName(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	fallback := ""
	lang := os.Getenv("LANG")
	if i := strings.IndexByte(lang, '.'); i >= 0 {
		lang = lang[:i]
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Name") {
			continue
		}
		// Name[zh_CN]=… wins when it matches the locale; plain Name= is
		// the fallback and a no-locale build's answer too.
		if rest, ok := strings.CutPrefix(line, "Name["); ok {
			if key, val, ok := strings.Cut(rest, "]="); ok && key == lang && val != "" {
				return val, true
			}
			continue
		}
		if val, ok := strings.CutPrefix(line, "Name="); ok && val != "" && fallback == "" {
			fallback = val
		}
	}
	return fallback, fallback != ""
}

// desktopExec builds the command a .desktop file says to run, with the
// file taking the place of its field codes. The registry records apps by
// their Name=, so the lookup matches either the file id or the name.
func desktopExec(app, path string) (*exec.Cmd, bool) {
	return desktopExecIn(appDesktopDirs, app, path)
}

// desktopExecIn is desktopExec over explicit application folders, for
// callers on other layouts and for tests.
func desktopExecIn(dirs []string, app, path string) (*exec.Cmd, bool) {
	for _, id := range desktopAppIds(dirs, app) {
		for _, dir := range dirs {
			f, err := os.ReadFile(filepath.Join(dir, id+".desktop"))
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(f), "\n") {
				if !strings.HasPrefix(line, "Exec=") {
					continue
				}
				fields := strings.Fields(strings.TrimPrefix(line, "Exec="))
				if len(fields) == 0 {
					continue
				}
				args := make([]string, 0, len(fields))
				for _, fl := range fields[1:] {
					switch fl {
					case "%f", "%F", "%u", "%U":
						args = append(args, path)
					case "%%":
						args = append(args, "%")
					default:
						args = append(args, fl)
					}
				}
				return exec.Command(fields[0], args...), true
			}
		}
	}
	return nil, false
}

// desktopAppIds resolves the .desktop file ids an app name may launch
// with: the name itself as an id, then every launcher whose Name= says
// the app. First match wins.
func desktopAppIds(dirs []string, app string) []string {
	ids := []string{app}
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".desktop") {
				continue
			}
			id := strings.TrimSuffix(f.Name(), ".desktop")
			if id == app {
				continue
			}
			if name, ok := desktopName(filepath.Join(dir, f.Name())); ok && name == app {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
