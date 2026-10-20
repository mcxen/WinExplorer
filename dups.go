package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// dups.go is the 重复文件查找 plugin: the pane background menu gains an
// item that scans the current folder's subtree for files with identical
// contents — grouped first by size, then by SHA-256 — and offers to
// delete every copy but the oldest, through the trash like 删除 does,
// with the same system confirmation 清空回收站 asks.
//
// The dialog's open flag lives here rather than on App (state.go), keyed
// by window so a second window (Ctrl+N) does not get a dialog it did not
// ask for; closed windows leave a small flag behind, which is harmless.
// The scan results are process-wide, so the dialog shows the folder its
// scan ran for and the last scan wins.

const (
	dupsPluginID   = "dups"
	dupsTimeout    = 8 * time.Second // the walk's deadline, as search's
	dupsMaxEntries = 20000           // files the walk collects before it stops
	dupsMaxHashed  = 2000            // files digested before hashing stops
	dupsWorkers    = 4               // concurrent digests, as thumb's
)

var (
	// dupsDlg is the dialog flag per window (plugintask.go).
	dupsDlg = appFlag{}
	// dupsResult is the latest scan, swapped in atomically.
	dupsResult atomic.Pointer[dupsScan]
	// dupsRunner serializes the scans: one at a time, a request that
	// arrives mid-scan replacing the queued one, so 删除多余项's rescan
	// never runs a folder the dialog has already moved past.
	dupsRunner = &taskRunner[dupsRequest]{run: runDupsScan}
)

// registerDupsPlugin registers the 重复文件查找 plugin: the pane
// background menu's scan item for the current folder.
func registerDupsPlugin() {
	RegisterPlugin(Plugin{
		ID:   dupsPluginID,
		Name: "重复文件查找",
		Desc: "在当前文件夹查找重复文件",
		MenuItems: func(a *App, sel []Entry) []PluginMenuItem {
			if len(sel) != 0 {
				return nil // the pane background's menu only
			}
			return []PluginMenuItem{{
				Label:    "重复文件查找",
				Disabled: specialPath(a.cur().path),
				Run:      a.openDups,
			}}
		},
	})
}

// dupsRequest is one scan the runner owes: the folder and whether the
// user shows hidden items at the time it was asked for.
type dupsRequest struct {
	root   string
	hidden bool
}

// dupsScan is one finished scan: the duplicate groups and what it covered.
type dupsScan struct {
	root      string
	groups    [][]Entry
	files     int
	truncated bool
}

// dupsOpenFlag returns this window's dialog flag, allocating it on first
// use.
func (a *App) dupsOpenFlag() *bool {
	return dupsDlg.get(a)
}

// openDups opens the dialog and kicks a scan of the current folder.
func (a *App) openDups() {
	*a.dupsOpenFlag() = true
	a.requestDups()
}

// requestDups starts the subtree scan for the current folder in the
// background, unless the page is a special one. The runner serializes:
// one scan at a time, the newest request next.
func (a *App) requestDups() {
	if !a.pluginEnabled(dupsPluginID) {
		return
	}
	root := a.cur().path
	if root == "" || specialPath(root) {
		return
	}
	dupsRunner.submit(dupsRequest{root: root, hidden: a.showHidden})
}

// runDupsScan is the runner's task: one scan, its result swapped in
// atomically. The runner repaints once it returns.
func runDupsScan(req dupsRequest) {
	dupsResult.Store(scanDuplicates(req.root, req.hidden))
}

// dupsScanningNow is whether a scan is running, for the status line.
func dupsScanningNow() bool {
	return dupsRunner.busy()
}

// scanDuplicates walks root's subtree and groups its files into duplicate
// sets. It is the background half: bounded by a deadline, the entry cap
// and the digest cap, so a huge tree cannot wedge the app.
func scanDuplicates(root string, hidden bool) *dupsScan {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(dupsTimeout))
	defer cancel()
	files := dupsWalk(root, hidden, ctx)
	res := &dupsScan{root: root, files: len(files)}
	var hashed atomic.Int64
	truncated := atomic.Bool{}
	digest := func(path string) (string, error) {
		if hashed.Add(1) > dupsMaxHashed || ctx.Err() != nil {
			truncated.Store(true)
			return "", fmt.Errorf("已达上限")
		}
		return fileSHA256(path)
	}
	res.groups = groupDuplicates(files, digest)
	res.truncated = truncated.Load() || ctx.Err() != nil
	return res
}

