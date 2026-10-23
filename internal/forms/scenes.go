package forms

import (
	"path/filepath"
)

// Scene IDs accepted by present_form preset=… and the OpenUI demo slash.
const (
	SceneSchedule = "schedule"
	SceneRename   = "rename"
	SceneShare    = "share"
	SceneSearch   = "search"
	SceneClassify = "classify"
	SceneBackup   = "backup"
)

// SceneNames is the ordered demo catalog (Chinese labels for slash / docs).
func SceneNames() []struct{ ID, Label, Detail string } {
	return []struct{ ID, Label, Detail string }{
		{SceneSchedule, "创建定时任务", "路径 + 时间 + 星期 + 动作"},
		{SceneRename, "批量重命名", "模式 + 预览表选择"},
		{SceneShare, "分享设置", "过期、密码、权限"},
		{SceneSearch, "高级搜索过滤", "类型 chips + 标签 + 滑块"},
		{SceneClassify, "AI 分类确认", "目标文件夹多选"},
		{SceneBackup, "备份任务向导", "源目标 + 进度 + 确认按钮组"},
	}
}

// Scene builds a demo / present_form preset by id. Unknown ids return ok=false.
func Scene(id string, prefill map[string]string) (Form, bool) {
	get := func(k, def string) string {
		if prefill != nil {
			if v, ok := prefill[k]; ok && v != "" {
				return v
			}
		}
		return def
	}
	switch id {
	case SceneSchedule, "定时任务", "sched":
		f := ScheduleAdd(prefill)
		return f, true
	case SceneRename, "批量重命名", "batch_rename":
		return Form{
			ID:          "rename_rule",
			Title:       "批量重命名规则",
			Description: "用模式生成新名；勾选预览表里要应用的行后提交。",
			Fields: []Field{
				{ID: "pattern", Kind: KindText, Label: "匹配模式", Placeholder: "*.jpg 或 IMG_*.HEIC", Required: true, Value: get("pattern", "IMG_*.jpg")},
				{ID: "template", Kind: KindText, Label: "新名模板", Placeholder: "旅行_{n:03}_{date}", Required: true, Value: get("template", "旅行_{n:03}")},
				{ID: "start", Kind: KindNumber, Label: "起始序号", Placeholder: "1", Value: get("start", "1")},
				{ID: "case", Kind: KindSegmented, Label: "大小写", Options: []string{"保持", "小写", "大写"}, Value: get("case", "保持")},
				{ID: "dry_run", Kind: KindSwitch, Label: "仅预览不写入", Value: get("dry_run", "true")},
				{ID: "rows", Kind: KindTableSelect, Label: "预览（勾选应用）", Required: true, Options: []string{
					"IMG_0001.jpg → 旅行_001.jpg",
					"IMG_0002.jpg → 旅行_002.jpg",
					"IMG_0003.jpg → 旅行_003.jpg",
					"截图 10.png → （不匹配）",
				}, Value: get("rows", "IMG_0001.jpg → 旅行_001.jpg,IMG_0002.jpg → 旅行_002.jpg,IMG_0003.jpg → 旅行_003.jpg")},
			},
			Actions: []Action{
				{ID: "apply", Label: "应用重命名", Variant: "primary", Message: "按表单应用批量重命名"},
				{ID: "cancel", Label: "取消", Variant: "secondary", Message: "取消批量重命名"},
			},
		}, true
	case SceneShare, "分享设置", "share_settings":
		return Form{
			ID:          "share_settings",
			Title:       "分享设置",
			Description: "生成链接前确认过期、密码与权限。",
			Fields: []Field{
				{ID: "path", Kind: KindPath, Label: "分享内容", Placeholder: "报告.pdf", PathMode: PathFile, Required: true, Value: get("path", PreferPath(filepath.Join("Documents", "报告.pdf")))},
				{ID: "expire", Kind: KindDate, Label: "过期日期", Placeholder: "YYYY-MM-DD", Required: true, Value: get("expire", "")},
				{ID: "perm", Kind: KindSegmented, Label: "权限", Options: []string{"仅查看", "可下载", "可编辑"}, Value: get("perm", "可下载")},
				{ID: "need_pass", Kind: KindSwitch, Label: "需要密码", Value: get("need_pass", "true")},
				{ID: "password", Kind: KindPassword, Label: "访问密码", Placeholder: "至少 6 位", Value: get("password", "")},
				{ID: "notify", Kind: KindSwitch, Label: "有人打开时通知我", Value: get("notify", "false")},
			},
			Actions: []Action{
				{ID: "create", Label: "创建分享链接", Variant: "primary", Message: "按表单创建分享链接"},
				{ID: "copy", Label: "仅复制设置", Variant: "secondary", Message: "复制分享设置到对话"},
			},
		}, true
	case SceneSearch, "高级搜索", "search_filter":
		return Form{
			ID:          "search_filter",
			Title:       "高级搜索过滤",
			Description: "类型、标签与大小范围会叠加到当前搜索。",
			Fields: []Field{
				{ID: "query", Kind: KindText, Label: "关键词", Placeholder: "会议 纪要", Value: get("query", "")},
				{ID: "types", Kind: KindFileTypes, Label: "文件类型", Options: []string{".pdf", ".docx", ".md", ".jpg", ".png", ".zip"}, Value: get("types", ".pdf,.md")},
				{ID: "tags", Kind: KindTags, Label: "标签", Placeholder: "工作, 紧急", Value: get("tags", "工作")},
				{ID: "modified", Kind: KindSelect, Label: "修改时间", Options: []string{"不限", "今天", "近 7 天", "近 30 天", "今年"}, Value: get("modified", "近 30 天")},
				{ID: "max_mb", Kind: KindSlider, Label: "最大体积 (MB)", Min: 0, Max: 500, Step: 5, Value: get("max_mb", "50")},
				{ID: "folders_only", Kind: KindSwitch, Label: "只看文件夹", Value: get("folders_only", "false")},
			},
			Actions: []Action{
				{ID: "search", Label: "应用过滤", Variant: "primary", Message: "按表单应用高级搜索过滤"},
				{ID: "reset", Label: "重置", Variant: "secondary", Message: "重置搜索过滤"},
			},
		}, true
	case SceneClassify, "AI分类", "classify_confirm":
		docs := PreferPath("Documents")
		return Form{
			ID:          "classify_confirm",
			Title:       "AI 分类确认",
			Description: "勾选建议的目标文件夹；可改根目录后再确认。",
			Fields: []Field{
				{ID: "root", Kind: KindPath, Label: "分类根目录", PathMode: PathFolder, Required: true, Value: get("root", PreferPath("Downloads"))},
				{ID: "dests", Kind: KindTableSelect, Label: "目标文件夹", Required: true, Options: []string{
					filepath.Join(docs, "文档") + " · 12 个 PDF/Word",
					filepath.Join(docs, "图片") + " · 8 张照片",
					filepath.Join(docs, "压缩包") + " · 3 个 zip",
					PreferPath("Movies") + " · 1 个视频",
				}, Value: get("dests", filepath.Join(docs, "文档")+" · 12 个 PDF/Word,"+filepath.Join(docs, "图片")+" · 8 张照片")},
				{ID: "confidence", Kind: KindSlider, Label: "最低置信度 (%)", Min: 50, Max: 99, Value: get("confidence", "75")},
				{ID: "move", Kind: KindSegmented, Label: "操作", Options: []string{"移动", "复制"}, Value: get("move", "移动")},
				{ID: "note", Kind: KindTextArea, Label: "备注", Placeholder: "给本次分类留一句说明（可空）", Value: get("note", "")},
			},
			Actions: []Action{
				{ID: "confirm", Label: "确认分类", Variant: "primary", Message: "确认 AI 分类到所选文件夹"},
				{ID: "reject", Label: "全部否决", Variant: "destructive", Message: "否决本次 AI 分类建议"},
			},
		}, true
	case SceneBackup, "备份向导", "backup_wizard":
		return Form{
			ID:          "backup_wizard",
			Title:       "备份任务向导",
			Description: "选好源与目标后确认；进度条仅演示当前向导步骤。",
			Fields: []Field{
				{ID: "step", Kind: KindProgress, Label: "向导进度", Value: get("step", "0.66")},
				{ID: "name", Kind: KindText, Label: "任务名称", Placeholder: "周末文档备份", Required: true, Value: get("name", "周末文档备份")},
				{ID: "source", Kind: KindPath, Label: "源目录", PathMode: PathFolder, Required: true, Value: get("source", PreferPath("Documents"))},
				{ID: "dest", Kind: KindPath, Label: "备份目标", PathMode: PathFolder, Required: true, Value: get("dest", PreferPath("Backups"))},
				{ID: "types", Kind: KindFileTypes, Label: "包含类型", Options: []string{".pdf", ".docx", ".xlsx", ".md", ".jpg"}, Value: get("types", ".pdf,.docx,.md")},
				{ID: "when", Kind: KindTime, Label: "开始时间", Placeholder: "09:00", Required: true, Value: get("when", "09:00")},
				{ID: "days", Kind: KindDays, Label: "重复", Value: get("days", "周末")},
				{ID: "encrypt", Kind: KindSwitch, Label: "加密备份", Value: get("encrypt", "false")},
			},
			Actions: []Action{
				{ID: "start", Label: "开始备份", Variant: "primary", Message: "按表单开始备份任务"},
				{ID: "schedule", Label: "仅加入定时", Variant: "secondary", Message: "把备份任务加入定时"},
				{ID: "abort", Label: "放弃", Variant: "destructive", Message: "放弃备份向导"},
			},
		}, true
	default:
		return Form{}, false
	}
}

// ScenePresetIDs lists machine ids for docs and present_form help text.
func ScenePresetIDs() []string {
	return []string{SceneSchedule, SceneRename, SceneShare, SceneSearch, SceneClassify, SceneBackup}
}
