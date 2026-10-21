package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/schedule"
)

// schedule_mcp.go registers schedule_* tools on the in-process sunvale MCP
// server so the assistant can create and manage the same jobs the Settings
// 定时任务 page edits. Persistence is schedules.json; the minute ticker
// (schedule.go) picks them up without a restart.

type scheduleListArgs struct{}

type scheduleCreateArgs struct {
	Name         string `json:"name" jsonschema:"任务名称，例如 每晚整理下载文档"`
	Time         string `json:"time" jsonschema:"本地时间 HH:MM，例如 19:00"`
	Days         string `json:"days,omitempty" jsonschema:"每天、工作日 或 周末，默认每天"`
	Action       string `json:"action,omitempty" jsonschema:"file_rule、script 或 assistant，默认 file_rule"`
	Enabled      *bool  `json:"enabled,omitempty" jsonschema:"是否启用，默认 true"`
	Source       string `json:"source_dir,omitempty" jsonschema:"文件规则：源目录绝对路径"`
	Dest         string `json:"dest_dir,omitempty" jsonschema:"文件规则：目标目录绝对路径"`
	Match        string `json:"match,omitempty" jsonschema:"匹配：.pdf,.docx 或 *.md，空表示全部"`
	Op           string `json:"op,omitempty" jsonschema:"move 或 copy，默认 move"`
	DateStamp    bool   `json:"date_stamp,omitempty" jsonschema:"复制到目标下按日期建子文件夹，适合备份"`
	ScriptPath   string `json:"script_path,omitempty" jsonschema:"脚本路径 .py/.sh/.bat"`
	InlineScript string `json:"inline_script,omitempty" jsonschema:"内联脚本，保存到 sunvale/scripts/"`
	Prompt       string `json:"prompt,omitempty" jsonschema:"助手执行：自然语言提示词"`
	Folder       string `json:"folder,omitempty" jsonschema:"助手执行：工作目录"`
	Timeout      int    `json:"timeout_sec,omitempty" jsonschema:"脚本或助手超时秒数"`
}

type scheduleUpdateArgs struct {
	ID string `json:"id" jsonschema:"任务 id，来自 schedule_list"`
	scheduleCreateArgs
}

type scheduleDeleteArgs struct {
	ID string `json:"id" jsonschema:"要删除的任务 id"`
}

type scheduleRunArgs struct {
	ID string `json:"id" jsonschema:"立即运行的任务 id，不改调度时间"`
}

func (s *sunvaleTools) scheduleList(_ context.Context, _ *mcp.CallToolRequest, _ scheduleListArgs) (*mcp.CallToolResult, any, error) {
	st := ensureScheduleStore()
	jobs := st.Jobs()
	if len(jobs) == 0 {
		return mcpText("暂无定时任务。可用 schedule_create 添加，例如每天 19:00 把下载里的文档移到文档文件夹。"), nil, nil
	}
	var b strings.Builder
	for i, j := range jobs {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "id=%s name=%s enabled=%v time=%02d:%02d cron=%s action=%s",
			j.ID, j.Name, j.Enabled, j.Hour, j.Minute, j.Cron, j.Action)
		switch j.Action {
		case schedule.ActionFileRule:
			fmt.Fprintf(&b, " op=%s match=%q source=%s dest=%s date_stamp=%v",
				j.Op, j.Match, j.SourceDir, j.DestDir, j.DateStamp)
		case schedule.ActionScript:
			target := j.ScriptPath
			if target == "" {
				target = "(inline)"
			}
			fmt.Fprintf(&b, " script=%s", target)
		case schedule.ActionAssistant:
			fmt.Fprintf(&b, " prompt=%q folder=%s", truncateRunes(j.Prompt, 60), j.Folder)
		}
		if !j.NextRunAt.IsZero() {
			fmt.Fprintf(&b, " next=%s", j.NextRunAt.In(scheduleLoc()).Format("2006-01-02 15:04"))
		}
		if !j.LastRunAt.IsZero() {
			fmt.Fprintf(&b, " last=%s/%s", j.LastRunAt.In(scheduleLoc()).Format("01-02 15:04"), j.LastStatus)
		}
	}
	return mcpText(b.String()), nil, nil
}

