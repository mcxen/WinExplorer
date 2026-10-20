package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sunvale/internal/fsops"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// 拖拽：文件行既是窗口内拖拽源（Drag），也是系统拖拽源（DragDataFrom）——
// 后者把 file URL 交给 Finder / 资源管理器 / 任何原生放置目标，文件因此
// 能拖出窗口。侧边栏里的文件夹和固定项同样是拖拽源。文件夹行、侧边栏位置、
// 面包屑、标签页是目标（Drop），把文件移动到对应层级；「快速访问」标题不是
// 文件夹，拖上去是固定那些路径，不移动也不复制。右侧助手的对话记录和输入框
// 同样不是文件夹：拖上去只把路径挂到这一轮（bindAssistDrop）。同一个目标也
// 接原生放置（DataDrop），所以从 Finder / 资源管理器拖进来的文件既落到目标
// 文件夹，也能落在空白处（当前文件夹），拖到「快速访问」上则固定，拖到助手上
// 则指定。按住 Cmd/Ctrl 拖 = 复制，同左键拖到同文件夹是空操作——与 Explorer
// 的左键拖拽一致。

// dragPayload 是窗口内拖拽携带的路径集合。
type dragPayload struct{ paths []string }

// dragFormats are the formats a drop may offer this window: a file list,
// and an URI list for the applications that only send URLs.
var dragFormats = []transfer.Format{transfer.FileList, transfer.URIList}

// dropOptions is what this window takes from other applications: files, by
// copy or move, as Finder and Explorer send them. A drop from another app
// is always a copy of its files into this window; the drop never moves the
// originals, which belong to the other app. Copy is therefore the effect
// accepted, and dropModifiers keeps the copy whatever key is held.
func dropOptions() transfer.DropOptions {
	return transfer.DropOptions{Formats: dragFormats, Operations: transfer.Copy}
}

// dropModifiers keeps a drop from another application a copy, whatever is
// held: its originals are not this window's to move.
func dropModifiers(m ui.Modifiers) ui.Modifiers { return m | ui.Ctrl }

// dragDropModifiers is the modifiers of a drop this window took, from the
// drag's own keys: copy, or move.
func dragDropModifiers(copyDrag bool) ui.Modifiers {
	if copyDrag {
		return ui.Ctrl
	}
	return 0
}

// dragSourceData builds the native transfer of a drag that starts on the
// selection (or the row alone): every path as a file URL, offered for copy
// or move. It runs as the gesture starts, so the selection it reads is the
// one the drag began with.
func dragSourceData(paths []string) transfer.Data {
	if len(paths) == 0 {
		return transfer.Data{}
	}
	data, err := transfer.FileData(paths...)
	if err != nil {
		return transfer.Data{}
	}
	return data
}

// bindDragSource makes el a native drag source as well as a window-internal
// one. The value is the payload window-internal targets take; the data is
// the same paths as file URLs, so Finder, Explorer and every native drop
// target take them. Both are built when the gesture starts, from the
// selection the drag began with.
//
// The destination performs a file move itself: Finder and Explorer rename
// the sources into the drop, and so do this app's own targets (doDropInto).
// Nothing is deleted here when a drop reports move — the shell's convention
// is that the target owns the file operation, and a source that deleted too
// would lose files that a target had only copied. A completed drag only
// refreshes the listing, since a move out of the window changed it.
func (a *App) bindDragSource(el ui.Element, path string) {
	el.Drag(a.dragValueFor(path))
	el.DragDataFrom(func() transfer.Data {
		return dragSourceData(a.dragValueFor(path).paths)
	}, transfer.DragOptions{
		Operations: transfer.Copy | transfer.Move,
		Done: func(r transfer.Result) {
			if r.Operation == transfer.None {
				return
			}
			// A drop this window took already reloaded and said what it
			// did; reloading again here would wipe that message.
			if a.dropTookOwnDrag {
				a.dropTookOwnDrag = false
				return
			}
			a.reload()
		},
	})
}

// dragValueFor 计算按住一行拖拽时应携带的集合：行在当前选择内则拖
// 整个选择，否则只拖这一行——Explorer 的规则。
func (a *App) dragValueFor(path string) dragPayload {
	if a.isSelected(path) {
		var paths []string
		for _, e := range a.selectedEntries() {
			if e.Path != "" && e.Path != pathThisPC {
				paths = append(paths, e.Path)
			}
		}
		if len(paths) > 0 {
			return dragPayload{paths: paths}
		}
	}
	return dragPayload{paths: []string{path}}
}

