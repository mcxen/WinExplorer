package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// settingsdialog.go is the 设置 dialog shell: categories, appearance/view/
// tray/plugin/update groups, and shared settings item helpers. Extension
// editing lives in settings_ext.go (LAY1 split).
//
// The shell — sidebar, search, page title, close — is components.Settings.
// Every change writes through at once. 恢复默认 puts appearance and view
// settings back; plugins and the updater keep their own.

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
// Settings fills a fixed box: it scrolls its own page and needs a parent
// with a real height.
func (a *App) settingsModal(c *ui.Context) {
	if !a.settingsOpen {
		a.hotkeyCapture = false
		return
	}
	// A light scrim under the dialog sets it off the window behind.
	ui.Overlay(c, func() {
		ui.Box(c.Key("settings-scrim")).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.12))
	})
	// The dialog takes a reasonable share of the window: up to 960 × 640.
	w, h := c.Size()
	dw := clampF(w-120, 640, 960)
	dh := clampF(h-180, 420, 640)
	floatDialog(c, &a.settingsOpen, "设置", dw, false, func() {
		// A tighter spacing scale for the dialog: the category list and the
		// pages read denser, as Explorer's own settings pages do.
		th := c.Theme()
		dense := *th
		dense.Spacing = th.Space(1) * 0.85
		c.SetTheme(&dense)
		defer c.SetTheme(th)
		ui.Box(c).FillWidth().Height(dh).Children(func() {
			components.Settings(c, &a.settingsUI, a.settingsCategories(), components.SettingsOptions{
				Title:             "设置",
				SearchLabel:       "搜索设置",
				SearchPlaceholder: "搜索设置",
				CategoryLabel:     "类别",
				ResultsTitle:      "搜索结果",
				EmptyText:         "没有匹配的设置",
				CloseLabel:        "关闭",
				Close:             func() { a.settingsOpen = false },
				HeaderActions: func(c *ui.Context) {
					if components.Button(c, "恢复默认", components.ButtonSecondary).Clicked() {
						a.applySettings(func(x *appSettings) { *x = *defaultSettings() })
						useAppearance(a.store, mygo.ThemeSystem)
						// The stored chord is the default again; drop a
						// custom global bind or it would outlive the reset.
						_ = applySearchHotkey("")
					}
				},
			})
		})
	})
}

