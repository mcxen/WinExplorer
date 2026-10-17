package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// 窗口内拖拽：文件行是拖拽源（Drag），文件夹行、侧边栏位置、面包屑、
// 标签页是目标（Drop），把文件移动到对应层级。按住 Cmd/Ctrl 拖 =
// 复制，同左键拖到同文件夹是空操作——与 Explorer 的左键拖拽一致。
// 从其他应用（Finder）拖进来的文件走 DroppedFiles，同样落到目标文件夹。

// dragPayload 是窗口内拖拽携带的路径集合。
type dragPayload struct{ paths []string }

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
// 时高亮。此电脑与目标自身的拖拽源被忽略。
func (a *App) bindDropFolder(c *ui.Context, el ui.Element, dstDir string) {
	if dstDir == "" || dstDir == pathThisPC || dstDir == pathDisks {
		return
	}
	if over, ok := ui.DragOver[dragPayload](el); ok && over.paths != nil {
		if !a.dropAllInside(dstDir, over.paths) {
			el.Background(colorSelected().Alpha(0.75))
		}
	}
	if p, ok := ui.Drop[dragPayload](el); ok && len(p.paths) > 0 {
		a.doDropInto(c.Modifiers(), dstDir, p.paths)
	}
	// 从其他应用拖入的文件落到此文件夹。
	if files := el.DroppedFiles(); len(files) > 0 {
		a.doDropInto(c.Modifiers(), dstDir, files)
	}
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
	if dstDir == "" || dstDir == pathThisPC || len(paths) == 0 {
		return
	}
	copyMode := mods&(ui.Cmd|ui.Ctrl) != 0
	var failed, skipped []string
	moved := 0
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
		if !copyMode && pathWithin(dstDir, srcClean) {
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
				if err := copyDir(srcClean, dst); err != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
			} else if err := copyFile(srcClean, dst, info); err != nil {
				failed = append(failed, filepath.Base(src))
				continue
			}
			undoCreated = append(undoCreated, dst)
			placed = true
		} else if err := os.Rename(srcClean, dst); err != nil {
			// 跨设备（EXDEV）回退为复制后删除。
			if info.IsDir() {
				if derr := copyDir(srcClean, dst); derr != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
				if rerr := os.RemoveAll(srcClean); rerr != nil {
					failed = append(failed, filepath.Base(src))
					continue
				}
			} else if derr := copyFile(srcClean, dst, info); derr != nil {
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
	a.requestPreview()
}
