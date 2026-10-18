package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

func (a *App) doOpen() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	if len(sel) == 1 && sel[0].IsDir {
		a.navigate(sel[0].Path)
		return
	}
	var failed []string
	for _, e := range sel {
		if e.IsDir {
			continue
		}
		if err := openWithOS(e.Path); err != nil {
			failed = append(failed, e.Name)
			continue
		}
		a.recordRecent(e.Path)
	}
	if len(failed) > 0 {
		a.statusMsg = "无法打开：" + strings.Join(failed, "、")
	}
}

// openWithOS opens a file with the OS default application (platform.go).
func openWithOS(path string) error { return sysOpen(path) }

func (a *App) doCopy() {
	a.storeClip(false)
}

func (a *App) doCut() {
	a.storeClip(true)
}

func (a *App) storeClip(cut bool) {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	paths := make([]string, 0, len(sel))
	for _, e := range sel {
		if e.Path == "" || e.Path == pathThisPC {
			continue
		}
		paths = append(paths, e.Path)
	}
	if len(paths) == 0 {
		return
	}
	a.clipPaths = paths
	a.clipCut = cut
	a.statusMsg = ""
	// Explorer's clipboard is the system's: files copied here paste into
	// other apps too. Cut is an in-app notion — Finder pastes them as a
	// copy, which is how Windows behaves against apps that ignore the
	// preferred effect.
	if data, err := transfer.FileData(paths...); err == nil {
		if err := mygo.Clipboard.Write(data); err != nil {
			a.statusMsg = "写入系统剪贴板失败：" + err.Error()
		}
	}
}

// systemClipboardFiles returns the local file paths on the system
// clipboard, cached briefly so per-frame affordances stay cheap.
func (a *App) systemClipboardFiles() []string {
	if time.Since(a.sysClipAt) < 400*time.Millisecond {
		return a.sysClip
	}
	a.sysClipAt = time.Now()
	a.sysClip = nil
	if !mygo.App.IsReady() {
		return nil
	}
	data, err := mygo.Clipboard.Read()
	if err != nil {
		return nil
	}
	paths, err := data.Files()
	if err != nil {
		return nil
	}
	a.sysClip = paths
	return paths
}

// canPaste reports whether anything can land in the current folder: the
// app's own clipboard, or files another app put on the system's.
func (a *App) canPaste() bool {
	return len(a.clipPaths) > 0 || len(a.systemClipboardFiles()) > 0
}

func (a *App) doPaste() {
	if specialPath(a.cur().path) {
		a.statusMsg = "无法粘贴到此页面"
		return
	}
	srcPaths, cut := a.clipPaths, a.clipCut
	if len(srcPaths) == 0 {
		// Nothing of our own: paste files another application copied,
		// as a copy — Finder and Windows cannot signal a cut.
		srcPaths, cut = a.systemClipboardFiles(), false
		if len(srcPaths) == 0 {
			return
		}
	}
	dstDir := a.cur().path
	if cut {
		for _, src := range srcPaths {
			if src == "" || src == pathThisPC {
				continue
			}
			if pathWithin(dstDir, src) {
				a.statusMsg = "无法将文件夹粘贴到其自身或子文件夹中"
				return
			}
		}
	}
	var failed []string
	var pasted []string
	var moved []string
	var undoCreated []string
	var undoMoved []undoMove
	for _, src := range srcPaths {
		if src == "" || src == pathThisPC {
			continue
		}
		// Cutting into the folder that already holds the item is a no-op.
		// Removing it afterwards would delete the only copy.
		if cut && filepath.Clean(filepath.Dir(src)) == filepath.Clean(dstDir) {
			pasted = append(pasted, src)
			continue
		}
		name, err := pasteOne(src, dstDir)
		if err != nil {
			failed = append(failed, filepath.Base(src))
			continue
		}
		dst := filepath.Join(dstDir, name)
		pasted = append(pasted, dst)
		if cut {
			moved = append(moved, src)
			undoMoved = append(undoMoved, undoMove{From: src, To: dst})
		} else {
			undoCreated = append(undoCreated, dst)
		}
	}
	if cut && len(failed) == 0 && len(moved) > 0 {
		var removeFailed []string
		for _, src := range moved {
			if err := os.RemoveAll(src); err != nil {
				removeFailed = append(removeFailed, filepath.Base(src))
			}
		}
		if len(removeFailed) == 0 {
			// Only the app's own clipboard had the cut; the system's copy
			// stays as it was.
			a.clipPaths = nil
			a.clipCut = false
		} else {
			failed = append(failed, removeFailed...)
		}
	}
	if n := len(undoMoved) + len(undoCreated); n > 0 {
		desc := "复制"
		if cut {
			desc = "移动"
		}
		if n > 1 {
			desc = fmt.Sprintf("%s %d 个项目", desc, n)
		}
		a.pushUndo(undoOp{desc: desc, created: undoCreated, moved: undoMoved})
	}
	a.reload()
	if len(pasted) > 0 {
		a.cur().selected = map[string]bool{}
		for _, p := range pasted {
			a.cur().selected[p] = true
		}
		a.cur().anchor = pasted[0]
	}
	if len(failed) > 0 {
		a.statusMsg = "粘贴失败：" + strings.Join(failed, "、")
	}
}