// applySettings persists the change and applies it to every open window
// at once, so the settings page edits what the user sees immediately.
func (a *App) applySettings(fn func(s *appSettings)) {
	a.store.updateSettings(fn)
	applyLogLevel(a.store.settings())
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

// settingsCategories is the dialog's catalog. Built every frame so a switch
// flipped in another window shows up here. IDs stay stable; titles are the
// Chinese labels.
func (a *App) settingsCategories() []components.SettingsCategory {
	return []components.SettingsCategory{
		{ID: "appearance", Title: "外观", Icon: lucide.Palette(), Groups: a.settingsAppearanceGroups()},
		{ID: "view", Title: "查看", Icon: lucide.LayoutList(), Groups: a.settingsViewGroups()},
		{ID: "openwith", Title: "打开方式", Icon: lucide.ExternalLink(), Groups: a.settingsOpenWithGroups()},
		{ID: "tray", Title: "托盘", Icon: lucide.HardDrive(), Groups: a.settingsTrayGroups()},
		{ID: "extensions", Title: "扩展", Icon: lucide.Puzzle(), Groups: a.settingsExtensionGroups()},
		{ID: "plugins", Title: "插件", Icon: lucide.Blocks(), Groups: a.settingsPluginGroups()},
		{ID: "assistant", Title: "助手", Icon: lucide.Bot(), Groups: a.settingsAssistantGroups()},
		{ID: "schedule", Title: "定时任务", Icon: lucide.AlarmClock(), Groups: a.settingsScheduleGroups()},
		{ID: "updates", Title: "更新", Icon: lucide.RefreshCw(), Groups: a.settingsUpdateGroups()},
		{ID: "logs", Title: "日志", Icon: lucide.FileText(), Groups: a.settingsLogGroups()},
	}
}

func settingsSwitchItem(id, title, desc string, get func() bool, set func(bool)) components.SettingsItem {
	return components.SettingsItem{
		ID: id, Title: title, Description: desc,
		Build: func(c *ui.Context) {
			v := get()
			sw := ui.Switch(c, &v).Shrink(0).Label(title)
			if sw.Changed() {
				set(v)
			}
		},
	}
}

func settingsSelectItem(id, title, desc, current string, options []string, set func(string)) components.SettingsItem {
	return components.SettingsItem{
		ID: id, Title: title, Description: desc,
		Build: func(c *ui.Context) {
			cur := current
			sel := ui.Select(c, &cur, options).Shrink(0)
			if sel.Changed() {
				set(cur)
			}
		},
	}
}

func settingsActionItem(id, title, desc, label string, run func()) components.SettingsItem {
	return components.SettingsItem{
		ID: id, Title: title, Description: desc,
		Build: func(c *ui.Context) {
			if components.Button(c, label, components.ButtonSecondary).Clicked() && run != nil {
				run()
			}
		},
	}
}

// appearanceIndex maps the stored 外观 setting to the segmented control's
// index: 跟随系统, 浅色, 深色.
func appearanceIndex(setting string) int {
	switch setting {
	case appearanceLight:
		return 1
	case appearanceDark:
		return 2
	}
	return 0
}

func (a *App) settingsAppearanceGroups() []components.SettingsGroup {
	v := appearanceIndex(a.store.settings().Appearance)
	return []components.SettingsGroup{{
		ID: "theme",
		Items: []components.SettingsItem{{
			ID: "mode", Title: "外观模式",
			Build: func(c *ui.Context) {
				seg := ui.Segmented(c, &v, "跟随系统", "浅色", "深色").Shrink(0)
				if seg.Changed() {
					source := []mygo.ThemeSource{
						mygo.ThemeSystem, mygo.ThemeLight, mygo.ThemeDark,
					}[v]
					useAppearance(a.store, source)
				}
			},
		}, {
			ID: "visual", Title: "视觉风格",
			Build: func(c *ui.Context) {
				sv := 0
				if a.store.settings().VisualStyle == "v2" {
					sv = 1
				}
				if ui.Segmented(c, &sv, "经典", "v2").Shrink(0).Changed() {
					a.applySettings(func(x *appSettings) { x.VisualStyle = []string{"", "v2"}[sv] })
				}
			},
		}, {
			ID: "progress", Title: "助手任务进度样式",
			Build: func(c *ui.Context) {
				pv := 0
				if a.assistProgressStyle() == progressStyleGauge {
					pv = 1
				}
				seg := ui.Segmented(c, &pv, "条形", "仪表盘").Shrink(0)
				if seg.Changed() {
					style := []string{progressStyleBar, progressStyleGauge}[pv]
					a.store.updateSettings(func(s *appSettings) { s.ProgressStyle = style })
				}
			},
		}},
	}}
}

func (a *App) settingsViewGroups() []components.SettingsGroup {
	s := a.store.settings()
	cur := viewModeLabel(ViewMode(s.Mode))
	return []components.SettingsGroup{
		{ID: "display", Title: "显示", Items: []components.SettingsItem{
			settingsSwitchItem("ext", "文件扩展名", "",
				func() bool { return a.store.settings().ShowExt },
				func(v bool) { a.applySettings(func(x *appSettings) { x.ShowExt = v }) }),
			settingsSwitchItem("hidden", "隐藏的项目", "",
				func() bool { return a.store.settings().ShowHidden },
				func(v bool) { a.applySettings(func(x *appSettings) { x.ShowHidden = v }) }),
			settingsSwitchItem("treeexpand", "展开到打开的文件夹", "导航时侧栏树自动展开到当前文件夹",
				func() bool { return a.store.settings().treeExpand() },
				func(v bool) { a.applySettings(func(x *appSettings) { x.TreeExpand, x.TreeNoExpand = &v, false }) }),
			settingsSwitchItem("checks", "项目复选框", "",
				func() bool { return a.store.settings().ItemCheckboxes },
				func(v bool) { a.applySettings(func(x *appSettings) { x.ItemCheckboxes = v }) }),
		}},
		{ID: "window", Title: "窗口", Items: []components.SettingsItem{
			settingsSwitchItem("compact", "紧凑视图", "",
				func() bool { return a.store.settings().Compact },
				func(v bool) { a.applySettings(func(x *appSettings) { x.Compact = v }) }),
			settingsSwitchItem("preview", "预览窗格", "",
				func() bool { return a.store.settings().PreviewOpen },
				func(v bool) { a.applySettings(func(x *appSettings) { x.PreviewOpen = v }) }),
			settingsSwitchItem("warmup", "网络位置缩略图预热", "先解码网络文件夹的前几张图片",
				func() bool { return a.store.settings().networkWarmup() },
				func(v bool) {
					a.applySettings(func(x *appSettings) { x.NoNetworkWarmup = !v })
				}),
			settingsSelectItem("mode", "默认视图", "", cur, viewModeLabels(), func(label string) {
				if mode, ok := viewModeNamed(label); ok {
					a.applySettings(func(x *appSettings) { x.Mode = int(mode) })
				}
			}),
		}},
		{ID: "startup", Title: "启动", Items: []components.SettingsItem{
			settingsSwitchItem("restore", "恢复上次打开的窗口与标签页", "下次启动时回到退出前的位置",
				func() bool { return a.store.settings().restoreSession() },
				func(restore bool) {
					a.applySettings(func(x *appSettings) { x.NoRestoreSession = !restore })
				}),
			settingsActionItem("clear", "会话记录", "退出时保存的窗口与标签页", "清除", func() {
				a.store.clearSession()
				a.statusMsg = "已清除会话记录"
			}),
		}},
	}
}

// settingsOpenWithGroups lists the 打开方式 defaults the user has recorded
// (openwith.go's 始终使用), each with a delete button; an empty record
// explains where the entries come from.
func (a *App) settingsOpenWithGroups() []components.SettingsGroup {
	defaults := a.store.openWithDefaults()
	exts := make([]string, 0, len(defaults))
	for ext := range defaults {
		exts = append(exts, ext)
	}
	slices.Sort(exts)
	items := make([]components.SettingsItem, 0, len(exts)+1)
	if len(exts) == 0 {
		items = append(items, components.SettingsItem{
			ID:          "empty",
			Title:       "还没有默认应用",
			Description: "在文件的右键「打开方式 ▸ 始终使用…」里记录",
		})
	}
	for _, ext := range exts {
		app := defaults[ext]
		items = append(items, components.SettingsItem{
			ID:          "ow-" + ext,
			Title:       "." + ext,
			Description: "始终使用 " + app,
			Content: func(c *ui.Context) {
				if components.Button(c, "删除", components.ButtonSecondary).Clicked() {
					a.store.setOpenWithDefault(ext, "")
					a.statusMsg = "已去掉 ." + ext + " 的默认应用"
				}
			},
		})
	}
	return []components.SettingsGroup{{ID: "defaults", Title: "始终使用", Items: items}}
}

func (a *App) settingsTrayGroups() []components.SettingsGroup {
	trayRefresh := func() {
		trayDiskWatch()
		trayRebuild()
	}
	acc := a.store.settings().SearchHotkey
	hotkeyDesc := "在任意应用中唤起快速搜索"
	if a.hotkeyCapture {
		hotkeyDesc = "按下新快捷键，Backspace 恢复默认，Esc 取消"
	}
	return []components.SettingsGroup{
		{ID: "disks", Title: "磁盘信息", Items: []components.SettingsItem{
			settingsSwitchItem("cap", "显示磁盘容量", "",
				func() bool { return trayDiskShown(a.store.settings()).capOn },
				func(v bool) {
					a.applySettings(func(x *appSettings) { x.NoTrayDiskCap = !v })
					trayRefresh()
				}),
			settingsSwitchItem("speed", "显示读写速度", "",
				func() bool { return trayDiskShown(a.store.settings()).speedOn },
				func(v bool) {
					a.applySettings(func(x *appSettings) { x.NoTrayDiskSpeed = !v })
					trayRefresh()
				}),
		}},
		{ID: "search", Title: "快速搜索", Items: []components.SettingsItem{
			{
				ID: "hotkey", Title: "快捷键", Description: hotkeyDesc,
				Build: func(c *ui.Context) { a.settingsHotkeyButton(c, acc) },
			},
			{ID: "scope", Title: "搜索范围", Description: "全部磁盘，索引即时应答、变动目录实时补全"},
			settingsSwitchItem("index", "文件索引", searchIndexSettingsLine(),
				func() bool { return !a.store.settings().NoSearchIndex },
				func(v bool) {
					a.applySettings(func(x *appSettings) { x.NoSearchIndex = !v })
					setSearchIndexOn(v)
				}),
			settingsActionItem("index-rebuild", "重建索引", "只重读修改过的目录", "重建", func() {
				if searchIndexOn() {
					searchIndexRebuild()
				}
			}),
			{ID: "sort", Title: "结果排序", Description: "Everything 式按名称排序，支持查询语法"},
		}},
	}
}

// settingsHotkeyButton is the chord control. The row's title and description
// belong to Settings; this is only the button that records the next chord.
func (a *App) settingsHotkeyButton(c *ui.Context, acc string) {
	label := shortcutLabel(acc)
	if a.hotkeyCapture {
		label = "请按键…"
	}
	btn := ui.ButtonBase(c.Key("search-hotkey")).Focusable().Height(30).
		Padding(0, 10).MinWidth(108).Radius(4).
		Background(colorAddress()).Border(1, colorDivider()).Label(label)
	if a.hotkeyCapture {
		btn.Border(1, colorAccent())
		btn.Focus()
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(colorText()).SingleLine()
	})
	if btn.Clicked() {
		a.hotkeyCapture = true
	}
	if !a.hotkeyCapture {
		return
	}
	btn.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputKeyDown || ev.Repeat {
			return ev.Kind == ui.InputKeyDown
		}
		switch ev.Key {
		case ui.KeyEscape:
			a.hotkeyCapture = false
			return true
		case ui.KeyBackspace:
			if ev.Mods == 0 {
				a.hotkeyCapture = false
				a.setSearchHotkey("")
				return true
			}
		}
		chord, ok := accelFromKey(ev.Mods, ev.Key)
		if !ok {
			return true
		}
		a.hotkeyCapture = false
		a.setSearchHotkey(chord)
		return true
	})
}

