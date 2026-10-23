package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/assistant"
)

// assistant_demo_test.go plays the offline demo's scripts against a seeded
// sandbox: each scenario changes the disk as its cards say, the delete
// waits for its confirmation card, and every finished card's 撤销 puts the
// disk back.

func demoApp(t *testing.T) (*App, string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	assistDemoForce, assistDemoSync = true, true
	t.Cleanup(func() { assistDemoForce, assistDemoSync = false, false })
	root, err := os.MkdirTemp("", "sunvale-demo-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err := assistDemoSeed(root); err != nil {
		t.Fatal(err)
	}
	a := newStoreApp(t, root)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	a.assist.ensure()
	return a, root
}

func demoSend(a *App, text string) {
	a.assist.draft = text
	a.assistantSubmit()
}

func lastOp(t *testing.T, a *App) (int, *assistant.FileOp) {
	t.Helper()
	for i := len(a.assist.turns) - 1; i >= 0; i-- {
		if op := a.assist.turns[i].Op; op != nil {
			return i, op
		}
	}
	t.Fatal("no card")
	return -1, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestAssistDemoScenariosAndUndo(t *testing.T) {
	a, root := demoApp(t)

	demoSend(a, "新建一个周报草稿")
	i, op := lastOp(t, a)
	if op.Status != assistant.OpDone || !exists(filepath.Join(root, "周报草稿.md")) {
		t.Fatalf("new: %+v", op)
	}
	a.assistOpUndo(i)
	if op.Status != assistant.OpUndone || exists(filepath.Join(root, "周报草稿.md")) {
		t.Fatalf("new undo: %+v", op)
	}

	demoSend(a, "把会议纪要里的时间修改为周五下午三点")
	i, op = lastOp(t, a)
	data, _ := os.ReadFile(filepath.Join(root, "会议纪要.md"))
	if op.Status != assistant.OpDone || string(data) != op.After {
		t.Fatalf("edit: %+v", op)
	}
	a.assistOpUndo(i)
	data, _ = os.ReadFile(filepath.Join(root, "会议纪要.md"))
	if string(data) != op.Before {
		t.Fatal("edit undo left the new text")
	}

	demoSend(a, "把 IMG_0001.jpg 按拍摄日期重命名并归类到图片文件夹")
	if !exists(filepath.Join(root, "图片", "2026-10-18 海边.jpg")) || exists(filepath.Join(root, "IMG_0001.jpg")) {
		t.Fatal("rename-move did not land in 图片")
	}

	demoSend(a, "删除所有临时文件")
	i, op = lastOp(t, a)
	if op.Status != assistant.OpPending || !op.Danger || !exists(filepath.Join(root, "cache.tmp")) {
		t.Fatalf("delete ran before its confirmation: %+v", op)
	}
	if a.assist.running {
		t.Fatal("a run waiting on a card should not block the composer")
	}
	a.assistOpDecide(i, true)
	if op.Status != assistant.OpDone || exists(filepath.Join(root, "cache.tmp")) {
		t.Fatalf("delete: %+v", op)
	}
	a.assistOpUndo(i)
	if !exists(filepath.Join(root, "cache.tmp")) {
		t.Fatal("delete undo did not restore")
	}

	demoSend(a, "批量给所有 txt 笔记加上日期前缀")
	_, op = lastOp(t, a)
	if op.Status != assistant.OpDone || op.Done() != len(op.Items) || len(op.Items) != 3 {
		t.Fatalf("batch: %+v", op)
	}

	demoSend(a, "总结一下这个文件夹里的文档")
	_, op = lastOp(t, a)
	if op.Kind != assistant.OpRead || op.Status != assistant.OpDone {
		t.Fatalf("summary: %+v", op)
	}
	if last := a.assist.turns[len(a.assist.turns)-1]; last.Role != "assistant" || last.At.IsZero() {
		t.Fatalf("summary reply: %+v", last)
	}

	// The window paints the cards with their status and the reply's copy.
	a.assistantOpen = true
	tt := ui.NewTester(a.view, 1360, 900)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"读取内容 · 4 项", "完成", "晴谷助手", "当前目录"} {
		if !tt.HasText(want) {
			t.Fatalf("%q missing: %v", want, tt.Texts())
		}
	}
}

// TestAssistDemoCancel: 取消 on a pending card leaves the files alone.
func TestAssistDemoCancel(t *testing.T) {
	a, root := demoApp(t)
	demoSend(a, "删除所有临时文件")
	i, op := lastOp(t, a)
	a.assistOpDecide(i, false)
	if op.Status != assistant.OpCancelled || !exists(filepath.Join(root, "cache.tmp")) {
		t.Fatalf("cancel: %+v", op)
	}
}
