//go:build darwin

package main

import (
	"context"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sunvale/internal/fsops"

	"github.com/egoist/mygo/ui"
)

// sys_darwin.go is macOS: open / Quick Look (qlmanage) / mdls / afplay /
// Terminal.app / ~/.Trash / /Volumes and the /Applications registry.

func sysOpen(path string) error {
	return exec.Command("open", path).Run()
}

func sysOpenWith(app, path string) error {
	return exec.Command("open", "-a", app, path).Run()
}

// sysOpenAs has no macOS counterpart: the registry lists apps already.
func sysOpenAs(path string) error {
	return sysOpen(path)
}

func sysReveal(path string) error {
	return exec.Command("open", "-R", path).Run()
}

func sysTerminal(dir string) error {
	return exec.Command("open", "-a", "Terminal", dir).Run()
}

// sysTerminalRun runs script in a new Terminal window.
func sysTerminalRun(script string) error {
	script = strings.ReplaceAll(script, `\`, `\\`)
	script = strings.ReplaceAll(script, `"`, `\"`)
	return exec.Command("osascript", "-e",
		`tell application "Terminal" to do script "`+script+`"`).Run()
}

// sysThumbImage renders a thumbnail with Quick Look, which understands
// HEIC, TIFF, SVG, PDF pages and movie frames alike.
func sysThumbImage(path string, size int) (image.Image, error) {
	dir, err := os.MkdirTemp("", "we-ql")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), sysCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qlmanage", "-t", "-s", fmt.Sprint(size), "-o", dir, path)
	cmd.WaitDelay = time.Second
	if b, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("%s: %v", string(b), e)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(matches) == 0 {
		return nil, fmt.Errorf("没有生成缩略图")
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// sysFileMeta adds the Spotlight attributes mdls knows.
func sysFileMeta(path string) ([]propRow, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), sysCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "mdls", "-name", "kMDItemPixelWidth",
		"-name", "kMDItemPixelHeight", "-name", "kMDItemNumberOfPages",
		"-name", "kMDItemFSOwnerName", path).Output()
	if err != nil {
		return nil, false
	}
	vals := parseMdls(string(out))
	var rows []propRow
	if w, ok := vals["kMDItemPixelWidth"]; ok && w != "(null)" {
		if h := vals["kMDItemPixelHeight"]; h != "" && h != "(null)" {
			rows = append(rows, propRow{Key: "尺寸", Val: w + " × " + h + " 像素"})
		}
	}
	if p, ok := vals["kMDItemNumberOfPages"]; ok && p != "(null)" {
		rows = append(rows, propRow{Key: "页数", Val: p})
	}
	return rows, len(rows) > 0
}

// sysAudioPlay plays the formats MyGo's decoders do not cover through
// afplay, with afinfo for the duration.
func sysAudioPlay(path string) (name string, args []string, dur time.Duration, ok bool) {
	if _, err := exec.LookPath("afplay"); err != nil {
		return "", nil, 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), sysCmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "afinfo", path).Output()
	if err != nil {
		return "", nil, 0, false
	}
	d, ok := parseAfinfoDuration(string(out))
	if !ok {
		return "", nil, 0, false
	}
	return "afplay", []string{path}, d, true
}

func sysHiddenName(_ string, e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }

// sysVolumes lists the root volume and the mounted ones.
func sysVolumes() []Place {
	root := "/"
	places := []Place{{Name: sysRootName(), Path: root, Kind: "drive"}}
	vols, err := os.ReadDir("/Volumes")
	if err != nil {
		return places
	}
	rootDev := deviceID(root)
	for _, v := range vols {
		path := filepath.Join("/Volumes", v.Name())
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if same, ok := sameDevice(path, rootDev); ok && same {
			continue
		}
		places = append(places, Place{Name: v.Name(), Path: path, Kind: "drive"})
	}
	return places
}

// The trash is ~/.Trash, moved by rename; the restore log (statestore.go)
// maps items back to their original paths.

func sysTrashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("找不到用户目录")
	}
	dir := filepath.Join(home, ".Trash")
	// A fresh account has no ~/.Trash; the Finder creates it on first
	// use, and so does this.
	if _, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("找不到废纸篓")
		}
	}
	return dir, nil
}

// sysTrashMove renames path into the trash, uniquified, and returns where
// it landed plus the key the restore log files it under.
func sysTrashMove(path string) (string, string, error) {
	trash, err := sysTrashDir()
	if err != nil {
		return "", "", err
	}
	base := filepath.Base(path)
	if base == "" || base == "." || base == string(filepath.Separator) {
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

func sysTrashRestore(src, dst string) error { return os.Rename(src, dst) }

func sysTrashRemove(src string) error { return os.RemoveAll(src) }

func sysTrashEmpty() error {
	trash, err := sysTrashDir()
	if err != nil {
		return err
	}
	dir, err := os.ReadDir(trash)
	if err != nil {
		return err
	}
	for _, d := range dir {
		if d.Name() == ".DS_Store" {
			continue
		}
		_ = os.RemoveAll(filepath.Join(trash, d.Name()))
	}
	return nil
}

// sysTrashRecyclable — the local trash always takes the delete here.
func sysTrashRecyclable(string) bool { return true }

// sysKnownFolders is nil: macOS keeps the known folders at their
// home-relative names, which is the fallback in fs.go.
func sysKnownFolders() map[string]string { return nil }

// The 打开方式 registry scans the Applications folders with plutil.

func sysOpenWithScan() { scanOpenWithAppsMac() }

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
	if m.Item("在 /Applications 中查看…").Chosen() {
		a.navigate("/Applications")
	}
}

// sysRootName is the root volume's name in the sidebar, the breadcrumb and
// This PC: Finder's name for the startup volume.
func sysRootName() string { return "Macintosh HD" }

// sysCmdTimeout bounds blocking helper commands (qlmanage, mdls, afinfo)
// so a stuck Quick Look generator cannot hang previews or tests.
const sysCmdTimeout = 8 * time.Second
