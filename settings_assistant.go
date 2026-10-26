package main

import (
	"fmt"
	"strings"

	"github.com/oneo-me/onevra/source/components/chat"
	"github.com/oneo-me/onevra/source/icons/lucide"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/fileview"
)

// settings_assistant.go is the 助手 category of the system Settings dialog:
// model providers, extra system prompt, skill toggles, built-in MCP info,
// behavior (autopilot / memory) and the archive password book. Provider
// CRUD reuses saveAssistantProvider / activate / delete so the open pane
// stays in sync through the config epoch.

// settingsAssistForm is per-window draft state for the 助手 settings page.
type settingsAssistForm struct {
	editID                string
	name, url, model, key string
	providerID            string // models.dev provider the form was filled from
	catPick               chat.PickerState
	catOptions            []chat.Option
	vision                string // assistant.VisionAuto / VisionOn / VisionOff
	extraDraft            string
	extraLoaded           bool
	pwName, pwSecret      string
	// web search drafts; webLoaded mirrors extraLoaded for the new group.
	webProvider, webKey, webURL string
	webLoaded                   bool
}

// openAssistantSettings focuses the Settings dialog on the 助手 category.
func (a *App) openAssistantSettings() {
	a.settingsOpen = true
	a.settingsUI.Category = "assistant"
	a.settingsUI.Query = ""
	a.assist.showHist = false
	a.assist.showProv = false
	a.assist.showBook = false
	a.settingsAssist.extraLoaded = false
}

func (a *App) settingsAssistantGroups() []components.SettingsGroup {
	return []components.SettingsGroup{
		a.settingsAssistantModelsGroup(),
		a.settingsAssistantWebSearchGroup(),
		a.settingsAssistantReaderGroup(),
		a.settingsAssistantPromptGroup(),
		a.settingsAssistantSkillsGroup(),
		a.settingsAssistantMCPGroup(),
		a.settingsAssistantBehaviorGroup(),
		a.settingsAssistantPasswordsGroup(),
	}
}

func (a *App) settingsAssistantModelsGroup() components.SettingsGroup {
	disk, err := loadAssistantConfig()
	items := []components.SettingsItem{}
	if err != nil {
		items = append(items, components.SettingsItem{
			ID: "models-err", Title: "无法读取配置", Description: err.Error(),
		})
	} else {
		for _, p := range disk.Providers {
			id := p.ID
			label := p.Name
			desc := p.Model + " · " + p.BaseURL
			if info, ok := p.CatalogueInfo(); ok {
				if badges := assistant.ModelBadges(info); badges != "" {
					desc += " · " + badges
				}
				if ctx := assistant.FormatContext(info.Context); ctx != "" {
					desc += " · 上下文 " + ctx
				}
			}
			if p.ID == disk.ActiveID {
				label = "当前 · " + label
			}
			keyNote := "未配置密钥"
			if p.APIKey != "" {
				keyNote = "已保存密钥"
			}
			desc = desc + " · " + keyNote + " · " + visionLabel(p)
			pid := id
			items = append(items, components.SettingsItem{
				ID: "prov-" + id, Title: label, Description: desc, Keywords: []string{"供应商", "模型", "密钥"},
				Build: func(c *ui.Context) {
					ui.Row(c).Gap(4).Shrink(0).Children(func() {
						if p.ID != disk.ActiveID {
							if components.Button(c, "设为当前", components.ButtonGhost).Clicked() {
								_ = activateAssistantProvider(pid)
								a.assist.refreshConfig()
							}
						}
						if components.Button(c, "编辑", components.ButtonGhost).Clicked() {
							a.settingsAssist.editID = pid
							a.settingsAssist.name = p.Name
							a.settingsAssist.url = p.BaseURL
							a.settingsAssist.model = p.Model
							a.settingsAssist.key = ""
							a.settingsAssist.vision = p.Vision
						}
						if components.Button(c, "删除", components.ButtonGhost).Clicked() {
							_ = deleteAssistantProvider(pid)
							if a.settingsAssist.editID == pid {
								a.settingsAssist = settingsAssistForm{}
							}
							a.assist.refreshConfig()
						}
					})
				},
			})
		}
	}
	formTitle := "添加供应商"
	if a.settingsAssist.editID != "" {
		formTitle = "编辑供应商"
	}
	items = append(items, components.SettingsItem{
		ID: "prov-form", Title: formTitle,
		Description: "密钥只保存在本机 assistant.json。编辑时留空密钥则保留原值。",
		Keywords:    []string{"API", "OpenAI", "密钥"},
		Stacked:     true,
		Build: func(c *ui.Context) {
			a.settingsAssistCatalogueRow(c)
			a.settingsAssistField(c, "名称", &a.settingsAssist.name, "OpenAI")
			a.settingsAssistField(c, "接口", &a.settingsAssist.url, "https://api.openai.com/v1")
			a.settingsAssistField(c, "模型", &a.settingsAssist.model, "gpt-4o-mini")
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "密钥").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
				components.TextBox(c, &a.settingsAssist.key, components.TextBoxOptions{}).
					Grow(1).MinWidth(0).Height(28).FontSize(12).
					Placeholder("sk-…").Label("密钥").Password()
			})
			a.settingsAssistVisionRow(c)
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				label := "保存"
				if a.settingsAssist.editID == "" {
					label = "添加"
				}
				if components.Button(c, label, components.ButtonSecondary).Clicked() {
					a.settingsAssistSaveProvider()
				}
				if a.settingsAssist.editID != "" {
					if components.Button(c, "取消编辑", components.ButtonGhost).Clicked() {
						a.settingsAssist = settingsAssistForm{extraDraft: a.settingsAssist.extraDraft, extraLoaded: a.settingsAssist.extraLoaded}
					}
				}
			})
		},
	})
	return components.SettingsGroup{ID: "models", Title: "模型与密钥", Items: items}
}

