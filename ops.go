package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sunvale/internal/fsops"
	"sunvale/internal/rename"
	"sunvale/internal/tasks"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

func (a *App) doOpen() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	if len(sel) == 1 && sel[0].IsDir {
		a.openDirEntry(sel[0])
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

// openDirEntry navigates into a folder, or on macOS launches an .app the
// way Finder's double-click does. Other package directories still open as
// folders so 显示包内容 stays reachable without a menu.
func (a *App) openDirEntry(e Entry) {
	if !e.IsDir {
		return
	}
	if shouldLaunchAppBundle(e) {
		if err := openWithOS(e.Path); err != nil {
			a.statusMsg = "无法打开：" + e.Name
			return
		}
		a.recordRecent(e.Path)
		return
	}
	a.navigate(e.Path)
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

// readSystemClipboardFiles 读取系统剪贴板上的本地文件路径。X11 上这是一次
// 与剪贴板持有者的往返（GTK 在其中跑嵌套主循环，持有者不响应时要等到超时），
// 所以只能在用户动作或窗口获得焦点时调用，绝不能在逐帧构建的视图里调用。
var readSystemClipboardFiles = func() ([]string, error) {
	if !mygo.App.IsReady() {
		return nil, nil
	}
	// Only the file list: no text, no image bytes from the owner.
	d, err := mygo.Clipboard.Read(transfer.FileList)
	if err != nil {
		return nil, err
	}
	return d.Files()
}

// systemClipboardFiles 返回上次刷新时系统剪贴板上的文件路径，只读缓存。
// 视图（命令栏、右键菜单的「粘贴」）每帧都会问它，v0.3.5 之前它每 400ms
// 同步读一次整个剪贴板，在 X11 上把主循环卡住数秒：窗口不重绘、输入被吞、
// CPU 跑满。刷新改由 refreshSystemClipboard 在窗口获得焦点时做。
func (a *App) systemClipboardFiles() []string { return a.sysClip }

// refreshSystemClipboardAsync 在 goroutine 里发起读取，结果回到主线程写入缓存并
// 重绘；窗口获得焦点时用它，焦点回调本身立即返回。同一时刻只有一次在途。
func (a *App) refreshSystemClipboardAsync() {
	if !a.sysClipBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.sysClipBusy.Store(false)
		paths, err := readSystemClipboardFiles()
		if err != nil {
			paths = nil
		}
		a.postAssist(func() { a.sysClip = paths })
	}()
}

// refreshSystemClipboard 重新读取系统剪贴板，在窗口获得焦点（用户可能刚在
// 别的程序里复制了文件）和真正粘贴时调用。
func (a *App) refreshSystemClipboard() []string {
	paths, err := readSystemClipboardFiles()
	if err != nil {
		paths = nil
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
		srcPaths, cut = a.refreshSystemClipboard(), false
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
			if fsops.PathWithin(dstDir, src) {
				a.statusMsg = "无法将文件夹粘贴到其自身或子文件夹中"
				return
			}
		}
	}
	var srcs []string
	for _, src := range srcPaths {
		if src != "" && src != pathThisPC {
			srcs = append(srcs, src)
		}
	}
	if len(srcs) == 0 {
		return
	}
	kind := tasks.KindCopy
	if cut {
		kind = tasks.KindMove
	}
	// A background task: the list stays live, a name clash is asked in
	// the task card, and the result lands when the run ends.
	a.startFileJob(fileJob{Kind: kind, Source: tasks.SourceUser, Srcs: srcs, DstDir: dstDir}, func(res fileJobResult) {
		if cut && len(res.Failed) == 0 && res.Err == nil {
			// Only the app's own clipboard had the cut; the system's copy
			// stays as it was.
			a.clipPaths = nil
			a.clipCut = false
		}
		a.finishFileJob(res, "粘贴失败：")
	})
}

// finishFileJob files a copy/move/delete run's outcome: the undo entry,
// the refreshed list with what landed selected, and the status line.
func (a *App) finishFileJob(res fileJobResult, failPrefix string) {
	for _, l := range res.Logs {
		a.store.logTrash(l[0], l[1])
	}
	a.pushUndo(res.Undo)
	a.reload()
	if len(res.Placed) > 0 {
		a.cur().selected = map[string]bool{}
		for _, p := range res.Placed {
			a.cur().selected[p] = true
		}
		a.cur().anchor = res.Placed[0]
	}
	switch {
	case len(res.Failed) > 0:
		a.statusMsg = failPrefix + joinNames(res.Failed)
	case errors.Is(res.Err, tasks.ErrCanceled):
		a.statusMsg = "已取消" + res.Verb
	}
}

func (a *App) doDelete() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	paths := make([]string, len(sel))
	for i, e := range sel {
		paths[i] = e.Path
	}
	if !a.confirmUnrecyclable(paths) {
		return
	}
	a.cur().renaming = ""
	a.cur().renameBuf = ""
	a.startFileJob(fileJob{Kind: tasks.KindDelete, Source: tasks.SourceUser, Srcs: paths}, func(res fileJobResult) {
		res.Placed = nil
		a.finishFileJob(res, "无法删除：")
		a.clearSelection()
		if len(res.Failed) == 0 && res.Trashed > 0 && res.Err == nil {
			a.statusMsg = "已将 " + itoa(res.Trashed) + " 个项目移到回收站"
		}
	})
}

// trashDir is where this OS's trash lives (platform.go).
func trashDir() (string, error) { return sysTrashDir() }

