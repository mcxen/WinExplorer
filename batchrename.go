package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"sunvale/internal/rename"
)

// batchrename.go is Explorer's rename of many selected items at once: with a
// multiple selection, F2 or 重命名 opens a dialog over the items, in the
// view's order, with one rule for the lot — live-previewed, row by row, and
// draggable: dragging a row moves the item's place in the list, and with it
// the item's number, which is what reordering a numbered batch means.
//
// Every row can also be typed over. A typed row is "pinned": the rule leaves
// it alone until the row is reset, so the pattern prepares the batch and the
// hand fixes what the pattern cannot say.
//
// The rule itself is internal/rename's, shared with the remote server's
// batch_rename endpoint, so the two never disagree about what a plan does.

// batchRenameState is the dialog's inputs and its live preview.
type batchRenameState struct {
	open     bool
	mode     rename.Mode
	pattern  string
	replace  string
	start    int
	startBuf string
	rows     []rename.Row
	// editing is the index of the row whose target name is being typed, -1
	// for none. focus is the row the keyboard is on, which ↑/↓ move.
	editing int
	focus   int
	// list keeps the drag-reorder and the keyboard place of the rows.
	list ui.ListState
	// dragHint marks the row the pointer is dragging over, so the drop
	// line shows before the release.
	message string
}

// doRenameStart is F2: the single selection renames inline, as before; a
// multiple selection opens the batch dialog over the items, in the view's
// order.
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
	a.batchOpen(sel)
}

// batchOpen starts a plan over the given entries with the shipped defaults.
func (a *App) batchOpen(sel []Entry) {
	a.batch = batchRenameState{
		open:     true,
		mode:     rename.ModeTemplate,
		pattern:  "{n} {name}",
		start:    1,
		startBuf: "1",
		rows:     renameRowsFromEntries(sel),
		editing:  -1,
		focus:    -1,
	}
	a.batchReplan()
}

// batchOptions is the dialog's inputs as the engine reads them.
func (a *App) batchOptions() rename.Options {
	return rename.Options{
		Mode:    a.batch.mode,
		Pattern: a.batch.pattern,
		Replace: a.batch.replace,
		Start:   a.batch.start,
	}
}

// batchReplan recomputes the targets and the clash marks for the dialog's
// current inputs.
func (a *App) batchReplan() {
	if !a.batch.open {
		return
	}
	dir := a.cur().path
	if specialPath(dir) {
		dir = ""
	}
	rename.Replan(a.batch.rows, a.batchOptions(), dir)
}

// batchHasClash reports whether the current preview has any conflict, for
// the commit button's enablement and the message.
func (a *App) batchHasClash() bool {
	for _, r := range a.batch.rows {
		if r.Err != "" {
			return true
		}
	}
	return false
}

// batchMoveRows is the drag-reorder handler: it takes the rows dragged, in
// order, to before row to, and recomputes the numbering that follows from
// the new order.
func (a *App) batchMoveRows(rows []int, to int) {
	dragged := make([]rename.Row, 0, len(rows))
	rest := make([]rename.Row, 0, len(a.batch.rows))
	chosen := make(map[int]bool, len(rows))
	for _, i := range rows {
		if i >= 0 && i < len(a.batch.rows) {
			chosen[i] = true
			dragged = append(dragged, a.batch.rows[i])
		}
	}
	for i, r := range a.batch.rows {
		if !chosen[i] {
			rest = append(rest, r)
		}
	}
	at := to
	if at > len(rest) {
		at = len(rest)
	}
	if at < 0 {
		at = 0
	}
	out := make([]rename.Row, 0, len(a.batch.rows))
	out = append(out, rest[:at]...)
	out = append(out, dragged...)
	out = append(out, rest[at:]...)
	a.batch.rows = out
	a.batch.editing = -1
	if a.batch.focus >= len(a.batch.rows) {
		a.batch.focus = len(a.batch.rows) - 1
	}
	a.batchReplan()
}

