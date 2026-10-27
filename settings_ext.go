package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// settings_ext.go is the 扩展 programs page of Settings: the extension
// list group and the add/edit modal. Split from settingsdialog.go (LAY1).

func (a *App) settingsExtensionGroups() []components.SettingsGroup {
	var items []components.SettingsItem
	for i, e := range a.shellExtErrs {
		msg := e.Msg
		if e.Name != "" {
			msg = "扩展 “" + e.Name + "” 加载失败：" + msg
		}
		items = append(items, components.SettingsItem{
			ID: "err-" + strconv.Itoa(i), Title: "加载失败", Description: msg,
		})
	}
	if len(a.shellExts) == 0 && len(a.shellExtErrs) == 0 {
		items = append(items, components.SettingsItem{
			ID: "empty", Title: "还没有扩展", Description: "点下方按钮添加一条右键命令",
		})
	}
	for i := range a.shellExts {
		x := &a.shellExts[i]
		idx := i
		items = append(items, components.SettingsItem{
			ID:          "ext-" + strconv.Itoa(i),
			Title:       x.Name,
			Description: strings.Join(x.Match, "，") + " · " + x.Command,
			Content: func(c *ui.Context) {
				row := ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center)
				row.Children(func() {
					col := ui.Column(c).Grow(1).MinWidth(0).Gap(2)
					col.Children(func() {
						ui.Text(c, x.Name).FontSize(13).TextColor(colorText()).SingleLine()
						ui.Text(c, strings.Join(x.Match, "，")+" · "+truncateRunes(x.Command, 36)).
							FontSize(12).TextColor(colorMuted()).SingleLine()
					})
					settingsSwitchInline(c, !x.Disabled, func(on bool) {
						x.Disabled = !on
						a.saveShellExts()
					})
					if components.Button(c, "编辑", components.ButtonSecondary).Clicked() {
						a.extEditBegin(idx)
					}
					if components.Button(c, "删除", components.ButtonSecondary).Clicked() {
						a.shellExts = slices.Delete(a.shellExts, idx, idx+1)
						a.saveShellExts()
					}
				})
			},
		})
	}
	items = append(items, settingsActionItem("add", "添加扩展", "登记一条右键命令", "添加", func() {
		a.extEditBegin(-1)
	}))
	return []components.SettingsGroup{{ID: "commands", Title: "扩展命令", Items: items}}
}

// settingsSwitchInline is a bare switch without a settings row around it,
// for the compact extension rows.
func settingsSwitchInline(c *ui.Context, on bool, set func(bool)) {
	v := on
	sw := ui.Switch(c, &v)
	if sw.Changed() {
		set(v)
	}
}

// truncateRunes cuts s to n runes, adding an ellipsis when it cut.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// extEditState is the 扩展 editor dialog's inputs. index is -1 when the
// dialog adds a new extension, otherwise the shellExts slot it edits.
type extEditState struct {
	open     bool
	index    int
	name     string
	matchBuf string // 逗号分隔的匹配规则
	command  string
	terminal bool
}

// extEditBegin opens the editor: the extension's values for an edit, empty
// fields for an add.
func (a *App) extEditBegin(i int) {
	e := &a.extEdit
	if i >= 0 && i < len(a.shellExts) {
		x := a.shellExts[i]
		*e = extEditState{
			open:     true,
			index:    i,
			name:     x.Name,
			matchBuf: strings.Join(x.Match, ", "),
			command:  x.Command,
			terminal: x.Terminal,
		}
		return
	}
	*e = extEditState{open: true, index: -1}
}

// extDraft is the extension the form currently describes, valid or not.
func (e *extEditState) extDraft() shellExt {
	return shellExt{
		Name:     strings.TrimSpace(e.name),
		Match:    parseExtMatches(e.matchBuf),
		Command:  strings.TrimSpace(e.command),
		Terminal: e.terminal,
	}
}

// extEditValid reports whether the form can commit: a name, at least one
// usable match rule, and a command.
func (e *extEditState) extEditValid() bool {
	d := e.extDraft()
	return d.Name != "" && len(d.Match) > 0 && d.Command != ""
}

