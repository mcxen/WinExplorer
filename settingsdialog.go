package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

// settingsdialog.go is the 设置 dialog, Windows 11's Settings app in a
// modal over the window: the appearance picker, the view toggles
// Explorer's folder options keep, and the updater plugin's preferences.
// Every change writes through at once, so there is nothing to save —
// 确定 only closes the dialog.

// viewModeNames pairs the view modes with the labels Explorer's 查看 menu
// uses, largest first.
var viewModeNames = []struct {
	label string
	mode  ViewMode
}{
	{"超大图标", ViewExtraLarge},
	{"大图标", ViewLargeIcons},
	{"中等图标", ViewMediumIcons},
	{"小图标", ViewSmallIcons},
	{"列表", ViewList},
	{"详细信息", ViewDetails},
}

// viewModeLabels are viewModeNames' labels, the 默认视图 dropdown's
// options.
func viewModeLabels() []string {
	out := make([]string, len(viewModeNames))
	for i, m := range viewModeNames {
		out[i] = m.label
	}
	return out
}

// viewModeLabel is the label Explorer's 查看 menu uses for a view mode,
// 详细信息 for anything out of range.
func viewModeLabel(mode ViewMode) string {
	for _, m := range viewModeNames {
		if m.mode == mode {
			return m.label
		}
	}
	return viewModeNames[len(viewModeNames)-1].label
}

// viewModeNamed is the view mode for one of the dropdown's labels.
func viewModeNamed(label string) (ViewMode, bool) {
	for _, m := range viewModeNames {
		if m.label == label {
			return m.mode, true
		}
	}
	return 0, false
}

// settingsModal shows the dialog while it is open. Called from view.
func (a *App) settingsModal(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	ui.Modal(c, &a.settingsOpen, func() {
		a.settingsBody(c)
	})
}

// applySettings persists the change and applies it to every open window
// at once, so the settings page edits what the user sees immediately.
func (a *App) applySettings(fn func(s *appSettings)) {
	a.store.updateSettings(fn)
	appsMu.Lock()
	all := slices.Clone(apps)
	appsMu.Unlock()
	for _, app := range all {
		wasOpen := app.previewOpen
		app.loadPersistedSettings()
		// A settings change can close the preview pane; as togglePreview,
		// closing it stops a playing preview's audio.
		if wasOpen && !app.previewOpen {
			if st := app.preview.Load(); st != nil && st.media.player != nil {
				st.media.player.stop()
			}
		}
	}
}

// settingsBody builds the dialog: the title, the sections scrolling once
// they no longer fit the window, and the buttons that stay at the bottom.
func (a *App) settingsBody(c *ui.Context) {
	ui.Column(c).Gap(0).MinWidth(440).Children(func() {
		ui.Text(c, "设置").FontSize(14).FontWeight(600).TextColor(colorText()).
			Padding(14, 16, 8, 16)
		ui.Scroll(c).FillWidth().MaxHeight(320).Padding(0, 16, 4, 16).Children(func() {
			a.settingsAppearance(c)
			settingsDivider(c)
			a.settingsView(c)
			settingsDivider(c)
			a.settingsUpdates(c)
			settingsDivider(c)
			a.settingsPlugins(c)
			settingsDivider(c)
			a.settingsExtensions(c)
		})
		btns := ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Padding(10, 16, 12, 16)
		btns.Children(func() {
			// 恢复默认 puts the appearance and the view settings back;
			// the updater keeps its own preferences.
			uikitSecondaryButton(c, "恢复默认", func() {
				mygo.Theme.SetSource(mygo.ThemeSystem)
				a.applySettings(func(x *appSettings) { *x = *defaultSettings() })
			})
			uikitPrimaryButton(c, "确定", func() {
				a.settingsOpen = false
			})
		})
	})
}

// settingsDivider is the hairline between sections.
func settingsDivider(c *ui.Context) {
	ui.Box(c).Height(1).FillWidth().Margin(6, 0).Background(colorDivider())
}

// settingsSectionHead is a section's caption, 外观/查看/更新.
func settingsSectionHead(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(colorText()).
		Padding(10, 0, 2, 0)
}

