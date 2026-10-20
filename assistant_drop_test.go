package main

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// assistant_drop_test.go covers a drop on the assistant: the paths are
// named for the next message and left where they were.

// TestAttachDroppedPathsLeavesOriginals pins a file URL and a folder without
// moving either, and refuses a remote link.
func TestAttachDroppedPathsLeavesOriginals(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "chat note.txt")
	folder := filepath.Join(dir, "chat-folder")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	raw := (&url.URL{Scheme: "file", Path: note}).String()
	a.attachDroppedPaths([]string{raw, folder})
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the named file was moved or removed: %v", err)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("the named folder was moved or removed: %v", err)
	}
	if len(a.assist.files) != 2 || a.assist.files[0].Path != note || a.assist.files[0].Name != "chat note.txt" ||
		a.assist.files[1].Path != folder || a.assist.files[1].Detail != "文件夹" {
		t.Fatalf("files = %+v", a.assist.files)
	}
	if a.assist.draft != "请阅读这些文件" || a.statusMsg != "已加入 2 个文件" {
		t.Fatalf("draft %q status %q", a.assist.draft, a.statusMsg)
	}
	entries, err := os.ReadDir(folder)
	if err != nil || len(entries) != 0 {
		t.Fatalf("folder gained a copy: %v %v", entries, err)
	}

	a.attachDroppedPaths([]string{note})
	if len(a.assist.files) != 2 || a.statusMsg != "没有新的文件" {
		t.Fatalf("repeat: %d %q", len(a.assist.files), a.statusMsg)
	}
	a.attachDroppedPaths([]string{"https://example.com/a.txt"})
	if len(a.assist.files) != 2 || a.statusMsg != "只能指定本地文件或文件夹" {
		t.Fatalf("rejected: %d %q", len(a.assist.files), a.statusMsg)
	}
}

// TestDragOntoAssistantAttaches drives the real pane: a drop on the header
// does nothing, a drop on the transcript and one on the composer name the
// files, and the originals stay put.
func TestDragOntoAssistantAttaches(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "chat-note.txt")
	folder := filepath.Join(dir, "chat-folder")
	if err := os.WriteFile(note, []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.assist.loaded = true
	a.assist.width = 420
	payload := dragPayload{paths: []string{note}}
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(appTheme(c))
		ui.Row(c).Fill().Children(func() {
			src := ui.Box(c).Width(140).FillHeight().Label("拖来源")
			src.Drag(payload)
			src.Children(func() {
				ui.Text(c, "拖来源").Padding(20)
			})
			a.assistantPane(c)
		})
	}, 980, 720)
	tt.Frame()

	dragTo := func(label string) {
		t.Helper()
		from, ok := tt.Find("拖来源")
		if !ok {
			t.Fatalf("drag source missing: %v", tt.Texts())
		}
		to, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%s missing: %v", label, tt.Texts())
		}
		x1, y1 := from.X+from.W/2, from.Y+from.H/2
		x2, y2 := to.X+to.W/2, to.Y+to.H/2
		tt.Press(x1, y1)
		for k := 1; k <= 4; k++ {
			f := float32(k) / 4
			tt.Move(x1+(x2-x1)*f, y1+(y2-y1)*f)
		}
		tt.Release(x2, y2)
	}

	dragTo("新会话")
	if len(a.assist.files) != 0 {
		t.Fatalf("header took the drop: %+v", a.assist.files)
	}
	dragTo("晴谷助手")
	if len(a.assist.files) != 1 || a.assist.files[0].Path != note {
		t.Fatalf("transcript drop: %+v", a.assist.files)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("transcript drop moved the file: %v", err)
	}
	if a.statusMsg != "已加入 1 个文件" || a.assist.draft != "请阅读这些文件" || !tt.HasText("chat-note.txt") {
		t.Fatalf("status %q draft %q texts %v", a.statusMsg, a.assist.draft, tt.Texts())
	}

	payload = dragPayload{paths: []string{folder}}
	dragTo("给助手的消息")
	if len(a.assist.files) != 2 || a.assist.files[1].Path != folder || a.assist.files[1].Detail != "文件夹" {
		t.Fatalf("composer drop: %+v", a.assist.files)
	}
	if _, err := os.Stat(folder); err != nil {
		t.Fatalf("composer drop moved the folder: %v", err)
	}
	if a.statusMsg != "已加入 1 个文件" || !tt.HasText("chat-folder") || !tt.HasText("文件夹") {
		t.Fatalf("status %q texts %v", a.statusMsg, tt.Texts())
	}
	payload = dragPayload{paths: []string{note}}
	dragTo("给助手的消息")
	if len(a.assist.files) != 2 || a.statusMsg != "没有新的文件" {
		t.Fatalf("repeat drop: %d %q", len(a.assist.files), a.statusMsg)
	}
}
