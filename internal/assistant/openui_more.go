package assistant

import (
	"strconv"
	"strings"
)

// openui_more.go realizes the OPENUI-MORE components: Files (file rows with
// icon, size and time), Steps (vertical status timeline), StatCards (metric
// tiles) and Gallery (thumbnail grid). Each accepts a positional call or an
// object per item, the way MediaList does.
//
//	root = Files([FileCard("/d/a.pdf", "报告", "2.1 MB", "昨天"), "/d/b.png"], "大文件")
//	root = Steps([Step("扫描", "312 个文件", "done"), {title: "去重", status: "active"}])
//	root = StatCards([StatCard("已用", "412 GB", "+12 GB", "warning")])
//	root = Gallery(["/p/1.jpg", MediaItem("/p/2.mp4", "开场")], "相册")

const openMoreMax = 60

// openMoreItems walks a list argument, realizing each entry with one.
func openMoreItems(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, one func(OUIExpr) *OUIView) []*OUIView {
	var out []*OUIView
	for _, it := range openList(env, e, stack) {
		if len(out) >= openMoreMax {
			break
		}
		if v := one(openDeref(env, it, stack)); v != nil {
			out = append(out, v)
		}
	}
	return out
}

// openMoreFields reads an item: positional strings land in pos order on
// the named slots, object fields by alias.
func openMoreFields(env map[string]OUIExpr, e OUIExpr, stack map[string]bool, calls []string, pos []string, alias map[string]string) (map[string]string, bool) {
	out := map[string]string{}
	switch e.kind {
	case "call":
		ok := false
		for _, n := range calls {
			ok = ok || e.name == n
		}
		if !ok {
			return nil, false
		}
		for i, slot := range pos {
			if s, ok := openText(env, openArg(e.args, i), stack); ok {
				out[slot] = strings.TrimSpace(s)
			}
		}
		for _, arg := range e.args {
			if arg.kind == "object" {
				openMoreObject(env, arg, stack, alias, out)
			}
		}
	case "object":
		openMoreObject(env, e, stack, alias, out)
	case "string":
		if len(pos) == 0 {
			return nil, false
		}
		s, _ := openText(env, e, stack)
		out[pos[0]] = strings.TrimSpace(s)
	default:
		return nil, false
	}
	return out, true
}

func openMoreObject(env map[string]OUIExpr, obj OUIExpr, stack map[string]bool, alias map[string]string, out map[string]string) {
	for _, f := range obj.items {
		val := f
		if f.kind == "field" && len(f.items) > 0 {
			val = f.items[0]
		}
		if slot, ok := alias[strings.ToLower(f.name)]; ok {
			s, _ := openText(env, val, stack)
			out[slot] = strings.TrimSpace(s)
		}
	}
}

var openFileAlias = map[string]string{
	"path": "path", "file": "path", "src": "path", "url": "path",
	"name": "name", "title": "name", "label": "name",
	"size": "size", "bytes": "size",
	"mtime": "time", "time": "time", "modified": "time", "date": "time",
	"note": "note", "desc": "note", "text": "note", "reason": "note",
}

// openFiles is Files(items, title?): Text title, Items of kind "file" with
// Path, Text (display name, may be empty), Sub (note), Label (size) and
// Placeholder (time). The painter stats local files for what is missing.
func openFiles(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	items := openMoreItems(env, openArg(e.args, 0), stack, func(it OUIExpr) *OUIView {
		m, ok := openMoreFields(env, it, stack, []string{"FileCard", "FileItem", "File"},
			[]string{"path", "note", "size", "time", "name"}, openFileAlias)
		if !ok || m["path"] == "" {
			return nil
		}
		return &OUIView{Kind: "file", Path: m["path"], Text: m["name"], Sub: m["note"], Label: m["size"], Placeholder: m["time"]}
	})
	if len(items) == 0 {
		return nil
	}
	title, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: "files", Text: strings.TrimSpace(title), Items: items}
}

// OpenStepStatus normalizes a Step status to done|active|pending|error.
func OpenStepStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "done", "complete", "completed", "ok", "success", "finished":
		return "done"
	case "active", "current", "doing", "running", "in_progress", "progress":
		return "active"
	case "error", "failed", "fail", "danger":
		return "error"
	}
	return "pending"
}

// openSteps is Steps(items, title?): Items of kind "step" with Text, Sub
// and Variant (status).
func openSteps(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	items := openMoreItems(env, openArg(e.args, 0), stack, func(it OUIExpr) *OUIView {
		m, ok := openMoreFields(env, it, stack, []string{"Step", "TimelineItem"},
			[]string{"title", "text", "status", "time"}, map[string]string{
				"title": "title", "label": "title", "name": "title",
				"text": "text", "body": "text", "desc": "text", "detail": "text",
				"status": "status", "state": "status",
				"time": "time", "date": "time", "at": "time",
			})
		if !ok || (m["title"] == "" && m["text"] == "") {
			return nil
		}
		return &OUIView{Kind: "step", Text: m["title"], Sub: m["text"], Variant: OpenStepStatus(m["status"]), Label: m["time"]}
	})
	if len(items) == 0 {
		return nil
	}
	title, _ := openText(env, openArg(e.args, 1), stack)
	return &OUIView{Kind: "steps", Text: strings.TrimSpace(title), Items: items}
}