// pathWithin reports whether path is dir or a descendant of dir.
func pathWithin(path, dir string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pasteOne(src, dstDir string) (string, error) {
	info, err := os.Lstat(src)
	if err != nil {
		return "", err
	}
	name, err := uniqueCopyName(dstDir, filepath.Base(src), info.IsDir())
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, name)
	if info.IsDir() {
		if err := copyDir(src, dst); err != nil {
			return "", err
		}
		return name, nil
	}
	if err := copyFile(src, dst, info); err != nil {
		return "", err
	}
	return name, nil
}

// uniqueCopyName is Windows-style: "名称 - 副本", then "名称 - 副本 (2)",
// with the suffix before the extension for files.
func uniqueCopyName(dir, name string, isDir bool) (string, error) {
	candidate := copyName(name, isDir, 1)
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = copyName(name, isDir, n)
	}
	return "", fmt.Errorf("无法生成不冲突的名称")
}

func copyName(name string, isDir bool, n int) string {
	suffix := " - 副本"
	if n > 1 {
		suffix = fmt.Sprintf(" - 副本 (%d)", n)
	}
	if isDir {
		return name + suffix
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		return name + suffix
	}
	return stem + suffix + ext
}

func copyFile(src, dst string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return closeErr
	}
	_ = os.Chtimes(dst, info.ModTime(), info.ModTime())
	return nil
}

func copyDir(src, dst string) error {
	srcClean := filepath.Clean(src)
	seen := map[string]struct{}{srcClean: {}}
	return copyDirRec(srcClean, dst, seen)
}

func copyDirRec(src, dst string, seen map[string]struct{}) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		fi, err := os.Lstat(srcPath)
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := copyFile(srcPath, dstPath, fi); err != nil {
				return err
			}
			continue
		}
		if fi.IsDir() {
			real, err := filepath.EvalSymlinks(srcPath)
			if err == nil {
				real = filepath.Clean(real)
				if _, ok := seen[real]; ok {
					continue
				}
				seen[real] = struct{}{}
			}
			if err := copyDirRec(srcPath, dstPath, seen); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath, fi); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) doDelete() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	var failed []string
	var moved []undoMove
	var desc string
	for _, e := range sel {
		if e.Path == "" || e.Path == pathThisPC || e.Path == "/" {
			failed = append(failed, e.Name)
			continue
		}
		dst, key, err := moveToTrash(e.Path)
		if err != nil {
			failed = append(failed, e.Name)
			continue
		}
		if key != "" {
			a.store.logTrash(e.Path, key)
		}
		if dst != "" {
			moved = append(moved, undoMove{From: e.Path, To: dst})
		}
	}
	if n := len(moved); n > 0 {
		desc = "删除"
		if n > 1 {
			desc = fmt.Sprintf("删除 %d 个项目", n)
		}
		a.pushUndo(undoOp{desc: desc, moved: moved})
	}
	a.reload()
	a.clearSelection()
	a.cur().renaming = ""
	a.cur().renameBuf = ""
	if len(failed) > 0 {
		a.statusMsg = "无法删除：" + strings.Join(failed, "、")
	}
}

// trashDir is where this OS's trash lives (platform.go).
func trashDir() (string, error) { return sysTrashDir() }

// moveToTrash sends path to this OS's trash, returning where it landed
// plus the key the restore log files it under (platform.go).
func moveToTrash(path string) (string, string, error) { return sysTrashMove(path) }

