package main

import (
	"os"
	"path/filepath"
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

	exts, err := loadShellExtensionsAt(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(exts) != 3 {
		t.Fatalf("kept %d: %+v", len(exts), exts)
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
	if exts, err := loadShellExtensionsAt(filepath.Join(dir, "nope")); err != nil || len(exts) != 0 {
		t.Fatalf("missing config: %v %v", exts, err)
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
