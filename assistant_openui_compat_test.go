package main

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
	"github.com/egoist/mygo/ui"
)

func TestOpenUIStreamAndHistory(t *testing.T) {
	partial := "前文\n```openui\nroot = Stack([\n"
	if streamShown(partial) != "前文" {
		t.Fatalf("unclosed fence leaked: %q", streamShown(partial))
	}
	closed := "甲\n```openui\nroot = TextContent(\"好\", \"medium\")\n```\n乙\n```openui\nroot = Stack([\n"
	shown := streamShown(closed)
	if !strings.Contains(shown, "好") || !strings.Contains(shown, "乙") || strings.Contains(shown, "Stack") {
		t.Fatalf("second fence: %q", shown)
	}
	marker := "看\n]]>openui:content\nroot = TextContent(\"甲\", \"medium\")\n]]>openui:end\n完"
	prose, blocks := splitOpenUI(marker)
	if prose != "看\n\n完" || len(blocks) != 1 || parseOpenUI(blocks[0]) == nil {
		t.Fatalf("marker prose %q blocks %#v", prose, blocks)
	}
	raw := "看过了\n```openui\nroot = TextContent(\"好\", \"medium\")\n```"
	turn, ok, ch := messageTurn(schema.Assistant, "", &schema.Message{Role: schema.Assistant, Content: raw})
	if !ok || ch != nil || !strings.Contains(turn.Text, "```openui") || !strings.Contains(turn.Text, "看过了") {
		t.Fatalf("history lost the fence: %+v", turn)
	}
	stopped, ok, _ := messageTurn(schema.Assistant, "", &schema.Message{Role: schema.Assistant, Content: partial})
	if !ok || strings.Contains(stopped.Text, "openui") || stopped.Text != "前文" {
		t.Fatalf("stopped turn: %+v %v", stopped, ok)
	}
	tool, ok, _ := messageTurn(schema.Tool, "read_file", &schema.Message{Role: schema.Tool, Content: partial})
	if !ok || !strings.Contains(tool.Text, "```openui") {
		t.Fatalf("tool fence was hidden: %+v", tool)
	}
	in := []aiTurn{{Role: "assistant", Text: "a", OpenSent: []string{"打开"}}}
	copied := diskTurns(in)
	copied[0].OpenSent[0] = "改了"
	if in[0].OpenSent[0] != "打开" || copied[0].Text != "a" {
		t.Fatalf("alias %v copy %v", in, copied)
	}
}

func TestAssistantInstructionMentionsOpenUI(t *testing.T) {
	for _, want := range []string{"{folder}", "{mode}", "{selection}", "{memory}", "```openui", "ToAssistant", "offer_choices", "CardHeader", "TreeItem"} {
		if !strings.Contains(assistantInstruction, want) {
			t.Fatalf("instruction missing %s", want)
		}
	}
	stripped := assistantInstruction
	for _, keep := range []string{"{folder}", "{mode}", "{selection}", "{memory}"} {
		stripped = strings.ReplaceAll(stripped, keep, "")
	}
	if strings.ContainsAny(stripped, "{}") {
		t.Fatalf("extra braces in instruction: %s", stripped)
	}
}

func TestTranscriptDrawsOpenUI(t *testing.T) {
	body := "左边有两个大文件夹。\n```openui\n" +
		"root = Stack([card])\n" +
		"card = Card([head, tbl, actions])\n" +
		"head = CardHeader(\"下载和文档\", \"按大小\")\n" +
		"tbl = Table([name, size])\n" +
		"name = Col(\"名称\", [\"下载\", \"文档\"])\n" +
		"size = Col(\"大小\", [\"2 GB\", \"400 MB\"])\n" +
		"actions = Buttons([openBtn], \"row\")\n" +
		"openBtn = Button(\"打开下载\", @ToAssistant(\"打开下载文件夹\"), \"primary\")\n" +
		"```\n可以继续问。"
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "比较一下"},
		{Role: "assistant", Text: body},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 560, 860)
	tt.Frame()
	for _, want := range []string{"比较一下", "左边有两个大文件夹", "下载和文档", "按大小", "下载", "2 GB", "打开下载", "可以继续问"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %s in %v", want, tt.Texts())
		}
	}
	for _, hidden := range []string{"```openui", "root =", "ToAssistant"} {
		if tt.HasText(hidden) {
			t.Fatalf("source leaked %s in %v", hidden, tt.Texts())
		}
	}
}

func findOpen(v *ouiView, kind string) *ouiView {
	if v == nil {
		return nil
	}
	if v.Kind == kind {
		return v
	}
	for _, ch := range v.Children {
		if got := findOpen(ch, kind); got != nil {
			return got
		}
	}
	for _, ch := range v.Items {
		if got := findOpen(ch, kind); got != nil {
			return got
		}
	}
	return nil
}

func findOpenLabel(v *ouiView, label string) *ouiView {
	if v == nil {
		return nil
	}
	if v.Kind == "button" && v.Label == label {
		return v
	}
	for _, ch := range v.Children {
		if got := findOpenLabel(ch, label); got != nil {
			return got
		}
	}
	for _, ch := range v.Items {
		if got := findOpenLabel(ch, label); got != nil {
			return got
		}
	}
	return nil
}

func findOpenText(v *ouiView, text string) bool {
	if v == nil {
		return false
	}
	if v.Text == text || v.Sub == text || v.Label == text {
		return true
	}
	for _, col := range v.Cols {
		for _, cell := range col.Cells {
			if cell == text {
				return true
			}
		}
	}
	for _, ch := range v.Children {
		if findOpenText(ch, text) {
			return true
		}
	}
	for _, ch := range v.Items {
		if findOpenText(ch, text) {
			return true
		}
	}
	return false
}

func dumpOpen(v *ouiView) string {
	if v == nil {
		return "<nil>"
	}
	var b strings.Builder
	dumpOpenInto(&b, v, 0)
	return b.String()
}

func dumpOpenInto(b *strings.Builder, v *ouiView, depth int) {
	if v == nil {
		return
	}
	b.WriteString(strings.Repeat("  ", depth))
	b.WriteString(v.Kind)
	if v.Text != "" {
		b.WriteString(" ")
		b.WriteString(v.Text)
	}
	if v.Label != "" {
		b.WriteString(" ")
		b.WriteString(v.Label)
	}
	b.WriteByte('\n')
	for _, ch := range v.Children {
		dumpOpenInto(b, ch, depth+1)
	}
	for _, ch := range v.Items {
		dumpOpenInto(b, ch, depth+1)
	}
}
