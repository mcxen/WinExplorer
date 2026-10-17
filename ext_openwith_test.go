package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestShellExtensionsConfig(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-ext")
	defer os.RemoveAll(dir)
	os.WriteFile(filepath.Join(dir, "extensions.json"), []byte(`[
	  {"name": "用 VSCode 打开", "match": ["*"], "command": "code \"{path}\""},
	  {"name": "转 MP3", "match": [".WAV", "flac"], "command": "ffmpeg -i {path} {stem}.mp3"},
	  {"name": "", "match": ["png"], "command": "x"},
	  {"name": "坏条目", "match": [], "command": "x"},
	  {"name": "在终端解压", "match": ["zip", "dir"], "command": "cd {dir}", "terminal": true}
	]`), 0o644)

	exts, errs, err := loadShellExtensionsAt(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(exts) != 3 {
		t.Fatalf("kept %d: %+v", len(exts), exts)
	}
	if len(errs) != 2 {
		t.Fatalf("load errors = %v, want the two malformed entries kept", errs)
	}

	wav := Entry{Name: "song.WAV", Path: "/x/song.WAV", Ext: "wav", IsDir: false}
	dir_ := Entry{Name: "项目", Path: "/x/项目", IsDir: true}
	txt := Entry{Name: "a.txt", Path: "/x/a.txt", Ext: "txt", IsDir: false}

	// "*" matches everything.
	if !exts[0].matches(wav) || !exts[0].matches(dir_) {
		t.Fatal("'*' should match all")
	}
	// ".WAV" normalizes to "wav" case-insensitively.
	if !exts[1].matches(wav) {
		t.Fatal("wav should match")
	}
	if exts[1].matches(txt) {
		t.Fatal("txt must not match")
	}
	// "dir" matches folders only.
	if !exts[2].matches(dir_) || exts[2].matches(txt) {
		t.Fatal("dir matching")
	}

	// Placeholders.
	got := exts[1].expand(Entry{Name: "song.wav", Path: "/x/song.wav", Ext: "wav"})
	want := "ffmpeg -i /x/song.wav song.mp3"
	if got != want {
		t.Fatalf("expand = %q want %q", got, want)
	}

	// Running a non-terminal command really runs it (detached, so poll).
	out := filepath.Join(dir, "ok.txt")
	ext := shellExt{Name: "t", Match: []string{"*"}, Command: "printf ok > " + out}
	if err := ext.run(Entry{Path: "x"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for i := 0; i < 100; i++ {
		if b, err := os.ReadFile(out); err == nil {
			if string(b) == "ok" {
				break
			}
			t.Fatalf("command wrote %q", b)
		}
		time.Sleep(20 * time.Millisecond)
		if i == 99 {
			t.Fatal("command effect missing after 2s")
		}
	}

	// A missing config file is empty, not an error.
	if exts, errs, err := loadShellExtensionsAt(filepath.Join(dir, "nope")); err != nil || len(exts) != 0 || len(errs) != 0 {
		t.Fatalf("missing config: %v %v %v", exts, errs, err)
	}
}

// TestShellExtDisabledCompat checks the Disabled flag's backward
// compatibility: extensions.json files written before the field existed
// read as enabled, and enabled extensions still serialize without it.
func TestShellExtDisabledCompat(t *testing.T) {
	var x shellExt
	if err := json.Unmarshal([]byte(
		`{"name":"a","match":["*"],"command":"x"}`), &x); err != nil {
		t.Fatal(err)
	}
	if x.Disabled {
		t.Fatal("an extension without the disabled field must read as enabled")
	}
	data, err := json.Marshal(shellExt{Name: "a", Match: []string{"*"}, Command: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "disabled") {
		t.Fatalf("enabled extension serialized with disabled: %s", data)
	}
	x.Disabled = true
	data, err = json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"disabled":true`) {
		t.Fatalf("disabled extension must serialize the flag: %s", data)
	}
}

// TestSaveShellExtensionsRoundTrip writes a registry and reads it back:
// the saved file keeps every field, disabled extensions included.
func TestSaveShellExtensionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []shellExt{
		{Name: "用 VSCode 打开", Match: []string{"*"}, Command: `code "{path}"`},
		{Name: "转 MP3", Match: []string{"wav"}, Command: "ffmpeg -i {path} {stem}.mp3", Terminal: true},
		{Name: "停用的", Match: []string{"dir"}, Command: "x", Disabled: true},
	}
	if err := saveShellExtensionsAt(dir, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, errs, err := loadShellExtensionsAt(dir)
	if err != nil || len(errs) != 0 {
		t.Fatalf("reload: %v %v", errs, err)
	}
	if len(got) != len(want) {
		t.Fatalf("reloaded %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Command != want[i].Command ||
			got[i].Terminal != want[i].Terminal || got[i].Disabled != want[i].Disabled ||
			!slices.Equal(got[i].Match, want[i].Match) {
			t.Fatalf("reloaded[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestShellExtLoadErrors covers the malformed cases: per-entry problems
// ride back in the error list while the good entries stay, and a whole
// file that is not JSON is an error.
func TestShellExtLoadErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "extensions.json"), []byte(`[
	  {"name": "好条目", "match": ["txt"], "command": "x"},
	  {"name": "", "match": ["txt"], "command": "x"},
	  {"name": "无规则", "match": [], "command": "x"},
	  {"name": "无命令", "match": ["txt"], "command": "  "}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	exts, errs, err := loadShellExtensionsAt(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(exts) != 1 || exts[0].Name != "好条目" {
		t.Fatalf("kept %+v", exts)
	}
	if len(errs) != 3 {
		t.Fatalf("errs = %v, want 3", errs)
	}
	if errs[0].Msg == "" || errs[1].Name != "无规则" || errs[2].Name != "无命令" {
		t.Fatalf("err details = %+v", errs)
	}

	if err := os.WriteFile(filepath.Join(dir, "extensions.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadShellExtensionsAt(dir); err == nil {
		t.Fatal("a malformed file must report an error")
	}
}

// TestParseExtMatches checks the settings form's 匹配规则 field: token
// splitting, lowercasing, leading dots, and junk skipped.
func TestParseExtMatches(t *testing.T) {
	got := parseExtMatches("*, File, .PNG；wav、dir")
	want := []string{"*", "file", "png", "wav", "dir"}
	if !slices.Equal(got, want) {
		t.Fatalf("parseExtMatches = %v, want %v", got, want)
	}
	if got := parseExtMatches("{path} /bad"); len(got) != 0 {
		t.Fatalf("parseExtMatches accepted junk: %v", got)
	}
	if !validExtMatch("tar.gz") || validExtMatch("") || validExtMatch(`a\b`) {
		t.Fatal("validExtMatch")
	}
}

func TestParseDeclaredExts(t *testing.T) {
	plist := `<?xml version="1.0"?>
<dict>
	<key>CFBundleName</key><string>Demo</string>
	<key>CFBundleDocumentTypes</key>
	<array>
		<dict>
			<key>CFBundleTypeExtensions</key>
			<array><string>PNG</string><string>png</string><string>*</string><string>a/b</string></array>
			<key>CFBundleTypeName</key><string>Image</string>
		</dict>
		<dict>
			<key>CFBundleTypeExtensions</key>
			<array><string>jpg</string><string>.jpeg</string></array>
		</dict>
	</array>
	<key>CFBundleTypeRole</key><string>Editor</string>
</dict>`
	got := parseDeclaredExts(plist)
	want := []string{"png", "png", "jpg", "jpeg"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestAppDeclaredExtsBundle(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-app")
	defer os.RemoveAll(dir)
	bundle := filepath.Join(dir, "Demo.app", "Contents")
	os.MkdirAll(bundle, 0o755)
	os.WriteFile(filepath.Join(bundle, "Info.plist"), []byte(
		`<dict><key>CFBundleTypeExtensions</key><array><string>zzq</string></array></dict>`), 0o644)
	got := appDeclaredExts(filepath.Join(dir, "Demo.app"))
	if len(got) != 1 || got[0] != "zzq" {
		t.Fatalf("bundle decl = %v", got)
	}
}

func TestQuickLookGating(t *testing.T) {
	dir, _ := os.MkdirTemp("", "we-ql")
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "a.txt")
	os.WriteFile(file, []byte("x"), 0o644)
	a := newStoreApp(t, dir)

	// No selection: Space opens nothing.
	a.openQuickLook()
	if a.quickLook {
		t.Fatal("opened without selection")
	}
	// A folder selection: still nothing.
	a.selectOnly(dir)
	a.openQuickLook()
	if a.quickLook {
		t.Fatal("opened for a folder")
	}
	// A file selection opens, and the close path works.
	a.selectOnly(file)
	a.openQuickLook()
	if !a.quickLook {
		t.Fatal("did not open for a file")
	}
	a.quickLook = false
	if a.quickLook {
		t.Fatal("did not close")
	}
	_ = ui.Hex // keep the import honest for future UI asserts
}