func (s *sunvaleTools) scheduleCreate(_ context.Context, _ *mcp.CallToolRequest, args scheduleCreateArgs) (*mcp.CallToolResult, any, error) {
	j, err := scheduleJobFromArgs("", args)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	j.ID = schedule.NewID()
	if args.Enabled == nil {
		j.Enabled = true
	} else {
		j.Enabled = *args.Enabled
	}
	if err := ensureScheduleStore().Upsert(j, time.Now(), scheduleLoc()); err != nil {
		return mcpErr(err), nil, nil
	}
	next := j.NextRunAt
	if got, ok := ensureScheduleStore().Get(j.ID); ok {
		next = got.NextRunAt
	}
	msg := fmt.Sprintf("已创建定时任务 id=%s name=%s time=%02d:%02d action=%s",
		j.ID, j.Name, j.Hour, j.Minute, j.Action)
	if !next.IsZero() {
		msg += " next=" + next.In(scheduleLoc()).Format("2006-01-02 15:04")
	}
	return mcpText(msg), nil, nil
}

func (s *sunvaleTools) scheduleUpdate(_ context.Context, _ *mcp.CallToolRequest, args scheduleUpdateArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return mcpErr(fmt.Errorf("需要 id")), nil, nil
	}
	st := ensureScheduleStore()
	old, ok := st.Get(id)
	if !ok {
		return mcpErr(fmt.Errorf("任务不存在：%s", id)), nil, nil
	}
	merged := scheduleMergeUpdate(old, args.scheduleCreateArgs)
	merged.ID = id
	merged.Normalize()
	if err := st.Upsert(merged, time.Now(), scheduleLoc()); err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText(fmt.Sprintf("已更新定时任务 id=%s name=%s", merged.ID, merged.Name)), nil, nil
}

func (s *sunvaleTools) scheduleDelete(_ context.Context, _ *mcp.CallToolRequest, args scheduleDeleteArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return mcpErr(fmt.Errorf("需要 id")), nil, nil
	}
	if _, ok := ensureScheduleStore().Get(id); !ok {
		return mcpErr(fmt.Errorf("任务不存在：%s", id)), nil, nil
	}
	if err := ensureScheduleStore().Delete(id); err != nil {
		return mcpErr(err), nil, nil
	}
	return mcpText("已删除定时任务 " + id), nil, nil
}

func (s *sunvaleTools) scheduleRun(_ context.Context, _ *mcp.CallToolRequest, args scheduleRunArgs) (*mcp.CallToolResult, any, error) {
	id := strings.TrimSpace(args.ID)
	if id == "" {
		return mcpErr(fmt.Errorf("需要 id")), nil, nil
	}
	if _, ok := ensureScheduleStore().Get(id); !ok {
		return mcpErr(fmt.Errorf("任务不存在：%s", id)), nil, nil
	}
	runScheduledJobNow(id)
	return mcpText("已开始立即运行 " + id), nil, nil
}

func scheduleJobFromArgs(id string, args scheduleCreateArgs) (schedule.Job, error) {
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return schedule.Job{}, fmt.Errorf("需要 name")
	}
	hour, minute, err := parseHHMM(args.Time)
	if err != nil {
		return schedule.Job{}, err
	}
	j := schedule.Job{
		ID: id, Name: name, Hour: hour, Minute: minute,
		Weekdays:   weekdaysFromLabel(strings.TrimSpace(args.Days)),
		TimeoutSec: args.Timeout,
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	switch action {
	case "", "file_rule", "文件规则":
		j.Action = schedule.ActionFileRule
		j.SourceDir = expandUserPath(args.Source)
		j.DestDir = expandUserPath(args.Dest)
		j.Match = strings.TrimSpace(args.Match)
		j.DateStamp = args.DateStamp
		op := strings.ToLower(strings.TrimSpace(args.Op))
		if op == "copy" || op == "复制" {
			j.Op = schedule.OpCopy
		} else {
			j.Op = schedule.OpMove
		}
		if j.SourceDir == "" || j.DestDir == "" {
			return schedule.Job{}, fmt.Errorf("文件规则需要 source_dir 和 dest_dir")
		}
	case "script", "运行脚本":
		j.Action = schedule.ActionScript
		j.ScriptPath = expandUserPath(args.ScriptPath)
		j.InlineScript = args.InlineScript
		if j.ScriptPath == "" && strings.TrimSpace(j.InlineScript) == "" {
			return schedule.Job{}, fmt.Errorf("脚本任务需要 script_path 或 inline_script")
		}
	case "assistant", "助手执行":
		j.Action = schedule.ActionAssistant
		j.Prompt = strings.TrimSpace(args.Prompt)
		j.Folder = expandUserPath(args.Folder)
		if j.Prompt == "" {
			return schedule.Job{}, fmt.Errorf("助手任务需要 prompt")
		}
	default:
		return schedule.Job{}, fmt.Errorf("未知 action：%s", args.Action)
	}
	j.Normalize()
	return j, nil
}