// dupsWalk collects the regular files under root with search's walk
// discipline: a deadline, hidden entries skipped unless asked, unreadable
// folders skipped, and a cap so a huge tree cannot wedge the scan. WalkDir
// does not follow symlinks, so symlinked dirs are not walked and their
// entries are dropped by the regular-file check below. Empty files are
// skipped — they all share one size and one digest, and "copies" of an
// empty file are noise.
func dupsWalk(root string, hidden bool, ctx context.Context) []Entry {
	files := make([]Entry, 0, 256)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable folder: skip it, keep walking
		}
		if ctx.Err() != nil || len(files) >= dupsMaxEntries {
			return filepath.SkipAll
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") && !hidden {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if e := dupsEntry(path, d); e.Size > 0 {
			files = append(files, e)
		}
		return nil
	})
	return files
}

// dupsEntry builds an Entry for a scanned file.
func dupsEntry(path string, d os.DirEntry) Entry {
	e := Entry{Name: d.Name(), Path: path}
	if info, err := d.Info(); err == nil {
		e.Size = info.Size()
		e.ModTime = info.ModTime()
	}
	e.Ext = lowerExt(e.Name)
	return e
}

// groupDuplicates groups files into sets of two or more with identical
// contents: first by size, then by the digest hash returns for the files
// a shared size leaves. It is pure — hash does the reading, so the test
// passes the real digester or a fake. Files hash fails on are skipped,
// singles are dropped, and groups and their members keep first-seen order.
func groupDuplicates(files []Entry, hash func(path string) (string, error)) [][]Entry {
	var sizes []int64
	bySize := map[int64][]Entry{}
	for _, e := range files {
		if _, seen := bySize[e.Size]; !seen {
			sizes = append(sizes, e.Size)
		}
		bySize[e.Size] = append(bySize[e.Size], e)
	}
	var groups [][]Entry
	for _, size := range sizes {
		if bucket := bySize[size]; len(bucket) >= 2 {
			groups = append(groups, contentGroups(bucket, hash)...)
		}
	}
	return groups
}

// contentGroups digests one size bucket and returns its identical-content
// groups of two or more, in first-seen order.
func contentGroups(bucket []Entry, hash func(path string) (string, error)) [][]Entry {
	sums := make([]string, len(bucket))
	digestAll(bucket, hash, sums)
	bySum := map[string][]Entry{}
	for i, e := range bucket {
		if sums[i] != "" {
			bySum[sums[i]] = append(bySum[sums[i]], e)
		}
	}
	var groups [][]Entry
	found := map[string]bool{}
	for _, s := range sums {
		if s == "" || found[s] {
			continue
		}
		found[s] = true
		if g := bySum[s]; len(g) >= 2 {
			groups = append(groups, g)
		}
	}
	return groups
}

// digestAll fills sums with the digest of each entry's file, "" where
// hash fails. Four digests run at once, as thumb's decodes do.
func digestAll(bucket []Entry, hash func(path string) (string, error), sums []string) {
	parallelFor(len(bucket), dupsWorkers, func(i int) {
		if sum, err := hash(bucket[i].Path); err == nil {
			sums[i] = sum
		}
	})
}

// dupsModal shows the dialog while it is open. Called from view (files.go's
// filePane, the per-frame hook this file has without touching main.go).
func (a *App) dupsModal(c *ui.Context) {
	flag := a.dupsOpenFlag()
	if !*flag {
		return
	}
	floatDialog(c, flag, "重复文件查找", 460, false, func() {
		a.dupsBody(c, flag)
	})
}

// dupsBody is the dialog: the folder scanned, a status line, the groups
// with their 删除多余项 buttons, and 关闭.
func (a *App) dupsBody(c *ui.Context, flag *bool) {
	res := dupsResult.Load()
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		ui.Text(c, "重复文件查找").FontSize(14).FontWeight(600).TextColor(colorText())
		a.dupsStatus(c, res)
		if res != nil && len(res.groups) > 0 {
			list := ui.Scroll(c).FillWidth().MaxHeight(360).Gap(10)
			list.Children(func() {
				for i, g := range res.groups {
					a.dupsGroup(c, i+1, g)
				}
			})
		}
		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			uikitPrimaryButton(c, "关闭", func() {
				*flag = false
			})
		})
	})
}

