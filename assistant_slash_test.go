package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAssistSlashDraftAndFilter(t *testing.T) {
	if !assistSlashDraft("/") || !assistSlashDraft("/记") || assistSlashDraft("/记\n续") || assistSlashDraft("记") {
		t.Fatal("draft detection")
	}
	if assistSlashQuery("/整理") != "整理" || assistSlashQuery("/") != "" {
		t.Fatal("query")
	}
	a := &App{}
	all := a.assistSlashCatalog()
	if len(all) < 6 {
		t.Fatalf("catalog=%d", len(all))
	}
	got := assistSlashFilter(all, "整")
	if len(got) != 1 || got[0].ID != "organize" {
		t.Fatalf("filter 整: %+v", got)
	}
	got = assistSlashFilter(all, "清空")
	if len(got) != 1 || got[0].ID != "clear" {
		t.Fatalf("filter 清空: %+v", got)
	}
	got = assistSlashFilter(all, "/新")
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("filter /新: %+v", got)
	}
	a.assist.disabledSkills = []string{"files", "memory", "archive"}
	off := a.assistSlashCatalog()
	for _, it := range off {
		if strings.HasPrefix(it.ID, "skill-") {
			t.Fatalf("disabled skill still listed: %+v", it)
		}
	}
}

func TestAssistSlashApplyActions(t *testing.T) {
	a := &App{}
	a.assist.draft = "/记忆"
	a.assistSlashApply(assistSlashItem{
		ID: "mem", Kind: slashAction,
		Run: func(app *App) {
			app.assist.showHist = true
			app.assist.draft = "记住："
		},
	})
	if !a.assist.showHist || a.assist.draft != "记住：" {
		t.Fatalf("memory: hist=%v draft=%q", a.assist.showHist, a.assist.draft)
	}
	a.assist.draft = "/整理"
	a.assistSlashApply(assistSlashItem{ID: "organize", Kind: slashFill, Fill: "整理当前文件夹，把同类文件归到一起"})
	if a.assist.draft != "整理当前文件夹，把同类文件归到一起" {
		t.Fatalf("fill: %q", a.assist.draft)
	}
	a.assist.draft = "/设置"
	a.assistSlashApply(assistSlashItem{
		ID: "settings", Kind: slashAction,
		Run: func(app *App) {
			app.assist.draft = ""
			app.openAssistantSettings()
		},
	})
	if a.assist.draft != "" || !a.settingsOpen || a.settingsUI.Category != "assistant" {
		t.Fatalf("settings: draft=%q open=%v cat=%q", a.assist.draft, a.settingsOpen, a.settingsUI.Category)
	}
}

func TestAssistantComposerEnterShiftEnter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	a := newStoreApp(t, dir)
	a.assist.ensure()
	a.assist.providers = nil
	a.assist.activeID = ""
	// No provider: Enter takes the send path but returns before clearing the
	// draft or starting a run — enough to prove GlassComposer wiring.
	a.assist.draft = "你好"

	tt := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 480, 360)
	tt.Frame()
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Shift, ui.KeyEnter)
	if a.assist.draft != "你好\n" {
		t.Fatalf("Shift+Enter newline: %q", a.assist.draft)
	}
	if a.assist.running || len(a.assist.turns) != 0 {
		t.Fatal("Shift+Enter must not send")
	}
	tt.Key(0, ui.KeyEnter)
	if a.assist.draft != "你好\n" {
		t.Fatalf("Enter without provider must keep draft: %q", a.assist.draft)
	}
	if a.statusMsg != "请先在设置 → 助手中添加供应商" {
		t.Fatalf("Enter send path: status=%q", a.statusMsg)
	}
	if a.assist.running || len(a.assist.turns) != 0 {
		t.Fatal("Enter without provider must not start a run")
	}
}

func TestAssistantSlashMenuOpenFilterSelect(t *testing.T) {
	a := &App{}
	a.assist.draft = "/"
	tt := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 480, 420)
	tt.Frame()
	if !tt.HasText("/记忆") || !tt.HasText("/整理") || !tt.HasText("/设置") {
		t.Fatalf("menu open: %v", tt.Texts())
	}
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Type("整")
	tt.Frame()
	if a.assist.draft != "/整" {
		// Type appends; draft may be "/整" if started as "/"
		t.Fatalf("typed filter draft=%q", a.assist.draft)
	}
	if !tt.HasText("/整理") || tt.HasText("/设置") {
		t.Fatalf("filtered: %v", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if a.assist.draft != "整理当前文件夹，把同类文件归到一起" {
		t.Fatalf("select fill: %q", a.assist.draft)
	}

	a.assist.draft = "/记"
	a.assist.slashHide = false
	a.assist.slashIdx = 0
	tt.Frame()
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	// First match for "记" is /记忆 (action) or depending on order; after Down may be skill.
	if a.assist.draft == "/记" {
		t.Fatal("Enter did not apply a slash item")
	}

	a.assist.draft = "/xyz"
	a.assist.slashHide = false
	tt.Frame()
	if !tt.HasText("无匹配命令") {
		t.Fatalf("empty filter: %v", tt.Texts())
	}
	if err := tt.Click("给助手的消息"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEscape)
	if !a.assist.slashHide {
		t.Fatal("Esc should dismiss")
	}
	tt.Frame()
	if tt.HasText("无匹配命令") || tt.HasText("/整理") {
		t.Fatalf("dismissed menu still visible: %v", tt.Texts())
	}
}

func TestAssistantSlashNewSessionWhenIdle(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{{Role: "user", Text: "hi"}, {Role: "assistant", Text: "ok"}}
	a.assist.draft = "/新会话"
	a.assistSlashApply(assistSlashItem{
		ID: "new", Kind: slashAction,
		Run: func(app *App) {
			app.assist.draft = ""
			app.assistantNewSession()
		},
	})
	if len(a.assist.turns) != 0 || a.assist.draft != "" {
		t.Fatalf("new session: turns=%d draft=%q", len(a.assist.turns), a.assist.draft)
	}
	a.assist.turns = []aiTurn{{Role: "user", Text: "hi"}}
	a.assist.running = true
	a.assist.stop = func() {}
	a.assist.draft = "/清空会话"
	before := len(a.assist.turns)
	a.assistSlashApply(assistSlashItem{
		ID: "clear", Kind: slashAction,
		Run: func(app *App) {
			app.assist.draft = ""
			app.assistantNewSession()
		},
	})
	if len(a.assist.turns) != before {
		t.Fatal("must not clear while busy")
	}
}

func TestAssistantSlashStopWhileRunningDraft(t *testing.T) {
	a := &App{}
	stopped := false
	a.assist.running = true
	a.assist.draft = "/整"
	a.assist.stop = func() { stopped = true }
	tt := ui.NewTester(func(c *ui.Context) { a.assistantComposer(c) }, 480, 360)
	tt.Frame()
	if !tt.HasText("停止") {
		t.Fatalf("stop missing while running with draft: %v", tt.Texts())
	}
	if err := tt.Click("停止"); err != nil {
		t.Fatal(err)
	}
	if !stopped || a.assist.status != "正在停止…" {
		t.Fatalf("stop: stopped=%v status=%q", stopped, a.assist.status)
	}
}
