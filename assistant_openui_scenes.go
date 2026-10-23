package main

import (
	"os"
	"strings"

	"sunvale/internal/forms"
)

// assistant_openui_scenes.go is the offline OpenUI scene demo: slash rows
// and SUNVALE_OPENUI_DEMO=1 keyword replies inject the same present_form
// presets the model would emit, without calling a model.

const openUIDemoEnv = "SUNVALE_OPENUI_DEMO"

func openUIDemoOn() bool { return os.Getenv(openUIDemoEnv) == "1" }

// assistInjectOpenUIScene appends a user line and an assistant OpenUI form
// for the given scene id (forms.Scene*). Returns false if the id is unknown.
func (a *App) assistInjectOpenUIScene(sceneID, userText string) bool {
	f, ok := forms.Scene(sceneID, nil)
	if !ok {
		return false
	}
	f.Normalize()
	if userText == "" {
		userText = "演示：" + f.Title
	}
	program := f.ToOpenUI()
	intro := "下面是「" + f.Title + "」表单（OpenUI / present_form 预设 `" + sceneID + "`）。填好后点提交即可。"
	body := intro + "\n\n" + forms.FenceOpenUI(program)
	a.assist.turns = append(a.assist.turns,
		aiTurn{Role: "user", Text: userText},
		aiTurn{Role: "assistant", Text: body},
	)
	a.seedOpenUIFormValues(program, f)
	// Point the file pane at the demo sandbox so PathField defaults exist.
	_ = forms.EnsureDemoSandbox()
	if len(a.tabs) > 0 {
		dl := forms.PreferPath("Downloads")
		cur := a.cur()
		if cur == nil || cur.path == "" || specialPath(cur.path) || openUIDemoOn() {
			a.navigate(dl)
		}
	}
	a.assist.draft = ""
	a.assist.status = ""
	a.assist.running = false
	return true
}

// seedOpenUIFormValues copies Form.Value into the live OpenUI field map so
// demo presets and present_form prefill show up without waiting for a type.
func (a *App) seedOpenUIFormValues(program string, f forms.Form) {
	key := openBlockKey(program)
	for _, field := range f.Fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		*a.openValue(key, field.ID) = field.Value
	}
}

// assistOpenUIDemoSubmit answers the draft with a scene form when demo mode
// is on. Returns true if it handled the message.
func (a *App) assistOpenUIDemoSubmit() bool {
	if !openUIDemoOn() {
		return false
	}
	text := strings.TrimSpace(a.assist.draft)
	if text == "" || a.assist.running {
		return false
	}
	id := openUIDemoMatch(text)
	if id == "" {
		return false
	}
	return a.assistInjectOpenUIScene(id, text)
}

func openUIDemoMatch(text string) string {
	switch {
	case strings.Contains(text, "定时") || strings.Contains(text, "schedule"):
		return forms.SceneSchedule
	case strings.Contains(text, "重命名") || strings.Contains(text, "rename"):
		return forms.SceneRename
	case strings.Contains(text, "分享") || strings.Contains(text, "share"):
		return forms.SceneShare
	case strings.Contains(text, "搜索") || strings.Contains(text, "过滤") || strings.Contains(text, "search"):
		return forms.SceneSearch
	case strings.Contains(text, "分类") || strings.Contains(text, "classify"):
		return forms.SceneClassify
	case strings.Contains(text, "备份") || strings.Contains(text, "backup"):
		return forms.SceneBackup
	}
	return ""
}

func (a *App) assistSlashOpenUIScenes() []assistSlashItem {
	var items []assistSlashItem
	for _, sc := range forms.SceneNames() {
		id := sc.ID
		label := sc.Label
		detail := sc.Detail
		items = append(items, assistSlashItem{
			ID: "openui-" + id, Trigger: "/表单-" + label, Label: "表单·" + label, Detail: detail,
			Kind: slashAction,
			Run: func(app *App) {
				app.assistInjectOpenUIScene(id, "打开表单："+label)
			},
		})
	}
	return items
}
