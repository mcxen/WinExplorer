package main

import (
	"testing"
)

// 整个窗口的视图（含右键菜单）逐帧构建时，不能碰剪贴板或别的阻塞调用。
func TestViewNeverBlocks(t *testing.T) {
	viewGuardFail = func(msg string) { t.Error(msg) }
	defer func() { viewGuardFail = nil }()
	warmTerminal() // main does it off the UI thread at startup
	a, tt, _ := keynavWindow(t, 3)
	a.clipPaths = nil
	for range 5 {
		tt.Frame()
	}
	if err := tt.RightClick("f001.txt"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.CloseMenu()
	a.assistantOpen = true
	tt.Frame()
}

// 守卫本身：视图期间读剪贴板被拒并报告。
func TestViewGuardRefusesClipboardInView(t *testing.T) {
	var got string
	viewGuardFail = func(msg string) { got = msg }
	defer func() { viewGuardFail = nil }()
	a := &App{sysClip: []string{"/cached"}}
	enterView()
	paths := a.refreshSystemClipboard()
	leaveView()
	if got == "" || len(paths) != 1 || paths[0] != "/cached" {
		t.Fatalf("guard msg %q, paths %v", got, paths)
	}
}