// batchResetRow drops a row's manual name, handing it back to the pattern.
func (a *App) batchResetRow(i int) {
	if i < 0 || i >= len(a.batch.rows) {
		return
	}
	a.batch.rows[i].Manual = false
	a.batchReplan()
}

// commitBatchRename applies the previewed renames in one pass, pushing one
// undo for the lot; a single-row fix is described as a rename, a batch as
// one batch. Rows still marked are skipped and named on the status bar.
func (a *App) commitBatchRename() {
	if !a.batch.open {
		return
	}
	dir := a.cur().path
	a.batchReplan()
	if specialPath(dir) {
		a.batch.message = "无法重命名此页面中的项目"
		return
	}
	moved, failed := rename.Apply(dir, a.batch.rows)
	a.batch.open = false
	if n := len(moved); n > 0 {
		desc := "批量重命名"
		if n > 1 {
			desc = fmt.Sprintf("批量重命名 %d 个项目", n)
		}
		undo := make([]undoMove, len(moved))
		for i, m := range moved {
			undo[i] = undoMove{From: m.From, To: m.To}
		}
		a.pushUndo(undoOp{Desc: desc, Moved: undo})
		a.statusMsg = fmt.Sprintf("已重命名 %d 个项目", n)
	}
	if len(failed) > 0 {
		a.statusMsg = "部分项目未重命名：" + strings.Join(failed, "、")
	}
	a.reload()
	if len(moved) > 0 {
		a.selectOnly(moved[0].To)
	}
	a.requestPreview()
}

// batchRenameModal shows the dialog while it is open. Called from view.
func (a *App) batchRenameModal(c *ui.Context) {
	if !a.batch.open {
		return
	}
	floatDialog(c, &a.batch.open, "批量重命名", 560, false, func() {
		a.batchRenameBody(c)
	})
}

// batchModeChip is one mode chip: the accent fill when chosen, a hover wash
// otherwise.
func (a *App) batchModeChip(c *ui.Context, key string, m rename.Mode) {
	on := a.batch.mode == m
	chip := ui.ButtonBase(c.Key(key)).Height(26).Padding(0, 10).Radius(13).Label(rename.ModeLabel(m))
	if on {
		chip.Background(colorAccent())
	} else if chip.Hovered() {
		chip.Background(colorHover())
	}
	chip.Children(func() {
		ui.Text(c, rename.ModeLabel(m)).FontSize(12).
			TextColor(loColor(on, colorBtnText(), colorText()))
	})
	if chip.Clicked() && !on {
		a.batch.mode = m
		a.batchReplan()
	}
}

// loColor picks one of two colors by on, for the label of a chosen chip.
func loColor(on bool, yes, no ui.Color) ui.Color {
	if on {
		return yes
	}
	return no
}