// openStatCards is StatCards(items): Items of kind "stat" with Label,
// Text (value), Sub (delta or hint) and Tone.
func openStatCards(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	items := openMoreItems(env, openArg(e.args, 0), stack, func(it OUIExpr) *OUIView {
		m, ok := openMoreFields(env, it, stack, []string{"StatCard", "Metric"},
			[]string{"label", "value", "delta", "tone"}, map[string]string{
				"label": "label", "title": "label", "name": "label",
				"value": "value", "metric": "value",
				"delta": "delta", "change": "delta", "hint": "delta", "sub": "delta",
				"tone": "tone",
			})
		if !ok || m["label"] == "" || m["value"] == "" {
			return nil
		}
		return &OUIView{Kind: "stat", Label: OpenShort(m["label"], 40), Text: OpenShort(m["value"], 24), Sub: OpenShort(m["delta"], 40), Tone: openTone(m["tone"])}
	})
	if len(items) == 0 {
		return nil
	}
	return &OUIView{Kind: "stats", Items: items}
}

// OpenGalleryMaxCols caps Gallery's cols.
const OpenGalleryMaxCols = 8

// openGalleryCols reads Gallery's column count: a number at arg 1 or 2, or
// {cols: n} anywhere after the items. 0 means auto fit.
func openGalleryCols(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) int {
	n := 0.0
	for i := 1; i < len(e.args); i++ {
		a := openDeref(env, e.args[i], stack)
		switch a.kind {
		case "number":
			n = a.n
		case "object":
			for _, f := range a.items {
				if k := strings.ToLower(f.name); k == "cols" || k == "columns" {
					val := f
					if f.kind == "field" && len(f.items) > 0 {
						val = openDeref(env, f.items[0], stack)
					}
					if val.kind == "number" {
						n = val.n
					}
				}
			}
		}
	}
	if n < 1 {
		return 0
	}
	if n > OpenGalleryMaxCols {
		return OpenGalleryMaxCols
	}
	return int(n)
}

// openGallery is Gallery(items, title?, cols?): Items of kind "mediaitem" (Path,
// Text caption, Variant kind); a bare string is a path.
func openGallery(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	items := openMoreItems(env, openArg(e.args, 0), stack, func(it OUIExpr) *OUIView {
		if it.kind == "string" {
			s, _ := openText(env, it, stack)
			if s = strings.TrimSpace(s); s == "" {
				return nil
			}
			return &OUIView{Kind: "mediaitem", Path: s, Variant: OpenMediaKind("", s)}
		}
		v := openMediaItem(env, it, stack)
		if v == nil || v.Path == "" {
			return nil
		}
		return v
	})
	if len(items) == 0 {
		return nil
	}
	title := ""
	if t := openDeref(env, openArg(e.args, 1), stack); t.kind == "string" {
		title, _ = openText(env, t, stack)
	}
	return &OUIView{Kind: "gallery", Text: strings.TrimSpace(title), Items: items, Extra: openGalleryCols(env, e, stack)}
}

// openTaskProgress is TaskProgress(title, value?, meta?, file?, status?) or
// {title, value, meta, file, status}: one compact task row. value is 0–1,
// a percent (42 or "42%"), or empty for indeterminate (Min = -1). Text is
// the title, Label the numbers line, Sub the current file or error, and
// Variant the status (done|active|pending|error).
func openTaskProgress(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) *OUIView {
	m, ok := openMoreFields(env, e, stack, []string{"TaskProgress", "ArchiveProgress"},
		[]string{"title", "value", "meta", "file", "status"}, map[string]string{
			"title": "title", "label": "title", "name": "title",
			"value": "value", "percent": "value", "progress": "value",
			"meta": "meta", "detail": "meta", "speed": "meta",
			"file": "file", "current": "file", "error": "file",
			"status": "status", "state": "status",
			"style": "style", "variant": "style",
		})
	if !ok || m["title"] == "" {
		return nil
	}
	return &OUIView{Kind: "taskprogress", Text: OpenShort(m["title"], 80), Label: OpenShort(m["meta"], 80),
		Sub: m["file"], Variant: OpenStepStatus(m["status"]), Min: OpenFraction(m["value"]), Size: OpenProgressStyle(m["style"])}
}

// OpenFraction reads 0–1, 0–100 or "42%" as a fraction; empty or junk is -1.
func OpenFraction(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if s == "" {
		return -1
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return -1
	}
	if v > 1 {
		v /= 100
	}
	if v > 1 {
		v = 1
	}
	return v
}

// OpenProgressStyle normalizes a progress style: "gauge" (dial, meter,
// 仪表盘) or "bar"; empty means the default.
func OpenProgressStyle(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "gauge", "dial", "meter", "仪表盘":
		return "gauge"
	case "bar", "line", "条形":
		return "bar"
	}
	return ""
}

// openStyleArg reads {style: …} from any object argument of e.
func openStyleArg(env map[string]OUIExpr, e OUIExpr, stack map[string]bool) string {
	for _, a := range e.args {
		a = openDeref(env, a, stack)
		if a.kind != "object" {
			continue
		}
		m := map[string]string{}
		openMoreObject(env, a, stack, map[string]string{"style": "style", "variant": "style"}, m)
		if s := OpenProgressStyle(m["style"]); s != "" {
			return s
		}
	}
	return ""
}