// settingsAssistVisionRow is the 看图 selector: auto asks the model name,
// on forces pictures even at a failed request, off keeps a text-only route
// private and cheap.
func (a *App) settingsAssistVisionRow(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "看图").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
		for _, opt := range []struct {
			label, value string
		}{
			{"自动判断", assistant.VisionAuto},
			{"总是看图", assistant.VisionOn},
			{"不看图", assistant.VisionOff},
		} {
			opt := opt
			style := components.ButtonGhost
			if a.settingsAssist.vision == opt.value {
				style = components.ButtonSecondary
			}
			if components.Button(c, opt.label, style).Clicked() {
				a.settingsAssist.vision = opt.value
			}
		}
	})
	guess := visionGuess(a.settingsAssist.model)
	source := "按模型名判断"
	if _, ok := (aiProvider{Model: a.settingsAssist.model}).CatalogueInfo(); ok {
		source = "models.dev 模态数据"
	}
	hint := source + "：当前模型" + map[bool]string{true: "会看图", false: "不看图"}[guess] + "。图片、PDF 页和视频帧都会直接发给模型。"
	if a.settingsAssist.vision == assistant.VisionOn {
		hint = "强制看图。模型不支持时会自动退回纯文字。"
	} else if a.settingsAssist.vision == assistant.VisionOff {
		hint = "不发送图片，只给文字描述与文件元数据。"
	}
	ui.Text(c, hint).FontSize(11).TextColor(colorMuted()).FillWidth().Wrap()
}

// settingsAssistantReaderGroup lists what this machine can extract, so a
// missing frame or a PDF that came back as a picture has an explanation one
// click away instead of a support question.
func (a *App) settingsAssistantReaderGroup() components.SettingsGroup {
	return components.SettingsGroup{
		ID: "reader", Title: "文件读取",
		Items: []components.SettingsItem{{
			ID: "reader-backends", Title: "读取后端",
			Description: "助手按文件类型选择读取方式：内置解析优先，缺什么再用命令行工具或 Python 脚本兜底。",
			Keywords:    []string{"读取", "图片", "视频", "ffmpeg", "Python", "PDF"},
			Stacked:     true,
			Build: func(c *ui.Context) {
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					for _, line := range fileview.ExtractorStatus() {
						ui.Text(c, line).FontSize(11).TextColor(colorText()).Wrap()
					}
				})
			},
		}},
	}
}

func visionLabel(p aiProvider) string {
	switch p.Vision {
	case assistant.VisionOn:
		return "总是看图"
	case assistant.VisionOff:
		return "不看图"
	}
	if visionGuess(p.Model) {
		return "自动·看图"
	}
	return "自动·不看图"
}

// visionGuess is the auto answer for one model: models.dev first, name
// heuristic when the catalogue does not know it.
func visionGuess(model string) bool {
	if info, ok := (aiProvider{Model: model}).CatalogueInfo(); ok {
		return info.SeesImages()
	}
	return assistant.ModelSeesImages(model)
}

