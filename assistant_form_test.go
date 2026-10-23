package main

import (
	"context"
	"strings"
	"testing"
)

func TestPresentFormSchedulePreset(t *testing.T) {
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(t, sess, "present_form", map[string]any{
		"preset":         "schedule",
		"submit_label":   "创建任务",
		"submit_message": "按表单创建定时任务",
	})
	if !strings.Contains(text, "```openui") || !strings.Contains(text, "TimeField(") ||
		!strings.Contains(text, "PathField(") || !strings.Contains(text, "DayChips(") ||
		!strings.Contains(text, "创建任务") {
		t.Fatalf("present_form output:\n%s", text)
	}
	if strings.Contains(text, "```form") {
		t.Fatal("present_form should emit OpenUI only, not a duplicate form fence")
	}
}

func TestPresentFormFieldsJSON(t *testing.T) {
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := callToolText(t, sess, "present_form", map[string]any{
		"title":       "备份确认",
		"fields_json": `[{"id":"path","kind":"path","label":"目录","required":true,"path_mode":"folder"},{"id":"op","kind":"segmented","label":"操作","options":["移动","复制"]}]`,
	})
	if !strings.Contains(text, "PathField(") || !strings.Contains(text, "Segmented(") ||
		!strings.Contains(text, "备份确认") {
		t.Fatalf("fields_json output:\n%s", text)
	}
}

func TestPresentFormScenePresets(t *testing.T) {
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{"rename", "share", "search", "classify", "backup"} {
		text := callToolText(t, sess, "present_form", map[string]any{"preset": preset})
		if !strings.Contains(text, "```openui") {
			t.Fatalf("%s: %s", preset, text)
		}
	}
}
