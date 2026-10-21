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