func (a *App) settingsAssistSaveProvider() {
	in := assistantConfigInput{
		ID: a.settingsAssist.editID, Name: a.settingsAssist.name,
		BaseURL: a.settingsAssist.url, Model: a.settingsAssist.model,
		APIKey: a.settingsAssist.key, Vision: a.settingsAssist.vision,
		Active: a.settingsAssist.editID == "",
	}
	if _, err := saveAssistantProvider(in); err != nil {
		a.statusMsg = err.Error()
		return
	}
	a.settingsAssist.editID = ""
	a.settingsAssist.name, a.settingsAssist.url, a.settingsAssist.model, a.settingsAssist.key = "", "", "", ""
	a.assist.refreshConfig()
	a.statusMsg = "已保存供应商"
}

func (a *App) settingsAssistField(c *ui.Context, label string, value *string, hint string) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
		components.TextBox(c, value, components.TextBoxOptions{}).Grow(1).MinWidth(0).
			Height(28).FontSize(12).Placeholder(hint)
	})
}

func (a *App) settingsAssistantPromptGroup() components.SettingsGroup {
	disk, _ := loadAssistantConfig()
	if !a.settingsAssist.extraLoaded {
		a.settingsAssist.extraDraft = disk.Prefs.ExtraInstruction
		a.settingsAssist.extraLoaded = true
	}
	return components.SettingsGroup{
		ID: "prompt", Title: "系统提示词",
		Items: []components.SettingsItem{{
			ID: "extra", Title: "附加提示词",
			Description: "内置 Instruction 固定不可改。附加内容会接在内置提示词后面，用空行分隔。",
			Keywords:    []string{"提示词", "Instruction", "系统"},
			Stacked:     true,
			Build: func(c *ui.Context) {
				ui.Text(c, "内置系统提示词由应用维护，列出全部 MCP 工具与行为规则，不能在此替换。").
					FontSize(11).TextColor(colorMuted()).Wrap()
				components.TextBox(c, &a.settingsAssist.extraDraft, components.TextBoxOptions{Multiline: true}).
					FillWidth().MinHeight(96).FontSize(12).Lines(1, 6).
					Placeholder("例如：回复尽量简短；优先用中文。").Label("附加提示词")
				ui.Row(c).Gap(6).Children(func() {
					if components.Button(c, "保存附加提示词", components.ButtonSecondary).Clicked() {
						draft := a.settingsAssist.extraDraft
						if err := updateAssistantPrefs(func(p *assistPrefs) {
							p.ExtraInstruction = strings.TrimSpace(draft)
						}); err != nil {
							a.statusMsg = err.Error()
						} else {
							a.settingsAssist.extraDraft = strings.TrimSpace(draft)
							a.assist.refreshConfig()
							a.statusMsg = "已保存附加提示词"
						}
					}
					if components.Button(c, "恢复附加为空", components.ButtonGhost).Clicked() {
						a.settingsAssist.extraDraft = ""
						if err := updateAssistantPrefs(func(p *assistPrefs) {
							p.ExtraInstruction = ""
						}); err != nil {
							a.statusMsg = err.Error()
						} else {
							a.assist.refreshConfig()
							a.statusMsg = "已清空附加提示词"
						}
					}
				})
			},
		}},
	}
}

func (a *App) settingsAssistantSkillsGroup() components.SettingsGroup {
	skillSwitch := func(id, title, desc, skill string) components.SettingsItem {
		return settingsSwitchItem(id, title, desc,
			func() bool {
				disk, _ := loadAssistantConfig()
				return !assistant.SkillDisabled(disk.Prefs, skill)
			},
			func(on bool) {
				_ = updateAssistantPrefs(func(p *assistPrefs) {
					p.DisabledSkills = assistant.SetSkillDisabled(p.DisabledSkills, skill, !on)
				})
				a.assist.refreshConfig()
			})
	}
	return components.SettingsGroup{
		ID: "skills", Title: "Skills",
		Items: []components.SettingsItem{
			skillSwitch("skill-files", "文件能力", "关闭后 A2A 卡片不再声明 files 技能（本机工具仍可用）。", assistant.SkillFiles),
			skillSwitch("skill-memory", "记忆能力", "关闭后不向模型注入记忆，也不提供记忆工具入口；会话抽屉隐藏记忆列表。", assistant.SkillMemory),
			skillSwitch("skill-archive", "压缩与密码", "可选标记；解压密码本仍可在下方管理。", assistant.SkillArchive),
			skillSwitch("skill-web", "网页搜索", "关闭后 web_search 与 web_read 即使配置了也不接受调用。", assistant.SkillWeb),
		},
	}
}