func scheduleMergeUpdate(old schedule.Job, args scheduleCreateArgs) schedule.Job {
	j := old
	if strings.TrimSpace(args.Name) != "" {
		j.Name = strings.TrimSpace(args.Name)
	}
	if strings.TrimSpace(args.Time) != "" {
		if h, m, err := parseHHMM(args.Time); err == nil {
			j.Hour, j.Minute = h, m
		}
	}
	if strings.TrimSpace(args.Days) != "" {
		j.Weekdays = weekdaysFromLabel(strings.TrimSpace(args.Days))
	}
	if args.Enabled != nil {
		j.Enabled = *args.Enabled
	}
	if args.Timeout > 0 {
		j.TimeoutSec = args.Timeout
	}
	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action != "" {
		switch action {
		case "file_rule", "文件规则":
			j.Action = schedule.ActionFileRule
		case "script", "运行脚本":
			j.Action = schedule.ActionScript
		case "assistant", "助手执行":
			j.Action = schedule.ActionAssistant
		}
	}
	if s := expandUserPath(args.Source); s != "" {
		j.SourceDir = s
	}
	if d := expandUserPath(args.Dest); d != "" {
		j.DestDir = d
	}
	if strings.TrimSpace(args.Match) != "" {
		j.Match = strings.TrimSpace(args.Match)
	}
	if op := strings.ToLower(strings.TrimSpace(args.Op)); op != "" {
		if op == "copy" || op == "复制" {
			j.Op = schedule.OpCopy
		} else {
			j.Op = schedule.OpMove
		}
	}
	if args.DateStamp {
		j.DateStamp = true
	}
	if s := expandUserPath(args.ScriptPath); s != "" {
		j.ScriptPath = s
	}
	if args.InlineScript != "" {
		j.InlineScript = args.InlineScript
	}
	if p := strings.TrimSpace(args.Prompt); p != "" {
		j.Prompt = p
	}
	if f := expandUserPath(args.Folder); f != "" {
		j.Folder = f
	}
	return j
}

// expandUserPath turns ~/… into an absolute path under the home directory.
func expandUserPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// registerScheduleTools adds schedule_* to the MCP server. Called from
// newSunvaleServer so the list stays next to the other registrations.
func registerScheduleTools(s *sunvaleTools, server *mcp.Server) {
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "schedule_list", Description: "列出本机定时任务（设置 → 定时任务 / schedules.json）。",
	}, s.scheduleList)
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "schedule_create", Description: "新建定时任务。常用：每天 19:00 把下载里的文档移走——action=file_rule、time=19:00、source_dir=~/Downloads、dest_dir=~/Documents、match=.pdf,.docx、op=move。备份用 op=copy 且 date_stamp=true。",
	}, s.scheduleCreate)
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "schedule_update", Description: "按 id 更新定时任务；只传要改的字段。",
	}, s.scheduleUpdate)
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "schedule_delete", Description: "按 id 删除定时任务。",
	}, s.scheduleDelete)
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "schedule_run", Description: "立即运行一个定时任务，不改下次调度时间的定义（仍会写入上次运行状态）。",
	}, s.scheduleRun)
}
