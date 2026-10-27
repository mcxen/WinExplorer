package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/internal/forms"
	"sunvale/internal/schedule"
)

// settings_schedule_form.go is the schema-driven create/edit form for
// scheduled jobs (internal/forms.ScheduleAdd) plus presets. Split from
// settings_schedule.go (LAY1).

func (a *App) settingsScheduleFormGroup() components.SettingsGroup {
	a.settingsScheduleDefaults()
	f := &a.settingsSchedule
	title := "添加任务"
	if f.editID != "" {
		title = "编辑任务"
	}
	schema := forms.ScheduleAdd(a.settingsSchedulePrefill())
	vis := forms.VisibleForAction(f.action)
	return components.SettingsGroup{
		ID: "sched-form", Title: title,
		Items: []components.SettingsItem{{
			ID: "sched-form-body", Title: "任务内容",
			Description: schema.Description,
			Keywords:    []string{"定时", "备份", "脚本", "助手", "cron", "表单"},
			Stacked:     true,
			Build: func(c *ui.Context) {
				ptrs := a.settingsSchedulePtrs()
				for i, field := range schema.Fields {
					if !vis[field.ID] {
						continue
					}
					ptr := ptrs[field.ID]
					if ptr == nil {
						continue
					}
					label := field.Label
					if field.Required {
						label += " *"
					}
					a.paintFormField(c.Key(i), field.Kind, label, ptr, formFieldOpts{
						PathMode: field.PathMode,
						Options:  field.Options,
						Hint:     field.Placeholder,
					})
				}
				a.settingsScheduleSyncDateStamp()
				a.settingsScheduleApplyPresetIfNeeded()
				ui.Row(c).FillWidth().Gap(6).Children(func() {
					label := "保存"
					if f.editID == "" {
						label = "添加"
					}
					if components.Button(c, label, components.ButtonSecondary).Clicked() {
						a.settingsScheduleSave()
					}
					if f.editID != "" {
						if components.Button(c, "取消编辑", components.ButtonGhost).Clicked() {
							a.settingsSchedule = settingsScheduleForm{}
						}
					}
					if f.action == "文件规则" {
						if components.Button(c, "填入「每晚整理下载文档」示例", components.ButtonGhost).Clicked() {
							a.settingsScheduleFillExample()
						}
					}
					if f.action == "助手执行" {
						if components.Button(c, "填入「分类下载」预设", components.ButtonGhost).Clicked() {
							a.settingsScheduleFillClassifyPreset()
						}
					}
				})
			},
		}},
	}
}

func (a *App) settingsSchedulePrefill() map[string]string {
	f := a.settingsSchedule
	return map[string]string{
		"edit_id": f.editID, "name": f.name, "time": f.time, "days": f.days,
		"action": f.action, "source": f.source, "dest": f.dest, "match": f.match,
		"op": f.op, "date_stamp": boolString(f.dateStamp),
		"script_path": f.scriptPath, "inline": f.inline,
		"prompt": f.prompt, "folder": f.folder, "timeout": f.timeout,
		"preset": f.preset, "dest_root": f.destRoot,
	}
}

func (a *App) settingsSchedulePtrs() map[string]*string {
	f := &a.settingsSchedule
	dateStamp := boolString(f.dateStamp)
	// date_stamp is mirrored through a side string each frame; Switch writes
	// back into dateStamp via a dedicated binder below.
	_ = dateStamp
	return map[string]*string{
		"name": &f.name, "time": &f.time, "days": &f.days, "action": &f.action,
		"source": &f.source, "dest": &f.dest, "match": &f.match, "op": &f.op,
		"script_path": &f.scriptPath, "inline": &f.inline,
		"prompt": &f.prompt, "folder": &f.folder, "timeout": &f.timeout,
		"preset": &f.preset, "dest_root": &f.destRoot,
		"date_stamp": a.settingsScheduleDateStampPtr(),
	}
}

// settingsScheduleDateStampPtr keeps the switch on a string the shared
// painter understands while the form struct stores a bool.
func (a *App) settingsScheduleDateStampPtr() *string {
	f := &a.settingsSchedule
	if a.settingsScheduleDateStampStr == nil {
		a.settingsScheduleDateStampStr = new(string)
	}
	*a.settingsScheduleDateStampStr = boolString(f.dateStamp)
	// The painter mutates the string; sync back after paint would be too late.
	// Instead wrap: we sync on next frame from the string before painting, and
	// after Switch.Changed the string is updated — read it back at end of Build.
	return a.settingsScheduleDateStampStr
}

func (a *App) settingsScheduleSyncDateStamp() {
	if a.settingsScheduleDateStampStr != nil {
		a.settingsSchedule.dateStamp = parseBoolString(*a.settingsScheduleDateStampStr)
	}
}

func (a *App) settingsScheduleFillExample() {
	ex := forms.ScheduleExampleFill()
	a.settingsSchedule = settingsScheduleForm{
		name: ex["name"], time: ex["time"], days: ex["days"], action: ex["action"],
		source: ex["source"], dest: ex["dest"], match: ex["match"], op: ex["op"],
	}
}