// bindDropFolder 让文件夹 el 接受窗口内拖拽移动到 dstDir，并在拖过
// 时高亮。此电脑与目标自身的拖拽源被忽略；从其他应用拖入的文件也落
// 到这个文件夹（见 bindNativeDrop）。
func (a *App) bindDropFolder(c *ui.Context, el ui.Element, dstDir string) {
	dstDir = a.dropDir(dstDir)
	if dstDir == "" {
		return
	}
	if over, ok := ui.DragOver[dragPayload](el); ok && over.paths != nil {
		if !a.dropAllInside(dstDir, over.paths) {
			el.Background(colorSelected().Alpha(0.75))
		}
	}
	a.bindNativeDrop(el, func(paths []string) {
		a.doDropInto(dropModifiers(c.Modifiers()), dstDir, paths)
	})
	if p, ok := ui.Drop[dragPayload](el); ok && len(p.paths) > 0 {
		// A native session takes the pointer's input, so what was held as
		// it began comes with the drag's offer (the toolkit's suggestion:
		// Ctrl on Windows, Option on macOS) rather than from Modifiers.
		copyDrag := a.dragSuggested == transfer.Copy || c.Modifiers()&(ui.Cmd|ui.Ctrl) != 0
		a.doDropInto(dragDropModifiers(copyDrag), dstDir, p.paths)
		a.dropTookOwnDrag = true
	}
	// 从其他应用拖入的文件落到此文件夹。
	if files := el.DroppedFiles(); len(files) > 0 {
		a.doDropInto(dropModifiers(c.Modifiers()), dstDir, files)
	}
}

// bindNativeDrop makes el a destination of files dropped from another
// application. Finder and Explorer hand native UI their file lists as data
// drops, so an element that only asked DroppedFiles never saw them; the
// formats are negotiated here so the drop lands on the element under the
// pointer, which highlights while the drag is over it.
func (a *App) bindNativeDrop(el ui.Element, take func([]string)) {
	opts := dropOptions()
	if offer, over := ui.DataDragOver(el, opts); over {
		// The pointer's keys, for the window's own drags: a native session
		// takes the input, so Context.Modifiers has what was held as it
		// began.
		a.dragSuggested = offer.Suggested
		el.Background(colorSelected().Alpha(0.75))
	}
	if drop, ok := ui.DropData(el, opts); ok {
		if paths, err := drop.Data.Files(); err == nil && len(paths) > 0 {
			take(paths)
			return
		}
		// A URI list that is not local files is still a drop to take: the
		// row or pane it landed on says so, and the shell opened nothing.
		if urls, err := drop.Data.URLs(); err == nil && len(urls) > 0 {
			take(urls)
		}
	}
}

// dropDir is where a target's drags land: the folder it names, or "" when
// it names a page rather than a folder — 此电脑, 磁盘健康, 主文件夹 or the
// trash, which are not places in the file system to put files in.
func (a *App) dropDir(path string) string {
	switch path {
	case "", pathThisPC, pathDisks, pathHome, pathTrash, pathGallery:
		return ""
	}
	return path
}

// dropAllInside 报告 payload 是否全部就是 dstDir 的直接子项（拖到自
// 己所在文件夹，高亮时不必显示为可放）。
func (a *App) dropAllInside(dstDir string, paths []string) bool {
	for _, p := range paths {
		if filepath.Clean(filepath.Dir(p)) != filepath.Clean(dstDir) {
			return false
		}
	}
	return len(paths) > 0
}

// doDropInto 把 paths 移动（或 Cmd/Ctrl 拖时复制）到 dstDir。同名自
// 动按“名称 - 副本”避让；跨盘移动回退为复制后删除原文件；把文件夹
// 拖进它自身或其子目录会跳过该项。
func (a *App) doDropInto(mods ui.Modifiers, dstDir string, paths []string) {
	dstDir = a.dropDir(dstDir)
	if dstDir == "" || len(paths) == 0 {
		return
	}
	// A URI that is no local file — a page, a mail attachment URL — has no
	// path to move or copy where it lands.
	var foreign []string
	local := make([]string, 0, len(paths))
	for _, p := range paths {
		if localPath(p) {
			local = append(local, p)
		} else {
			foreign = append(foreign, p)
		}
	}
	if len(local) == 0 {
		a.statusMsg = "只能放置本地文件或文件夹"
		return
	}
	paths = local
	copyMode := mods&(ui.Cmd|ui.Ctrl) != 0
	moved := 0
	var failed, skipped []string
	var undoCreated []string
	var undoMoved []undoMove
	for _, src := range paths {
		if src == "" || src == pathThisPC {
			continue
		}
		srcClean := filepath.Clean(src)
		if srcClean == filepath.Clean(dstDir) {
			continue
		}
		if !copyMode && fsops.PathWithin(dstDir, srcClean) {
			skipped = append(skipped, filepath.Base(src))
			continue
		}
		if !copyMode && filepath.Dir(srcClean) == filepath.Clean(dstDir) {
			continue // 原地移动是空操作
		}
		info, err := os.Lstat(srcClean)
		if err != nil {
			failed = append(failed, filepath.Base(src))
			continue
		}
		name, err := uniqueFreeName(dstDir, filepath.Base(srcClean), info.IsDir())
		if err != nil {
			failed = append(failed, filepath.Base(src))
			continue
		}
		dst := filepath.Join(dstDir, name)
		placed := false
		if copyMode {
			if info.IsDir() {
				if err := fsops.CopyDir(srcClean, dst); err != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
			} else if err := fsops.CopyFile(srcClean, dst, info); err != nil {
				failed = append(failed, filepath.Base(src))
				continue
			}
			undoCreated = append(undoCreated, dst)
			placed = true
		} else if err := os.Rename(srcClean, dst); err != nil {
			// 跨设备（EXDEV）回退为复制后删除。
			if info.IsDir() {
				if derr := fsops.CopyDir(srcClean, dst); derr != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
				if rerr := os.RemoveAll(srcClean); rerr != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
			} else if derr := fsops.CopyFile(srcClean, dst, info); derr != nil {
				failed = append(failed, filepath.Base(src))
				continue
			} else if rerr := os.Remove(srcClean); rerr != nil {
				failed = append(failed, filepath.Base(src))
				continue
			}
			undoMoved = append(undoMoved, undoMove{From: srcClean, To: dst})
			placed = true
		} else {
			undoMoved = append(undoMoved, undoMove{From: srcClean, To: dst})
			placed = true
		}
		if placed {
			moved++
		}
	}
	if n := len(undoCreated) + len(undoMoved); n > 0 {
		desc := "移动"
		if copyMode {
			desc = "复制"
		}
		if n > 1 {
			desc = fmt.Sprintf("%s %d 个项目", desc, n)
		}
		a.pushUndo(undoOp{desc: desc, created: undoCreated, moved: undoMoved})
	}
	a.reload()
	if moved > 0 {
		verb := "移动"
		if copyMode {
			verb = "复制"
		}
		a.statusMsg = fmt.Sprintf("已%s %d 个项目到 %s", verb, moved, filepath.Base(dstDir))
	}
	if len(skipped) > 0 {
		a.statusMsg = "无法将文件夹移动到其自身：" + strings.Join(skipped, "、")
	}
	if len(failed) > 0 {
		a.statusMsg = "无法" + map[bool]string{true: "复制", false: "移动"}[copyMode] +
			"：" + strings.Join(failed, "、")
	}
	if len(foreign) > 0 {
		a.statusMsg = "已忽略无法放置的链接：" + strings.Join(foreign, "、")
	}
	a.requestPreview()
}

