package assistant

import (
	"strings"
	"testing"
)

// openui_recover_test.go pins the parser's leniency: a model that strays
// from the vocabulary still gets a card drawn, and a pathological program
// degrades to prose instead of taking the pane down.

// TestParseOpenUILenientShapes: the two most common strays — an action
// written without its parens, and a Table column written as a JSON-ish
// object — draw, as do the spec's own positional forms.
func TestParseOpenUILenientShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // "" means the program must not parse
	}{
		{"spec action", `root = Card([Button("确定", @ToAssistant("打开下载"))])`, "确定"},
		{"bare action", `root = Card([Button("确定", @confirm)])`, "确定"},
		{"spec table", "root = Stack([Table([n, s])])\nn = Col(\"名称\", [\"下载\"])\ns = Col(\"大小\", [\"2 GB\"])", "名称"},
		{"object table", `root = Stack([Table([{label:"名称", cells:["甲","乙"]}, {label:"大小", cells:["1KB"]}])])`, "名称"},
		{"object table alt keys", `root = Stack([Table([{title:"名称", values:["甲"]}])])`, "名称"},
		{"spec input", `root = Card([Input("目录", "选择文件夹", "path")])`, "目录"},
		{"object input", `root = Card([Input({name:"目录", placeholder:"选择文件夹"})])`, "目录"},
		{"object select", `root = Card([Select({name:"模式", options:["移动","复制"]})])`, "模式"},
		{"object segmented", `root = Card([Segmented({name:"op", options:["移动","复制"]})])`, "op"},
		{"truncated program", `root = Card([CardHeader("标题", `, ""},
		{"not openui", `这是一段普通说明文字`, ""},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := ParseOpenUI(tc.src)
			if tc.want == "" {
				if v != nil {
					t.Fatalf("a broken program parsed: %+v", v)
				}
				return
			}
			if v == nil {
				t.Fatalf("a lenient program failed to parse")
			}
			if !openViewHasText(v, tc.want) {
				t.Fatalf("parsed view does not carry %q: %+v", tc.want, v)
			}
		})
	}
}

// TestParseOpenUISurvivesPathologicalInput: whatever a runaway stream
// produces, the parser answers a view or nil — never a panic.
func TestParseOpenUISurvivesPathologicalInput(t *testing.T) {
	inputs := []string{
		strings.Repeat("[", 5000),
		strings.Repeat("(", 5000),
		"root = " + strings.Repeat("Stack([", 400) + "1" + strings.Repeat("])", 400),
		"a = a\nroot = Card([a])",
		"root = Card([" + strings.Repeat(`Button("x",@y),`, 2000) + "])",
		"root = Stack([Card([Card([Card([TextContent(\"深\")])])])])",
		"root = TextContent(\"未闭合的字符串",
		strings.Repeat("\"\\\"", 1000),
		"root = Table([" + strings.Repeat(`{label:"列",cells:["值"]},`, 500) + "])",
	}
	for i, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("input %d panicked: %v", i, r)
				}
			}()
			v := ParseOpenUI(in)
			if v != nil {
				// A drawn view is walked, so a deep or wide tree cannot
				// hurt the painter either.
				openViewHasText(v, "x")
			}
		}()
	}
}

// openViewHasText reports whether the tree carries text anywhere.
func openViewHasText(v *OUIView, want string) bool {
	if v == nil {
		return false
	}
	if strings.Contains(v.Text, want) || strings.Contains(v.Label, want) ||
		strings.Contains(v.Sub, want) || strings.Contains(v.Name, want) ||
		strings.Contains(v.Placeholder, want) {
		return true
	}
	for _, c := range v.Children {
		if openViewHasText(c, want) {
			return true
		}
	}
	for _, it := range v.Items {
		if openViewHasText(it, want) {
			return true
		}
	}
	for _, col := range v.Cols {
		if strings.Contains(col.Label, want) {
			return true
		}
		for _, cell := range col.Cells {
			if strings.Contains(cell, want) {
				return true
			}
		}
	}
	for _, o := range v.Options {
		if strings.Contains(o, want) {
			return true
		}
	}
	return false
}
