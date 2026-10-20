package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestLinkMarkdownPaths(t *testing.T) {
	cases := []struct{ in, want string }{
		{"看 `C:\\Users\\a\\下载`", "看 [`C:\\Users\\a\\下载`](<file:///C:/Users/a/下载>)"},
		{"用 `C:\\Program Files\\Sunvale\\资料`", "用 [`C:\\Program Files\\Sunvale\\资料`](<file:///C:/Program Files/Sunvale/资料>)"},
		{"在 /home/a/文档 里", "在 [/home/a/文档](<file:///home/a/文档>) 里"},
		// Bare prose paths stop at spaces today; callers wanting a Windows
		// path with spaces to be one link wrap it in a code span.
		{"C:\\Program Files\\Sunvale", "[C:\\\\Program](<file:///C:/Program>) Files\\Sunvale"},
		{"比例 1/2 不是路径", "比例 1/2 不是路径"},
		{"[已有](https://x.test/a/b)", "[已有](https://x.test/a/b)"},
		{"```\n/etc/hosts\n```", "```\n/etc/hosts\n```"},
		{"放到 ~/归档。", "放到 [~/归档](<~/归档>)。"},
	}
	for _, c := range cases {
		if got := linkMarkdownPaths(c.in); got != c.want {
			t.Errorf("linkMarkdownPaths(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for url, want := range map[string]string{
		"file:///home/a/x": "/home/a/x",
		"/tmp/x":           "/tmp/x",
	} {
		if got, ok := linkPath(url); !ok || got != want {
			t.Errorf("linkPath(%q) = %q %v", url, got, ok)
		}
	}
	if _, ok := linkPath("https://go.dev"); ok {
		t.Error("a web address read as a path")
	}
}

func assistTester(a *App, h int) *ui.Tester {
	return ui.NewTester(func(c *ui.Context) {
		c.SetTheme(appTheme(c))
		ui.Column(c).Fill().Children(func() { a.assistantTranscript(c) })
	}, 460, h)
}

// TestAssistantRendersMarkdownWithInlineCard: the reply is Markdown, not its
// syntax, the code block copies with a Chinese label, and the OpenUI card
// sits where the model wrote it, before the text that followed it.
func TestAssistantRendersMarkdownWithInlineCard(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "看看"},
		{Role: "assistant", Text: "## 建议标题\n\n这是 **重点**。\n\n```openui\nroot = Card([h])\nh = CardHeader(\"卡片标题\", \"说明\")\n```\n\n卡片后面的话\n\n```sh\nls -la\n```\n"},
	}
	tt := assistTester(a, 900)
	tt.Frame()
	for _, s := range []string{"建议标题", "卡片标题", "卡片后面的话", "复制", "ls -la"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q in %v", s, tt.Texts())
		}
	}
	if tt.HasText("## ") || tt.HasText("**") || tt.HasText("openui") {
		t.Fatalf("Markdown syntax shows: %v", tt.Texts())
	}
	card, _ := tt.Find("卡片标题")
	after, _ := tt.Find("卡片后面的话")
	if card.Y >= after.Y {
		t.Fatalf("the card (%v) is not above the text after it (%v)", card, after)
	}
}

// TestAssistantStreamingHoldsTheCardsPlace: an OpenUI program still being
// written shows a placeholder, not its source.
func TestAssistantStreamingHoldsTheCardsPlace(t *testing.T) {
	a := &App{}
	a.assist.running = true
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "看看"},
		{Role: "assistant", Text: streamLive("先说一句\n\n```openui\nroot = Card([h])\nh = CardHea")},
	}
	tt := assistTester(a, 600)
	tt.Frame()
	tt.Advance(2 * time.Second)
	if !tt.HasText("正在生成界面") {
		t.Fatalf("no placeholder: %v", tt.Texts())
	}
	if tt.HasText("CardHea") {
		t.Fatalf("the half program shows: %v", tt.Texts())
	}
	if got := streamLive("前文\n<<<choices\n- a"); got != "前文" {
		t.Fatalf("a half choice block shows: %q", got)
	}
}

// TestAssistantOpenPlaceholderMatchesCardHeight: closing the OpenUI fence
// swaps equal-height surfaces, so the text below does not jump.
func TestAssistantOpenPlaceholderMatchesCardHeight(t *testing.T) {
	const text = "```openui\nroot = Card([h])\nh = CardHeader(\"卡片标题\", \"说明\")\n```"
	a := &App{}
	a.assist.running = true
	a.assist.turns = []aiTurn{{Role: "user", Text: "x"}, {Role: "assistant", Text: streamLive(text[:len(text)-4])}}
	tt := assistTester(a, 500)
	tt.Frame()
	before, ok := tt.Find("OpenUI placeholder")
	if !ok {
		t.Fatalf("no placeholder: %v", tt.Texts())
	}

	a.assist.running = false
	a.assist.turns[1].Text = text
	tt.Advance(time.Second)
	after, ok := tt.Find("OpenUI card")
	if !ok {
		t.Fatalf("no card: %v", tt.Texts())
	}
	if diff := abs32(before.H - after.H); diff > 2 {
		t.Fatalf("placeholder/card height jumped by %.1f DIP: %.1f -> %.1f", diff, before.H, after.H)
	}
}

// TestAssistantCodeCopyFeedbackLastsFourSeconds: the inline confirmation has
// the same lifetime as mygo's ordinary toast.
func TestAssistantCodeCopyFeedbackLastsFourSeconds(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "x"},
		{Role: "assistant", Text: "```sh\nls -la\n```"},
	}
	tt := assistTester(a, 500)
	tt.Frame()
	if err := tt.Click("复制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已复制") {
		t.Fatalf("copy did not confirm: %v", tt.Texts())
	}
	tt.Advance(assistCopyFeedbackDuration - time.Millisecond)
	if !tt.HasText("已复制") {
		t.Fatal("copy feedback ended before the toast lifetime")
	}
	tt.Advance(20 * time.Millisecond)
	tt.Frame()
	if tt.HasText("已复制") {
		t.Fatal("copy feedback outlived the toast lifetime")
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestAssistLiveDropsStalePartials: a partial landing after the finished
// line it led to does not reopen it.
func TestAssistLiveDropsStalePartials(t *testing.T) {
	a := &App{}
	a.assist.live.seq = 2
	a.applyAssistLive(&assistLiveUpdate{seq: 2, id: 1, turn: aiTurn{Role: "assistant", Text: "完整"}})
	a.applyAssistLive(&assistLiveUpdate{seq: 1, id: 1, turn: aiTurn{Role: "assistant", Text: "半"}, partial: true})
	if len(a.assist.turns) != 1 || a.assist.turns[0].Text != "完整" || a.assist.liveOn {
		t.Fatalf("turns %+v live %v", a.assist.turns, a.assist.liveOn)
	}
	if !strings.Contains(normalizeOpenMarkers("a]]>openui:content\nx\n]]>openui:end"), "```openui") {
		t.Fatal("markers were not made a fence")
	}
}
