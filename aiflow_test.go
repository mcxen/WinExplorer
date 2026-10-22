package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// aiflow_test.go drives the conversation-wide lightwork: the header strip
// that flows while a run is alive and flourishes at its end, the status
// bar's heat count, and the pending tool's verb line.

// TestAssistFlowStrip: the strip animates while a run is alive, keeps
// animating through the cooling sweep right after, and is gone once that
// ends.
func TestAssistFlowStrip(t *testing.T) {
	a := &App{}
	a.assist.running = true
	tt := ui.NewTester(func(c *ui.Context) { a.assistFlowStrip(c) }, 480, 60)
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("a running run's strip does not flow")
	}
	a.assist.running = false
	a.assist.flowCool = time.Now().Add(assistFlowCool)
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("the cooling sweep should still flow")
	}
	a.assist.flowCool = time.Now().Add(-time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("the strip kept animating after the cool sweep")
	}
}

// TestPendingToolActivityUsesVerbs: the pending activity names what it is
// doing in words, with no detail — the toolkit's pulse dot only shows on
// a detail-less running row.
func TestPendingToolActivityUsesVerbs(t *testing.T) {
	for name, want := range map[string]string{
		"read_file":    "正在读取文件",
		"web_search":   "正在搜索网页",
		"web_read":     "正在读取网页",
		"search_names": "正在搜索文件名",
		"trash_items":  "正在整理文件",
		"unzip":        "正在解压",
		"something":    "正在调用 something",
	} {
		if got := assistToolVerb(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}

	a := &App{}
	a.assist.running = true
	a.assist.pending = "read_file"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看看"}}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 720)
	tt.Frame()
	if !tt.HasText("正在读取文件") {
		t.Fatalf("pending activity: %v", tt.Texts())
	}
}

// TestStatusBarAssistantHeat: the bar breathes out the count while the
// assistant works, then keeps the tally a few seconds.
func TestStatusBarAssistantHeat(t *testing.T) {
	fx, clk := swapTestFx(t)
	dir := t.TempDir()
	a := newStoreApp(t, dir)
	fx.markBusy(dir+"/a.txt", dir+"/b.txt")
	if n := fx.activeNow(); n != 2 {
		t.Fatalf("activeNow = %d", n)
	}
	tt := ui.NewTester(func(c *ui.Context) { a.statusBar(c) }, 800, 40)
	tt.Frame()
	if !tt.HasText("处理中 2 项") {
		t.Fatalf("busy count: %v", tt.Texts())
	}
	if !tt.FramePending() {
		t.Fatal("the heat dot stopped breathing")
	}

	fx.endRun()
	if n, rem := fx.recentDone(statusRecentDone); n != 2 || rem <= 0 {
		t.Fatalf("recentDone = %d, %v", n, rem)
	}
	tt.Frame()
	if !tt.HasText("刚处理了 2 项") {
		t.Fatalf("done tally: %v", tt.Texts())
	}
	clk.add(statusRecentDone + time.Second)
	if n, _ := fx.recentDone(statusRecentDone); n != 0 {
		t.Fatalf("recentDone after the window = %d", n)
	}
	if n := fx.activeNow(); n != 0 {
		t.Fatalf("activeNow after endRun = %d", n)
	}
}

// TestAIFxLitCountsDistinctPaths: one path read twice counts once, and the
// tally moves to doneInfo when the run ends.
func TestAIFxLitCountsDistinctPaths(t *testing.T) {
	fx, _ := swapTestFx(t)
	fx.markBusy("/d/a.txt", "/d/a.txt", "/d/b.txt")
	if _, rem := fx.recentDone(statusRecentDone); rem != 0 {
		t.Fatal("recentDone set mid-run")
	}
	fx.endRun()
	if n, rem := fx.recentDone(statusRecentDone); n != 2 || rem <= 0 {
		t.Fatalf("recentDone = %d, %v", n, rem)
	}
	fx.markBusy("/d/c.txt")
	if n := fx.activeNow(); n != 1 {
		t.Fatalf("activeNow = %d after a new run's mark", n)
	}
}