// trashPaths sends explicit paths to the bin, logging them for restore
// and undo, the way doDelete handles the selection. The extract toast's
// 「删除原归档」 uses it, and its status line keeps the 撤销 toast.
func (a *App) trashPaths(paths []string) {
	if !a.confirmUnrecyclable(paths) {
		return
	}
	var failed []string
	var moved []undoMove
	for _, p := range paths {
		if p == "" || p == pathThisPC || p == "/" {
			failed = append(failed, filepath.Base(p))
			continue
		}
		dst, key, err := moveToTrash(p)
		if err != nil {
			failed = append(failed, filepath.Base(p))
			continue
		}
		if key != "" {
			a.store.logTrash(p, key)
		}
		if dst != "" {
			moved = append(moved, undoMove{From: p, To: dst})
		}
	}
	if len(moved) > 0 {
		a.pushUndo(undoOp{Desc: fmt.Sprintf("删除 %d 个原归档", len(moved)), Moved: moved})
	}
	a.reload()
	if len(failed) > 0 {
		a.statusMsg = "未能删除：" + strings.Join(failed, "、")
	} else if len(moved) > 0 {
		a.statusMsg = "已将 " + itoa(len(moved)) + " 个原归档移到回收站"
	}
}

// moveToTrash sends path to this OS's trash, returning where it landed
// plus the key the restore log files it under (platform.go).
func moveToTrash(path string) (string, string, error) { return sysTrashMove(path) }

func (a *App) commitRename() {
	if err := a.commitRenameIn(a.cur()); err != nil {
		a.renameStatus(err)
	}
}

// commitRenameIn finishes the editor a tab leaves behind. Tab activation can
// name a tab other than the one coming forward while it is still current, so
// the operation works on that state rather than implicitly on a.cur().
func (a *App) commitRenameIn(t *tabState) error {
	if t.renaming == "" {
		return nil
	}
	oldName := t.renaming
	newName := strings.TrimSpace(t.renameBuf)
	t.renaming = ""
	t.renameBuf = ""
	if newName == "" || newName == oldName {
		return nil
	}
	if newName == "." || newName == ".." || strings.ContainsAny(newName, `/\`) {
		return fmt.Errorf("名称无效")
	}
	// The OS's own rules — Windows' reserved device names and illegal
	// characters, macOS' colon — block here with a reason, not later as a
	// raw syscall error after the file seemed renamed.
	if why := rename.LocalNameProblem(newName); why != "" {
		return fmt.Errorf("%s", why)
	}
	if specialPath(t.path) {
		return fmt.Errorf("无法重命名此页面中的项目")
	}
	oldPath := filepath.Join(t.path, oldName)
	newPath := filepath.Join(t.path, newName)
	// os.Rename replaces an existing file on Unix. A rename must never turn
	// a duplicate name into an overwrite; a case-only change is still the
	// same file and is allowed on case-insensitive filesystems.
	if info, err := os.Lstat(newPath); err == nil {
		oldInfo, oldErr := os.Lstat(oldPath)
		if oldErr != nil || !os.SameFile(oldInfo, info) {
			return fmt.Errorf("重命名失败：目标名称已存在")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("重命名失败：%w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		if t == a.cur() {
			a.reload()
		}
		return fmt.Errorf("重命名失败：%w", err)
	}
	a.pushUndo(undoOp{Desc: "重命名", Moved: []undoMove{{From: oldPath, To: newPath}}})
	if t == a.cur() {
		a.reload()
		a.selectOnly(newPath)
		t.reveal = newPath
	}
	return nil
}

// renameStatus gives Enter and tab handoff the same visible outcome, including
// a fresh toast when the same failure happens twice in one window.
func (a *App) renameStatus(err error) {
	if err == nil {
		return
	}
	a.statusMsg = err.Error()
	a.statusToasted = ""
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
	name, err := fsops.UniqueNewFolderName(a.cur().path)
	if err != nil {
		a.statusMsg = "新建文件夹失败：" + err.Error()
		return
	}
	dir := filepath.Join(a.cur().path, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		a.statusMsg = "新建文件夹失败：" + err.Error()
		return
	}
	a.pushUndo(undoOp{Desc: "新建文件夹", Created: []string{dir}})
	a.reload()
	a.selectOnly(dir)
	a.cur().reveal = dir
	a.cur().renaming = name
	a.cur().renameBuf = name
}

func (a *App) doRefresh() {
	clearThumbs()
	// 刷新 asks for the world as it is now: every folder the tree has read is
	// forgotten, not just the one being shown.
	a.navChildrenClear()
	a.navRecentReset()
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
	if len(paths) == 1 {
		copyPathText(paths[0])
	} else {
		mygo.Clipboard.WriteText(strings.Join(paths, "\n"))
	}
	a.statusMsg = "已复制路径"
}

// copyPathText puts one absolute path on the system clipboard. The search
// preview uses the same rule as 复制为路径 without selecting the file first.
func copyPathText(path string) bool {
	return copyPathTextWith(path, mygo.Clipboard.WriteText)
}

// copyPathTextWith keeps the path validation and write rule shared by the
// window commands and UI callbacks. A view callback supplies its context's
// clipboard writer, which is the safe path under the headless tester.
func copyPathTextWith(path string, write func(string)) bool {
	if path == "" || path == pathThisPC {
		return false
	}
	write(path)
	return true
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
	a.pushUndo(undoOp{Desc: "新建文本文档", Created: []string{path}})
	a.reload()
	a.selectOnly(path)
	a.cur().reveal = path
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
