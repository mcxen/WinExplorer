package main

import (
	"strings"
	"testing"

	"sunvale/internal/rename"
)

const formProgram = `root = Card([head, form])
head = CardHeader("新建项目", "填好后发给助手")
form = Form("proj", actions, [nameCtl, kindCtl])
nameCtl = FormControl("名称", Input("name", "项目名", "text", {required: true}))
kindCtl = FormControl("类型", Select("kind", ["文档", SelectItem("img", "图片")]))
actions = Buttons([ok, wipe], "row")
ok = Button("创建", @ToAssistant("新建项目文件夹"), "primary")
wipe = Button("清空下载", @ToAssistant("清空下载文件夹"), "destructive")
`

func TestParseOpenUIForm(t *testing.T) {
	v := parseOpenUI(formProgram)
	form := findOpen(v, "form")
	if form == nil {
		t.Fatalf("no form: %s", dumpOpen(v))
	}
	var fields []*ouiView
	for _, ch := range form.Children {
		if openIsField(ch.Kind) {
			fields = append(fields, ch)
		}
	}
	if len(fields) != 2 || fields[0].Name != "name" || !fields[0].Required || fields[0].Label != "名称" ||
		fields[1].Kind != "select" || strings.Join(fields[1].Options, ",") != "文档,图片" {
		t.Fatalf("fields %+v %+v", fields[0], fields[1])
	}
	a := &App{}
	p := &openPaint{key: "k", form: form}
	ok := findOpenLabel(v, "创建")
	if _, missing := a.openMessage(ok, p); missing != "名称" {
		t.Fatalf("an empty required field was let through: %q", missing)
	}
	*a.openValue("k", "name") = "年报"
	*a.openValue("k", "kind") = "图片"
	msg, missing := a.openMessage(ok, p)
	if missing != "" || msg != "新建项目文件夹\n名称：年报\n类型：图片" {
		t.Fatalf("message %q missing %q", msg, missing)
	}
}

// TestOpenCardLocksAndConfirms: a danger button asks first; an answered card
// is locked and says what answered it.
func TestOpenCardLocksAndConfirms(t *testing.T) {
	a := &App{}
	text := "```openui\n" + formProgram + "```"
	a.assist.turns = []aiTurn{{Role: "user", Text: "建"}, {Role: "assistant", Text: text}}
	tt := assistTester(a, 900)
	tt.Frame()
	for _, s := range []string{"新建项目", "名称 *", "类型", "创建", "清空下载", "Alt+1 至 Alt+2 选择"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q: %v", s, tt.Texts())
		}
	}
	if err := tt.Click("清空下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("确定要「清空下载」吗？") || !tt.HasText("取消") {
		t.Fatalf("a danger button sent without asking: %v", tt.Texts())
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("确定要") {
		t.Fatal("cancel left the confirm up")
	}
	a.assist.turns[1].OpenDone = map[string]string{openBlockKey(formProgram): "创建"}
	tt.Frame()
	if !tt.HasText("已选择「创建」") || tt.HasText("Alt+1") {
		t.Fatalf("locked card: %v", tt.Texts())
	}
	if len(a.assist.open.keys) != 0 {
		t.Fatal("a locked card kept its shortcuts")
	}
}

func TestOpenFailedShowsSource(t *testing.T) {
	a := &App{}
	a.assist.turns = []aiTurn{{Role: "user", Text: "x"}, {Role: "assistant", Text: "```openui\nroot = (((\n```"}}
	tt := assistTester(a, 500)
	tt.Frame()
	if !tt.HasText("这块界面没能画出来") {
		t.Fatalf("no fallback: %v", tt.Texts())
	}
	if err := tt.Click("查看源码"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("root = (((") || !tt.HasText("收起源码") {
		t.Fatalf("source not shown: %v", tt.Texts())
	}
}

func TestSingleChoiceIsRadioWithCustom(t *testing.T) {
	a := &App{}
	ch := &aiChoice{Prompt: "照片怎么处理？", Options: []string{"按日期重命名", "不处理"}, On: make([]bool, 2)}
	a.assist.turns = []aiTurn{{Role: "user", Text: "x"}, {Role: "assistant", Text: "照片怎么处理？", Choice: ch}}
	tt := assistTester(a, 500)
	tt.Frame()
	if !tt.HasText("按日期重命名") || tt.HasText("发送所选") || !tt.HasText("发送") {
		t.Fatalf("single choice: %v", tt.Texts())
	}
	if n := strings.Count(strings.Join(tt.Texts(), "\n"), "照片怎么处理？"); n != 1 {
		t.Fatalf("the question shows %d times: %v", n, tt.Texts())
	}
	if len(a.assist.open.keys) != 2 {
		t.Fatalf("shortcuts %d", len(a.assist.open.keys))
	}
	ch.Sent, ch.Custom = true, "先放着"
	tt.Frame()
	if !tt.HasText("已选择「先放着」") {
		t.Fatalf("sent choice: %v", tt.Texts())
	}
}

func TestPlanConfirmCard(t *testing.T) {
	id := sunvaleToolState.putPlan(aiRenamePlan{Dir: "/tmp/x", Rows: []rename.Row{
		{Name: "IMG_1.jpg", To: "2026-10-01.jpg"},
		{Name: "IMG_2.jpg", To: "2026-10-01.jpg", Err: "重名"},
	}})
	a := &App{}
	a.assist.turns = []aiTurn{{Role: "user", Text: "改名"}, {Role: "tool", Tool: "rename_plan", Text: "方案", Plan: id}}
	tt := assistTester(a, 700)
	tt.Frame()
	for _, s := range []string{"确认重命名", "共 1 项", "1 项有冲突", "原名称", "新名称", "IMG_1.jpg", "2026-10-01.jpg", "（重名）", "应用方案", "不应用"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q: %v", s, tt.Texts())
		}
	}
	if err := tt.Click("不应用"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("已选择「不应用」") || tt.HasText("应用方案") {
		t.Fatalf("dropped plan: %v", tt.Texts())
	}
}

func TestOpenTableLinksPathsAndRequiredWord(t *testing.T) {
	v := parseOpenUI(`root = Card([tbl, f])
tbl = Table([Col("位置", ["/tmp", "备注"])])
f = Form("x", [], [Input("n", "名字", "text", "required")])`)
	in := findOpen(v, "input")
	if in == nil || !in.Required {
		t.Fatalf("required word: %s", dumpOpen(v))
	}
	a := &App{}
	a.assist.turns = []aiTurn{{Role: "user", Text: "x"}, {Role: "assistant", Text: "```openui\nroot = Card([tbl])\ntbl = Table([Col(\"位置\", [\"/tmp\", \"备注\"])])\n```"}}
	tt := assistTester(a, 500)
	tt.Frame()
	if _, ok := tt.Find("打开 /tmp"); !ok {
		t.Fatalf("the path cell is not a link: %v", tt.Texts())
	}
	if tt.HasText("第 1 行") {
		t.Fatal("rows are still labelled by number")
	}
}