func (a *App) settingsAssistantMCPGroup() components.SettingsGroup {
	tools := sunvaleBuiltinToolNames()
	toolDesc := fmt.Sprintf("共 %d 个工具：%s", len(tools), strings.Join(tools, "、"))
	items := []components.SettingsItem{{
		ID: "mcp-local", Title: "进程内 MCP",
		Description: "名称 sunvale。助手与 A2A 共用这一份工具注册表。",
		Keywords:    []string{"MCP", "sunvale", "工具"},
		Stacked:     true,
		Build: func(c *ui.Context) {
			ui.Text(c, toolDesc).FontSize(11).TextColor(colorMuted()).Wrap()
		},
	}}
	if a.pluginEnabled(remotePluginID) {
		url := strings.TrimSuffix(a.remoteLANURL(), "/") + "/mcp"
		hint := "远程访问插件已启用时，外部客户端可使用 Streamable HTTP MCP（需管理员令牌）。"
		if !a.remoteConf().Enabled {
			hint += " 服务未开启时请先在命令栏打开远程访问。"
		}
		items = append(items, components.SettingsItem{
			ID: "mcp-remote", Title: "远程 MCP", Description: hint,
			Keywords: []string{"MCP", "远程", "HTTP"},
			Stacked:  true,
			Build: func(c *ui.Context) {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, url).FontSize(12).TextColor(colorText()).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
					if components.Button(c, "复制", components.ButtonSecondary).Clicked() {
						mygo.Clipboard.WriteText(url)
						a.statusMsg = "已复制远程 MCP 地址"
					}
				})
			},
		})
	} else {
		items = append(items, components.SettingsItem{
			ID: "mcp-remote-off", Title: "远程 MCP",
			Description: "启用「远程访问」插件并开启服务后，这里会显示 MCP 地址。",
		})
	}
	return components.SettingsGroup{ID: "mcp", Title: "MCP", Items: items}
}

func (a *App) settingsAssistantBehaviorGroup() components.SettingsGroup {
	root := assistantSideRootFor(assistantConfigPath())
	hist := "没有用户配置目录"
	if root != "" {
		hist = homeish(root)
	}
	return components.SettingsGroup{
		ID: "behavior", Title: "行为与记忆",
		Items: []components.SettingsItem{
			settingsSwitchItem("autopilot", "默认全托管",
				"新会话与远程调用的默认值；窗格上的全托管开关仍可临时覆盖并写回。",
				func() bool {
					disk, _ := loadAssistantConfig()
					return disk.Prefs.Autopilot
				},
				func(on bool) {
					_ = updateAssistantPrefs(func(p *assistPrefs) { p.Autopilot = on })
					a.assist.refreshConfig()
				}),
			settingsSwitchItem("memory-on", "启用记忆",
				"与 Skills 中的记忆能力同步。关闭后不注入记忆、不提供记忆工具入口。",
				func() bool {
					disk, _ := loadAssistantConfig()
					return !assistant.SkillDisabled(disk.Prefs, assistant.SkillMemory)
				},
				func(on bool) {
					_ = updateAssistantPrefs(func(p *assistPrefs) {
						p.DisabledSkills = assistant.SetSkillDisabled(p.DisabledSkills, assistant.SkillMemory, !on)
					})
					a.assist.refreshConfig()
				}),
			settingsActionItem("clear-memory", "清空全部记忆", "删除 memory.json 中的短事实，不可恢复。", "清空记忆", func() {
				if err := clearAssistantMemories(); err != nil {
					a.statusMsg = err.Error()
					return
				}
				a.assist.ensure()
				a.assist.memories = nil
				a.statusMsg = "已清空记忆"
			}),
			{
				ID:    "freq-files",
				Title: "常用文件记录",
				Description: fmt.Sprintf("使用频率账本共 %d 条：打开、访问、固定和拖给助手的文件都会积累，供助手回答「我常看的」类问题。",
					a.storeFreqSnapshot()),
				Keywords: []string{"常用", "频率", "使用记录"},
				Build: func(c *ui.Context) {
					if a.store == nil {
						return
					}
					if components.Button(c, "清除并重新开始", components.ButtonGhost).Clicked() {
						n := a.store.clearFreq()
						a.statusMsg = fmt.Sprintf("已清除 %d 条常用文件记录", n)
					}
				},
			},
			{
				ID: "history-path", Title: "会话与记忆路径", Description: hist,
				Keywords: []string{"路径", "历史", "sessions"},
			},
		},
	}
}

