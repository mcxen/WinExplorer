package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/schedule"
)

// assistant_task_run.go is the one-shot headless Autopilot tool (SCH2):
// same runner as schedule ActionAssistant, but does not persist a Job.
// Calls are asynchronous so the tool never nests under assistantRunMu
// while the interactive turn still holds it.

type assistantTaskRunArgs struct {
	Prompt  string `json:"prompt,omitempty" jsonschema:"自然语言提示词；与 preset 二选一或同时提供（同时时以 prompt 为准）"`
	Folder  string `json:"folder,omitempty" jsonschema:"工作目录，绝对路径或 ~/…；空则按预设默认"`
	Preset  string `json:"preset,omitempty" jsonschema:"内置预设：classify_downloads、inbox_triage、rename_by_content，或中文名「分类下载」等"`
	DestDir string `json:"dest_dir,omitempty" jsonschema:"分类类预设的目标根目录；空则用预设默认"`
	Timeout int    `json:"timeout_sec,omitempty" jsonschema:"超时秒数，默认 600"`
}

func (s *sunvaleTools) assistantTaskRun(_ context.Context, _ *mcp.CallToolRequest, args assistantTaskRunArgs) (*mcp.CallToolResult, any, error) {
	j, err := assistantTaskJobFromArgs(args)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	go runOneShotAssistantJob(j)
	label := j.Name
	if label == "" {
		label = "助手任务"
	}
	msg := fmt.Sprintf("已开始执行「%s」（不保存为定时任务）", label)
	if j.Preset != "" {
		msg += " preset=" + j.Preset
	}
	if j.Folder != "" {
		msg += " folder=" + j.Folder
	}
	return mcpText(msg), nil, nil
}

func assistantTaskJobFromArgs(args assistantTaskRunArgs) (schedule.Job, error) {
	preset := schedule.ResolvePresetID(args.Preset)
	prompt := strings.TrimSpace(args.Prompt)
	folder := expandUserPath(args.Folder)
	dest := expandUserPath(args.DestDir)
	j := schedule.Job{
		ID:         "oneshot-" + schedule.NewID(),
		Action:     schedule.ActionAssistant,
		TimeoutSec: args.Timeout,
		Enabled:    true,
	}
	if j.TimeoutSec <= 0 {
		j.TimeoutSec = 600
	}
	if preset != "" {
		if err := schedule.ApplyPreset(&j, preset, folder, dest); err != nil {
			return schedule.Job{}, err
		}
		if prompt != "" {
			// Caller overrode the template text but kept the preset label.
			j.Prompt = prompt
			if folder != "" {
				j.Folder = folder
			}
			if dest != "" {
				j.DestDir = dest
			}
		}
	} else {
		if prompt == "" {
			return schedule.Job{}, fmt.Errorf("需要 prompt 或 preset")
		}
		j.Prompt = prompt
		j.Folder = folder
		j.Name = "一次性助手任务"
	}
	j.Normalize()
	if strings.TrimSpace(j.Prompt) == "" {
		return schedule.Job{}, fmt.Errorf("助手提示词不能为空")
	}
	return j, nil
}

// runOneShotAssistantJob executes an in-memory assistant Job and writes a
// short log under schedule-logs; it does not touch schedules.json.
func runOneShotAssistantJob(j schedule.Job) {
	schedMu.Lock()
	defer schedMu.Unlock()
	status, detail, errMsg := executeScheduledJob(j)
	body := time.Now().Format("15:04:05") + " " + status + " " + detail
	if errMsg != "" {
		body += "\n" + errMsg
	}
	_ = schedule.WriteLog(j.ID, body)
}

func registerAssistantTaskTools(s *sunvaleTools, server *mcp.Server) {
	addSunvaleTool(s, server, &mcp.Tool{
		Name: "assistant_task_run",
		Description: "立刻用全托管助手执行一次任务（不写入定时任务）。用户说「现在把下载按内容分类」「立刻整理这个文件夹」时用。" +
			"preset=classify_downloads|inbox_triage|rename_by_content，或只传 prompt。" +
			"按内容分类/读完再整理必须用本工具或带 preset 的 schedule_create，不要用仅按扩展名的 file_rule。",
	}, s.assistantTaskRun)
}
