package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
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

// shellExt is one registered context-menu command.
type shellExt struct {
	Name     string   `json:"name"`
	Match    []string `json:"match"`
	Command  string   `json:"command"`
	Terminal bool     `json:"terminal"`
}

const (
	extMatchAll  = "*"
	extMatchFile = "file"
	extMatchDir  = "dir"
)

// loadShellExtensions reads the registry; a missing file is empty, a
// malformed one is reported as an error the caller can show.
func loadShellExtensions() ([]shellExt, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return loadShellExtensionsAt(filepath.Join(dir, "winexplorer"))
}

func loadShellExtensionsAt(dir string) ([]shellExt, error) {
	data, err := os.ReadFile(filepath.Join(dir, "extensions.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var exts []shellExt
	if err := json.Unmarshal(data, &exts); err != nil {
		return nil, err
	}
	out := exts[:0]
	for _, x := range exts {
		x.Name = strings.TrimSpace(x.Name)
		x.Match = compactPaths(x.Match)
		if x.Name == "" || len(x.Match) == 0 || strings.TrimSpace(x.Command) == "" {
			continue
		}
		for i := range x.Match {
			x.Match[i] = strings.ToLower(strings.TrimPrefix(x.Match[i], "."))
		}
		out = append(out, x)
	}
	return out, nil
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
	cmd := x.expand(e)
	if cmd == "" {
		return nil
	}
	if x.Terminal {
		return sysTerminalRun(cmd)
	}
	c := exec.Command("sh", "-c", cmd)
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

// extMenuItems fills the menu with the extensions that match any selected
// entry, Explorer's extension-verb block, in registration order.
func (a *App) extMenuItems(m *ui.Menu) {
	sel := a.selectedEntries()
	for _, x := range a.shellExts {
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