func (a *App) settingsPluginGroups() []components.SettingsGroup {
	plugins := registeredPlugins()
	groups := make([]components.SettingsGroup, 0, len(plugins))
	for _, p := range plugins {
		id := p.ID
		items := []components.SettingsItem{
			settingsSwitchItem("enabled", "启用", p.Desc,
				func() bool { return a.pluginEnabled(id) },
				func(on bool) { a.pluginSetEnabled(id, on) }),
		}
		for i, s := range a.pluginSettings(p) {
			items = append(items, pluginSettingItem(strconv.Itoa(i), s))
		}
		groups = append(groups, components.SettingsGroup{ID: id, Title: p.Name, Items: items})
	}
	return groups
}

// pluginSettingItem is one row a plugin declared. The plugin does not draw;
// the kind picks the control Settings puts on the right of the row.
func pluginSettingItem(id string, s PluginSetting) components.SettingsItem {
	switch s.Kind {
	case PluginSettingSwitch:
		return settingsSwitchItem(id, s.Name, s.Desc,
			func() bool { return s.On },
			func(on bool) {
				if s.Change == nil {
					return
				}
				if on {
					s.Change("on")
				} else {
					s.Change("off")
				}
			})
	case PluginSettingSelect:
		return settingsSelectItem(id, s.Name, s.Desc, s.Value, s.Options, func(v string) {
			if s.Change != nil {
				s.Change(v)
			}
		})
	case PluginSettingAction:
		return settingsActionItem(id, s.Name, s.Desc, s.Action, s.Run)
	default:
		return components.SettingsItem{ID: id, Title: s.Name, Description: s.Value}
	}
}

