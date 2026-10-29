package main

import (
	"github.com/oneo-me/onevra/source/components/chat"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/fileview"
)

// settings_assistant.go is the 助手 category entry: memory and model
// provider groups (including vision and catalogue). Prompt/skills/MCP/
// behavior/password book live in settings_assistant_more.go (LAY1 split).
//
// Provider CRUD reuses saveAssistantProvider / activate / delete so the open
// pane stays in sync through the config epoch.

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
	mcpTesting                  bool
	mcpStatus                   string
	mcpError                    string
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
		a.settingsAssistantMemoryGroup(),
		a.settingsAssistantPasswordsGroup(),
	}
}

// settingsAssistantMemoryGroup lists what the assistant remembers across
// sessions (moved here from the history column, CHATHIST).
func (a *App) settingsAssistantMemoryGroup() components.SettingsGroup {
	a.assist.ensure()
	items := []components.SettingsItem{}
	for _, m := range append([]aiMemory(nil), a.assist.memories...) {
		id := m.ID
		items = append(items, components.SettingsItem{
			ID: "mem-" + id, Title: m.Text, Keywords: []string{"记忆", "记住"},
			Build: func(c *ui.Context) {
				if components.Button(c, "删除", components.ButtonGhost).Clicked() {
					a.assist.deleteMemory(id)
				}
			},
		})
	}
	items = append(items, components.SettingsItem{
		ID: "mem-add", Title: "添加记忆", Keywords: []string{"记忆", "记住"}, Stacked: true,
		Build: func(c *ui.Context) {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				components.TextBox(c, &a.assist.memDraft, components.TextBoxOptions{}).
					Grow(1).MinWidth(0).Height(28).FontSize(12).Placeholder("下载按月份归档").Label("记忆")
				if components.Button(c, "记住", components.ButtonSecondary).Clicked() {
					if msg, err := a.assist.addMemory(a.assist.memDraft); err != nil {
						a.statusMsg = err.Error()
					} else {
						a.assist.memDraft = ""
						a.statusMsg = msg
					}
				}
			})
		},
	})
	return components.SettingsGroup{ID: "memory", Title: "记忆", Items: items}
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
