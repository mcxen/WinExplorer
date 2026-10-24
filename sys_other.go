//go:build !darwin && !windows && !linux

package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// sys_other.go keeps the package compiling on the remaining platforms —
// the browser build among them — with everything the OS does not offer
// reporting as unavailable, rather than refusing to build.

func sysOpen(path string) error {
	return fmt.Errorf("此平台不支持打开文件")
}

func sysOpenWith(app, path string) error { return sysOpen(path) }

func sysOpenAs(path string) error { return sysOpen(path) }

func sysReveal(path string) error { return sysOpen(path) }

func sysTerminal(dir string) error { return fmt.Errorf("此平台不支持终端") }

func sysTerminalRun(script string) error { return fmt.Errorf("此平台不支持终端") }

func sysThumbImage(path string, size int) (image.Image, error) {
	return nil, fmt.Errorf("系统缩略图不可用")
}

func sysFileMeta(path string) ([]propRow, bool) { return nil, false }

func sysAudioPlay(path string) (string, []string, time.Duration, bool) {
	return "", nil, 0, false
}

func sysHiddenName(_ string, e os.DirEntry) bool { return strings.HasPrefix(e.Name(), ".") }

func sysVolumes() []Place {
	home, _ := os.UserHomeDir()
	return []Place{{Name: "文件", Path: home, Kind: "drive"}}
}

func sysTrashDir() (string, error) { return "", fmt.Errorf("找不到回收站") }

func sysTrashMove(path string) (string, string, error) {
	return "", "", fmt.Errorf("此平台不支持回收站")
}

func sysTrashList() []Entry { return nil }

func sysTrashKey(trashPath string) string { return filepath.Base(trashPath) }

func sysTrashRestore(src, dst string) error { return os.Rename(src, dst) }

func sysTrashRemove(src string) error { return os.RemoveAll(src) }

func sysTrashEmpty() error { return fmt.Errorf("此平台不支持回收站") }

// sysTrashRecyclable — there is no bin here; the delete flow confirms
// nothing and reports the move's own failure instead.
func sysTrashRecyclable(string) bool { return true }

// sysKnownFolders is nil: the home-relative fallback in fs.go applies.
func sysKnownFolders() map[string]string { return nil }

func sysOpenWithScan() {}

// sysOpenWithMenuTail: no system chooser and no registry here, so the
// shared menu simply ends without a tail.
func sysOpenWithMenuTail(a *App, m *ui.Menu, e Entry) {}

// sysRootName is the root volume's name in the sidebar, the breadcrumb and
// This PC: the generic Unix root.
func sysRootName() string { return "文件系统" }
