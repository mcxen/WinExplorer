package main

import (
	"context"
	"strings"
	"testing"

	"sunvale/internal/schedule"
)

func TestAssistantTaskJobFromArgsPreset(t *testing.T) {
	j, err := assistantTaskJobFromArgs(assistantTaskRunArgs{
		Preset: "classify_downloads",
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Action != schedule.ActionAssistant || j.Preset != schedule.PresetClassifyDownloads {
		t.Fatalf("%+v", j)
	}
	if j.Prompt == "" || strings.Contains(j.Prompt, "{{") {
		t.Fatalf("prompt %q", j.Prompt)
	}
	if j.Folder == "" {
		t.Fatal("folder empty")
	}
}

func TestAssistantTaskJobFromArgsPromptOnly(t *testing.T) {
	j, err := assistantTaskJobFromArgs(assistantTaskRunArgs{
		Prompt: "整理桌面",
		Folder: "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Preset != "" || j.Prompt != "整理桌面" || j.Folder != "/tmp" {
		t.Fatalf("%+v", j)
	}
}

func TestAssistantTaskJobFromArgsRequiresPromptOrPreset(t *testing.T) {
	_, err := assistantTaskJobFromArgs(assistantTaskRunArgs{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAssistantTaskRunStartsAsync(t *testing.T) {
	tools := sunvaleToolState
	res, _, err := tools.assistantTaskRun(context.Background(), nil, assistantTaskRunArgs{
		Prompt: "只是测试，不要改文件",
		Folder: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := mcpTextOf(res)
	if res.IsError || !strings.Contains(out, "已开始执行") {
		t.Fatalf("%s", out)
	}
}

func TestPromptListsAssistantTaskRun(t *testing.T) {
	if !strings.Contains(assistantInstruction, "assistant_task_run") {
		t.Fatal("Instruction missing assistant_task_run")
	}
	found := false
	for _, n := range sunvaleBuiltinToolNames() {
		if n == "assistant_task_run" {
			found = true
		}
	}
	if !found {
		t.Fatal("sunvaleBuiltinToolNames missing assistant_task_run")
	}
}

func TestScheduleCreateWithPreset(t *testing.T) {
	st := useTempScheduleStore(t)
	res, _, err := sunvaleToolState.scheduleCreate(context.Background(), nil, scheduleCreateArgs{
		Name: "每晚分类下载", Time: "19:00", Days: "每天",
		Action: "assistant", Preset: "分类下载",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := mcpTextOf(res)
	if res.IsError || !strings.Contains(out, "已创建") {
		t.Fatalf("%s", out)
	}
	jobs := st.Jobs()
	if len(jobs) != 1 || jobs[0].Preset != schedule.PresetClassifyDownloads {
		t.Fatalf("%+v", jobs)
	}
	if jobs[0].Prompt == "" || strings.Contains(jobs[0].Prompt, "{{") {
		t.Fatalf("prompt %q", jobs[0].Prompt)
	}
}