// dupsStatus is the muted line under the title: what the scan is doing,
// and the folder it ran for.
func (a *App) dupsStatus(c *ui.Context, res *dupsScan) {
	line := "正在扫描…"
	switch {
	case dupsScanningNow():
	case res == nil:
	case len(res.groups) == 0:
		line = "没有找到重复文件"
	default:
		line = fmt.Sprintf("%d 组重复 · 共扫描 %d 个文件", len(res.groups), res.files)
		if res.truncated {
			line += "（已达上限，结果可能不全）"
		}
	}
	ui.Text(c, line).FontSize(12).TextColor(colorMuted())
	if res != nil {
		ui.Text(c, res.root).FontSize(11).TextColor(colorMuted()).SingleLine().FillWidth()
	}
}

// dupsGroup is one duplicate set: its header with the 删除多余项 button,
// and the files, the keeper marked.
func (a *App) dupsGroup(c *ui.Context, n int, group []Entry) {
	var total int64
	for _, e := range group {
		total += e.Size
	}
	oldest := dupsOldest(group)
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		head := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
		head.Children(func() {
			ui.Text(c, fmt.Sprintf("组 %d", n)).FontSize(12).FontWeight(600).
				TextColor(colorText()).SingleLine()
			ui.Text(c, fmt.Sprintf("%d 个文件 · %s", len(group), formatBytes(total))).
				FontSize(11).TextColor(colorMuted()).Grow(1).MinWidth(0).SingleLine()
			btn := ui.ButtonBase(c).Height(24).Padding(0, 12).MinWidth(72).Shrink(0).Radius(4).
				Background(colorAddress()).Border(1, colorDivider()).Label("删除多余项")
			btn.Children(func() {
				ui.Text(c, "删除多余项").FontSize(11).TextColor(colorText())
			})
			if btn.Clicked() {
				a.deleteDuplicateExtras(group, oldest)
			}
		})
		for i, e := range group {
			name := e.Name
			if i == oldest {
				name += "（保留）"
			}
			ui.Text(c, name).FontSize(12).TextColor(colorText()).
				MaxLines(1).FillWidth()
			ui.Text(c, e.Path).FontSize(11).TextColor(colorMuted()).
				MaxLines(1).FillWidth()
		}
	})
}

// dupsOldest is the index of the group's oldest file, as 删除多余项's
// keeper; ties keep first-seen order.
func dupsOldest(group []Entry) int {
	oldest := 0
	for i, e := range group {
		if e.ModTime.Before(group[oldest].ModTime) {
			oldest = i
		}
	}
	return oldest
}

// dupsVictims is the group without its oldest file, the one 删除多余项
// keeps.
func dupsVictims(group []Entry, keep int) []Entry {
	victims := make([]Entry, 0, len(group)-1)
	for i, e := range group {
		if i != keep {
			victims = append(victims, e)
		}
	}
	return victims
}

// deleteDuplicateExtras confirms, then moves every file of the group but
// the oldest to the trash and rescans, so the dialog shows what is left.
func (a *App) deleteDuplicateExtras(group []Entry, keep int) {
	victims := dupsVictims(group, keep)
	if len(victims) == 0 {
		return
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  a.win,
		Type:    mygo.MessageWarning,
		Title:   "删除多余项",
		Message: fmt.Sprintf("组内 %d 个文件内容相同，确定要删除除“%s”以外的 %d 个吗？", len(group), group[keep].Name, len(victims)),
		Detail:  "删除的文件会被移入废纸篓，Ctrl+Z 或回收站的“还原”可以放回。",
		Buttons: []string{"删除多余项", "取消"},
	})
	if err != nil || res.Button != 0 {
		return
	}
	moved, failed := a.dupsTrash(victims)
	a.reload()
	if len(failed) > 0 {
		a.statusMsg = "无法删除：" + strings.Join(failed, "、")
	} else if moved > 0 {
		a.statusMsg = fmt.Sprintf("已删除 %d 个重复文件", moved)
	}
	a.requestDups()
}

// dupsTrash moves paths to the trash with 删除's semantics: the trash log
// records where each came from and the move joins the undo stack. It
// returns how many moved and the names that failed.
func (a *App) dupsTrash(victims []Entry) (int, []string) {
	var moved []undoMove
	var failed []string
	for _, e := range victims {
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
	if len(moved) > 0 {
		a.pushUndo(undoOp{Desc: "删除多余项", Moved: moved})
	}
	return len(moved), failed
}

// fileSHA256 is the digest of a file's contents, streamed: what the scan
// groups the files it kept by size with.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
