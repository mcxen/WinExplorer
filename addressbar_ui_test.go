package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// addressbar_ui_test.go drives the address bar through the real window:
// every way into the editor, what Enter does with what was typed, the
// completions, and the crumbs' menus and overflow.

// emptyBarPoint is a point of the crumb bar past the crumbs.
func emptyBarPoint(t *testing.T, tt *ui.Tester) (float32, float32) {
	t.Helper()
	r, ok := tt.Find("地址栏空白")
	if !ok {
		t.Fatalf("no empty address-bar area: %v", tt.Texts())
	}
	return r.X + r.W/2, r.Y + r.H/2
}

// typeIntoAddress types s and reports the draft afterwards.
func typeIntoAddress(a *App, tt *ui.Tester, s string) string {
	tt.Type(s)
	tt.Frame()
	return a.cur().pathDraft
}

// TestAddressBarTakesTypingEveryWayIn: each way into the editor gives it the
// keyboard with the path selected, so typing replaces it — including after
// an edit lost the focus to the list, which used to leave an editor no key
// reached and F4 unable to give it back.
func TestAddressBarTakesTypingEveryWayIn(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b")
	a.navigate(filepath.Join(root, "a"))
	tt.Frame()
	ways := []struct {
		name string
		open func()
	}{
		{"空白处单击", func() { tt.ClickAt(emptyBarPoint(t, tt)) }},
		{"编辑按钮", func() { tt.Click("编辑地址") }},
		{"F4", func() { tt.Key(0, ui.KeyF4) }},
		{"Ctrl+L", func() { tt.Key(ui.Ctrl, ui.KeyL) }},
		{"Alt+D", func() { tt.Key(ui.Alt, ui.KeyD) }},
	}
	for _, w := range ways {
		w.open()
		tt.Frame()
		if !a.cur().editingPath || !a.addr.hadFocus {
			t.Fatalf("%s: editing %v, editor focused %v", w.name, a.cur().editingPath, tt.Texts())
		}
		if got := typeIntoAddress(a, tt, "x"); got != "x" {
			t.Fatalf("%s: typing gave %q, want the selected path replaced by x", w.name, got)
		}
		tt.Key(0, ui.KeyEscape) // closes the dropdown
		tt.Key(0, ui.KeyEscape) // closes the editor
		tt.Frame()
		if a.cur().editingPath || a.cur().path != filepath.Join(root, "a") {
			t.Fatalf("%s: Esc left editing %v at %s", w.name, a.cur().editingPath, a.cur().path)
		}
	}

	// The focus leaving the editor ends the edit, and F4 then works again.
	tt.Key(0, ui.KeyF4)
	tt.Frame()
	tt.ClickAt(700, 500)
	tt.Frame()
	if a.cur().editingPath {
		t.Fatal("a click in the list left the editor up without the focus")
	}
	tt.Key(0, ui.KeyF4)
	tt.Frame()
	if got := typeIntoAddress(a, tt, "y"); got != "y" {
		t.Fatalf("F4 after the editor lost the focus: typing gave %q", got)
	}
	// Bringing another tab forward drops the edit.
	tt.Key(ui.Ctrl, ui.KeyT)
	tt.Frame()
	tt.Key(ui.Ctrl, ui.KeyL)
	tt.Frame()
	a.switchTo(0)
	tt.Frame()
	if a.cur().editingPath || a.tabs[1].editingPath {
		t.Fatal("a tab switch left an edit open")
	}
	// A crumb navigates rather than editing.
	a.navigate(filepath.Join(root, "a"))
	tt.Frame()
	if err := tt.Click(filepath.Base(root)); err != nil {
		t.Fatal(err)
	}
	if a.cur().path != root || a.cur().editingPath {
		t.Fatalf("crumb click: at %s editing %v", a.cur().path, a.cur().editingPath)
	}
}