func (a *App) batchRenameBody(c *ui.Context) {
	b := &a.batch
	a.batchHandleKeys(c)
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		head := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8)
		head.Children(func() {
			ui.Text(c, "批量重命名").FontSize(14).FontWeight(600).TextColor(colorText()).Grow(1)
			ui.Text(c, fmt.Sprintf("选中 %d 个项目", len(b.rows))).
				FontSize(12).TextColor(colorMuted())
		})

		// The mode chips, then the one field that mode reads.
		modes := ui.Row(c).Gap(6)
		modes.Children(func() {
			for i, m := range []rename.Mode{rename.ModeTemplate, rename.ModeRegex, rename.ModeFindReplace} {
				a.batchModeChip(c, fmt.Sprintf("batch-mode-%d", i), m)
			}
		})

		patternLabel := "模式"
		placeholder := "{n} {name} · 占位符：{name} {stem} {ext} {n} {n:3}"
		switch b.mode {
		case rename.ModeRegex:
			patternLabel = "正则"
			placeholder = `^IMG_(\d+) · 用 $1 引用捕获组`
		case rename.ModeFindReplace:
			patternLabel = "查找"
			placeholder = "查找的文字（在所有选中项的名称里替换）"
		}
		ui.Text(c, patternLabel).FontSize(12).TextColor(colorMuted())
		pat := ui.TextInputBase(c.Key("batch-pattern"), &b.pattern).
			FillWidth().Height(30).FontSize(13).AutoFocus().Placeholder(placeholder)
		if pat.Changed() {
			a.batchReplan()
		}
		if pat.Submitted() {
			a.commitBatchRename()
			return
		}

		if b.mode != rename.ModeTemplate {
			ui.Text(c, "替换为").FontSize(12).TextColor(colorMuted())
			rep := ui.TextInputBase(c.Key("batch-replace"), &b.replace).
				FillWidth().Height(30).FontSize(13)
			if b.mode == rename.ModeRegex {
				rep.Placeholder("$1 引用捕获组 · 可用 {modified_year} 等日期占位符")
			}
			if rep.Changed() {
				a.batchReplan()
			}
		}

		if b.mode == rename.ModeTemplate {
			row := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
			row.Children(func() {
				ui.Text(c, "起始编号").FontSize(12).TextColor(colorMuted())
				num := ui.TextInputBase(c.Key("batch-start"), &b.startBuf).
					Width(64).Height(28).FontSize(13)
				if num.Changed() {
					if n, err := atoiStrict(b.startBuf); err == nil {
						b.start = n
						a.batchReplan()
					}
				}
				ui.Text(c, "拖动行可调整顺序，编号随之改变；双击名称可逐项改写").
					FontSize(11).TextColor(colorMuted()).Grow(1).SingleLine()
			})
		} else {
			ui.Text(c, "拖动行可调整顺序；双击名称可逐项改写").
				FontSize(11).TextColor(colorMuted())
		}

		ui.Box(c).Height(1).FillWidth().Background(colorDivider())

		a.batchRowsList(c)

		if a.batchHasClash() {
			ui.Text(c, "存在名称冲突或无效名称：标红的行不会重命名，其余照常应用").
				FontSize(12).TextColor(ui.Hex("#C4361D"))
		}
		if b.message != "" {
			ui.Text(c, b.message).FontSize(12).TextColor(ui.Hex("#C4361D"))
		}

		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			uikitSecondaryButton(c, "取消", func() { a.batch.open = false })
			a.batchResetAllButton(c)
			label := "重命名"
			if n := rename.FixableRows(b.rows); n > 0 {
				label = fmt.Sprintf("重命名 %d 项", n)
			}
			btn := ui.ButtonBase(c).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(4).
				Background(colorAccent()).Label(label).Disabled(rename.FixableRows(b.rows) == 0)
			btn.Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(colorBtnText())
			})
			if btn.Clicked() {
				a.commitBatchRename()
			}
		})
	})
}

// batchResetAllButton drops every manual name, visible only when some row
// was typed over.
func (a *App) batchResetAllButton(c *ui.Context) {
	manual := 0
	for _, r := range a.batch.rows {
		if r.Manual {
			manual++
		}
	}
	if manual == 0 {
		return
	}
	uikitSecondaryButton(c, "恢复模式", func() {
		for i := range a.batch.rows {
			a.batch.rows[i].Manual = false
		}
		a.batchReplan()
	})
}

// batchRowsList is the draggable, editable preview: one row per item, its
// grip, its place number, the current name and the target name.
func (a *App) batchRowsList(c *ui.Context) {
	b := &a.batch
	// The list's own drag-reorder moves the rows — with its drop line and its
	// scroll near the edges — and the callback renumbers the plan.
	b.list.Key = func(i int) any {
		if i < 0 || i >= len(b.rows) {
			return i
		}
		return b.rows[i].Name
	}
	b.list.Reorder = func(rows []int, to int) { a.batchMoveRows(rows, to) }
	list := ui.List(c, &b.list, len(b.rows), func(i int) {
		a.batchRow(c, i)
	}).FillWidth().Shrink(0).MaxHeight(260).MinHeight(0)
	list.Children(func() {
		if len(b.rows) == 0 {
			ui.Text(c, "没有选中项").FontSize(12).TextColor(colorMuted()).Padding(6, 4)
		}
	})
}

