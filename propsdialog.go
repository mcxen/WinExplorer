package main

import (
	"os"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// propsdialog.go is Explorer's 属性 dialog (the General tab): the menu
// item and Alt+Enter open a modal with the file's icon and name, its full
// property table, a thumbnail for pictures, and the actions Windows puts
// there — reveal in Finder ("打开文件位置") and open.

// propsFacts is the dialog's own load for folders, which the preview
// pipeline does not cover.
type propsFacts struct {
	path string
	meta []propRow
}

var propsState atomic.Pointer[propsFacts]

// showProperties opens the properties dialog for the single selection —
// a file or a folder, as Windows does.
func (a *App) showProperties() {
	e := a.singleSelectedAny()
	if e.Path == "" {
		return
	}
	a.propsOpen = true
	if e.IsDir {
		go a.loadFolderProps(e.Path)
		return
	}
	a.requestPreview()
}

// loadFolderProps gathers a folder's facts in the background; the dialog
// shows "正在读取属性…" until they land.
func (a *App) loadFolderProps(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		propsState.Store(&propsFacts{path: path})
		repaint()
		return
	}
	propsState.Store(&propsFacts{path: path, meta: a.fileProps(path, info, previewMeta)})
	repaint()
}

// propertiesModal shows the dialog while it is open. Called from view.
func (a *App) propertiesModal(c *ui.Context) {
	if !a.propsOpen {
		return
	}
	floatDialog(c, &a.propsOpen, false, func() {
		a.propertiesBody(c)
	})
}

func (a *App) propertiesBody(c *ui.Context) {
	e := a.singleSelectedAny()
	st := a.preview.Load()
	loading := st == nil || st.path != e.Path
	meta := st.meta
	if e.IsDir {
		if f := propsState.Load(); f != nil && f.path == e.Path {
			meta, loading = f.meta, false
		}
	}
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).MinWidth(360).Children(func() {
		head := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {})
		head.Children(func() {
			glyph := ui.Box(c).Size(40, 40).Shrink(0)
			glyph.Children(func() {
				a.fileIcon(c, e, 40)
			})
			ui.Text(c, e.Name).FontSize(14).FontWeight(600).TextColor(colorText()).
				Grow(1).MinWidth(0).MaxLines(2)
		})
		if loading {
			ui.Text(c, "正在读取属性…").FontSize(12).TextColor(colorMuted()).Padding(0, 12)
			return
		}
		// A picture shows itself small; other kinds show their facts only.
		if st.bitmap != nil && (st.kind == previewImage) {
			body := ui.Box(c).FillWidth().MaxHeight(220).Shrink(0).Radius(6).
				Background(colorAddress()).Border(1, colorDivider()).Padding(8, 8).Clip()
			body.Children(func() {
				ui.Image(c, st.bitmap).Fit(ui.Contain)
			})
		}
		ui.Box(c).Height(1).FillWidth().Background(colorDivider())
		for _, r := range meta {
			row := ui.Row(c).FillWidth().Gap(10).Padding(0, 4)
			row.Children(func() {
				ui.Text(c, r.Key).FontSize(12).TextColor(colorMuted()).
					Width(84).Shrink(0).SingleLine()
				ui.Text(c, r.Val).FontSize(12).TextColor(colorText()).
					Grow(1).MinWidth(0).MaxLines(2)
			})
		}
		// The plugins' metadata rows, for files.
		if !e.IsDir {
			for _, r := range a.pluginMetaRows(e) {
				a.pluginMetaRowValue(c, r, 84)
			}
		}
		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Padding(0, 4, 0, 0)
		btns.Children(func() {
			uikitSecondaryButton(c, "在 Finder 中显示", func() {
				_ = sysReveal(e.Path)
			})
			uikitSecondaryButton(c, "打开", func() {
				if err := openWithOS(e.Path); err != nil {
					a.statusMsg = "无法打开：" + err.Error()
				} else {
					a.recordRecent(e.Path)
				}
				a.propsOpen = false
			})
			uikitPrimaryButton(c, "确定", func() {
				a.propsOpen = false
			})
		})
	})
}
