package main

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/forms"
)

func TestTranscriptDrawsOpenUIMedia(t *testing.T) {
	_ = forms.EnsureDemoSandbox()
	a := &App{}
	if !a.assistInjectOpenUIScene(forms.SceneFileList, "演示清单") {
		t.Fatal("inject")
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 560, 1100)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"批量文件清单", "demo-photo.png", "会议纪要.md", "暂无匹配文件", "处理所选"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}

	a.assist.turns = nil
	a.assist.open = assistOpenState{}
	if !a.assistInjectOpenUIScene(forms.SceneImage, "演示图片") {
		t.Fatal("inject image")
	}
	tt = ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 560, 900)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"图片预览确认", "预览", "坏图占位", "无法解码图片", "确认使用"} {
		if !tt.HasText(want) {
			t.Fatalf("image missing %q in %v", want, tt.Texts())
		}
	}

	a.assist.turns = nil
	a.assist.open = assistOpenState{}
	if !a.assistInjectOpenUIScene(forms.SceneVideo, "演示视频") {
		t.Fatal("inject video")
	}
	tt = ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 560, 900)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"视频关键帧预览", "打开播放", "无内嵌播放器", "归档此片段"} {
		if !tt.HasText(want) {
			t.Fatalf("video missing %q in %v", want, tt.Texts())
		}
	}
}
