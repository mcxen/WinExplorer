package main

import (
	"os"
	"path/filepath"
	"strconv"
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
	if strings.Contains(text, "图文") || strings.Contains(text, "medialist") {
		a.assistInjectMediaDemo(text)
		return true
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
	case strings.Contains(text, "清单") || strings.Contains(text, "filelist") || strings.Contains(text, "批量文件"):
		return forms.SceneFileList
	case strings.Contains(text, "图片预览") || strings.Contains(text, "image"):
		return forms.SceneImage
	case strings.Contains(text, "视频") || strings.Contains(text, "video") || strings.Contains(text, "关键帧"):
		return forms.SceneVideo
	case strings.Contains(text, "磁盘") || strings.Contains(text, "看板") || strings.Contains(text, "disk") ||
		strings.Contains(text, "概览") || strings.Contains(text, "dashboard"):
		return forms.SceneDisk
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

// openUIMediaDemoProgram is the offline MediaList the 图文 demo keyword
// injects: the sandbox's picture, clip, PDF and a note (icon fallback).
// SUNVALE_DEMO_MEDIA_DIR may point at a folder of real samples instead.
func openUIMediaDemoProgram() string {
	root := forms.EnsureDemoSandbox()
	pick := map[string]string{
		"image": filepath.Join(root, "Downloads", "demo-photo.png"),
		"video": filepath.Join(root, "Movies", "demo-clip.mp4"),
		"doc":   filepath.Join(root, "Documents", "报告.pdf"),
	}
	if dir := os.Getenv("SUNVALE_DEMO_MEDIA_DIR"); dir != "" {
		if ents, err := os.ReadDir(dir); err == nil {
			for _, e := range ents {
				ext := lowerExt(e.Name())
				kind := ""
				switch {
				case isImageExt(ext):
					kind = "image"
				case isVideoExt(ext):
					kind = "video"
				case ext == "pdf":
					kind = "doc"
				}
				if kind != "" {
					pick[kind] = filepath.Join(dir, e.Name())
				}
			}
		}
	}
	note := filepath.Join(root, "Downloads", "会议纪要.md")
	q := func(s string) string { return strconv.Quote(filepath.ToSlash(s)) }
	return "root = MediaList([m1, m2, m3, m4], \"整理素材\", \"numbered\")\n" +
		"m1 = MediaItem(" + q(pick["image"]) + ", \"选定封面\", \"横图构图完整，色彩饱和度适中，适合作为相册封面；原图保留在下载文件夹。\", \"image\")\n" +
		"m2 = MediaItem(" + q(pick["video"]) + ", \"截取开场片段\", \"保留前 5 秒作为片头，其余部分归档到影片文件夹。点缩略图可逐帧预览。\", \"video\")\n" +
		"m3 = MediaItem(" + q(pick["doc"]) + ", \"核对报告\", \"第 1 页的数据与表格一致，可直接分享。\", \"doc\")\n" +
		"m4 = MediaItem(" + q(note) + ", \"补充会议纪要\", \"没有可用缩略图时显示文件图标。\")\n"
}

func (a *App) assistInjectMediaDemo(userText string) {
	body := "素材已按顺序整理：\n\n" + forms.FenceOpenUI(openUIMediaDemoProgram())
	a.assist.turns = append(a.assist.turns,
		aiTurn{Role: "user", Text: userText},
		aiTurn{Role: "assistant", Text: body},
	)
	a.assist.draft = ""
	a.assist.status = ""
	a.assist.running = false
}
