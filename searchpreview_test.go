package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// searchpreview_test.go drives the quick-search preview through the same
// headless window as the result list, including the clipboard and the narrow
// layout where the preview gives the list the whole panel.

func searchPreviewWindow(t *testing.T, width int) (*App, *ui.Tester, []Entry) {
	t.Helper()
	dir, err := os.MkdirTemp("", "we-search-preview")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, name := range []string{"report-alpha.txt", "report-beta.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := newStoreApp(t, dir)
	a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
	tt := ui.NewTester(a.view, width, 760)
	tt.Key(ui.Ctrl, ui.KeyK)
	tt.Frame()
	a.searchBox.scope = scopeFolder
	tt.Type("report")
	tt.Frame()
	waitSearchBox(t, tt, a)
	a.searchBox.sortBy = sortName
	a.searchBox.sortHits()
	a.searchBox.cursor = a.searchBox.cursorRow()
	a.searchBox.list.ScrollIntoView(a.searchBox.cursor)
	tt.Frame()
	if len(a.searchBox.results) != 2 {
		t.Fatalf("preview fixture found %d hits, want 2: %v", len(a.searchBox.results), namesOf(a.searchBox.results))
	}
	return a, tt, append([]Entry(nil), a.searchBox.results...)
}

func TestSearchBoxPreviewFollowsSecondHit(t *testing.T) {
	a, tt, hits := searchPreviewWindow(t, 1200)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	second := hits[1]
	if a.searchBox.cursorPath != second.Path {
		t.Fatalf("cursor = %q, want second hit %q", a.searchBox.cursorPath, second.Path)
	}
	if _, ok := tt.Find(second.Path); !ok {
		t.Fatalf("preview does not show the selected full path %q: %v", second.Path, tt.Texts())
	}
	score := a.searchBox.scores[second.Path]
	if !tt.HasText("排序依据（得分 " + itoa(score) + "）") {
		t.Fatalf("preview does not show score %d: %v", score, tt.Texts())
	}
	for _, reason := range a.searchBox.reasons[second.Path] {
		if !tt.HasText(reason.Text) {
			t.Fatalf("preview omits Score reason %q: %v", reason.Text, tt.Texts())
		}
	}

	if err := tt.Click("复制路径"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != second.Path {
		t.Fatalf("copied path = %q, want %q", got, second.Path)
	}
}

func TestSearchBoxPreviewOpenLocationMatchesReveal(t *testing.T) {
	a, tt, hits := searchPreviewWindow(t, 1200)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	second := hits[1]
	if err := tt.Click("打开所在位置"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.searchBox.open {
		t.Fatal("preview's 打开所在位置 left the search panel open")
	}
	if a.cur().path != second.Dir {
		t.Fatalf("所在位置 = %q, want %q", a.cur().path, second.Dir)
	}
	if !a.cur().selected[second.Path] {
		t.Fatalf("所在位置 did not select %q", second.Path)
	}
}

func TestSearchBoxCtrlCCopiesHitOnlyWithoutQuerySelection(t *testing.T) {
	a, tt, hits := searchPreviewWindow(t, 1200)
	tt.Key(0, ui.KeyEnd)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.searchBox.querySelected {
		t.Fatal("End did not collapse the query selection")
	}
	second := hits[1]
	tt.Key(ui.Ctrl, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != second.Path {
		t.Fatalf("Ctrl+C copied %q, want hit path %q", got, second.Path)
	}

	// A selection in the query belongs to the editor's normal copy command.
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if got := tt.Clipboard(); got != "report" {
		t.Fatalf("Ctrl+C with query selection copied %q, want query text", got)
	}
}

func TestSearchBoxPreviewHiddenBelow900(t *testing.T) {
	_, tt, hits := searchPreviewWindow(t, 850)
	if _, ok := tt.Find(hits[1].Path); ok {
		t.Fatal("narrow window still shows the preview's full path")
	}
	if _, ok := tt.Find("复制路径"); ok {
		t.Fatal("narrow window still shows preview actions")
	}
	if !tt.HasText(hits[1].Name) {
		t.Fatalf("narrow window lost the result list: %v", tt.Texts())
	}
}
