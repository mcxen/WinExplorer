package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/internal/forms"
	"sunvale/internal/schedule"
)

// settings_schedule.go is the 定时任务 category list: jobs, enable switches,
// next/last run and「立即运行」. Create/edit form and presets live in
// settings_schedule_form.go (LAY1 split).

type settingsScheduleForm struct {
	editID string
	name   string
	time   string // "19:00"
	days   string // 每天 | 工作日 | 周末
	action string // 文件规则 | 运行脚本 | 助手执行

	source, dest, match, op string
	dateStamp               bool

	scriptPath, inline string
	timeout            string

	prompt, folder, preset, destRoot string
	lastPresetApplied                string // tracks select changes for autofill
}

func (a *App) openScheduleSettings() {
	a.settingsOpen = true
	a.settingsUI.Category = "schedule"
	a.settingsUI.Query = ""
}

func (a *App) settingsScheduleGroups() []components.SettingsGroup {
	return []components.SettingsGroup{
		a.settingsScheduleListGroup(),
		a.settingsScheduleFormGroup(),
	}
}

func (a *App) settingsScheduleListGroup() components.SettingsGroup {
	st := ensureScheduleStore()
	jobs := st.Jobs()
	items := []components.SettingsItem{}
	if len(jobs) == 0 {
		items = append(items, components.SettingsItem{
			ID: "sched-empty", Title: "暂无定时任务",
			Description: "用下方表单添加，例如每晚 19:00 把下载目录里的文档移到文档文件夹。",
			Keywords:    []string{"定时", "计划", "备份"},
		})
	}
	for _, j := range jobs {
		job := j
		desc := scheduleJobSummary(job)
		items = append(items, components.SettingsItem{
			ID: "sched-" + job.ID, Title: job.Name, Description: desc,
			Keywords: []string{"定时", "计划", job.Name, job.Action},
			Stacked:  true,
			Build: func(c *ui.Context) {
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					on := job.Enabled
					sw := ui.Switch(c, &on).Shrink(0).Label("启用")
					if sw.Changed() {
						_ = st.SetEnabled(job.ID, on, time.Now(), scheduleLoc())
					}
					if components.Button(c, "立即运行", components.ButtonSecondary).Clicked() {
						runScheduledJobNow(job.ID)
						a.statusMsg = "已开始运行：" + job.Name
					}
					if components.Button(c, "编辑", components.ButtonGhost).Clicked() {
						a.settingsScheduleLoad(job)
					}
					if components.Button(c, "删除", components.ButtonGhost).Clicked() {
						_ = st.Delete(job.ID)
						if a.settingsSchedule.editID == job.ID {
							a.settingsSchedule = settingsScheduleForm{}
						}
					}
				})
				ui.Text(c, scheduleJobStatusLine(job)).FontSize(11).TextColor(colorMuted()).Wrap()
			},
		})
	}
	return components.SettingsGroup{ID: "sched-list", Title: "任务列表", Items: items}
}

func scheduleJobSummary(j schedule.Job) string {
	days := "每天"
	if len(j.Weekdays) == 5 {
		days = "工作日"
	} else if len(j.Weekdays) == 2 {
		days = "周末"
	} else if len(j.Weekdays) > 0 {
		days = "每周指定日"
	}
	clock := fmt.Sprintf("%02d:%02d", j.Hour, j.Minute)
	switch j.Action {
	case schedule.ActionFileRule:
		op := "移动"
		if j.Op == schedule.OpCopy {
			op = "复制"
		}
		if j.DateStamp {
			op += "（按日期备份）"
		}
		return fmt.Sprintf("%s %s · 文件规则 · %s · %s → %s", days, clock, op, shrinkPath(j.SourceDir), shrinkPath(j.DestDir))
	case schedule.ActionScript:
		target := j.ScriptPath
		if target == "" {
			target = "内联脚本"
		}
		return fmt.Sprintf("%s %s · 运行脚本 · %s", days, clock, shrinkPath(target))
	case schedule.ActionAssistant:
		label := "助手执行"
		if j.Preset != "" {
			label = "助手 · " + schedule.PresetLabel(j.Preset)
		}
		return fmt.Sprintf("%s %s · %s · %s", days, clock, label, truncateRunes(j.Prompt, 40))
	default:
		return days + " " + clock
	}
}

func scheduleJobStatusLine(j schedule.Job) string {
	var parts []string
	if !j.LastRunAt.IsZero() {
		st := j.LastStatus
		if st == "" {
			st = "—"
		}
		line := "上次 " + j.LastRunAt.In(scheduleLoc()).Format("01-02 15:04") + " · " + st
		if j.LastDetail != "" {
			line += " · " + j.LastDetail
		}
		if j.LastError != "" {
			line += " · " + truncateRunes(j.LastError, 60)
		}
		parts = append(parts, line)
	} else {
		parts = append(parts, "尚未运行")
	}
	if j.Enabled && !j.NextRunAt.IsZero() {
		parts = append(parts, "下次 "+j.NextRunAt.In(scheduleLoc()).Format("01-02 15:04"))
	} else if !j.Enabled {
		parts = append(parts, "已停用")
	}
	return strings.Join(parts, "  ·  ")
}

func shrinkPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "—"
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return truncateRunes(p, 48)
}

func (a *App) settingsScheduleLoad(j schedule.Job) {
	f := settingsScheduleForm{
		editID: j.ID, name: j.Name,
		time:   fmt.Sprintf("%02d:%02d", j.Hour, j.Minute),
		source: j.SourceDir, dest: j.DestDir, match: j.Match,
		dateStamp:  j.DateStamp,
		scriptPath: j.ScriptPath, inline: j.InlineScript,
		prompt: j.Prompt, folder: j.Folder,
		preset: forms.PresetLabelFromID(j.Preset), destRoot: j.DestDir,
	}
	switch {
	case len(j.Weekdays) == 5:
		f.days = "工作日"
	case len(j.Weekdays) == 2 && containsWeekday(j.Weekdays, int(time.Saturday)) && containsWeekday(j.Weekdays, int(time.Sunday)):
		f.days = "周末"
	default:
		f.days = "每天"
	}
	switch j.Action {
	case schedule.ActionScript:
		f.action = "运行脚本"
	case schedule.ActionAssistant:
		f.action = "助手执行"
	default:
		f.action = "文件规则"
	}
	if j.Op == schedule.OpCopy {
		f.op = "复制"
	} else {
		f.op = "移动"
	}
	if j.TimeoutSec > 0 {
		f.timeout = strconv.Itoa(j.TimeoutSec)
	}
	a.settingsSchedule = f
}

func containsWeekday(days []int, d int) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

func (a *App) settingsScheduleDefaults() {
	f := &a.settingsSchedule
	if f.time == "" {
		f.time = "19:00"
	}
	if f.days == "" {
		f.days = "每天"
	}
	if f.action == "" {
		f.action = "文件规则"
	}
	if f.op == "" {
		f.op = "移动"
	}
	if f.match == "" {
		f.match = ".pdf,.docx,.doc,.xlsx,.pptx,.txt,.md"
	}
	if f.timeout == "" {
		f.timeout = "300"
	}
	if f.preset == "" {
		f.preset = "自定义"
	}
	if f.source == "" {
		if home, err := os.UserHomeDir(); err == nil {
			f.source = filepath.Join(home, "Downloads")
		}
	}
	if f.dest == "" {
		if home, err := os.UserHomeDir(); err == nil {
			f.dest = filepath.Join(home, "Documents")
		}
	}
}