// settingsPlugins is the 插件 section: one switch per registered plugin,
// in registration order, each reading its state fresh from the store like
// the sections above, so a toggle shows at once. Switching a plugin off
// stops what it is doing too — its OnDisable hook owns that. 恢复默认
// leaves the plugins alone: they are not appearance or view settings.
func (a *App) settingsPlugins(c *ui.Context) {
	settingsSectionHead(c, "插件")
	for _, p := range registeredPlugins() {
		id := p.ID
		settingsSwitch(c, p.Name, p.Desc,
			func() bool { return a.pluginEnabled(id) },
			func(on bool) { a.pluginSetEnabled(id, on) })
	}
}

// settingsRow is one setting's row, Windows 11 Settings' shape: the name
// and its one-line description on the left, the control on the right.
func settingsRow(c *ui.Context, name, desc string, control func()) {
	row := ui.Row(c).FillWidth().Gap(16).Padding(8, 0).AlignItems(ui.Center)
	row.Children(func() {
		text := ui.Column(c).Grow(1).MinWidth(0).Gap(2)
		text.Children(func() {
			ui.Text(c, name).FontSize(13).TextColor(colorText()).SingleLine()
			ui.Text(c, desc).FontSize(12).TextColor(colorMuted()).MaxLines(1)
		})
		control()
	})
}

// settingsSwitch is a switch row: the switch reads get fresh each frame,
// so a change made in another window shows here; set writes a change
// through on the UI thread.
func settingsSwitch(c *ui.Context, name, desc string, get func() bool, set func(bool)) {
	v := get()
	settingsRow(c, name, desc, func() {
		sw := ui.Switch(c, &v)
		if sw.Changed() {
			set(v)
		}
	})
}

// settingsInfo shows a value where a control would sit: 当前版本 and
// 上次检查, muted as Windows 11's are.
func settingsInfo(c *ui.Context, name, value string) {
	row := ui.Row(c).FillWidth().Gap(16).Padding(8, 0).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, name).FontSize(13).TextColor(colorText()).SingleLine().
			Grow(1).MinWidth(0)
		ui.Text(c, value).FontSize(12).TextColor(colorMuted()).SingleLine()
	})
}

// appearanceIndex maps mygo.Theme.Source to the segmented control's
// index. Before App.Run the theme has nothing to read, and counts as
// 跟随系统, as darkMode treats the pre-ready appearance.
func appearanceIndex() int {
	source := mygo.ThemeSystem
	if mygo.App.IsReady() {
		source = mygo.Theme.Source()
	}
	switch source {
	case mygo.ThemeLight:
		return 1
	case mygo.ThemeDark:
		return 2
	}
	return 0
}

// settingsAppearance is the 外观 section: the appearance seeded from
// mygo.Theme each frame, so the segments follow what the app shows.
func (a *App) settingsAppearance(c *ui.Context) {
	settingsSectionHead(c, "外观")
	v := appearanceIndex()
	settingsRow(c, "外观模式", "选择应用的外观", func() {
		seg := ui.Segmented(c, &v, "跟随系统", "浅色", "深色").Shrink(0)
		if seg.Changed() {
			switch v {
			case 1:
				mygo.Theme.SetSource(mygo.ThemeLight)
			case 2:
				mygo.Theme.SetSource(mygo.ThemeDark)
			default:
				mygo.Theme.SetSource(mygo.ThemeSystem)
			}
		}
	})
}

// settingsView is the 查看 section: the toggles Explorer's folder options
// keep, each writing through to every window, and the view new windows
// open in.
func (a *App) settingsView(c *ui.Context) {
	settingsSectionHead(c, "查看")
	settingsSwitch(c, "文件扩展名", "显示已知文件类型的扩展名",
		func() bool { return a.store.settings().ShowExt },
		func(v bool) { a.applySettings(func(x *appSettings) { x.ShowExt = v }) })
	settingsSwitch(c, "隐藏的项目", "显示隐藏的文件与文件夹",
		func() bool { return a.store.settings().ShowHidden },
		func(v bool) { a.applySettings(func(x *appSettings) { x.ShowHidden = v }) })
	settingsSwitch(c, "项目复选框", "选择项目时显示复选框",
		func() bool { return a.store.settings().ItemCheckboxes },
		func(v bool) { a.applySettings(func(x *appSettings) { x.ItemCheckboxes = v }) })
	settingsSwitch(c, "紧凑视图", "减小列表与详细信息的行距",
		func() bool { return a.store.settings().Compact },
		func(v bool) { a.applySettings(func(x *appSettings) { x.Compact = v }) })
	settingsSwitch(c, "预览窗格", "打开窗口时在右侧显示预览窗格",
		func() bool { return a.store.settings().PreviewOpen },
		func(v bool) { a.applySettings(func(x *appSettings) { x.PreviewOpen = v }) })
	settingsRow(c, "默认视图", "新窗口使用的视图", func() {
		cur := viewModeLabel(ViewMode(a.store.settings().Mode))
		sel := ui.Select(c, &cur, viewModeLabels()).Shrink(0)
		if sel.Changed() {
			if mode, ok := viewModeNamed(cur); ok {
				a.applySettings(func(x *appSettings) { x.Mode = int(mode) })
			}
		}
	})
}

