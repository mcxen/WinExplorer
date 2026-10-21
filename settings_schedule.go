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

	"sunvale/internal/schedule"
)

// settings_schedule.go is the 定时任务 category: list of jobs, enable
// switches, next/last run,「立即运行」, and a create/edit form. Persistence
// is schedules.json via ensureScheduleStore.

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

	prompt, folder string
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
		return fmt.Sprintf("%s %s · 助手执行 · %s", days, clock, truncateRunes(j.Prompt, 40))
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

func (a *App) settingsScheduleFormGroup() components.SettingsGroup {
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
	title := "添加任务"
	if f.editID != "" {
		title = "编辑任务"
	}
	return components.SettingsGroup{
		ID: "sched-form", Title: title,
		Items: []components.SettingsItem{{
			ID: "sched-form-body", Title: "任务内容",
			Description: "时间按本机时区。文件规则只处理源目录下的直接子项（不递归）。",
			Keywords:    []string{"定时", "备份", "脚本", "助手", "cron"},
			Stacked:     true,
			Build: func(c *ui.Context) {
				a.settingsScheduleField(c, "名称", &f.name, "每晚整理下载文档")
				a.settingsScheduleField(c, "时间", &f.time, "19:00")
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "重复").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
					cur := f.days
					sel := ui.Select(c, &cur, []string{"每天", "工作日", "周末"}).Shrink(0)
					if sel.Changed() {
						f.days = cur
					}
				})
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "动作").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
					cur := f.action
					sel := ui.Select(c, &cur, []string{"文件规则", "运行脚本", "助手执行"}).Shrink(0)
					if sel.Changed() {
						f.action = cur
					}
				})
				switch f.action {
				case "运行脚本":
					a.settingsScheduleField(c, "脚本", &f.scriptPath, "留空则用下方内联脚本")
					components.TextBox(c, &f.inline, components.TextBoxOptions{Multiline: true}).
						FillWidth().MinHeight(72).FontSize(12).Lines(1, 5).
						Placeholder("内联 Python / Shell，保存到 sunvale/scripts/").Label("内联脚本")
					a.settingsScheduleField(c, "超时", &f.timeout, "秒")
				case "助手执行":
					components.TextBox(c, &f.prompt, components.TextBoxOptions{Multiline: true}).
						FillWidth().MinHeight(64).FontSize(12).Lines(1, 4).
						Placeholder("例如：整理下载文件夹里的截图到图片/截图").Label("提示词")
					a.settingsScheduleField(c, "目录", &f.folder, "助手工作目录，可空")
					a.settingsScheduleField(c, "超时", &f.timeout, "秒")
				default:
					a.settingsScheduleField(c, "源目录", &f.source, "~/Downloads")
					a.settingsScheduleField(c, "目标", &f.dest, "~/Documents")
					a.settingsScheduleField(c, "匹配", &f.match, ".pdf,.docx 或 *.md")
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "操作").FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
						cur := f.op
						sel := ui.Select(c, &cur, []string{"移动", "复制"}).Shrink(0)
						if sel.Changed() {
							f.op = cur
						}
						ds := f.dateStamp
						sw := ui.Switch(c, &ds).Shrink(0).Label("按日期建子文件夹")
						if sw.Changed() {
							f.dateStamp = ds
						}
					})
				}
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
				})
			},
		}},
	}
}

func (a *App) settingsScheduleField(c *ui.Context, label string, value *string, hint string) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colorMuted()).Width(36).Shrink(0)
		components.TextBox(c, value, components.TextBoxOptions{}).Grow(1).MinWidth(0).
			Height(28).FontSize(12).Placeholder(hint)
	})
}

func (a *App) settingsScheduleFillExample() {
	home, _ := os.UserHomeDir()
	a.settingsSchedule = settingsScheduleForm{
		name:   "每晚整理下载文档",
		time:   "19:00",
		days:   "每天",
		action: "文件规则",
		source: filepath.Join(home, "Downloads"),
		dest:   filepath.Join(home, "Documents", "来自下载"),
		match:  ".pdf,.docx,.doc,.xlsx,.pptx,.txt,.md",
		op:     "移动",
	}
}

func (a *App) settingsScheduleSave() {
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
		j.Prompt = strings.TrimSpace(f.prompt)
		j.Folder = strings.TrimSpace(f.folder)
		j.TimeoutSec, _ = strconv.Atoi(strings.TrimSpace(f.timeout))
		if j.Prompt == "" {
			a.statusMsg = "请填写助手提示词"
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