func (a *App) settingsUpdateGroups() []components.SettingsGroup {
	version := mygo.App.Version()
	if version == "" {
		version = "晴谷"
	}
	items := []components.SettingsItem{
		{ID: "version", Title: "当前版本", Description: version},
	}
	if !mygo.Updater.Enabled() {
		items = append(items, components.SettingsItem{
			ID: "unsupported", Title: "自动更新", Description: "此构建不支持自动更新",
		})
		return []components.SettingsGroup{{ID: "app", Items: items}}
	}
	items = append(items,
		settingsSwitchItem("checks", "自动检查更新", "",
			updater.AutomaticChecks, updater.SetAutomaticChecks),
		settingsSwitchItem("downloads", "自动下载并安装", "后台下载并安装更新，下次启动时生效",
			updater.AutomaticDownloads, updater.SetAutomaticDownloads),
		components.SettingsItem{
			ID: "check", Title: "检查更新",
			Build: func(c *ui.Context) {
				if components.Button(c, "检查更新", components.ButtonPrimary).Clicked() {
					updater.CheckForUpdates()
				}
			},
		},
	)
	last := "从未"
	if t := updater.LastCheck(); !t.IsZero() {
		last = t.Local().Format("2006/01/02 15:04")
	}
	items = append(items, components.SettingsItem{ID: "last", Title: "上次检查", Description: last})
	// 下载线路按顺序换线（updatemirror.go），在状态文件的 updateMirrors 里配置。
	items = append(items, components.SettingsItem{ID: "mirrors", Title: "下载线路",
		Description: strings.Join(updateMirrorList(), " → ")})
	return []components.SettingsGroup{{ID: "app", Items: items}}
}

func clampF(v, lo, hi float32) float32 {
	return min(max(v, lo), hi)
}