// TestAddressBarEnter: what Enter does with each kind of input.
func TestAddressBarEnter(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b")
	t.Setenv("HOME", root)
	t.Setenv("WE_ADDR_TEST", filepath.Join(root, "b"))
	enter := func(text string) {
		t.Helper()
		tt.Key(ui.Ctrl, ui.KeyL)
		tt.Frame()
		tt.Type(text)
		tt.Frame()
		tt.Key(0, ui.KeyEnter)
		tt.Frame()
	}
	enter(filepath.Join(root, "nope"))
	if !a.cur().editingPath || !tt.HasText("找不到") || a.cur().path == filepath.Join(root, "nope") {
		t.Fatalf("a missing folder: editing %v, texts %v", a.cur().editingPath, tt.Texts())
	}
	// Typing clears the error; Escape twice leaves.
	tt.Type("x")
	tt.Frame()
	if tt.HasText("找不到") {
		t.Fatal("the error outlived an edit of the text")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()

	cases := []struct{ in, want string }{
		{"~/a", filepath.Join(root, "a")},
		{`"` + filepath.Join(root, "b") + `"`, filepath.Join(root, "b")},
		{"$WE_ADDR_TEST", filepath.Join(root, "b")},
		{"  " + root + "  ", root},
		{"a", filepath.Join(root, "a")}, // relative to the folder on show
	}
	for _, c := range cases {
		enter(c.in)
		if a.cur().path != c.want || a.cur().editingPath {
			t.Fatalf("%q: at %s editing %v, want %s", c.in, a.cur().path, a.cur().editingPath, c.want)
		}
		a.navigate(root)
	}
	file := filepath.Join(root, "b", "b.txt")
	enter(file)
	if a.cur().path != filepath.Join(root, "b") || !a.cur().selected[file] {
		t.Fatalf("a file: at %s selected %v", a.cur().path, a.cur().selected)
	}
}

// TestAddressBarCompletes: a separator lists the folder's subfolders, the
// rest of the name narrows them (prefix first, any case), Tab accepts,
// ↓ and Enter go, and hidden folders show only with the setting.
func TestAddressBarCompletes(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "Alpha", "beta", "alphabet", "xalp", ".alpine")
	tt.Key(ui.Ctrl, ui.KeyL)
	tt.Frame()
	tt.Type(root + "/")
	tt.Frame()
	folderNames.wait(root)
	tt.Frame()
	labels := func() []string {
		var out []string
		for _, s := range a.addr.sugs {
			out = append(out, s.label)
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"Alpha", "alphabet", "beta", "xalp"}) {
		t.Fatalf("after the slash: %v", got)
	}
	if !tt.HasText("beta") {
		t.Fatalf("the dropdown is not on screen: %v", tt.Texts())
	}
	tt.Type("alp")
	tt.Frame()
	if got := labels(); !slices.Equal(got, []string{"Alpha", "alphabet", "xalp"}) {
		t.Fatalf("after alp: %v", got)
	}
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	if a.cur().pathDraft != filepath.Join(root, "Alpha") || !a.cur().editingPath {
		t.Fatalf("Tab: draft %q", a.cur().pathDraft)
	}
	if got := typeIntoAddress(a, tt, "/"); !strings.HasSuffix(got, "Alpha/") {
		t.Fatalf("typing after an accepted completion: %q", got)
	}
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame() // ".../Alph"
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.cur().path != filepath.Join(root, "alphabet") {
		t.Fatalf("↓↓ Enter: at %s, draft %q", a.cur().path, a.cur().pathDraft)
	}
	a.showHidden = true
	tt.Key(ui.Ctrl, ui.KeyL)
	tt.Frame()
	tt.Type(root + "/.al")
	tt.Frame()
	if got := labels(); !slices.Equal(got, []string{".alpine"}) {
		t.Fatalf("hidden shown: %v", got)
	}
}

// TestAddressBarPlaces: with no folder typed yet, the dropdown offers the
// recent places matching the text.
func TestAddressBarPlaces(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "recentone", "b")
	a.navigate(filepath.Join(root, "recentone"))
	a.navigate(filepath.Join(root, "b"))
	tt.Key(ui.Ctrl, ui.KeyL)
	tt.Frame()
	tt.Type("recent")
	tt.Frame()
	if len(a.addr.sugs) == 0 || a.addr.sugs[0].path != filepath.Join(root, "recentone") {
		t.Fatalf("places for recent: %+v", a.addr.sugs)
	}
	if err := tt.Click("recentone"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.cur().path != filepath.Join(root, "recentone") || a.cur().editingPath {
		t.Fatalf("clicking a suggestion: at %s editing %v", a.cur().path, a.cur().editingPath)
	}
}

// TestBreadcrumbMenus: a separator's menu lists the folders beside the next
// crumb; a narrow bar folds the leading crumbs into 更多位置; right-click
// copies the path.
func TestBreadcrumbMenus(t *testing.T) {
	a, tt, root := tabsWindow(t, 1200, "a", "b", "c")
	deep := filepath.Join(root, "a", "level-one", "level-two", "level-three")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	a.navigate(filepath.Join(root, "a"))
	tt.Frame()
	if err := tt.Click(filepath.Base(root) + " 中的文件夹"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Menu(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("separator menu: %v", got)
	}
	if err := tt.ChooseMenuItem("c"); err != nil {
		t.Fatal(err)
	}
	if a.cur().path != filepath.Join(root, "c") {
		t.Fatalf("chose c: at %s", a.cur().path)
	}
	tt.Frame()
	tt.RightClickAt(emptyBarPoint(t, tt))
	if err := tt.ChooseMenuItem("复制地址"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != filepath.Join(root, "c") {
		t.Fatalf("copied %q", tt.Clipboard())
	}

	tt.SetSize(560, 800)
	a.navigate(deep)
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("更多位置"); !ok {
		t.Fatalf("a narrow bar shows no overflow: %v", tt.Texts())
	}
	if _, ok := tt.Find("level-three"); !ok {
		t.Fatal("the last crumb was folded away")
	}
	tt.Click("更多位置")
	m := tt.Menu()
	if len(m) == 0 || m[len(m)-1] != "此电脑" {
		t.Fatalf("overflow menu: %v", m)
	}
}
