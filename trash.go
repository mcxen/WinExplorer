package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// trash.go is the 回收站 page: ~/.Trash listed like Explorer's recycle
// bin, with 还原 (put back) for files this app deleted, 永久删除 and
// 清空回收站.

// trashEntries is this OS's trash listing (platform.go).
func trashEntries() []Entry { return sysTrashList() }

// trashMenu is the trash page's item menu: 还原 where the original path is
// known, 永久删除, properties.
func (a *App) trashMenu(m *xMenu, e Entry) {
	if !a.isSelected(e.Path) {
		a.selectOnly(e.Path)
	}
	sel := a.selectedEntries()
	allRestorable := len(sel) > 0
	for _, s := range sel {
		if _, ok := a.store.originalForTrash(sysTrashKey(s.Path)); !ok {
			allRestorable = false
			break
		}
	}
	if m.Item("还原").Disabled(!allRestorable).Chosen() {
		a.doRestore()
	}
	if m.Item("永久删除").Disabled(len(sel) == 0).Chosen() {
		a.doPermanentDelete()
	}
	m.Separator()
	if m.Item("属性").Disabled(len(sel) != 1).Chosen() {
		a.showProperties()
	}
}

// trashPaneMenu is the trash page's background menu.
func (a *App) trashPaneMenu(m *xMenu) {
	if m.Item("清空回收站").Chosen() {
		a.doEmptyTrash()
	}
	m.Separator()
	if m.Item("刷新").Chosen() {
		a.doRefresh()
	}
}

// confirmUnrecyclable asks once when some of paths sit where deleting
// bypasses the bin — a network share, on Windows. Explorer warns before a
// permanent delete there; without this the shell's own confirmation is
// suppressed and the delete would be silent and final. False means the
// user backed out and nothing should be deleted at all.
func (a *App) confirmUnrecyclable(paths []string) bool {
	var doomed int
	for _, p := range paths {
		if p == "" || p == pathThisPC || p == "/" {
			continue
		}
		if !sysTrashRecyclable(p) {
			doomed++
		}
	}
	if doomed == 0 {
		return true
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageWarning,
		Title:   "永久删除",
		Message: fmt.Sprintf("选中的 %d 个项目位于没有回收站的位置（如网络驱动器），将被永久删除。", doomed),
		Detail:  "这些项目不会移入回收站，删除后无法还原。",
		Buttons: []string{"永久删除", "取消"},
	})
	return err == nil && res.Button == 0
}

// doRestore moves the selected items back to where they were deleted from,
// as Explorer's 回收站 "还原" does. The original folder is recreated when
// it has since gone missing; an existing file in the way gets a
// “名称 - 副本” sibling.
func (a *App) doRestore() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	var failed []string
	var restored []string
	for _, e := range sel {
		original, ok := a.store.originalForTrash(sysTrashKey(e.Path))
		if !ok {
			continue
		}
		parent := filepath.Dir(original)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			failed = append(failed, e.Name)
			continue
		}
		name, err := uniqueFreeName(parent, filepath.Base(original), e.IsDir)
		if err != nil {
			failed = append(failed, e.Name)
			continue
		}
		dst := filepath.Join(parent, name)
		if err := sysTrashRestore(e.Path, dst); err != nil {
			failed = append(failed, e.Name)
			continue
		}
		a.store.forgetTrash(sysTrashKey(e.Path))
		a.pushUndo(undoOp{
			Desc:  "还原 " + filepath.Base(original),
			Moved: []undoMove{{From: e.Path, To: dst}},
		})
		restored = append(restored, dst)
	}
	a.reload()
	if len(restored) > 0 {
		a.cur().selected = map[string]bool{}
		for _, p := range restored {
			a.cur().selected[p] = true
		}
		a.cur().anchor = restored[0]
		a.statusMsg = fmt.Sprintf("已还原 %d 个项目", len(restored))
	}
	if len(failed) > 0 {
		a.statusMsg = "无法还原：" + strings.Join(failed, "、")
	}
	a.requestPreview()
}

// doPermanentDelete removes the selection outright, after the same
// confirmation Explorer's recycle bin asks for.
func (a *App) doPermanentDelete() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageWarning,
		Title:   "永久删除",
		Message: fmt.Sprintf("确定要永久删除这 %d 个项目吗？", len(sel)),
		Detail:  "永久删除的项目不会被移入废纸篓，无法还原。",
		Buttons: []string{"永久删除", "取消"},
	})
	if err != nil || res.Button != 0 {
		return
	}
	var failed []string
	for _, e := range sel {
		if err := sysTrashRemove(e.Path); err != nil {
			failed = append(failed, e.Name)
		}
	}
	a.reload()
	a.clearSelection()
	if len(failed) > 0 {
		a.statusMsg = "无法删除：" + strings.Join(failed, "、")
	}
}

// doEmptyTrash removes everything in ~/.Trash after a confirmation.
func (a *App) doEmptyTrash() {
	entries := trashEntries()
	if len(entries) == 0 {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageWarning,
		Title:   "清空回收站",
		Message: "确定要永久删除回收站中的全部项目吗？",
		Buttons: []string{"清空回收站", "取消"},
	})
	if err != nil || res.Button != 0 {
		return
	}
	if err := sysTrashEmpty(); err != nil {
		a.statusMsg = "无法清空回收站：" + err.Error()
		return
	}
	a.store.clearTrash()
	a.reload()
	a.clearSelection()
}
