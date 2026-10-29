package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// extensions.go is WinExplorer's shell extension protocol, Explorer's
// registry verb equivalent: commands the user registers in
// os.UserConfigDir()/winexplorer/extensions.json appear in the item
// context menu for the files they match, run with the file's details.
//
//	[
//	  {
//	    "name": "用 VSCode 打开",
//	    "match": ["*"],
//	    "command": "code \"{path}\""
//	  },
//	  {
//	    "name": "转为 MP3",
//	    "match": ["wav", "flac", "m4a"],
//	    "command": "ffmpeg -i \"{path}\" \"{stem}.mp3\"",
//	    "terminal": true
//	  },
//	  {
//	    "name": "在此打开终端",
//	    "match": ["dir"],
//	    "command": "cd \"{path}\" && clear"
//	  }
//	]

// shellExt is one registered context-menu command. Disabled keeps it in
// the registry but out of the menus; written only when true, so
// extensions.json files from before the field existed read as enabled.
type shellExt struct {
	Name     string   `json:"name"`
	Match    []string `json:"match"`
	Command  string   `json:"command"`
	Terminal bool     `json:"terminal"`
	Disabled bool     `json:"disabled,omitempty"`
}

// shellExtLoadError is one extensions.json entry the registry could not
// keep — a missing name, no match rules, no command. The settings dialog's
// 扩展 section lists them so a broken entry is visible beyond the boot
// status line's flash.
type shellExtLoadError struct {
	Name string // the entry's name, best effort
	Msg  string
}

const (
	extMatchAll  = "*"
	extMatchFile = "file"
	extMatchDir  = "dir"
)

// loadShellExtensions reads the registry; a missing file is empty, a
// malformed one is reported as an error the caller can show alongside the
// per-entry problems, which ride back in the second return.
func loadShellExtensions() ([]shellExt, []shellExtLoadError, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, err
	}
	return loadShellExtensionsAt(filepath.Join(dir, "winexplorer"))
}