// settingsUpdates is the 更新 section: the current version and the
// updater plugin's preferences, when the build can update itself at all.
// The updater calls run on the UI thread during the frame, as the
// immediate mode reads them fresh each frame.
func (a *App) settingsUpdates(c *ui.Context) {
	settingsSectionHead(c, "更新")
	version := mygo.App.Version()
	if version == "" {
		version = "晴谷"
	}
	settingsInfo(c, "当前版本", version)
	if !mygo.Updater.Enabled() {
		// Development builds cannot self-update: one muted line in place
		// of the controls.
		ui.Text(c, "此构建不支持自动更新").FontSize(12).TextColor(colorMuted()).
			Padding(8, 0)
		return
	}
	settingsSwitch(c, "自动检查更新", "定期在后台检查新版本",
		updater.AutomaticChecks, updater.SetAutomaticChecks)
	settingsSwitch(c, "自动下载并安装", "后台下载并安装更新，下次启动时生效",
		updater.AutomaticDownloads, updater.SetAutomaticDownloads)
	// 检查更新 opens the updater's own update window; calling it again
	// while a check is under way brings that window to the front.
	btnRow := ui.Row(c).FillWidth().Justify(ui.End).Padding(8, 0)
	btnRow.Children(func() {
		uikitPrimaryButton(c, "检查更新", func() {
			updater.CheckForUpdates()
		})
	})
	if last := updater.LastCheck(); last.IsZero() {
		settingsInfo(c, "上次检查", "从未")
	} else {
		settingsInfo(c, "上次检查", last.Local().Format("2006/01/02 15:04"))
	}
}

// settingsExtensions is the 扩展 section: the registered shell extensions,
// each with its enable switch, an 编辑 and a 删除 button, and the entries
// the registry could not keep, listed with their problem so a broken
// extensions.json is fixable without guessing. Every change writes through
// to extensions.json at once — the registry has no pending state.
func (a *App) settingsExtensions(c *ui.Context) {
	settingsSectionHead(c, "扩展")
	if len(a.shellExts) == 0 && len(a.shellExtErrs) == 0 {
		ui.Text(c, "还没有注册扩展，点下方按钮添加").FontSize(12).
			TextColor(colorMuted()).Padding(4, 0)
	}
	for _, e := range a.shellExtErrs {
		msg := e.Msg
		if e.Name != "" {
			msg = "扩展 “" + e.Name + "” 加载失败：" + msg
		}
		ui.Text(c, msg).FontSize(12).TextColor(ui.Hex("#C4361D")).Padding(2, 0)
	}
	for i := range a.shellExts {
		x := &a.shellExts[i]
		row := ui.Row(c).FillWidth().Gap(10).Padding(6, 0).AlignItems(ui.Center)
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
			uikitSecondaryButton(c, "编辑", func() { a.extEditBegin(i) })
			uikitSecondaryButton(c, "删除", func() {
				a.shellExts = slices.Delete(a.shellExts, i, i+1)
				a.saveShellExts()
			})
		})
	}
	btnRow := ui.Row(c).FillWidth().Justify(ui.Start).Padding(6, 0)
	btnRow.Children(func() {
		uikitSecondaryButton(c, "添加扩展", func() { a.extEditBegin(-1) })
	})
}

// settingsSwitchInline is a bare switch without the settings row around
// it, for the compact extension rows.
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
	ui.Modal(c, &a.extEdit.open, func() {
		a.extEditBody(c)
	})
}

func (a *App) extEditBody(c *ui.Context) {
	e := &a.extEdit
	title := "添加扩展"
	if e.index >= 0 {
		title = "编辑扩展"
	}
	ui.Column(c).Gap(10).Padding(4, 2, 4, 2).MinWidth(420).Children(func() {
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

		settingsSwitch(c, "在终端中运行", "在新的终端窗口中执行命令",
			func() bool { return e.terminal },
			func(v bool) { e.terminal = v })

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
			btn := ui.ButtonBase(c).Height(30).Radius(4).
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