func (a *App) batchRow(c *ui.Context, i int) {
	b := &a.batch
	r := &b.rows[i]
	// ButtonBase is a clickable row: a press on it begins the drag, and a
	// second quick press is the double click that starts typing the name.
	row := ui.ButtonBase(c.Key(r.Name)).FillWidth().Height(34).Gap(8).Padding(0, 6).
		AlignItems(ui.Center).Radius(4)
	if i == b.focus {
		row.Background(colorHover())
	}
	// The row is the drag source of its place. Drag comes after the children
	// are built (below), so its state is on the row itself, not on whatever
	// child the build happened to end with.
	row.Children(func() {
		// The grip is the visual affordance; the whole row is the drag source.
		grip := ui.Box(c).Size(18, 34).Shrink(0)
		grip.Children(func() {
			components.Icon(c, lucide.GripVertical()).Size(14, 14).TextColor(colorMuted())
		})
		ui.Text(c, fmt.Sprintf("%d", i+1)).FontSize(11).TextColor(colorMuted()).
			Width(22).Shrink(0).TextAlign(ui.Start)

		from := ui.Column(c).Grow(1).MinWidth(0).Gap(0)
		from.Children(func() {
			ui.Text(c, r.Name).FontSize(12).TextColor(colorMuted()).
				SingleLine().MinWidth(0)
		})

		to := ui.Column(c).Grow(1).MinWidth(0).Gap(0)
		to.Children(func() {
			if b.editing == i {
				buf := r.To
				text := components.Text(c, buf, components.TextOptions{Editable: &buf}).
					Label("新名称").AutoFocus()
				text.Height(24).FontSize(12).Padding(0, 4).Radius(3).
					Background(colorAddress()).Border(1, colorAccent())
				if text.Changed() {
					r.To = buf
					r.Manual = true
					a.batchReplan()
				}
				if text.Submitted() {
					b.editing = -1
					a.batchReplan()
				}
				return
			}
			ink := colorText()
			if r.Err != "" {
				ink = ui.Hex("#C4361D")
			}
			ui.Text(c, r.To).FontSize(12).TextColor(ink).SingleLine().MinWidth(0)
		})

		if r.Manual {
			reset := ui.ButtonBase(c.Key(fmt.Sprintf("batch-reset-%d", i))).
				Size(22, 22).Radius(3).Label("恢复").Tooltip("交还给模式")
			reset.Children(func() {
				components.Icon(c, lucide.RotateCcw()).Size(12, 12).TextColor(colorMuted())
			})
			if reset.Clicked() {
				a.batchResetRow(i)
			}
		}
		if r.Err != "" {
			ui.Text(c, r.Err).FontSize(11).TextColor(ui.Hex("#C4361D")).Shrink(0)
		}
	})

	// Double click (or Enter) starts typing the target name.
	if row.DoubleClicked() || (i == b.focus && c.Shortcut(0, ui.KeyEnter)) {
		b.editing = i
		b.focus = i
	}
	if row.Clicked() {
		b.focus = i
	}
}

// atoiStrict reads a plain non-negative decimal, the starting number's form.
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

// batchHandleKeys gives the preview the keyboard it needs: ↑/↓ move the
// focused row, F2 starts typing its name, Esc ends the typing. The panel
// closes on its own Escape handler.
func (a *App) batchHandleKeys(c *ui.Context) {
	b := &a.batch
	if b.editing >= 0 {
		if c.Shortcut(0, ui.KeyEscape) {
			b.editing = -1
		}
		return
	}
	n := len(b.rows)
	if n == 0 {
		return
	}
	if b.focus < 0 {
		b.focus = 0
	}
	switch {
	case c.Shortcut(0, ui.KeyDown):
		b.focus = min(b.focus+1, n-1)
		b.list.ScrollIntoView(b.focus)
	case c.Shortcut(0, ui.KeyUp):
		b.focus = max(b.focus-1, 0)
		b.list.ScrollIntoView(b.focus)
	case c.Shortcut(0, ui.KeyF2):
		b.editing = b.focus
	}
}