func loadShellExtensionsAt(dir string) ([]shellExt, []shellExtLoadError, error) {
	data, err := os.ReadFile(filepath.Join(dir, "extensions.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var exts []shellExt
	if err := json.Unmarshal(data, &exts); err != nil {
		return nil, nil, err
	}
	var errs []shellExtLoadError
	out := exts[:0]
	for i, x := range exts {
		x.Name = strings.TrimSpace(x.Name)
		x.Match = compactPaths(x.Match)
		switch {
		case x.Name == "":
			errs = append(errs, shellExtLoadError{Msg: entryDesc(i, x) + "名称缺失"})
		case len(x.Match) == 0:
			errs = append(errs, shellExtLoadError{Name: x.Name, Msg: "匹配规则缺失"})
		case strings.TrimSpace(x.Command) == "":
			errs = append(errs, shellExtLoadError{Name: x.Name, Msg: "命令缺失"})
		}
		if x.Name == "" || len(x.Match) == 0 || strings.TrimSpace(x.Command) == "" {
			continue
		}
		for i := range x.Match {
			x.Match[i] = strings.ToLower(strings.TrimPrefix(x.Match[i], "."))
		}
		out = append(out, x)
	}
	return out, errs, nil
}

// entryDesc names a malformed entry for the error list: its name when it
// has one, its position otherwise.
func entryDesc(i int, x shellExt) string {
	if x.Name != "" {
		return "扩展 “" + x.Name + "”："
	}
	return fmt.Sprintf("第 %d 个扩展：", i+1)
}

// saveShellExtensions writes the registry back, through a temporary file
// and a rename, so a crash mid-save cannot leave a half-written JSON.
func saveShellExtensions(exts []shellExt) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	return saveShellExtensionsAt(filepath.Join(dir, "winexplorer"), exts)
}

func saveShellExtensionsAt(dir string, exts []shellExt) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(exts, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "extensions.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "extensions.json"))
}

// validExtMatch reports whether one 匹配规则 token is usable: the three
// keywords or anything that can be an extension. The bar stays low —
// matching itself decides what applies.
func validExtMatch(m string) bool {
	switch m {
	case extMatchAll, extMatchFile, extMatchDir:
		return true
	}
	return m != "" && !strings.ContainsAny(m, `/\{}`)
}

// parseExtMatches normalizes the settings form's 匹配规则 field:
// comma-, space- or 、-separated tokens, lowercased, "." prefixes dropped,
// invalid ones skipped.
func parseExtMatches(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '，' || r == '；' || r == ';' || r == '、'
	})
	var out []string
	for _, f := range fields {
		m := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(f), "."))
		if !validExtMatch(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// matches reports whether the command applies to the entry: "*" every
// item, "file" files, "dir" folders, otherwise one of the extensions.
func (x shellExt) matches(e Entry) bool {
	for _, m := range x.Match {
		switch m {
		case extMatchAll:
			return true
		case extMatchFile:
			if !e.IsDir {
				return true
			}
		case extMatchDir:
			if e.IsDir {
				return true
			}
		default:
			if !e.IsDir && strings.EqualFold(e.Ext, m) {
				return true
			}
		}
	}
	return false
}

// expand fills the command's placeholders with the entry's details.
func (x shellExt) expand(e Entry) string {
	name := e.Name
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	cmd := x.Command
	for _, pair := range [][2]string{
		{"{path}", e.Path},
		{"{dir}", filepath.Dir(e.Path)},
		{"{name}", name},
		{"{stem}", stem},
		{"{ext}", e.Ext},
	} {
		cmd = strings.ReplaceAll(cmd, pair[0], pair[1])
	}
	return cmd
}

// run executes the command for the entry. terminal:true runs it in a new
// terminal window, as Explorer's shell verbs can launch a console;
// otherwise it runs through sh detached, so a slow tool never blocks the
// UI (the goroutine reaps it too).
func (x shellExt) run(e Entry) error {
	return x.runWith(exec.Command, e)
}

// runWith is run with a caller-chosen command constructor, so tests can
// observe the detached process without depending on a shell's timing.
func (x shellExt) runWith(command func(string, ...string) *exec.Cmd, e Entry) error {
	cmd := x.expand(e)
	if cmd == "" {
		return nil
	}
	if x.Terminal {
		return sysTerminalRun(cmd)
	}
	c := command("sh", "-c", cmd)
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

// runForSelection runs the command for every selected entry it matches,
// reporting how many ran. Started detached, so a slow tool does not block
// the UI; its own terminal or output is the user's to watch.
func (a *App) runShellExt(x shellExt) {
	sel := a.selectedEntries()
	n := 0
	var failed []string
	for _, e := range sel {
		if !x.matches(e) {
			continue
		}
		if err := x.run(e); err != nil {
			failed = append(failed, e.Name)
			continue
		}
		n++
	}
	if n > 0 {
		a.statusMsg = "已运行扩展：" + x.Name
	}
	if len(failed) > 0 {
		a.statusMsg = "扩展运行失败：" + x.Name + "（" + strings.Join(failed, "、") + "）"
	}
}

// extMenuItems fills the menu with the enabled extensions that match any
// selected entry, Explorer's extension-verb block, in registration order.
func (a *App) extMenuItems(m *xMenu) {
	sel := a.selectedEntries()
	for _, x := range a.shellExts {
		if x.Disabled {
			continue
		}
		any := false
		for _, e := range sel {
			if x.matches(e) {
				any = true
				break
			}
		}
		if !any {
			continue
		}
		if m.Item(x.Name).Chosen() {
			a.runShellExt(x)
		}
	}
}

// saveShellExts persists the registry after the settings dialog edits it,
// then refreshes every open window's copy from the file, so a change made
// in one window shows in the others' context menus at once.
func (a *App) saveShellExts() {
	if err := saveShellExtensions(a.shellExts); err != nil {
		a.statusMsg = "扩展保存失败：" + err.Error()
		return
	}
	exts, errs, err := loadShellExtensions()
	if err != nil {
		return
	}
	appsMu.Lock()
	all := slices.Clone(apps)
	appsMu.Unlock()
	for _, app := range all {
		app.shellExts = exts
		app.shellExtErrs = errs
	}
}
