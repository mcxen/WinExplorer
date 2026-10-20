package remote

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// OpenList object types: 0 unknown, 1 folder, 2 video, 3 audio, 4 text,
// 5 image.
const (
	FSTypeUnknown = 0
	FSTypeFolder  = 1
	FSTypeVideo   = 2
	FSTypeAudio   = 3
	FSTypeText    = 4
	FSTypeImage   = 5
)

// FSCleanVPath normalizes a virtual path: backslashes become slashes,
// a leading / is forced and path.Clean resolves it against the root, so no
// ".." segment can survive to escape it.
func FSCleanVPath(vpath string) string {
	p := strings.ReplaceAll(vpath, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return path.Clean(p)
}

// ReadReq decodes one /api/fs/* request the way gin's c.ShouldBind
// does: the JSON or form-encoded body first, the query string after it as a
// fallback, so POST bodies and GET requests work alike. An empty body is
// not an error — every field keeps its zero value — and unknown fields are
// ignored, as gin ignores them.
func ReadReq(r *http.Request, v any) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		remoteBindQuery(r, v)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 {
		jsonBody := strings.Contains(r.Header.Get("Content-Type"), "json")
		if !jsonBody && len(body) > 0 && (body[0] == '{' || body[0] == '[') {
			jsonBody = true
		}
		if jsonBody {
			if err := json.Unmarshal(body, v); err != nil {
				return err
			}
		} else if err := remoteBindValues(body, v); err != nil {
			return err
		}
	}
	remoteBindQuery(r, v)
	return nil
}

// BindQuery applies the query string to v, beside a body's values.
func remoteBindQuery(r *http.Request, v any) {
	if q := r.URL.RawQuery; q != "" {
		_ = remoteBindValues([]byte(q), v)
	}
}

// BindValues fills the json-tagged fields of the struct v points to
// from a form-encoded string — application/x-www-form-urlencoded bodies and
// query strings share the syntax, values percent-decoded by the parser.
func remoteBindValues(raw []byte, v any) error {
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		key := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if key == "" || key == "-" || !values.Has(key) {
			continue
		}
		text := values.Get(key)
		field := rv.Field(i)
		switch field.Kind() {
		case reflect.String:
			field.SetString(text)
		case reflect.Bool:
			b, err := strconv.ParseBool(text)
			if err != nil {
				return err
			}
			field.SetBool(b)
		case reflect.Int, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(n)
		}
	}
	return nil
}

// FSEncodePath percent-escapes every segment of a virtual path —
// url.PathEscape semantics, / separators kept — for the /p download URL.
func FSEncodePath(vpath string) string {
	segs := strings.Split(strings.TrimPrefix(FSCleanVPath(vpath), "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/" + strings.Join(segs, "/")
}

// FSType maps a file name to its OpenList type by lowercased
// extension. The four lists are OpenList's default audio_types,
// video_types, image_types and text_types settings, checked in the order
// its utils.GetFileType checks them, so a client's preview choice agrees
// with what a real OpenList server would report for the same name.
func FSType(name string, isDir bool) int {
	if isDir {
		return FSTypeFolder
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "mp3", "flac", "ogg", "m4a", "wav", "opus", "wma":
		return FSTypeAudio
	case "mp4", "mkv", "avi", "mov", "rmvb", "webm", "flv", "m3u8":
		return FSTypeVideo
	case "jpg", "tiff", "jpeg", "png", "gif", "bmp", "svg", "ico", "swf",
		"webp", "avif":
		return FSTypeImage
	case "txt", "htm", "html", "xml", "java", "properties", "sql", "js",
		"md", "json", "conf", "ini", "vue", "php", "py", "bat", "gitignore",
		"yml", "go", "sh", "c", "cpp", "h", "hpp", "tsx", "vtt", "srt", "ass",
		"rs", "lrc", "strm":
		return FSTypeText
	}
	return FSTypeUnknown
}

// RequestBase is the scheme-and-host prefix raw_url links are built
// on, the way OpenList's common.GetApiUrl follows the request: a TLS
// connection or X-Forwarded-Proto yields https, X-Forwarded-Host replaces
// r.Host, so a reverse proxy in front of the app still hands out working
// links.
func RequestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}