// localPath reports whether p names a place on this machine's file system,
// as a path or a file URL: a drop may hand either, and only local files can
// be moved, copied or pinned where they land.
func localPath(p string) bool {
	_, ok := localFilePath(p)
	return ok
}

// localFilePath resolves p to a local absolute path. A file URL is accepted
// only for this machine — a drag from another app sometimes offers those
// instead of plain paths — and a remote URL is not a path to pin or move.
func localFilePath(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil || (u.Host != "" && u.Host != "localhost") {
			return "", false
		}
		p = filepath.FromSlash(u.Path)
		// file:///C:/x arrives as \C:\x. The leading separator is the
		// URL's empty host, not part of the drive path.
		if runtime.GOOS == "windows" && len(p) >= 3 && (p[0] == '\\' || p[0] == '/') && p[2] == ':' {
			p = p[1:]
		}
		if p == "" || !filepath.IsAbs(p) {
			return "", false
		}
	} else if strings.Contains(p, "://") || !filepath.IsAbs(p) {
		return "", false
	}
	return filepath.Clean(p), true
}

// bindPinDrop makes el the 快速访问 heading (or the empty part of that
// section). A drop pins the paths and leaves the originals where they
// are: the heading is not a folder, so moving into it would invent a
// destination the user did not name.
func (a *App) bindPinDrop(el ui.Element) {
	if over, ok := ui.DragOver[dragPayload](el); ok && len(over.paths) > 0 {
		el.Background(colorSelected().Alpha(0.75))
	}
	a.bindNativeDrop(el, func(paths []string) {
		a.doPinDrop(paths)
	})
	if p, ok := ui.Drop[dragPayload](el); ok && len(p.paths) > 0 {
		a.doPinDrop(p.paths)
		a.dropTookOwnDrag = true
	}
	if files := el.DroppedFiles(); len(files) > 0 {
		a.doPinDrop(files)
	}
}

// doPinDrop pins paths onto 快速访问, newest first and in the dropped
// order. Nothing is moved or copied: a pin is a pointer, and the file
// the user dragged is still in the folder it came from.
func (a *App) doPinDrop(paths []string) {
	if len(paths) == 0 || a.store == nil {
		return
	}
	added, already := 0, 0
	rejected := false
	for i := len(paths) - 1; i >= 0; i-- {
		path, ok := localFilePath(paths[i])
		if !ok || specialPath(path) {
			rejected = true
			continue
		}
		if _, err := os.Stat(path); err != nil {
			rejected = true
			continue
		}
		if a.store.isPinned(path) {
			already++
			continue
		}
		if a.store.pin(path) {
			added++
		} else {
			already++
		}
	}
	if added > 0 {
		// reload clears the status line; the pin message is set after it.
		a.reload()
	}
	switch {
	case added > 0:
		a.statusMsg = fmt.Sprintf("已固定 %d 个项目到快速访问", added)
	case already > 0:
		a.statusMsg = "已在快速访问中"
	case rejected:
		a.statusMsg = "无法固定到快速访问"
	}
}
