package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// batchrename.go is Explorer's rename of many selected items at once:
// with a multiple selection, F2 or 重命名 opens a dialog with one name
// pattern for all of them, previewed live before anything changes.
//
// The pattern's placeholders, expanded per item:
//
//	{name}  the item's full original name, "报告 (2).txt"
//	{stem}  the name without its extension, "报告 (2)"
//	{ext}   the extension without the dot, "txt"
//	{n}     the item's number, from the start value; {n:3} pads it to
//	        three digits, 001, 002…
//
// A preview row that would collide with an unselected item — or a
// selected sibling renamed earlier into the same name — shows it, and
// Commit applies nothing: Explorer's rename fails on conflicts, it does
// not invent suffixes.

// batchRenameState is the dialog's inputs and its live preview.
type batchRenameState struct {
	open     bool
	pattern  string
	start    int
	startBuf string
	preview  []batchPreviewRow
}

type batchPreviewRow struct {
	from, to string
	clash    bool // the target exists already, or two rows want it
}

// doRenameStart is F2: the single selection renames inline, as before;
// a multiple selection opens the batch dialog over the items, in the
// view's order.
func (a *App) doRenameStart() {
	sel := a.selectedEntries()
	if len(sel) == 0 {
		return
	}
	if len(sel) == 1 {
		a.cur().renaming = sel[0].Name
		a.cur().renameBuf = sel[0].Name
		a.cur().editingPath = false
		return
	}
	a.batch.open = true
	a.batch.pattern = "{n} {name}"
	a.batch.start = 1
	a.batch.startBuf = "1"
	a.batch.preview = a.batchPreview()
}

