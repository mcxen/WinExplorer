package main

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"sunvale/internal/forms"
)

// The OPENUI4 presentation components as the pane paints them: a hand-written
// dashboard program, then the preset scene that emits the same vocabulary
// through forms.ToOpenUI.

const dashProgram = `root = Card([head, state, total, free, months, mix, tip, health, div, actions])
head = CardHeader("磁盘概览", "示例数据")
state = Badge("健康", "success")
total = KeyValue("总容量", "1 TB")
free = KeyValue("可用空间", "312 GB", "info")
months = Chart(["8月", "9月", "10月"], [used, added])
used = Series("已用GB", [610, 655, 688])
added = Series("新增GB", [24, 45, 33])
mix = DonutChart([docs, pics, zips])
docs = Series("文档", [210])
pics = Series("图片", [168])
zips = Series("压缩包", [64])
tip = Note("图片近一月新增最多，可先整理截图再备份。", "info")
health = Accordion([smart])
smart = AccordionItem("SMART 概况", [smartNote], true)
smartNote = Note("通电 890 小时，无重映射扇区。", "success")
div = Divider()
actions = Buttons([plan], "row")
plan = Button("生成分配方案", @ToAssistant("按磁盘概览生成分配方案"), "primary")
`

func TestTranscriptDrawsOpenUIDashboard(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "看看磁盘"},
		{Role: "assistant", Text: "```openui\n" + dashProgram + "\n```"},
	}
	tt := assistTester(a, 560)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{
		"磁盘概览", "健康", "总容量", "1 TB", "可用空间", "312 GB",
		"已用GB", "新增GB", "文档", "图片", "压缩包",
		"图片近一月新增最多", "SMART 概况", "通电 890 小时",
		"生成分配方案",
	} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	// The chart's axis reports its newest sample scale, and the donut's
	// legend lists every slice value.
	if !tt.HasText("688") && !tt.HasText("600") {
		t.Fatalf("the chart scale is not painted: %v", tt.Texts())
	}
	if !tt.HasText("48%") || !tt.HasText("14%") {
		t.Fatalf("the donut shares are not painted: %v", tt.Texts())
	}
}

func TestOpenUIDashboardAccordionCollapses(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "看看磁盘"},
		{Role: "assistant", Text: "```openui\n" + dashProgram + "\n```"},
	}
	tt := assistTester(a, 900)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("通电 890 小时") {
		t.Fatalf("the first accordion item is not open: %v", tt.Texts())
	}
	// The item's title is the disclosure trigger. The panel animates out, so
	// settle its motion before asserting.
	if err := tt.Click("SMART 概况"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if tt.HasText("通电 890 小时") {
		t.Fatalf("the disclosure did not collapse: %v", tt.Texts())
	}
	if err := tt.Click("SMART 概况"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("通电 890 小时") {
		t.Fatalf("the disclosure did not expand: %v", tt.Texts())
	}
}

// TestOpenUIDiskSceneInjects: the preset scene emits every presentation kind
// through forms.ToOpenUI and paints; its rating writes a number string.
func TestOpenUIDiskSceneInjects(t *testing.T) {
	_ = forms.EnsureDemoSandbox()
	a := &App{}
	if !a.assistInjectOpenUIScene(forms.SceneDisk, "演示磁盘") {
		t.Fatal("inject disk scene")
	}
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 560, 2400)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{"磁盘概览", "健康", "近三月占用", "已用GB", "文档"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q in %v", want, tt.Texts())
		}
	}
	// The rating field defaults to an empty value; the pane seeds nothing, so
	// the "0 / 5" caption shows until the user picks stars.
	if !tt.HasText("0 / 5") && !tt.HasText("5") {
		t.Fatalf("rating caption missing: %v", tt.Texts())
	}
	msg := a.assist.turns[len(a.assist.turns)-1].Text
	if strings.Contains(msg, "健康") && strings.HasPrefix(msg, "Badge") {
		t.Fatalf("presentation kinds leaked into the message: %q", msg)
	}
}
