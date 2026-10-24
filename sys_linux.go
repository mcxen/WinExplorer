//go:build linux

package main

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"sunvale/internal/fsops"

	"github.com/egoist/mygo/ui"
)

// sys_linux.go is the desktop Linux platform: xdg-open, the common
// terminals, the freedesktop Trash spec, /media volumes and the
// .desktop files of the 打开方式 registry.

func sysOpen(path string) error {
	return exec.Command("xdg-open", path).Start()
}

// sysOpenWith launches the .desktop application's command with the file.
func sysOpenWith(app, path string) error {
	cmd, ok := desktopExec(app, path)
	if !ok {
		return fmt.Errorf("找不到应用 %s", app)
	}
	return cmd.Start()
}

// sysOpenAs has no Linux counterpart: the registry lists applications.
func sysOpenAs(path string) error { return sysOpen(path) }

func sysReveal(path string) error {
	// Open the folder and let the file manager select; without a
	// selection verb, showing the parent is the useful part.
	if parent := filepath.Dir(path); parent != "" {
		return exec.Command("xdg-open", parent).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// linuxTerminals are the terminals that take a working directory, most
// specific first; whichever is installed runs.
var linuxTerminals = []struct {
	name string
	flag string
}{
	{"gnome-terminal", "--working-directory"},
	{"konsole", "--workdir"},
	{"xfce4-terminal", "--working-directory"},
	{"kitty", "--directory"},
	{"foot", "--working-directory"},
	{"x-terminal-emulator", "--working-directory"},
}

func sysTerminal(dir string) error {
	for _, t := range linuxTerminals {
		if _, err := exec.LookPath(t.name); err != nil {
			continue
		}
		return exec.Command(t.name, t.flag+"="+dir).Start()
	}
	return fmt.Errorf("找不到终端程序")
}

// sysTerminalRun runs script in a new terminal window, with sh.
func sysTerminalRun(script string) error {
	for _, t := range linuxTerminals {
		if _, err := exec.LookPath(t.name); err != nil {
			continue
		}
		return exec.Command(t.name, "-e", "sh", "-c", script).Start()
	}
	return fmt.Errorf("找不到终端程序")
}

// sysThumbImage: no standard thumbnail CLI; the stdlib formats cover the
// common pictures.
func sysThumbImage(path string, size int) (image.Image, error) {
	return nil, fmt.Errorf("系统缩略图不可用")
}

// sysFileMeta has no mdls equivalent; ffprobe covers media metadata.
func sysFileMeta(path string) ([]propRow, bool) { return nil, false }

// sysAudioPlay: ffplay, when the user has ffmpeg installed, plays the
// formats MyGo's decoders do not cover.
func sysAudioPlay(path string) (name string, args []string, dur time.Duration, ok bool) {
	if _, err := exec.LookPath("ffplay"); err != nil {
		return "", nil, 0, false
	}
	if d := videoDuration(path); d > 0 {
		return "ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path},
			time.Duration(d * float64(time.Second)), true
	}
	return "", nil, 0, false
}

func sysHiddenName(_ string, e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }

// sysVolumes lists the fixed root and the mounted media.
func sysVolumes() []Place {
	places := []Place{
		{Name: "文件系统", Path: "/", Kind: "drive"},
	}
	for _, dir := range []string{"/media", "/run/media"} {
		vols, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, v := range vols {
			// /media/$USER/<volume> on systemd machines: one level deep
			// under the user's folder, and flat on the older layout.
			path := filepath.Join(dir, v.Name())
			if v.IsDir() {
				if user := os.Getenv("USER"); user != "" && v.Name() == user {
					inner, err := os.ReadDir(path)
					if err == nil {
						for _, iv := range inner {
							if iv.IsDir() {
								places = append(places, Place{Name: iv.Name(), Path: filepath.Join(path, iv.Name()), Kind: "drive"})
							}
						}
						continue
					}
				}
				places = append(places, Place{Name: v.Name(), Path: path, Kind: "drive"})
			}
		}
	}
	return places
}

// — The freedesktop Trash: files in ~/.local/share/Trash, an info file
// beside each item carrying its original path, so other desktops see the
// same bin we do.

func trashHome() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("找不到用户目录")
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "Trash"), nil
}

func sysTrashDir() (string, error) {
	root, err := trashHome()
	if err != nil {
		return "", err
	}
	files := filepath.Join(root, "files")
	if err := os.MkdirAll(files, 0o700); err != nil {
		return "", err
	}
	_ = os.MkdirAll(filepath.Join(root, "info"), 0o700)
	return files, nil
}

