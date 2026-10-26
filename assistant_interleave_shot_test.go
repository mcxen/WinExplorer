package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"sunvale/internal/forms"
)

// interleaveDemoTurns is a multi-step ReAct reply: text, list_dir, text,
// two read_file calls, then a summary with an OpenUI card.
func interleaveDemoTurns() []aiTurn {
	at := time.Date(2026, 10, 26, 23, 20, 0, 0, time.Local)
	sum := "root = Card([stats, steps])\n" +
		"stats = StatCards([StatCard(\"文档\", \"3\", \"Markdown\"), StatCard(\"总字数\", \"4,210\", \"约 9 分钟\"), StatCard(\"待办\", \"5\", \"2 项本周\", \"warning\")])\n" +
		"steps = Steps([Step(\"发布 v0.9 测试版\", \"路线图.md\", \"active\", \"11/02\"), Step(\"迁移设置存储\", \"设计说明.md\", \"pending\"), Step(\"补全快捷键文档\", \"会议纪要.md\", \"pending\")], \"关键事项\")\n"
	return []aiTurn{
		{Role: "user", Text: "帮我总结一下 docs 文件夹里的文档", At: at},
		{Role: "assistant", Text: "好的，我先列出 docs 文件夹里的文件，看看有哪些文档。", At: at},
		{Role: "tool", Tool: "list_dir", Text: "docs/ 3 个文件\n路线图.md  6.2 KB\n设计说明.md  9.8 KB\n会议纪要.md  3.1 KB", At: at},
		{Role: "assistant", Text: "共有 **3 个 Markdown 文档**。接下来逐个读取内容。", At: at},
		{Role: "tool", Tool: "read_file", Text: "docs/路线图.md  142 行\n# 路线图 ...", At: at},
		{Role: "tool", Tool: "read_file", Text: "docs/设计说明.md  236 行\n# 设计说明 ...", At: at},
		{Role: "tool", Tool: "read_file", Text: "docs/会议纪要.md  71 行\n# 10 月 20 日 ...", At: at},
		{Role: "assistant", Text: "三份文档的要点如下：\n\n- **路线图**：v0.9 测试版定于 11 月 2 日发布。\n- **设计说明**：设置存储迁移到单文件 JSON。\n- **会议纪要**：快捷键文档需在本周补齐。\n\n" + forms.FenceOpenUI(sum), At: at},
	}
}

// TestToolInterleaveShots writes TOOL-INTERLEAVE shots when SUNVALE_SHOT_DIR
// is set. SUNVALE_SHOT_PREFIX picks the file names (before / after).
func TestToolInterleaveShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	t.Cleanup(func() { testDark = nil; resolveTokens() })
	shots := []struct {
		name string
		dark bool
	}{{"02-after.png", false}, {"03-after-dark.png", true}}
	if p := os.Getenv("SUNVALE_SHOT_PREFIX"); p != "" {
		shots = shots[:1]
		shots[0].name = p + ".png"
	}
	for _, shot := range shots {
		d := shot.dark
		testDark = &d
		resolveTokens()
		a := newStoreApp(t, t.TempDir())
		a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
		a.assist.turns = interleaveDemoTurns()
		tt := ui.NewTester(func(c *ui.Context) {
			c.SetTheme(appTheme(c))
			ui.Column(c).Fill().Background(colorSurface()).Children(func() { a.assistantTranscript(c) })
		}, 460, 900)
		// Show the turn's work open, as while reading it.
		for range 20 {
			tt.Frame()
			time.Sleep(50 * time.Millisecond)
		}
		for _, p := range a.assist.paint {
			p.turn.SetExpanded(true)
			p.group.SetExpanded(true)
		}
		for range 10 {
			tt.Frame()
			time.Sleep(50 * time.Millisecond)
		}
		f, err := os.Create(filepath.Join(dir, shot.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
