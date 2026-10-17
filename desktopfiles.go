package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// desktopfiles.go reads Linux's .desktop application entries — the
// MimeType patterns that name what an application opens, and the Exec
// line that runs it. Pure parsing, so it builds and tests everywhere.

// desktopMimeExts reads a .desktop file's MimeType line and returns the
// extensions its patterns name.
func desktopMimeExts(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MimeType=") {
			continue
		}
		for _, mime := range strings.Split(strings.TrimPrefix(line, "MimeType="), ";") {
			if !strings.HasPrefix(mime, "*.") {
				continue
			}
			ext := strings.ToLower(strings.TrimPrefix(mime, "*."))
			if ext != "" {
				out = append(out, ext)
			}
		}
	}
	return out
}

// desktopExec builds the command a .desktop file says to run, with the
// file taking the place of its field codes.
func desktopExec(app, path string) (*exec.Cmd, bool) {
	return desktopExecIn([]string{"/usr/share/applications", "/usr/local/share/applications"}, app, path)
}

// desktopExecIn is desktopExec over explicit application folders, for
// callers on other layouts and for tests.
func desktopExecIn(dirs []string, app, path string) (*exec.Cmd, bool) {
	for _, dir := range dirs {
		f, err := os.ReadFile(filepath.Join(dir, app+".desktop"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(f), "\n") {
			if !strings.HasPrefix(line, "Exec=") {
				continue
			}
			fields := strings.Fields(strings.TrimPrefix(line, "Exec="))
			if len(fields) == 0 {
				continue
			}
			args := make([]string, 0, len(fields))
			for _, fl := range fields[1:] {
				switch fl {
				case "%f", "%F", "%u", "%U":
					args = append(args, path)
				case "%%":
					args = append(args, "%")
				default:
					args = append(args, fl)
				}
			}
			return exec.Command(fields[0], args...), true
		}
	}
	return nil, false
}
