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

func TestVisibleForActionAssistantPreset(t *testing.T) {
	vis := VisibleForAction("助手执行")
	if !vis["preset"] || !vis["prompt"] || !vis["dest_root"] || vis["source"] {
		t.Fatalf("%+v", vis)
	}
	if PresetIDFromLabel("分类下载") != "classify_downloads" {
		t.Fatal(PresetIDFromLabel("分类下载"))
	}
	if PresetLabelFromID("inbox_triage") != "收件箱整理" {
		t.Fatal(PresetLabelFromID("inbox_triage"))
	}
}

func TestScenePresetsOpenUI(t *testing.T) {
	for _, id := range ScenePresetIDs() {
		f, ok := Scene(id, nil)
		if !ok {
			t.Fatalf("scene %s missing", id)
		}
		f.Normalize()
		if len(f.Fields) == 0 || f.Title == "" {
			t.Fatalf("%s empty: %+v", id, f)
		}
		prog := f.ToOpenUI()
		if !strings.Contains(prog, "Form(") {
			t.Fatalf("%s OpenUI:\n%s", id, prog)
		}
	}
	if _, ok := Scene("nope", nil); ok {
		t.Fatal("unknown scene should fail")
	}
}

func TestNewKindsNormalize(t *testing.T) {
	raw := `{
  "title": "x",
  "fields": [
    {"id": "d", "kind": "date", "label": "日"},
    {"id": "t", "kind": "tags", "label": "标签"},
    {"id": "ft", "kind": "filetypes", "label": "类型", "options": [".pdf"]},
    {"id": "s", "kind": "slider", "label": "滑", "min": 0, "max": 10, "value": "3"},
    {"id": "p", "kind": "progress", "label": "进度", "value": "0.5"},
    {"id": "r", "kind": "table_select", "label": "行", "options": ["a", "b"]}
  ]
}`
	f, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, field := range f.Fields {
		kinds[field.ID] = field.Kind
	}
	for id, want := range map[string]string{
		"d": KindDate, "t": KindTags, "ft": KindFileTypes,
		"s": KindSlider, "p": KindProgress, "r": KindTableSelect,
	} {
		if kinds[id] != want {
			t.Fatalf("%s: %q want %q in %v", id, kinds[id], want, kinds)
		}
	}
	prog := f.ToOpenUI()
	for _, needle := range []string{"DateField(", "TagsField(", "FileTypeChips(", "SliderField(", "ProgressBar(", "TableSelect("} {
		if !strings.Contains(prog, needle) {
			t.Fatalf("missing %s in\n%s", needle, prog)
		}
	}
}
