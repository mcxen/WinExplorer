package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"
)

// settings_websearch.go is the 网页搜索 group of the 助手 settings page:
// the toggle, the provider choice and the key the web_search and web_read
// tools run on. The key stays in the local assistant.json, as the model
// providers' keys do.

// settingsAssistantWebSearchGroup holds the web tools' configuration. A
// saved key is never rendered back: the field starts blank and a blank
// save keeps the stored one, the providers' edit form does the same.
func (a *App) settingsAssistantWebSearchGroup() components.SettingsGroup {
	disk, err := loadAssistantConfig()
	ws := disk.Prefs.WebSearch
	if !a.settingsAssist.webLoaded {
		a.settingsAssist.webLoaded = true
		a.settingsAssist.webProvider = ws.Provider
		a.settingsAssist.webURL = ws.BaseURL
		a.settingsAssist.webKey = ""
	}
	items := []components.SettingsItem{}
	if err != nil {
		items = append(items, components.SettingsItem{
			ID: "web-err", Title: "无法读取配置", Description: err.Error(),
		})
		return components.SettingsGroup{ID: "websearch", Title: "网页搜索", Items: items}
	}
	keyNote := "未配置密钥"
	if ws.APIKey != "" {
		keyNote = "已保存密钥"
	}
	providerName := map[string]string{"": "Tavily", "tavily": "Tavily", "bocha": "博查"}[ws.Provider]
	desc := "供应商 " + providerName + " · " + keyNote + "。配置后助手可以用 web_search 联网搜索、用 web_read 读取网页正文。"
	if !ws.Enabled {
		desc = "未开启。开启后助手可以联网搜索（web_search）并读取网页正文（web_read）。" + " · " + keyNote
	}
	items = append(items, settingsSwitchItem("web-enabled", "启用网页搜索",
		desc,
		func() bool {
			disk, _ := loadAssistantConfig()
			return disk.Prefs.WebSearch.Enabled
		},
		func(on bool) {
			if err := updateAssistantPrefs(func(p *assistPrefs) {
				p.WebSearch.Enabled = on
			}); err != nil {
				a.statusMsg = err.Error()
				return
			}
			a.assist.refreshConfig()
		}))
	items = append(items, components.SettingsItem{
		ID: "web-form", Title: "供应商与密钥",
		Description: "Tavily（api.tavily.com）或博查 Bocha（api.bochaai.com）。密钥只保存在本机 assistant.json；留空密钥保存则保留原值。接口地址留空用官方默认。",
		Keywords:    []string{"搜索", "网页", "Tavily", "博查", "Bocha", "联网"},
		Stacked:     true,
		Build: func(c *ui.Context) {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "供应商").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
				for _, p := range []struct{ id, label string }{{"tavily", "Tavily"}, {"bocha", "博查"}} {
					on := a.settingsAssist.webProvider == p.id || (a.settingsAssist.webProvider == "" && p.id == "tavily")
					style := components.ButtonGhost
					if on {
						style = components.ButtonSecondary
					}
					p := p
					if components.Button(c, p.label, style).Clicked() {
						a.settingsAssist.webProvider = p.id
					}
				}
			})
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "密钥").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
				components.TextBox(c, &a.settingsAssist.webKey, components.TextBoxOptions{}).
					Grow(1).MinWidth(0).Height(28).FontSize(12).Placeholder("API Key").
					Label("网页搜索密钥")
			})
			a.settingsAssistField(c, "接口", &a.settingsAssist.webURL, "留空用供应商默认地址")
			ui.Row(c).FillWidth().Gap(6).Justify(ui.End).Children(func() {
				if components.Button(c, "保存网页搜索", components.ButtonSecondary).Clicked() {
					provider := a.settingsAssist.webProvider
					if provider == "" {
						provider = "tavily"
					}
					key := strings.TrimSpace(a.settingsAssist.webKey)
					url := strings.TrimSpace(a.settingsAssist.webURL)
					if err := updateAssistantPrefs(func(p *assistPrefs) {
						p.WebSearch.Provider = provider
						p.WebSearch.BaseURL = url
						if key != "" {
							p.WebSearch.APIKey = key
						}
					}); err != nil {
						a.statusMsg = err.Error()
						return
					}
					a.settingsAssist.webKey = ""
					a.settingsAssist.webLoaded = false
					a.assist.refreshConfig()
					a.statusMsg = "已保存网页搜索配置"
				}
			})
		},
	})
	return components.SettingsGroup{ID: "websearch", Title: "网页搜索", Items: items}
}
