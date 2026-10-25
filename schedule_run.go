package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"sunvale/internal/schedule"
)

// schedule_run.go executes one Job: file rules via internal/schedule,
// scripts with a timeout, and assistant prompts through the same
// headless runner A2A uses.

// scheduleAssistantExec runs ActionAssistant jobs. Assigned in init so the
// MCP server registration cannot form an init cycle with runAssistantHeadless
// (newSunvaleServer → schedule_run → headless → sunvaleSession → server).
var scheduleAssistantExec func(schedule.Job) (status, detail, errMsg string)

func init() {
	scheduleAssistantExec = runScheduleAssistant
}

func executeScheduledJob(j schedule.Job) (status, detail, errMsg string) {
	switch j.Action {
	case schedule.ActionFileRule:
		return runScheduleFileRule(j)
	case schedule.ActionScript:
		return runScheduleScript(j)
	case schedule.ActionAssistant:
		if scheduleAssistantExec == nil {
			return "error", "", "助手调度未就绪"
		}
		return scheduleAssistantExec(j)
	default:
		return "error", "", "未知动作类型"
	}
}

func runScheduleFileRule(j schedule.Job) (string, string, string) {
	now := time.Now()
	ops, err := schedule.PlanFileRule(j, now)
	if err != nil {
		return "error", "", err.Error()
	}
	if len(ops) == 0 {
		return "ok", "没有匹配的文件", ""
	}
	n, err := schedule.ApplyFileRule(ops)
	detail := fmt.Sprintf("处理 %d/%d 个项目", n, len(ops))
	if err != nil {
		if n > 0 {
			return "error", detail, err.Error()
		}
		return "error", "", err.Error()
	}
	return "ok", detail, ""
}

func runScheduleScript(j schedule.Job) (string, string, string) {
	path := strings.TrimSpace(j.ScriptPath)
	inline := strings.TrimSpace(j.InlineScript)
	if path == "" && inline == "" {
		return "error", "", "请指定脚本路径或内联脚本"
	}
	var cleanup string
	if path == "" {
		dir, err := schedule.ScriptsDir()
		if err != nil {
			return "error", "", err.Error()
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "error", "", err.Error()
		}
		ext := ".sh"
		if runtime.GOOS == "windows" {
			ext = ".bat"
		}
		if strings.HasPrefix(inline, "#!") && strings.Contains(inline, "python") {
			ext = ".py"
		} else if strings.Contains(inline, "import ") || strings.HasPrefix(strings.TrimSpace(inline), "print(") {
			ext = ".py"
		}
		path = filepath.Join(dir, j.ID+ext)
		if err := os.WriteFile(path, []byte(inline+"\n"), 0o755); err != nil {
			return "error", "", err.Error()
		}
		cleanup = path
	}
	if cleanup != "" {
		// Keep inline scripts on disk under sunvale/scripts for editing;
		// do not delete after run.
		_ = cleanup
	}
	timeout := time.Duration(j.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := scheduleScriptCmd(ctx, path)
	if err != nil {
		return "error", "", err.Error()
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if len(out) > 4000 {
		out = out[:4000] + "…"
	}
	_ = schedule.WriteLog(j.ID, "script output:\n"+out)
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "error", "超时", runErr.Error()
		}
		return "error", shortOut(out), runErr.Error()
	}
	return "ok", shortOut(out), ""
}

func shortOut(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		return "脚本已完成"
	}
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func scheduleScriptCmd(ctx context.Context, path string) (*exec.Cmd, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py":
		bin := "python3"
		if runtime.GOOS == "windows" {
			bin = "py"
		}
		return exec.CommandContext(ctx, bin, path), nil
	case ".sh":
		return exec.CommandContext(ctx, "bash", path), nil
	case ".bat", ".cmd":
		if runtime.GOOS != "windows" {
			return nil, fmt.Errorf("当前系统无法运行 .bat")
		}
		return exec.CommandContext(ctx, "cmd", "/C", path), nil
	default:
		// Executable bit / shebang: run directly.
		return exec.CommandContext(ctx, path), nil
	}
}

func runScheduleAssistant(j schedule.Job) (string, string, string) {
	prompt := strings.TrimSpace(j.Prompt)
	if prompt == "" {
		return "error", "", "助手提示词不能为空"
	}
	folder := strings.TrimSpace(j.Folder)
	if folder == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "error", "", "未指定文件夹"
		}
		folder = home
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		return "error", "", err.Error()
	}
	prov, ok := activeAssistantProvider(disk)
	if !ok || prov.APIKey == "" {
		return "error", "", "请先在设置 → 助手中配置供应商"
	}
	timeout := time.Duration(j.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	slog.Info("schedule assistant run", "job", j.ID, "name", j.Name, "folder", folder, "timeout_s", int(timeout.Seconds()))
	run := assistRun{
		origin:    "schedule:" + j.ID,
		prov:      prov,
		root:      folder,
		book:      disk.ArchivePasswords,
		autopilot: true, // scheduled runs behave like Autopilot
		prefs:     disk.Prefs,
		hist:      []*schema.Message{schema.UserMessage(prompt)},
		memories:  readAssistantMemories(assistantSideRootFor(assistantConfigPath())),
	}
	assistantRunMu.Lock()
	result, runErr := runAssistantHeadless(ctx, run)
	saved := sunvaleToolState.drainPending()
	sunvaleToolState.drainUndos()
	sunvaleToolState.drainNav()
	memAdd, memDrop := sunvaleToolState.drainMemory()
	assistantRunMu.Unlock()
	// Apply side effects without a specific window when possible.
	if len(apps) > 0 {
		appsMu.Lock()
		a := apps[0]
		appsMu.Unlock()
		if a != nil {
			a.assistantApplyHeadlessSideEffects(saved, memAdd, memDrop)
		}
	}
	text := strings.TrimSpace(result.Text)
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120]) + "…"
	}
	if runErr != nil {
		return "error", text, runErr.Error()
	}
	if text == "" {
		text = "助手已完成"
	}
	return "ok", text, ""
}