// batchPreview expands the pattern over the selection, marking the
// clashes so the dialog can show them before anything is applied.
func (a *App) batchPreview() []batchPreviewRow {
	sel := a.selectedEntries()
	rows := make([]batchPreviewRow, len(sel))
	taken := make(map[string]bool, len(sel))
	for i, e := range sel {
		to := expandRenamePattern(a.batch.pattern, a.batch.start+i, e)
		rows[i] = batchPreviewRow{from: e.Name, to: to}
		if to == "" || to == "." || to == ".." || strings.ContainsAny(to, `/\`) {
			rows[i].clash = true
			continue
		}
		key := strings.ToLower(to)
		if taken[key] {
			rows[i].clash = true // two selected items want one name
			continue
		}
		taken[key] = true
	}
	// A target that an unselected item already has, in the same folder.
	if dir := a.cur().path; !specialPath(dir) {
		for i := range rows {
			if rows[i].clash {
				continue
			}
			if _, err := os.Lstat(filepath.Join(dir, rows[i].to)); err == nil {
				rows[i].clash = true
			}
		}
	}
	return rows
}

// batchHasClash reports whether the current preview has any conflict,
// for the commit button's enablement and the message.
func (a *App) batchHasClash() bool {
	for _, r := range a.batch.preview {
		if r.clash {
			return true
		}
	}
	return false
}

// expandRenamePattern fills the pattern's placeholders for one item.
func expandRenamePattern(pattern string, n int, e Entry) string {
	stem := e.Name
	ext := ""
	if !e.IsDir {
		ext = strings.TrimPrefix(filepath.Ext(e.Name), ".")
		stem = strings.TrimSuffix(e.Name, filepath.Ext(e.Name))
	}
	// {n:3} — the number padded to that many digits.
	pattern = replaceNumberToken(pattern, n)
	pattern = strings.ReplaceAll(pattern, "{name}", e.Name)
	pattern = strings.ReplaceAll(pattern, "{stem}", stem)
	pattern = strings.ReplaceAll(pattern, "{ext}", ext)
	return strings.TrimSpace(pattern)
}

// replaceNumberToken handles {n} and {n:width}, the only placeholder
// whose value differs per call.
func replaceNumberToken(pattern string, n int) string {
	for {
		i := strings.Index(pattern, "{n:")
		if i < 0 {
			break
		}
		end := strings.Index(pattern[i:], "}")
		if end < 0 {
			break
		}
		width := pattern[i+3 : i+end]
		fmted := fmt.Sprintf("%0*d", atoiOr(width, 1), n)
		pattern = pattern[:i] + fmted + pattern[i+end+1:]
	}
	return strings.ReplaceAll(pattern, "{n}", fmt.Sprintf("%d", n))
}

func atoiOr(s string, fallback int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return fallback
	}
	return n
}

// commitBatchRename applies the previewed renames in one pass, pushing
// one undo for the lot. A last-moment clash — something changed on disk
// since the preview — leaves that item its old name and says so.
func (a *App) commitBatchRename() {
	if !a.batch.open || len(a.batch.preview) == 0 {
		return
	}
	sel := a.selectedEntries()
	if len(sel) != len(a.batch.preview) {
		a.batch.open = false
		return
	}
	var moves []undoMove
	var failed []string
	for i, e := range sel {
		to := a.batch.preview[i].to
		if a.batch.preview[i].clash || to == e.Name {
			continue
		}
		from := e.Path
		dst := filepath.Join(a.cur().path, to)
		if err := os.Rename(from, dst); err != nil {
			failed = append(failed, e.Name)
			continue
		}
		moves = append(moves, undoMove{From: from, To: dst})
	}
	if n := len(moves); n > 0 {
		desc := "批量重命名"
		if n > 1 {
			desc = fmt.Sprintf("批量重命名 %d 个项目", n)
		}
		a.pushUndo(undoOp{desc: desc, moved: moves})
	}
	a.batch.open = false
	a.reload()
	if len(moves) > 0 {
		a.statusMsg = fmt.Sprintf("已重命名 %d 个项目", len(moves))
	}
	if len(failed) > 0 {
		a.statusMsg = "部分项目未重命名：" + strings.Join(failed, "、")
	}
	a.requestPreview()
}

// batchRenameModal shows the dialog while it is open. Called from view.
func (a *App) batchRenameModal(c *ui.Context) {
	if !a.batch.open {
		return
	}
	ui.Modal(c, &a.batch.open, func() {
		a.batchRenameBody(c)
	})
}

func (a *App) batchRenameBody(c *ui.Context) {
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).MinWidth(420).Children(func() {
		ui.Text(c, "批量重命名").FontSize(14).FontWeight(600).TextColor(colorText())

		ui.Text(c, "模式").FontSize(12).TextColor(colorMuted())
		pat := ui.TextInputBase(c.Key("batch-pattern"), &a.batch.pattern).
			FillWidth().Height(30).FontSize(13).AutoFocus().
			Placeholder("{n} {name} · 占位符：{name} {stem} {ext} {n} {n:3}")
		if pat.Changed() {
			a.batch.preview = a.batchPreview()
		}
		if pat.Submitted() {
			a.commitBatchRename()
			return
		}

		row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
		row.Children(func() {
			ui.Text(c, "起始编号").FontSize(12).TextColor(colorMuted())
			num := ui.TextInputBase(c.Key("batch-start"), &a.batch.startBuf).
				Width(64).Height(28).FontSize(13)
			if num.Changed() {
				if n, err := atoiStrict(a.batch.startBuf); err == nil {
					a.batch.start = n
					a.batch.preview = a.batchPreview()
				}
			}
			ui.Text(c, fmt.Sprintf("选中 %d 个项目", len(a.batch.preview))).
				FontSize(12).TextColor(colorMuted()).Grow(1)
		})

		ui.Box(c).Height(1).FillWidth().Background(colorDivider())

		// The live preview: every row from → to, clashes marked.
		ui.List(c, nil, min(len(a.batch.preview), 8), func(i int) {
			if i < 0 || i >= len(a.batch.preview) {
				return
			}
			r := a.batch.preview[i]
			ui.Row(c).FillWidth().Gap(8).Padding(0, 2).Children(func() {
				ui.Text(c, r.from).FontSize(12).TextColor(colorMuted()).
					Grow(1).MinWidth(0).SingleLine()
				ui.Text(c, "→").FontSize(12).TextColor(colorMuted()).Shrink(0)
				to := ui.Text(c, r.to).FontSize(12).SingleLine().
					Grow(1).MinWidth(0).TextAlign(ui.End)
				if r.clash {
					to.TextColor(ui.Hex("#C4361D"))
				} else {
					to.TextColor(colorText())
				}
				if r.clash {
					ui.Text(c, "冲突").FontSize(11).TextColor(ui.Hex("#C4361D")).Shrink(0)
				}
			})
		}).FillWidth().Shrink(0).MaxHeight(220)

		if a.batchHasClash() {
			ui.Text(c, "存在名称冲突或无效名称：解决后才能重命名（冲突行不会应用）").
				FontSize(12).TextColor(ui.Hex("#C4361D"))
		}

		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			cancel := ui.ButtonBase(c).Height(30).Radius(4).
				Background(colorAddress()).Border(1, colorDivider()).Label("取消")
			cancel.Children(func() {
				ui.Text(c, "取消").FontSize(12).TextColor(colorText())
			})
			if cancel.Clicked() {
				a.batch.open = false
			}
			ok := ui.ButtonBase(c).Height(30).Radius(4).
				Background(colorAccent()).Label("重命名").Disabled(a.batchHasClash())
			ok.Children(func() {
				ui.Text(c, "重命名").FontSize(12).TextColor(ui.Hex("#FFFFFF"))
			})
			if ok.Clicked() {
				a.commitBatchRename()
			}
		})
	})
}

func atoiStrict(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
