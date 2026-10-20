package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// dsstore.go is the 清理 .DS_Store plugin: the pane background's menu and a
// folder row's menu gain 「清理 .DS_Store」, which scans the folder's subtree in
// the background for Finder's metadata files and lists them; 全部删除 moves
// them into the 回收站 with the confirmation, the trash log and the undo stack
// that 删除 has. The dialog's flag is per window and the results process-wide,
// as the 重复文件查找 dialog's are.

const (
	dsstorePluginID = "dsstore"
	dsstoreTimeout  = 8 * time.Second // the walk's deadline, as dups'
	dsstoreMaxHits  = 20000           // files the walk collects before it stops
	// dsstoreName is Finder's metadata file, matched by name whatever case a
	// volume wrote it in.
	dsstoreName = ".DS_Store"
)

var (
	// dsstoreDlg is the dialog flag per window (plugintask.go).
	dsstoreDlg = appFlag{}
	// dsstoreResult is the latest scan, swapped in atomically.
	dsstoreResult atomic.Pointer[dsstoreScan]
	// dsstoreRunner serializes the scans: one at a time, a request that arrives
	// mid-scan replacing the queued one.
	dsstoreRunner = &taskRunner[dsstoreRequest]{run: runDSStoreScan}
)

// registerDSStorePlugin registers the 清理 .DS_Store plugin.
func registerDSStorePlugin() {
	RegisterPlugin(Plugin{
		ID:   dsstorePluginID,
		Name: "清理 .DS_Store",
		Desc: "扫描并清理文件夹子树里的 .DS_Store（Finder 元数据文件）",
		MenuItems: func(a *App, sel []Entry) []PluginMenuItem {
			// The pane background's item scans the folder it is showing; a row's
			// scans the one folder it is on. Nothing to offer on a special page
			// or a selection that is not one folder.
			root := ""
			switch {
			case len(sel) == 0:
				if p := a.cur().path; p != "" && !specialPath(p) {
					root = p
				}
			case len(sel) == 1 && sel[0].IsDir && !specialPath(sel[0].Path):
				root = sel[0].Path
			}
			if root == "" {
				return nil
			}
			return []PluginMenuItem{{
				Label: "清理 .DS_Store",
				Run:   func() { a.openDSStore(root) },
			}}
		},
	})
}

// dsstoreRequest is one scan the runner owes: the folder.
type dsstoreRequest struct{ root string }

// dsstoreScan is one finished scan: the .DS_Store files it found and what it
// covered.
type dsstoreScan struct {
	root      string
	files     []Entry
	bytes     int64
	truncated bool
}

// dsstoreOpenFlag returns this window's dialog flag, allocating it on first use.
func (a *App) dsstoreOpenFlag() *bool {
	return dsstoreDlg.get(a)
}

// openDSStore opens the dialog and scans folder's subtree.
func (a *App) openDSStore(root string) {
	*a.dsstoreOpenFlag() = true
	a.requestDSStore(root)
}

// requestDSStore starts the subtree scan for root in the background, unless the
// plugin is switched off or the folder is a special page.
func (a *App) requestDSStore(root string) {
	if !a.pluginEnabled(dsstorePluginID) {
		return
	}
	if root == "" || specialPath(root) {
		return
	}
	dsstoreRunner.submit(dsstoreRequest{root: root})
}

// runDSStoreScan is the runner's task: one scan, its result swapped in
// atomically. The runner repaints once it returns.
func runDSStoreScan(req dsstoreRequest) {
	dsstoreResult.Store(scanDSStore(req.root))
}

// scanDSStore walks root's subtree and collects the .DS_Store files. The
// hidden-entry rule is not applied: the file this is about is hidden by the dot
// its own name starts with, so the pane's discipline would skip the target.
// Bounded by a deadline and a count, as dups' scan is; unreadable folders are
// skipped rather than fatal, and symlinks are not followed.
func scanDSStore(root string) *dsstoreScan {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(dsstoreTimeout))
	defer cancel()
	res := &dsstoreScan{root: root}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || len(res.files) >= dsstoreMaxHits {
			return filepath.SkipAll
		}
		if path != root && !d.IsDir() && strings.EqualFold(d.Name(), dsstoreName) {
			if e := dupsEntry(path, d); e.Path != "" {
				res.files = append(res.files, e)
				res.bytes += e.Size
			}
		}
		return nil
	})
	res.truncated = ctx.Err() != nil || len(res.files) >= dsstoreMaxHits
	return res
}

