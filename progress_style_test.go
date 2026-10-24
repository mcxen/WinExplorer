package main

import (
	"testing"
	"time"
)

func TestProgressEasedApproaches(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key := t.Name()
	if v, moving := progressEased(key, 0.2, now, false); v != 0.2 || moving {
		t.Fatalf("first sight %v %v", v, moving)
	}
	v, moving := progressEased(key, 1, now.Add(progressEase), false)
	if !moving || v < 0.65 || v > 0.75 {
		t.Fatalf("after one time constant %v %v", v, moving)
	}
	if v, moving = progressEased(key, 1, now.Add(time.Second), false); v != 1 || moving {
		t.Fatalf("settled %v %v", v, moving)
	}
	if v, _ = progressEased(key, 0.1, now.Add(2*time.Second), false); v != 0.1 {
		t.Fatalf("a drop jumps: %v", v)
	}
	if v, moving = progressEased(key, 0.9, now.Add(2*time.Second), true); v != 0.9 || moving {
		t.Fatalf("reduced motion %v %v", v, moving)
	}
}

func TestProgressStyleSettingPicksGauge(t *testing.T) {
	isolateArchiveTasks(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := newStoreApp(t, t.TempDir())
	if a.assistProgressStyle() != progressStyleBar {
		t.Fatal("default not bar")
	}
	a.store.updateSettings(func(s *appSettings) { s.ProgressStyle = "gauge" })
	at := time.Now().Add(-time.Minute)
	a.assist.turns = []aiTurn{{Role: "user", Text: "解压", At: at}, {Role: "assistant", Text: "好", At: at}}
	tk := startArchiveTask("extract", "/d/big.zip", "ai", 1000)
	tk.add(420)
	tt := assistTester(a, 600)
	tt.Frame()
	for _, want := range []string{"42%", "解压 big.zip", "420 B / 1000 B"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	if err := tt.Click("取消解压 big.zip"); err != nil || !tk.isCanceled() {
		t.Fatalf("cancel: %v", err)
	}
}

func TestOpenUITaskProgressGauge(t *testing.T) {
	a := &App{}
	text := "```openui\nroot = Stack([TaskProgress(\"解压 a.zip\", 0.6, \"8 MB/s · 剩余 0:12\", \"x.pdf\", \"active\", {style: \"gauge\"}), ProgressBar(\"p\", 30, {style: \"dial\"})])\n```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "看"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 500)
	tt.Frame()
	for _, want := range []string{"60%", "8 MB/s · 剩余 0:12", "x.pdf", "30%"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
}