func (a *App) settingsAssistantPasswordsGroup() components.SettingsGroup {
	disk, _ := loadAssistantConfig()
	items := []components.SettingsItem{}
	if len(disk.ArchivePasswords) == 0 {
		items = append(items, components.SettingsItem{
			ID: "pw-empty", Title: "还没有解压密码",
			Description: "选中归档执行「全部解压」时按此列表从上到下试用；密码不会发给模型。",
		})
	}
	for i, pw := range disk.ArchivePasswords {
		id := pw.ID
		label := pw.Label
		if label == "" {
			label = fmt.Sprintf("解压密码 %d", i+1)
		}
		items = append(items, components.SettingsItem{
			ID: "pw-" + id, Title: label, Description: "已保存（不显示正文）",
			Keywords: []string{"解压", "密码本", "zip"},
			Build: func(c *ui.Context) {
				if components.Button(c, "删除", components.ButtonGhost).Clicked() {
					_ = deleteAssistantArchivePassword(id)
					a.assist.refreshConfig()
				}
			},
		})
	}
	items = append(items, components.SettingsItem{
		ID: "pw-add", Title: "添加解压密码",
		Description: "名称便于识别；密码只写在本机配置里。",
		Stacked:     true,
		Build: func(c *ui.Context) {
			a.settingsAssistField(c, "名称", &a.settingsAssist.pwName, "工作压缩包")
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "密码").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
				components.TextBox(c, &a.settingsAssist.pwSecret, components.TextBoxOptions{}).
					Grow(1).MinWidth(0).Height(28).FontSize(12).
					Placeholder("解压密码").Label("解压密码").Password()
			})
			if components.Button(c, "保存解压密码", components.ButtonSecondary).Clicked() {
				if err := saveAssistantArchivePassword(a.settingsAssist.pwName, a.settingsAssist.pwSecret); err != nil {
					a.statusMsg = err.Error()
				} else {
					a.settingsAssist.pwName, a.settingsAssist.pwSecret = "", ""
					a.assist.refreshConfig()
					a.statusMsg = "已保存解压密码"
				}
			}
		},
	})
	return components.SettingsGroup{ID: "passwords", Title: "解压密码本", Items: items}
}

// settingsAssistCatalogueRow is the models.dev shortcut above the provider
// form: pick a catalogue provider and the name and endpoint fields fill in,
// so adding a known provider is one click plus a key. Only providers that
// publish an OpenAI-compatible endpoint are offered — sunvale speaks that
// shape and nothing else.
func (a *App) settingsAssistCatalogueRow(c *ui.Context) {
	cat := currentCatalogue()
	if cat == nil {
		ensureModelsDevCatalogue()
		return
	}
	provs := cat.OpenAICompatibleProviders()
	if len(provs) == 0 {
		return
	}
	if a.settingsAssist.catPick.Open {
		// Rebuild while open so a just-loaded catalogue shows.
		a.settingsAssist.catOptions = a.settingsAssist.catOptions[:0]
		for _, p := range provs {
			a.settingsAssist.catOptions = append(a.settingsAssist.catOptions, chat.Option{
				ID: p.ID, Label: p.Name, Group: "models.dev 供应商",
				Icon:   providerLogo(p.ID).icon(),
				Detail: detailEndpoint(p.API),
			})
		}
	}
	res := chat.Picker(c, &a.settingsAssist.catPick, chat.PickerOptions{
		Label: "从 models.dev 选择", Icon: lucide.Sparkles,
		SearchLabel: "搜索供应商", EmptyText: "没有匹配的供应商",
		Options: a.settingsAssist.catOptions, Width: 360,
	})
	if res.Chosen == "" {
		return
	}
	prov := cat.Providers[res.Chosen]
	name, api, _ := cat.LookupProvider(res.Chosen)
	a.settingsAssist.name = name
	a.settingsAssist.url = api
	a.settingsAssist.providerID = res.Chosen
	_ = prov
	a.statusMsg = "已填入 " + name + " 的接口地址，补上密钥和模型即可"
}

func detailEndpoint(api string) string {
	if len(api) > 42 {
		return api[:42] + "…"
	}
	return api
}