// deleteDSStores confirms, then moves every .DS_Store of the scan into the
// 回收站 and rescans, so the dialog shows what is left rather than the files it
// just removed.
func (a *App) deleteDSStores(res *dsstoreScan) {
	if res == nil || len(res.files) == 0 {
		return
	}
	r, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageWarning,
		Title:   "清理 .DS_Store",
		Message: fmt.Sprintf("删除 %s 下找到的 %d 个 .DS_Store？", res.root, len(res.files)),
		Detail:  "删除的文件会被移入废纸篓，Ctrl+Z 或回收站的“还原”可以放回。",
		Buttons: []string{"删除", "取消"},
	})
	if err != nil || r.Button != 0 {
		return
	}
	a.dsstoreTrash(res.files)
	a.requestDSStore(res.root)
}

// dsstoreTrash moves every file into the 回收站, with the trash log and the
// undo stack 删除 has, and says how many went and which did not: one busy file
// does not stop the rest.
func (a *App) dsstoreTrash(files []Entry) (moved int, failed []string) {
	var undoes []undoMove
	for _, e := range files {
		dst, key, err := moveToTrash(e.Path)
		if err != nil {
			failed = append(failed, e.Path)
			continue
		}
		if key != "" {
			a.store.logTrash(e.Path, key)
		}
		if dst != "" {
			moved++
			undoes = append(undoes, undoMove{From: e.Path, To: dst})
		}
	}
	if len(undoes) > 0 {
		a.pushUndo(undoOp{Desc: fmt.Sprintf("清理 .DS_Store（%d 个）", len(undoes)), Moved: undoes})
	}
	a.reload()
	switch {
	case len(failed) > 0 && moved > 0:
		a.statusMsg = fmt.Sprintf("已清理 %d 个 .DS_Store，%d 个无法删除", moved, len(failed))
	case len(failed) > 0:
		a.statusMsg = fmt.Sprintf("无法删除 %d 个 .DS_Store（可能没有权限）", len(failed))
	default:
		a.statusMsg = fmt.Sprintf("已清理 %d 个 .DS_Store", moved)
	}
	return moved, failed
}

// dsstoreModal shows the dialog while it is open. Called from view with the
// pane's other overlays (files.go's filePane).
func (a *App) dsstoreModal(c *ui.Context) {
	flag := a.dsstoreOpenFlag()
	if !*flag {
		return
	}
	floatDialog(c, flag, "清理 .DS_Store", 460, false, func() {
		a.dsstoreBody(c, flag)
	})
}

// dsstoreBody is the dialog: the folder scanned, a status line, the files it
// found, and the buttons.
func (a *App) dsstoreBody(c *ui.Context, flag *bool) {
	res := dsstoreResult.Load()
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		ui.Text(c, "清理 .DS_Store").FontSize(14).FontWeight(600).TextColor(colorText())
		a.dsstoreStatus(c, res)
		if res != nil && len(res.files) > 0 {
			list := ui.Scroll(c).FillWidth().MaxHeight(360).Gap(2)
			list.Children(func() {
				for _, e := range res.files {
					a.dsstoreFile(c, e)
				}
			})
		}
		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			if res != nil && len(res.files) > 0 && !dsstoreRunner.busy() {
				uikitPrimaryButton(c, "全部删除", func() {
					a.deleteDSStores(res)
				})
			}
			uikitSecondaryButton(c, "关闭", func() {
				*flag = false
			})
		})
	})
}

// dsstoreStatus is the muted line under the title: what the scan is doing, and
// the folder it ran for — a spinner while it runs, a badge for the cap.
func (a *App) dsstoreStatus(c *ui.Context, res *dsstoreScan) {
	line := "正在扫描…"
	truncated := false
	switch {
	case dsstoreRunner.busy():
	case res == nil:
	case len(res.files) == 0:
		line = "没有找到 .DS_Store"
	default:
		line = fmt.Sprintf("%d 个 .DS_Store · %s", len(res.files), formatBytes(res.bytes))
		truncated = res.truncated
	}
	row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		if dsstoreRunner.busy() {
			components.Spinner(c, components.SpinnerRing)
		}
		ui.Text(c, line).FontSize(12).TextColor(colorMuted()).Grow(1).MinWidth(0).SingleLine()
		if truncated {
			components.Badge(c, "已达上限")
		}
		if res != nil {
			ui.Text(c, res.root).FontSize(11).TextColor(colorMuted()).Shrink(0).SingleLine()
		}
	})
}

// dsstoreFile is one found .DS_Store: its full path and its size.
func (a *App) dsstoreFile(c *ui.Context, e Entry) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, e.Path).FontSize(12).TextColor(colorText()).
			Grow(1).MinWidth(0).SingleLine()
		ui.Text(c, formatBytes(e.Size)).FontSize(11).TextColor(colorMuted()).Shrink(0)
	})
}
