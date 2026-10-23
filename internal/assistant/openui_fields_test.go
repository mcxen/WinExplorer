package assistant

import (
	"strings"
	"testing"
)

func TestParseOpenUIExtendedFields(t *testing.T) {
	src := `
root = Card([form])
form = Form("sched", actions, [timeCtl, daysCtl, pathCtl, opCtl, swCtl, numCtl])
timeCtl = FormControl("时间", TimeField("time", "19:00", "required"))
daysCtl = FormControl("重复", DayChips("days", ["每天", "工作日", "周末"]))
pathCtl = FormControl("源目录", PathField("source", "~/Downloads", "folder", "required"))
opCtl = FormControl("操作", Segmented("op", ["移动", "复制"]))
swCtl = FormControl("备份", SwitchField("date_stamp", "按日期建子文件夹"))
numCtl = FormControl("超时", Number("timeout", "300"))
actions = Buttons([ok], "row")
ok = Button("创建", @ToAssistant("创建定时任务"), "primary")
`
	view := ParseOpenUI(src)
	form := findOpen(view, "form")
	if form == nil {
		t.Fatalf("no form: %s", dumpOpen(view))
	}
	kinds := map[string]string{}
	for _, ch := range form.Children {
		if OpenIsField(ch.Kind) {
			kinds[ch.Name] = ch.Kind
		}
	}
	want := map[string]string{
		"time": "time", "days": "days", "source": "path",
		"op": "segmented", "date_stamp": "switch", "timeout": "number",
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("field %s: got %q want %q in %#v", name, kinds[name], kind, kinds)
		}
	}
	path := findOpenName(view, "source")
	if path == nil || path.PathMode != "folder" || !path.Required {
		t.Fatalf("path %+v", path)
	}
	if !strings.Contains(dumpOpen(view), "time") {
		t.Fatal(dumpOpen(view))
	}
}

func findOpenName(v *OUIView, name string) *OUIView {
	if v == nil {
		return nil
	}
	if v.Name == name {
		return v
	}
	for _, ch := range v.Children {
		if found := findOpenName(ch, name); found != nil {
			return found
		}
	}
	for _, ch := range v.Items {
		if found := findOpenName(ch, name); found != nil {
			return found
		}
	}
	return nil
}

func TestParseOpenUINewFields(t *testing.T) {
	src := `
root = Card([form])
form = Form("demo", actions, [dateCtl, tagsCtl, ftCtl, slCtl, prCtl, tbCtl])
dateCtl = FormControl("日期", DateField("expire", "2026-12-31", "required"))
tagsCtl = FormControl("标签", TagsField("tags", "工作, 紧急"))
ftCtl = FormControl("类型", FileTypeChips("types", [".pdf", ".md"]))
slCtl = FormControl("大小", SliderField("max_mb", 0, 500))
prCtl = FormControl("进度", ProgressBar("step", "0.5"))
tbCtl = FormControl("行", TableSelect("rows", ["a → 1", "b → 2"], "required"))
actions = Buttons([ok], "row")
ok = Button("提交", @ToAssistant("提交"), "primary")
`
	view := ParseOpenUI(src)
	form := findOpen(view, "form")
	if form == nil {
		t.Fatalf("no form: %s", dumpOpen(view))
	}
	kinds := map[string]string{}
	for _, ch := range form.Children {
		if OpenIsField(ch.Kind) {
			kinds[ch.Name] = ch.Kind
		}
	}
	want := map[string]string{
		"expire": "date", "tags": "tags", "types": "filetypes",
		"max_mb": "slider", "step": "progress", "rows": "table_select",
	}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("field %s: got %q want %q in %#v", name, kinds[name], kind, kinds)
		}
	}
	sl := findOpenName(view, "max_mb")
	if sl == nil || sl.Min != 0 || sl.Max != 500 {
		t.Fatalf("slider %+v", sl)
	}
}

func TestParseOpenUIMediaFields(t *testing.T) {
	src := `
root = Card([form])
form = Form("media", actions, [listCtl, imgCtl, vidCtl])
listCtl = FormControl("文件", ListField("files", ["a.png|1 KB", "b.md|2 B"], "multi", "required"))
imgCtl = FormControl("预览", ImageField("pic", "/tmp/demo.png"))
vidCtl = FormControl("视频", VideoField("clip", "/tmp/demo.mp4"))
actions = Buttons([ok], "row")
ok = Button("提交", @ToAssistant("提交"), "primary")
`
	view := ParseOpenUI(src)
	form := findOpen(view, "form")
	if form == nil {
		t.Fatalf("no form: %s", dumpOpen(view))
	}
	kinds := map[string]string{}
	for _, ch := range form.Children {
		if OpenIsField(ch.Kind) {
			kinds[ch.Name] = ch.Kind
		}
	}
	want := map[string]string{"files": "list", "pic": "image", "clip": "video"}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Fatalf("field %s: got %q want %q in %#v", name, kinds[name], kind, kinds)
		}
	}
	list := findOpenName(view, "files")
	if list == nil || list.PathMode != "multi" || !list.Required || len(list.Options) != 2 {
		t.Fatalf("list %+v", list)
	}
	img := findOpenName(view, "pic")
	if img == nil || img.Placeholder != "/tmp/demo.png" {
		t.Fatalf("img %+v", img)
	}
}
