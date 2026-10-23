package forms

import (
	"path/filepath"
)

// ScheduleAdd builds the Settings「添加任务」schema. Prefill overrides empty
// defaults (edit mode). The painter keeps values in a map keyed by Field.ID.
func ScheduleAdd(prefill map[string]string) Form {
	src := PreferPath("Downloads")
	dest := PreferPath("Documents")
	get := func(id, def string) string {
		if prefill != nil {
			if v, ok := prefill[id]; ok && v != "" {
				return v
			}
		}
		return def
	}
	f := Form{
		ID:          "schedule_add",
		Title:       "添加任务",
		Description: "时间按本机时区。文件规则只按扩展名/匹配处理源目录直接子项；按内容分类请选「助手执行」与预设。",
		Fields: []Field{
			{ID: "name", Kind: KindText, Label: "名称", Placeholder: "每晚整理下载文档", Required: true, Value: get("name", "")},
			{ID: "time", Kind: KindTime, Label: "时间", Placeholder: "19:00", Required: true, Pattern: `^\d{1,2}:\d{2}$`, Value: get("time", "19:00")},
			{ID: "days", Kind: KindDays, Label: "重复", Options: []string{"每天", "工作日", "周末"}, Value: get("days", "每天")},
			{ID: "action", Kind: KindSelect, Label: "动作", Options: []string{"文件规则", "运行脚本", "助手执行"}, Value: get("action", "文件规则")},
			// File rule
			{ID: "source", Kind: KindPath, Label: "源目录", Placeholder: "~/Downloads", PathMode: PathFolder, Value: get("source", src)},
			{ID: "dest", Kind: KindPath, Label: "目标", Placeholder: "~/Documents", PathMode: PathFolder, Value: get("dest", dest)},
			{ID: "match", Kind: KindText, Label: "匹配", Placeholder: ".pdf,.docx 或 *.md", Value: get("match", ".pdf,.docx,.doc,.xlsx,.pptx,.txt,.md")},
			{ID: "op", Kind: KindSegmented, Label: "操作", Options: []string{"移动", "复制"}, Value: get("op", "移动")},
			{ID: "date_stamp", Kind: KindSwitch, Label: "按日期建子文件夹", Value: get("date_stamp", "false")},
			// Script
			{ID: "script_path", Kind: KindPath, Label: "脚本", Placeholder: "留空则用下方内联脚本", PathMode: PathFile, Value: get("script_path", "")},
			{ID: "inline", Kind: KindTextArea, Label: "内联脚本", Placeholder: "内联 Python / Shell，保存到 sunvale/scripts/", Value: get("inline", "")},
			// Assistant
			{ID: "preset", Kind: KindSelect, Label: "助手预设", Options: []string{"自定义", "分类下载", "收件箱整理", "按内容重命名"}, Value: get("preset", "自定义")},
			{ID: "prompt", Kind: KindTextArea, Label: "提示词", Placeholder: "可编辑；选预设会填入模板，仍可改", Value: get("prompt", "")},
			{ID: "folder", Kind: KindPath, Label: "目录", Placeholder: "助手工作目录，可空", PathMode: PathFolder, Value: get("folder", "")},
			{ID: "dest_root", Kind: KindPath, Label: "目标根", Placeholder: "分类预设的目标根，可空", PathMode: PathFolder, Value: get("dest_root", "")},
			{ID: "timeout", Kind: KindNumber, Label: "超时", Placeholder: "秒", Value: get("timeout", "300")},
		},
		Actions: []Action{
			{ID: "save", Label: "添加", Variant: "primary", Message: "保存定时任务"},
		},
	}
	if get("edit_id", "") != "" {
		f.Title = "编辑任务"
		f.Actions[0].Label = "保存"
	}
	return f
}

// ScheduleExampleFill returns values for「每晚整理下载文档」.
func ScheduleExampleFill() map[string]string {
	return map[string]string{
		"name":   "每晚整理下载文档",
		"time":   "19:00",
		"days":   "每天",
		"action": "文件规则",
		"source": PreferPath("Downloads"),
		"dest":   PreferPath(filepath.Join("Documents", "来自下载")),
		"match":  ".pdf,.docx,.doc,.xlsx,.pptx,.txt,.md",
		"op":     "移动",
	}
}

// VisibleForAction returns field IDs that should show for a given 动作 value.
func VisibleForAction(action string) map[string]bool {
	vis := map[string]bool{
		"name": true, "time": true, "days": true, "action": true,
	}
	switch action {
	case "运行脚本":
		vis["script_path"] = true
		vis["inline"] = true
		vis["timeout"] = true
	case "助手执行":
		vis["preset"] = true
		vis["prompt"] = true
		vis["folder"] = true
		vis["dest_root"] = true
		vis["timeout"] = true
	default:
		vis["source"] = true
		vis["dest"] = true
		vis["match"] = true
		vis["op"] = true
		vis["date_stamp"] = true
	}
	return vis
}

// PresetSelectOptions are the Settings / present_form labels for assistant presets.
func PresetSelectOptions() []string {
	return []string{"自定义", "分类下载", "收件箱整理", "按内容重命名"}
}

// PresetLabelFromID maps schedule preset id to the select label.
func PresetLabelFromID(id string) string {
	switch id {
	case "classify_downloads":
		return "分类下载"
	case "inbox_triage":
		return "收件箱整理"
	case "rename_by_content":
		return "按内容重命名"
	default:
		return "自定义"
	}
}

// PresetIDFromLabel maps the select label to schedule preset id (empty = custom).
func PresetIDFromLabel(label string) string {
	switch label {
	case "分类下载":
		return "classify_downloads"
	case "收件箱整理":
		return "inbox_triage"
	case "按内容重命名":
		return "rename_by_content"
	default:
		return ""
	}
}
