package schedule

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllPresetsHaveIDsAndPlaceholders(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range AllPresets() {
		if p.ID == "" || p.Label == "" || p.Prompt == "" {
			t.Fatalf("incomplete preset %+v", p)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate id %s", p.ID)
		}
		seen[p.ID] = true
		if !strings.Contains(p.Prompt, "{{folder}}") {
			t.Fatalf("%s missing {{folder}}", p.ID)
		}
	}
	for _, id := range []string{PresetClassifyDownloads, PresetInboxTriage, PresetRenameByContent} {
		if !seen[id] {
			t.Fatalf("missing %s", id)
		}
	}
	if _, ok := PresetByID("nope"); ok {
		t.Fatal("unknown should miss")
	}
}

func TestApplyPresetFillsPrompt(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := ApplyPreset(&j, PresetClassifyDownloads, "", ""); err != nil {
		t.Fatal(err)
	}
	wantFolder := filepath.Join(home, "Downloads")
	wantDest := filepath.Join(home, "Documents", "来自下载")
	if j.Action != ActionAssistant || j.Preset != PresetClassifyDownloads {
		t.Fatalf("%+v", j)
	}
	if j.Folder != wantFolder || j.DestDir != wantDest {
		t.Fatalf("folder=%q dest=%q", j.Folder, j.DestDir)
	}
	if !strings.Contains(j.Prompt, wantFolder) || !strings.Contains(j.Prompt, wantDest) {
		t.Fatalf("prompt not filled: %s", j.Prompt[:80])
	}
	if strings.Contains(j.Prompt, "{{") {
		t.Fatal("placeholders left")
	}
	if j.Name != "分类下载" {
		t.Fatalf("name %q", j.Name)
	}
}

func TestResolvePresetID(t *testing.T) {
	if ResolvePresetID("分类下载") != PresetClassifyDownloads {
		t.Fatal(ResolvePresetID("分类下载"))
	}
	if ResolvePresetID("自定义") != "" || ResolvePresetID("") != "" {
		t.Fatal("custom")
	}
	if ResolvePresetID(PresetRenameByContent) != PresetRenameByContent {
		t.Fatal("passthrough")
	}
}

func TestFillPresetPrompt(t *testing.T) {
	got := FillPresetPrompt("a={{folder}} b={{dest}}", "/f", "/d")
	if got != "a=/f b=/d" {
		t.Fatal(got)
	}
}