// sysTrashMove renames path into the trash, records the original in the
// item's .trashinfo, and returns where it landed plus the log key.
func sysTrashMove(path string) (string, string, error) {
	trash, err := sysTrashDir()
	if err != nil {
		return "", "", err
	}
	base := filepath.Base(path)
	if base == "" || base == "." || base == "/" {
		return "", "", fmt.Errorf("无效的路径")
	}
	name, err := fsops.UniqueTrashName(trash, base)
	if err != nil {
		return "", "", err
	}
	dst := filepath.Join(trash, name)
	if err := os.Rename(path, dst); err != nil {
		return "", "", err
	}
	root := filepath.Dir(trash)
	info := filepath.Join(root, "info", name+".trashinfo")
	_ = os.WriteFile(info, []byte(fmt.Sprintf(
		"[Trash Info]\nPath=%s\nDeletionDate=%s\n",
		fsops.TrashinfoEscape(path), time.Now().Format(time.RFC3339))), 0o600)
	return dst, name, nil
}

func sysTrashList() []Entry {
	trash, err := sysTrashDir()
	if err != nil {
		return nil
	}
	dir, err := os.ReadDir(trash)
	if err != nil {
		return nil
	}
	entries := make([]Entry, 0, len(dir))
	for _, d := range dir {
		path := filepath.Join(trash, d.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name:    d.Name(),
			Path:    path,
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	return entries
}

// sysTrashKey identifies the item in the restore log: its trash name.
func sysTrashKey(trashPath string) string { return filepath.Base(trashPath) }

func sysTrashRestore(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(src))
	_ = os.Remove(filepath.Join(root, "info", filepath.Base(src)+".trashinfo"))
	return nil
}

func sysTrashRemove(src string) error {
	if err := os.RemoveAll(src); err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(src))
	_ = os.Remove(filepath.Join(root, "info", filepath.Base(src)+".trashinfo"))
	return nil
}

func sysTrashEmpty() error {
	trash, err := sysTrashDir()
	if err != nil {
		return err
	}
	dir, err := os.ReadDir(trash)
	if err != nil {
		return err
	}
	root := filepath.Dir(trash)
	for _, d := range dir {
		_ = os.RemoveAll(filepath.Join(trash, d.Name()))
		_ = os.Remove(filepath.Join(root, "info", d.Name()+".trashinfo"))
	}
	return nil
}

// sysTrashRecyclable — the local trash always takes the delete here.
func sysTrashRecyclable(string) bool { return true }

// sysKnownFolders is nil: XDG folders are not redirected on Linux, so the
// home-relative fallback in fs.go is the truth.
func sysKnownFolders() map[string]string { return nil }

// The 打开方式 registry reads the .desktop files of the installed
// applications: their MimeType lists name what they open.

func sysOpenWithScan() { scanOpenWithAppsLinux() }

func sysOpenWithMenu(a *App, m *ui.Menu, e Entry) {
	shown := 0
	for _, app := range openWithApps(e.Ext) {
		if shown >= 8 {
			break
		}
		if m.Item(app).Chosen() {
			if err := sysOpenWith(app, e.Path); err != nil {
				a.statusMsg = "无法用 " + app + " 打开：" + err.Error()
			}
		}
		shown++
	}
	if shown > 0 {
		m.Separator()
	}
	if m.Item("在应用文件夹中查看…").Chosen() {
		a.navigate("/usr/share/applications")
	}
}

// scanOpenWithAppsLinux fills the 打开方式 registry from the installed
// applications' .desktop files: the MimeType lines name the patterns each
// one opens, *.png and the like. One pass, published once.
func scanOpenWithAppsLinux() {
	r := &openWithRegistry{byExt: map[string][]string{}}
	for _, dir := range []string{"/usr/share/applications", "/usr/local/share/applications"} {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".desktop") {
				continue
			}
			name := strings.TrimSuffix(f.Name(), ".desktop")
			if strings.HasPrefix(name, "org.gnome.") || strings.HasPrefix(name, "gnome-") {
				// GNOME's own tools are noise in a file menu.
				continue
			}
			for _, ext := range desktopMimeExts(filepath.Join(dir, f.Name())) {
				r.byExt[ext] = append(r.byExt[ext], name)
			}
		}
	}
	for ext, apps := range r.byExt {
		sort.Strings(apps)
		r.byExt[ext] = slices.Compact(apps)
	}
	openWith.Store(r)
}

// sysRootName is the root volume's name in the sidebar, the breadcrumb and
// This PC: what the Linux file managers call the root; Macintosh HD would be a stranger here.
func sysRootName() string { return "文件系统" }
