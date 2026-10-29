package main

// files_list.go draws the list view: compact name-only rows flowing in
// columns. It was split out of files.go with the other per-view code.

import (
	"github.com/egoist/mygo/ui"
)

// listView is one column of compact icon-and-name rows, 28px each.
func (a *App) listView(c *ui.Context, entries []Entry) {
	rows := a.groupedRows(entries)
	t := a.cur()
	t.list.KeepOffset = true
	t.list.Key = groupRowKey(rows)
	a.scrollToReveal(rows, t.list.ScrollIntoView)
	list := ui.List(c, &t.list, len(rows), func(i int) {
		if i < 0 || i >= len(rows) {
			return
		}
		if rows[i].header != "" {
			a.groupHeaderRow(c, rows[i].header, rows[i].count)
			return
		}
		a.listRow(c, rows[i].e)
	}).Grow(1).FillWidth().MinHeight(0)
	list.Children(func() {
		if len(entries) == 0 {
			a.paneMessage(c, "此文件夹为空")
		}
	})
	a.bindPaneMenu(c, list)
}

func (a *App) listRow(c *ui.Context, e Entry) {
	row := ui.ButtonBase(c.Key(e.Path)).FillWidth().Height(a.listRowH()).
		Padding(0, 8).Gap(8).AlignItems(ui.Center).Margin(0, rowInset).
		Radius(4).Label(e.Name)
	a.paintSelect(row, e.Path)
	aiFxRow(c, row, e.Path, a.listRowH(), 4)
	row.Children(func() {
		a.nameCell(c, e, 16, 12)
	})
	a.bindItemInput(c, row, e)
	a.attachRowMenu(c, row, e)
}
