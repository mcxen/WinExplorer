package assistant

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseOpenUISubset(t *testing.T) {
	src := `
root = Stack([card])
card = Card([head, tbl, actions])
head = CardHeader("下载和文档", "按大小")
tbl = Table([name, size])
name = Col("名称", names)
size = Col("大小", ["2 GB", "400 MB"])
names = ["下载", "文档"]
actions = Buttons([openBtn, laterBtn], "row")
openBtn = Button("打开下载", @ToAssistant("打开下载文件夹"), "primary")
laterBtn = Button("先不打开", @ToAssistant("先不打开"), "secondary")
note = TextContent("不会出现", "small")
`
	prose, blocks := SplitOpenUI("上文\n```OpenUI\n" + src + "```\n下文")
	if prose != "上文\n\n下文" || len(blocks) != 1 {
		t.Fatalf("split prose %q blocks %d", prose, len(blocks))
	}
	view := ParseOpenUI(blocks[0])
	header := findOpen(view, "header")
	if header == nil || header.Text != "下载和文档" || header.Sub != "按大小" {
		t.Fatalf("header %+v", header)
	}
	table := findOpen(view, "table")
	if table == nil || len(table.Cols) != 2 || strings.Join(table.Cols[0].Cells, ",") != "下载,文档" || table.Cols[1].Cells[1] != "400 MB" {
		t.Fatalf("table %+v", table)
	}
	openBtn := findOpenLabel(view, "打开下载")
	later := findOpenLabel(view, "先不打开")
	if openBtn == nil || openBtn.Action != "打开下载文件夹" || openBtn.Variant != "primary" || later == nil || later.Action != "先不打开" {
		t.Fatalf("buttons %+v %+v", openBtn, later)
	}
	if findOpenText(view, "不会出现") {
		t.Fatal("unused statement was drawn")
	}
	if strings.Contains(dumpOpen(view), "不会出现") {
		t.Fatal("dump included unused text")
	}
}

func TestParseOpenUIDropsUnknownAndKeepsButton(t *testing.T) {
	view := ParseOpenUI(`
root = Stack([welcomeCard])
welcomeCard = Card([welcomeHeader, signupForm, actions])
welcomeHeader = CardHeader("欢迎", "开始")
signupForm = Form("signup", actions, [nameField])
nameField = Input("name", "你的名字", "text", {required: true, minLength: 2})
actions = Buttons([signUpBtn], "row")
signUpBtn = Button("注册", @ToAssistant("帮我注册"), "primary")
extra = TextContent("共 " + "2" + " 项", "small")
`)
	if !findOpenText(view, "欢迎") || findOpenLabel(view, "注册") == nil || findOpenLabel(view, "注册").Action != "帮我注册" {
		t.Fatalf("kept %+v", dumpOpen(view))
	}
	if findOpenText(view, "你的名字") || findOpenText(view, "共 2 项") {
		t.Fatal("form field or unused note was drawn")
	}
	if ParseOpenUI("root = (((") != nil || ParseOpenUI("root = Stack([root])") != nil {
		t.Fatal("garbage or a cycle produced a view")
	}
	cycled := ParseOpenUI("root = Stack([root, title])\ntitle = TextContent(\"还在\", \"medium\")\n")
	if !findOpenText(cycled, "还在") {
		t.Fatalf("cycle dropped the title: %+v", dumpOpen(cycled))
	}
	escaped := ParseOpenUI("root = TextContent(\"a\\\"b\", \"small\")\n")
	if escaped == nil || escaped.Text != "a\"b" {
		t.Fatalf("escape %+v", escaped)
	}
}

func TestParseOpenUITree(t *testing.T) {
	view := ParseOpenUI(`
root = Tree([top])
top = TreeItem("资料", "/tmp/资料", [docs, pics])
docs = TreeItem("文档", "/tmp/资料/文档", [report])
report = TreeItem("report.docx", "/tmp/资料/文档/report.docx")
pics = TreeItem("图片", "/tmp/资料/图片")
`)
	tree := findOpen(view, "tree")
	if tree == nil || len(tree.Items) != 1 {
		t.Fatalf("tree %s", dumpOpen(view))
	}
	top := tree.Items[0]
	if top.Text != "资料" || top.Path != "/tmp/资料" || len(top.Items) != 2 ||
		top.Items[0].Items[0].Text != "report.docx" {
		t.Fatalf("nested tree %+v", top)
	}
	compact := findOpen(ParseOpenUI(`root = Tree([TreeItem("root", [TreeItem("child")])])`), "tree")
	if compact == nil || len(compact.Items) != 1 || len(compact.Items[0].Items) != 1 ||
		compact.Items[0].Items[0].Text != "child" {
		t.Fatalf("compact TreeItem children: %s", dumpOpen(compact))
	}

	var nodes strings.Builder
	nodes.WriteString("root = Tree([")
	for i := 0; i < 205; i++ {
		if i > 0 {
			nodes.WriteString(", ")
		}
		nodes.WriteString(`TreeItem("n` + strconv.Itoa(i) + `")`)
	}
	nodes.WriteString("])")
	bounded := findOpen(ParseOpenUI(nodes.String()), "tree")
	if bounded == nil || len(bounded.Items) != 200 || bounded.Extra != 5 {
		t.Fatalf("node bound: items %d extra %d", len(bounded.Items), bounded.Extra)
	}

	deep := ParseOpenUI(`root = Tree([TreeItem("1", "", [TreeItem("2", "", [TreeItem("3", "", [TreeItem("4", "", [TreeItem("5", "", [TreeItem("6", "", [TreeItem("7", "", [TreeItem("8")])])])])])])])])`)
	level6 := findOpen(deep, "tree").Items[0]
	for range 5 {
		level6 = level6.Items[0]
	}
	if level6.Text != "6" || len(level6.Items) != 0 || level6.Extra != 2 {
		t.Fatalf("depth bound at level 6: %+v", level6)
	}
}

func findOpen(v *OUIView, kind string) *OUIView {
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

func findOpenLabel(v *OUIView, label string) *OUIView {
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

func findOpenText(v *OUIView, text string) bool {
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

func dumpOpen(v *OUIView) string {
	if v == nil {
		return "<nil>"
	}
	var b strings.Builder
	dumpOpenInto(&b, v, 0)
	return b.String()
}

func dumpOpenInto(b *strings.Builder, v *OUIView, depth int) {
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
