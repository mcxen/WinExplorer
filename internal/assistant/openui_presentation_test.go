package assistant

import (
	"strings"
	"testing"
)

// The OPENUI4 presentation components: tones, icons, chart data in both the
// positional and the JSON-minded shape, accordion disclosure defaults, and
// rating bounds. A shape the model writes that cannot be read drops that one
// statement, never the card.

func TestParseOpenUIPresentation(t *testing.T) {
	src := `
root = Card([head, state, total, free, months, mix, tip, health, score, more])
head = CardHeader("磁盘概览", "示例数据")
state = Badge("健康", "success")
total = KeyValue("总容量", "1 TB")
free = KeyValue("可用", "312 GB", "info")
months = Chart(["8月", "9月", "10月"], [used, added])
used = Series("已用GB", [610, 655, 688])
added = Series("新增GB", [24, 45, 33])
mix = DonutChart([docs, pics])
docs = Series("文档", [210])
pics = Series("图片", [168])
tip = Note("图片新增最多，先整理截图。", "info")
health = Accordion([smart, temp])
smart = AccordionItem("SMART 概况", [smartNote], true)
smartNote = Note("通电 890 小时，无重映射扇区。", "success")
temp = AccordionItem("温度与通电")
score = RatingField("score", "打分", 5)
more = Divider()
`
	view := ParseOpenUI(src)
	if view == nil {
		t.Fatal("parse failed")
	}
	badge := findOpen(view, "badge")
	if badge == nil || badge.Text != "健康" || badge.Tone != "success" {
		t.Fatalf("badge %+v", badge)
	}
	kv := findOpen(view, "keyvalue")
	if kv == nil || kv.Label != "总容量" || kv.Text != "1 TB" {
		t.Fatalf("keyvalue %+v", kv)
	}
	note := findOpen(view, "note")
	if note == nil || note.Tone != "info" {
		t.Fatalf("note %+v", note)
	}
	chartView := findOpen(view, "chart")
	if chartView == nil || len(chartView.Options) != 3 || len(chartView.Series) != 2 {
		t.Fatalf("chart %s", dumpOpen(chartView))
	}
	if got := chartView.Series[0].Values; len(got) != 3 || got[0] != 610 {
		t.Fatalf("series values %v", got)
	}
	donut := findOpen(view, "donut")
	if donut == nil || len(donut.Series) != 2 || donut.Series[0].Label != "文档" || donut.Series[0].Values[0] != 210 {
		t.Fatalf("donut %s", dumpOpen(donut))
	}
	acc := findOpen(view, "accordion")
	if acc == nil || len(acc.Items) != 2 {
		t.Fatalf("accordion %s", dumpOpen(acc))
	}
	if !acc.Items[0].Open || acc.Items[1].Open {
		t.Fatalf("accordion open flags: %+v %+v", acc.Items[0], acc.Items[1])
	}
	if len(acc.Items[0].Children) != 1 || acc.Items[0].Children[0].Kind != "note" {
		t.Fatalf("accordion children: %s", dumpOpen(acc.Items[0]))
	}
	rating := findOpen(view, "rating")
	if rating == nil || rating.Max != 5 || !OpenIsField("rating") {
		t.Fatalf("rating %+v", rating)
	}
	if findOpen(view, "divider") == nil {
		t.Fatal("no divider")
	}
}

// TestParseOpenUIChartObjectShape: {labels: […], series: [{label: …, values:
// […]}]} — the JSON-minded chart — reads the same as the positional one.
func TestParseOpenUIChartObjectShape(t *testing.T) {
	src := `
root = Card([c])
c = Chart({labels: ["一", "二"], series: [{label: "数值", values: [3, 7]}]})
`
	view := ParseOpenUI(src)
	c := findOpen(view, "chart")
	if c == nil || len(c.Options) != 2 || len(c.Series) != 1 || c.Series[0].Label != "数值" {
		t.Fatalf("object chart: %s", dumpOpen(view))
	}
	if got := c.Series[0].Values; len(got) != 2 || got[1] != 7 {
		t.Fatalf("values %v", got)
	}
}

// TestParseOpenUINoteDefaults: an unknown icon name is dropped rather than
// the note; tones accept the spellings a model reaches for.
func TestParseOpenUINoteDefaults(t *testing.T) {
	view := ParseOpenUI(`root = Card([a, b, c])
a = Note("留一句", "warn", "not-an-icon")
b = Note("亮一下", "", "lightbulb")
c = Badge("停", "error")`)
	if view == nil {
		t.Fatal("parse failed")
	}
	notes := findAllOpen(view, "note")
	if len(notes) != 2 {
		t.Fatalf("notes %s", dumpOpen(view))
	}
	if notes[0].Tone != "warning" || notes[0].Icon != "" {
		t.Fatalf("note tone/icon %+v", notes[0])
	}
	if notes[1].Icon != "lightbulb" {
		t.Fatalf("note icon %+v", notes[1])
	}
	badges := findAllOpen(view, "badge")
	if len(badges) != 1 || badges[0].Tone != "danger" {
		t.Fatalf("badge %+v", badges)
	}
}

// TestParseOpenUIDonutRejectsEmpty: a donut of nothing, or of negative-only
// values, is dropped rather than drawn as an empty ring; the sibling badge
// survives, which is the recover-as-far-as-readable rule.
func TestParseOpenUIDonutRejectsEmpty(t *testing.T) {
	view := ParseOpenUI(`root = Card([a, b, keep])
a = DonutChart([])
b = DonutChart([Series("空", [-1])])
keep = Badge("健康")`)
	if view == nil {
		t.Fatal("parse failed")
	}
	if strings.Contains(dumpOpen(view), "donut") {
		t.Fatalf("an empty donut was kept: %s", dumpOpen(view))
	}
	if findOpen(view, "badge") == nil {
		t.Fatalf("the sibling badge was dropped too: %s", dumpOpen(view))
	}
}

func findAllOpen(v *OUIView, kind string) []*OUIView {
	var out []*OUIView
	var walk func(v *OUIView)
	walk = func(v *OUIView) {
		if v == nil {
			return
		}
		if v.Kind == kind {
			out = append(out, v)
		}
		for _, ch := range v.Children {
			walk(ch)
		}
		for _, ch := range v.Items {
			walk(ch)
		}
	}
	walk(v)
	return out
}

// TestParseOpenUIAccordionItemNumberChildren: an AccordionItem whose children
// argument was written as a bare number becomes a text child of the item —
// the recover-as-far-as-readable rule — rather than losing the item.
func TestParseOpenUIAccordionItemNumberChildren(t *testing.T) {
	view := ParseOpenUI(`root = Card([a])
a = Accordion([AccordionItem("标题", 5)])`)
	if view == nil {
		t.Fatal("parse failed")
	}
	acc := findOpen(view, "accordion")
	if acc == nil || len(acc.Items) != 1 || acc.Items[0].Text != "标题" {
		t.Fatalf("accordion %s", dumpOpen(view))
	}
}