func (a *App) settingsScheduleSave() {
	a.settingsScheduleSyncDateStamp()
	f := a.settingsSchedule
	name := strings.TrimSpace(f.name)
	if name == "" {
		a.statusMsg = "请填写任务名称"
		return
	}
	hour, minute, err := parseHHMM(f.time)
	if err != nil {
		a.statusMsg = err.Error()
		return
	}
	j := schedule.Job{
		ID: f.editID, Name: name, Enabled: true,
		Hour: hour, Minute: minute, Weekdays: weekdaysFromLabel(f.days),
	}
	if j.ID == "" {
		j.ID = schedule.NewID()
	} else if old, ok := ensureScheduleStore().Get(j.ID); ok {
		j.Enabled = old.Enabled
		j.LastRunAt, j.LastStatus, j.LastError, j.LastDetail = old.LastRunAt, old.LastStatus, old.LastError, old.LastDetail
	}
	switch f.action {
	case "运行脚本":
		j.Action = schedule.ActionScript
		j.ScriptPath = strings.TrimSpace(f.scriptPath)
		j.InlineScript = f.inline
		j.TimeoutSec, _ = strconv.Atoi(strings.TrimSpace(f.timeout))
	case "助手执行":
		j.Action = schedule.ActionAssistant
		j.TimeoutSec, _ = strconv.Atoi(strings.TrimSpace(f.timeout))
		presetID := forms.PresetIDFromLabel(f.preset)
		folder := strings.TrimSpace(f.folder)
		dest := strings.TrimSpace(f.destRoot)
		if presetID != "" {
			if err := schedule.ApplyPreset(&j, presetID, folder, dest); err != nil {
				a.statusMsg = err.Error()
				return
			}
			j.Name = name
			if p := strings.TrimSpace(f.prompt); p != "" {
				j.Prompt = p
			}
		} else {
			j.Preset = ""
			j.Prompt = strings.TrimSpace(f.prompt)
			j.Folder = folder
			j.DestDir = dest
		}
		if j.Prompt == "" {
			a.statusMsg = "请填写助手提示词或选择预设"
			return
		}
	default:
		j.Action = schedule.ActionFileRule
		j.SourceDir = strings.TrimSpace(f.source)
		j.DestDir = strings.TrimSpace(f.dest)
		j.Match = strings.TrimSpace(f.match)
		j.DateStamp = f.dateStamp
		if f.op == "复制" {
			j.Op = schedule.OpCopy
		} else {
			j.Op = schedule.OpMove
		}
		if j.SourceDir == "" || j.DestDir == "" {
			a.statusMsg = "请填写源目录和目标目录"
			return
		}
	}
	j.Normalize()
	if err := ensureScheduleStore().Upsert(j, time.Now(), scheduleLoc()); err != nil {
		a.statusMsg = err.Error()
		return
	}
	a.settingsSchedule = settingsScheduleForm{}
	a.statusMsg = "已保存定时任务"
}

func parseHHMM(s string) (hour, minute int, err error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("时间格式应为 HH:MM")
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("小时无效")
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("分钟无效")
	}
	return hour, minute, nil
}

func weekdaysFromLabel(label string) []int {
	switch label {
	case "工作日":
		return []int{int(time.Monday), int(time.Tuesday), int(time.Wednesday), int(time.Thursday), int(time.Friday)}
	case "周末":
		return []int{int(time.Sunday), int(time.Saturday)}
	default:
		return nil
	}
}

// settingsScheduleApplyPresetIfNeeded fills prompt/folder when the preset
// select changes to a built-in template. Switching back to 自定义 does not
// clear an edited prompt.
func (a *App) settingsScheduleApplyPresetIfNeeded() {
	f := &a.settingsSchedule
	if f.action != "助手执行" {
		f.lastPresetApplied = f.preset
		return
	}
	if f.preset == f.lastPresetApplied {
		return
	}
	f.lastPresetApplied = f.preset
	id := forms.PresetIDFromLabel(f.preset)
	if id == "" {
		return
	}
	var j schedule.Job
	if err := schedule.ApplyPreset(&j, id, strings.TrimSpace(f.folder), strings.TrimSpace(f.destRoot)); err != nil {
		return
	}
	f.prompt = j.Prompt
	f.folder = j.Folder
	f.destRoot = j.DestDir
	if strings.TrimSpace(f.name) == "" {
		f.name = j.Name
	}
}

func (a *App) settingsScheduleFillClassifyPreset() {
	var j schedule.Job
	_ = schedule.ApplyPreset(&j, schedule.PresetClassifyDownloads, "", "")
	a.settingsSchedule = settingsScheduleForm{
		name: j.Name, time: "19:00", days: "每天", action: "助手执行",
		preset: forms.PresetLabelFromID(j.Preset),
		prompt: j.Prompt, folder: j.Folder, destRoot: j.DestDir,
		timeout: "600",
	}
}
