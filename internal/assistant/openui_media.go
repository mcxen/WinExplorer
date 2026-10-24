package assistant

import (
	"path/filepath"
	"strconv"
	"strings"
)

// openui_media.go realizes MediaList: rows of a thumbnail card (image,
// video poster or document page) on the left and a numbered title plus
// body on the right, the step list chat assistants draw.
//
//	root = MediaList([m1, m2], "标题", "numbered")
//	m1 = MediaItem("/p/a.mp4", "剪辑开场", "保留前 5 秒", "video")
//	m2 = {media: "https://x/y.png", kind: "image", title: "封面", text: "..."}

const openMediaListMax = 30

func openMediaList(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	var items []*OUIView
	for _, it := range openList(env, openArg(e.args, 0), stack) {
		if len(items) >= openMediaListMax {
			break
		}
		if v := openMediaItem(env, openDeref(env, it, stack), stack); v != nil {
			items = append(items, v)
		}
	}
	if len(items) == 0 {
		return nil
	}
	title, _ := openText(env, openArg(e.args, 1), stack)
	style, _ := openText(env, openArg(e.args, 2), stack)
	numbered := strings.EqualFold(strings.TrimSpace(style), "numbered")
	return &OUIView{Kind: "medialist", Text: strings.TrimSpace(title), Items: items, Row: numbered}
}

// openDeref follows a ref one or more steps to the expression it names.
func openDeref(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) OUIExpr {
	for i := 0; e.kind == "ref" && i < 16; i++ {
		next, ok := env[e.name]
		if !ok || stack[e.name] {
			return OUIExpr{}
		}
		e = next
	}
	return e
}

// openMediaItem reads MediaItem(media, title, text, kind?, n?) or an object
// {media|src|path|url, kind|type, title, text|body|desc, n|number}.
func openMediaItem(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	v := &OUIView{Kind: "mediaitem"}
	switch {
	case e.kind == "call" && (e.name == "MediaItem" || e.name == "MediaCard"):
		v.Path, _ = openText(env, openArg(e.args, 0), stack)
		v.Text, _ = openText(env, openArg(e.args, 1), stack)
		v.Sub, _ = openText(env, openArg(e.args, 2), stack)
		v.Variant, _ = openText(env, openArg(e.args, 3), stack)
		v.Label, _ = openText(env, openArg(e.args, 4), stack)
		for _, arg := range e.args {
			if arg.kind == "object" {
				openMediaFields(env, arg, stack, v)
			}
		}
	case e.kind == "object":
		openMediaFields(env, e, stack, v)
	default:
		return nil
	}
	v.Path = strings.TrimSpace(v.Path)
	v.Text = strings.TrimSpace(v.Text)
	v.Sub = strings.TrimSpace(v.Sub)
	v.Label = strings.TrimSpace(v.Label)
	if v.Path == "" && v.Text == "" && v.Sub == "" {
		return nil
	}
	v.Variant = OpenMediaKind(v.Variant, v.Path)
	return v
}

func openMediaFields(env map[string]OUIExpr, obj OUIExpr, stack map[string]bool, v *OUIView) {
	for _, f := range obj.items {
		val := f
		if f.kind == "field" && len(f.items) > 0 {
			val = f.items[0]
		}
		s, _ := openText(env, val, stack)
		switch strings.ToLower(f.name) {
		case "media", "src", "path", "url", "file", "image", "video", "doc":
			v.Path = s
		case "kind", "type":
			v.Variant = s
		case "title", "label", "name":
			v.Text = s
		case "text", "body", "desc", "description", "content":
			v.Sub = s
		case "n", "number", "index", "step":
			v.Label = s
		}
	}
}

// OpenMediaKind normalizes a MediaItem kind to image|video|doc, inferring
// it from the extension when the model left it out or wrote something else.
func OpenMediaKind(kind, src string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image", "img", "picture", "photo":
		return "image"
	case "video", "movie", "clip":
		return "video"
	case "doc", "document", "file", "pdf":
		return "doc"
	}
	u := src
	if i := strings.IndexAny(u, "?#"); i >= 0 && strings.Contains(u, "://") {
		u = u[:i]
	}
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(u), ".")) {
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "heic", "heif", "avif", "tif", "tiff":
		return "image"
	case "mp4", "m4v", "mov", "mkv", "avi", "webm", "wmv", "flv":
		return "video"
	}
	if strings.HasPrefix(src, "data:image/") {
		return "image"
	}
	return "doc"
}

// OpenMediaNumber is the row's number: the explicit n, else its index when
// the list is "numbered", else nothing.
func OpenMediaNumber(list, v *OUIView, i int) string {
	if v != nil && v.Label != "" {
		return v.Label
	}
	if list != nil && list.Row {
		return strconv.Itoa(i + 1)
	}
	return ""
}