// extEditCommit saves the form into the registry: replacing the slot it
// edits, appending otherwise, then persisting.
func (a *App) extEditCommit() {
	e := &a.extEdit
	if !e.extEditValid() {
		return
	}
	d := e.extDraft()
	if e.index >= 0 && e.index < len(a.shellExts) {
		a.shellExts[e.index] = d
	} else {
		a.shellExts = append(a.shellExts, d)
	}
	e.open = false
	a.saveShellExts()
}

// extEditTest runs the form's command once against the current selection —
// the first selected entry, or the current folder when nothing is
// selected — and reports the outcome on the status bar, as runShellExt
// does.
func (a *App) extEditTest() {
	e := &a.extEdit
	if !e.extEditValid() {
		a.statusMsg = "扩展不完整，无法测试"
		return
	}
	x := e.extDraft()
	t := Entry{}
	if sel := a.selectedEntries(); len(sel) > 0 {
		t = sel[0]
	} else {
		p := a.cur().path
		if specialPath(p) {
			p = a.homePath
		}
		t = Entry{Name: filepath.Base(p), Path: p, IsDir: true}
	}
	if err := x.run(t); err != nil {
		a.statusMsg = "扩展运行失败：" + x.Name + "（" + err.Error() + "）"
		return
	}
	a.statusMsg = "已运行扩展：" + x.Name
}

// extEditModal shows the editor while it is open. Called from view.
func (a *App) extEditModal(c *ui.Context) {
	if !a.extEdit.open {
		return
	}
	floatDialog(c, &a.extEdit.open, a.extEditTitle(), 420, false, func() {
		a.extEditBody(c)
	})
}

// extEditTitle names the extension editor's dialog: 编辑 when it edits an
// entry, 添加 when it adds one.
func (a *App) extEditTitle() string {
	if a.extEdit.index >= 0 {
		return "编辑扩展"
	}
	return "添加扩展"
}

func (a *App) extEditBody(c *ui.Context) {
	e := &a.extEdit
	title := a.extEditTitle()
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(colorText())

		ui.Text(c, "名称").FontSize(12).TextColor(colorMuted())
		ui.TextInputBase(c.Key("ext-name"), &e.name).
			FillWidth().Height(30).FontSize(13).Placeholder("用 VSCode 打开")

		ui.Text(c, "匹配规则").FontSize(12).TextColor(colorMuted())
		ui.TextInputBase(c.Key("ext-match"), &e.matchBuf).
			FillWidth().Height(30).FontSize(13).
			Placeholder("* 全部 · file 文件 · dir 文件夹 · .ext 扩展名，多个用逗号分隔")

		ui.Text(c, "命令").FontSize(12).TextColor(colorMuted())
		ui.TextInputBase(c.Key("ext-cmd"), &e.command).
			FillWidth().Height(30).FontSize(13).
			Placeholder(`code "{path}" · 占位符：{path} {dir} {name} {stem} {ext}`)

		ui.Box(c).FillWidth().Padding(6, 0).Children(func() {
			v := e.terminal
			row := ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center)
			row.Children(func() {
				col := ui.Column(c).Grow(1).MinWidth(0).Gap(2)
				col.Children(func() {
					ui.Text(c, "在终端中运行").FontSize(13).TextColor(colorText())
					ui.Text(c, "在新的终端窗口中执行命令").FontSize(12).TextColor(colorMuted())
				})
				sw := ui.Switch(c, &v).Shrink(0).Label("在终端中运行")
				if sw.Changed() {
					e.terminal = v
				}
			})
		})

		if !e.extEditValid() {
			ui.Text(c, "名称、匹配规则和命令都填写后才能确定").
				FontSize(12).TextColor(ui.Hex("#C4361D"))
		}

		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End)
		btns.Children(func() {
			uikitSecondaryButton(c, "取消", func() {
				e.open = false
			})
			uikitSecondaryButton(c, "测试", func() {
				a.extEditTest()
			})
			btn := ui.ButtonBase(c).Height(30).Padding(0, btnPadX).MinWidth(btnMinW).Radius(rowR()).
				Background(colorAccent()).Label("确定").Disabled(!e.extEditValid())
			btn.Children(func() {
				ui.Text(c, "确定").FontSize(12).TextColor(colorBtnText())
			})
			if btn.Clicked() {
				a.extEditCommit()
			}
		})
	})
}
