package main

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

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
	if _, ok := tt.Find("OpenUI placeholder"); !ok {
		t.Fatalf("no placeholder: %v", tt.Texts())
	}
	if tt.HasText("正在生成界面") || tt.HasText("CardHea") {
		t.Fatalf("spinner copy or half program shows: %v", tt.Texts())
	}
	if got := streamLive("前文\n<<<choices\n- a"); got != "前文" {
		t.Fatalf("a half choice block shows: %q", got)
	}
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
}