func uniqueTrashName(dir, name string) (string, error) {
	candidate := name
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem = name
		ext = ""
	}
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s %d%s", stem, n, ext)
	}
	return "", fmt.Errorf("废纸篓中名称冲突")
}

func (a *App) commitRename() {
	if a.cur().renaming == "" {
		return
	}
	oldName := a.cur().renaming
	newName := strings.TrimSpace(a.cur().renameBuf)
	a.cur().renaming = ""
	a.cur().renameBuf = ""
	if newName == "" || newName == oldName {
		return
	}
	if newName == "." || newName == ".." || strings.ContainsAny(newName, `/\`) {
		a.statusMsg = "名称无效"
		return
	}
	if specialPath(a.cur().path) {
		a.statusMsg = "无法重命名此页面中的项目"
		return
	}
	oldPath := filepath.Join(a.cur().path, oldName)
	newPath := filepath.Join(a.cur().path, newName)
	if err := os.Rename(oldPath, newPath); err != nil {
		a.statusMsg = "重命名失败：" + err.Error()
		a.reload()
		return
	}
	a.pushUndo(undoOp{desc: "重命名", moved: []undoMove{{From: oldPath, To: newPath}}})
	a.reload()
	a.selectOnly(newPath)
}

func (a *App) cancelRename() {
	a.cur().renaming = ""
	a.cur().renameBuf = ""
}

func (a *App) doNewFolder() {
	if specialPath(a.cur().path) {
		a.statusMsg = "无法在此页面中新建文件夹"
		return
	}
	name, err := uniqueNewFolderName(a.cur().path)
	if err != nil {
		a.statusMsg = "新建文件夹失败：" + err.Error()
		return
	}
	dir := filepath.Join(a.cur().path, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		a.statusMsg = "新建文件夹失败：" + err.Error()
		return
	}
	a.pushUndo(undoOp{desc: "新建文件夹", created: []string{dir}})
	a.reload()
	a.selectOnly(dir)
	a.cur().renaming = name
	a.cur().renameBuf = name
}

func uniqueNewFolderName(dir string) (string, error) {
	candidate := "新建文件夹"
	for n := 2; n < 10000; n++ {
		_, err := os.Lstat(filepath.Join(dir, candidate))
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("新建文件夹 (%d)", n)
	}
	return "", fmt.Errorf("无法生成不冲突的名称")
}

func (a *App) doRefresh() {
	clearThumbs()
	// 刷新 asks for the world as it is now: every folder the tree has read is
	// forgotten, not just the one being shown.
	a.navChildrenClear()
	a.reload()
}

// doCopyAsPath 把选中项目的绝对路径写入系统剪贴板，多选每行一个，
// 与资源管理器的“复制为路径”（Ctrl+Shift+C）一致。
func (a *App) doCopyAsPath() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	paths := make([]string, 0, len(sel))
	for _, e := range sel {
		if e.Path == "" || e.Path == pathThisPC {
			continue
		}
		paths = append(paths, e.Path)
	}
	if len(paths) == 0 {
		return
	}
	mygo.Clipboard.WriteText(strings.Join(paths, "\n"))
	a.statusMsg = "已复制路径"
}

// doNewTextFile 新建一个空的文本文档，选中并进入内联重命名，对应
// 资源管理器“新建 > 文本文档”。
func (a *App) doNewTextFile() {
	if specialPath(a.cur().path) {
		a.statusMsg = "无法在此页面中新建文件"
		return
	}
	name, err := uniqueFreeName(a.cur().path, "新建文本文档.txt", false)
	if err != nil {
		a.statusMsg = "新建文件失败：" + err.Error()
		return
	}
	path := filepath.Join(a.cur().path, name)
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		a.statusMsg = "新建文件失败：" + err.Error()
		return
	}
	a.pushUndo(undoOp{desc: "新建文本文档", created: []string{path}})
	a.reload()
	a.selectOnly(path)
	a.cur().renaming = name
	a.cur().renameBuf = name
}

// openInTerminal opens a terminal window at dir (platform.go).
func (a *App) openInTerminal(dir string) {
	if dir == "" || specialPath(dir) {
		return
	}
	if err := sysTerminal(dir); err != nil {
		a.statusMsg = "无法打开终端：" + err.Error()
	}
}
