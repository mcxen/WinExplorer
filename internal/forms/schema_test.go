package forms

import (
	"strings"
	"testing"
)

func TestParseJSONAndSubmit(t *testing.T) {
	raw := `{
  "id": "sched",
  "title": "定时任务",
  "fields": [
    {"id": "name", "kind": "text", "label": "名称", "required": true},
    {"id": "time", "kind": "time", "label": "时间", "value": "19:00"},
    {"id": "days", "kind": "days", "label": "重复"},
    {"id": "op", "kind": "segmented", "label": "操作", "options": ["移动", "复制"]},
    {"id": "on", "kind": "switch", "label": "启用"}
  ],
  "submit_actions": [{"label": "创建", "variant": "primary", "message": "创建定时任务"}]
}`
	f, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "定时任务" || len(f.Fields) != 5 || f.Fields[2].Kind != KindDays ||
		len(f.Fields[2].Options) != 3 || f.Fields[3].Kind != KindSegmented {
		t.Fatalf("parsed %+v", f)
	}
	_, missing := f.FormatSubmit(map[string]string{"time": "19:00"})
	if missing != "名称" {
		t.Fatalf("missing %q", missing)
	}
	body, missing := f.FormatSubmit(map[string]string{"name": "备份", "time": "19:00", "op": "复制"})
	if missing != "" || body != "名称：备份\n时间：19:00\n操作：复制" {
		t.Fatalf("body %q missing %q", body, missing)
	}
}

func TestScheduleAddOpenUI(t *testing.T) {
	prog := ScheduleAdd(nil).ToOpenUI()
	for _, want := range []string{
		"TimeField(", "DayChips(", "PathField(", "Segmented(", "SwitchField(",
		"FormControl(\"名称\"", "Number(",
	} {
		if !strings.Contains(prog, want) {
			t.Fatalf("OpenUI missing %s in:\n%s", want, prog)
		}
	}
	vis := VisibleForAction("文件规则")
	if !vis["source"] || vis["prompt"] {
		t.Fatalf("visibility %+v", vis)
	}
}

func TestKnownKinds(t *testing.T) {
	if len(KnownKinds()) < 10 {
		t.Fatal(KnownKinds())
	}
}
