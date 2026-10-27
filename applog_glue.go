package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/egoist/mygo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/oneo-me/onevra/source/components"

	"sunvale/internal/applog"
	assistant "sunvale/internal/assistant"
	"sunvale/internal/tasks"
)

// applog_glue.go wires the diagnostic log (internal/applog, docs/specs/
// 12-logging.md) into the app: early init in main, the settings level, the
// log folder actions, and the assistant's HTTP and run tracing.

// startLogging runs first in main, before single-instance and the state
// file, so a failure there is on disk even in a GUI-subsystem Windows
// build that has no console. stderr is added off Windows, or on Windows
// when SUNVALE_LOG_STDERR=1 (go run from a terminal).
func startLogging() {
	stderr := runtime.GOOS != "windows" || os.Getenv("SUNVALE_LOG_STDERR") != ""
	path, err := applog.Init(applog.Dir(), stderr)
	if err != nil {
		slog.Error("日志文件无法打开", "path", path, "err", err)
	}
	slog.Info("startup", "pid", os.Getpid(), "os", runtime.GOOS, "arch", runtime.GOARCH,
		"go", runtime.Version(), "exe", exePath(), "log", path)
}

func exePath() string { p, _ := os.Executable(); return p }

// logStartupEnv records what only the running app knows: the version and
// the displays' scale factors.
func logStartupEnv() {
	defer applog.Recover("startup-env", false)
	var scales []string
	for _, d := range mygo.Screen.Displays() {
		scales = append(scales, fmt.Sprintf("%s@%.2fx", d.Label, d.ScaleFactor))
	}
	slog.Info("app ready", "version", mygo.App.Version(), "os", runtime.GOOS, "arch", runtime.GOARCH,
		"displays", strings.Join(scales, ", "), "level", applog.LevelName(applog.Level()), "since_boot_ms", time.Since(bootStart).Milliseconds())
}

// applyLogLevel sets the level from settings; SUNVALE_LOG wins when set.
func applyLogLevel(s appSettings) {
	if os.Getenv("SUNVALE_LOG") != "" {
		return
	}
	applog.SetLevel(applog.ParseLevel(s.LogLevel))
}

func logLevelSetting(s appSettings) string {
	if s.LogLevel == "" {
		return "info"
	}
	return applog.LevelName(applog.ParseLevel(s.LogLevel))
}

// openLogFolder opens the log folder in the system file manager.
func openLogFolder() {
	dir := applog.Dir()
	_ = os.MkdirAll(dir, 0o755)
	if err := sysOpen(dir); err != nil {
		slog.Warn("打开日志文件夹失败", "dir", dir, "err", err)
	}
}

// openLogFile opens the active log in the default text viewer, falling back
// to the folder.
func openLogFile() {
	p := applog.Path()
	if p == "" {
		openLogFolder()
		return
	}
	if err := sysOpen(p); err != nil {
		openLogFolder()
	}
}

var lastLogExport struct {
	mu   sync.Mutex
	path string
	err  string
}

// exportLogs zips the log folder's files (makeZip, archive.go) into the
// Downloads folder (home when missing) and reveals the archive.
func exportLogs() (string, error) {
	dir := applog.Dir()
	var entries []Entry
	for _, f := range applog.Files(dir) {
		entries = append(entries, Entry{Name: filepath.Base(f), Path: f})
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("没有日志文件")
	}
	home, _ := os.UserHomeDir()
	out := filepath.Join(home, "Downloads")
	if st, err := os.Stat(out); err != nil || !st.IsDir() {
		out = home
	}
	dst := filepath.Join(out, "sunvale-logs-"+time.Now().Format("20060102-150405")+".zip")
	slog.Info("export logs", "dst", dst, "files", len(entries))
	if err := makeZip(dst, entries); err != nil {
		slog.Error("export logs failed", "err", err)
		return "", err
	}
	_ = sysReveal(dst)
	return dst, nil
}

func exportLogsAction() {
	p, err := exportLogs()
	lastLogExport.mu.Lock()
	lastLogExport.path, lastLogExport.err = p, ""
	if err != nil {
		lastLogExport.err = err.Error()
	}
	lastLogExport.mu.Unlock()
}

func newRunID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// redactURL drops userinfo and secret query values.
func redactURL(s string) string { return applog.Redact(stripUserinfo(s)) }

func stripUserinfo(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && at < strings.IndexAny(rest+"/", "/") {
			return s[:i+3] + "[REDACTED]@" + rest[at+1:]
		}
	}
	return s
}

// aiLogTransport traces the model's HTTP calls: start, status, latency,
// and for failures a redacted, truncated body snippet. Headers are never
// logged, so Authorization and API keys cannot leak.
type aiLogTransport struct {
	base  http.RoundTripper
	runID string
}

const aiLogBodyMax = 1500

func (t *aiLogTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	u := redactURL(req.URL.String())
	slog.Debug("ai http start", "run", t.runID, "method", req.Method, "url", u, "req_bytes", req.ContentLength)
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		slog.Error("ai http error", "run", t.runID, "url", u, "err", err, applog.Since(start))
		return nil, err
	}
	attrs := []any{"run", t.runID, "method", req.Method, "url", u, "status", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"), "ttfb_ms", time.Since(start).Milliseconds()}
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(buf), resp.Body), resp.Body}
		attrs = append(attrs, "body", applog.Trunc(string(buf), aiLogBodyMax))
		slog.Error("ai http failed", attrs...)
	} else {
		slog.Info("ai http", attrs...)
	}
	return resp, err
}

// aiHTTPHeaderTimeout bounds how long we wait for response headers. The
// client itself has no overall Timeout: a long streamed answer must keep
// the body open until the run cancels.
const aiHTTPHeaderTimeout = 60 * time.Second

func aiHTTPClient(runID string) *http.Client {
	base := http.DefaultTransport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		cloned := t.Clone()
		cloned.ResponseHeaderTimeout = aiHTTPHeaderTimeout
		base = cloned
	}
	return &http.Client{Transport: &aiLogTransport{base: base, runID: runID}}
}

// openUIParseLogged dedupes parse-failure records: the painter parses every
// frame, so one bad program would otherwise flood the file.
var openUIParseLogged sync.Map

func logOpenUIParseFail(src string) {
	key := src
	if len(key) > 256 {
		key = key[:256]
	}
	if _, dup := openUIParseLogged.LoadOrStore(key, true); dup {
		return
	}
	slog.Warn("openui parse failed", "src", applog.Trunc(src, 400))
}

// loggedRunner records a task kernel run: start, end with duration, and
// failure; a panic is logged with its stack before the kernel's own
// recover turns it into the task's error.
func loggedRunner(spec tasks.Spec, fn tasks.Runner) tasks.Runner {
	return func(r *tasks.Reporter) (err error) {
		start := time.Now()
		attrs := []any{"kind", string(spec.Kind), "title", spec.Title, "source", string(spec.Source), "origin", spec.Origin, "path", spec.Path}
		slog.Info("task start", attrs...)
		defer func() {
			if p := recover(); p != nil {
				applog.LogPanic("task:"+spec.Title, p)
				panic(p)
			}
			attrs = append(attrs, applog.Since(start))
			if err != nil {
				slog.Warn("task failed", append(attrs, "err", err)...)
			} else {
				slog.Info("task end", attrs...)
			}
		}()
		return fn(r)
	}
}

// aiToolCalls remembers when a tool call was issued, so its result line
// carries the duration as the model saw it.
var aiToolCalls sync.Map // callID → time.Time

const aiLogArgsMax = 800

// logAssistMessage records one finished model or tool message: streaming
// chunk counts (debug), token usage, tool calls with their arguments and
// tool results with durations.
func logAssistMessage(runID string, id int, role schema.RoleType, toolName string, msg *schema.Message, streaming bool, chunks int, start time.Time, streamErr error) {
	if streamErr != nil && !errors.Is(streamErr, errAssistantStopped) {
		slog.Error("ai stream error", "run", runID, "msg", id, "role", string(role), "chunks", chunks, "err", streamErr, applog.Since(start))
	}
	if msg == nil {
		return
	}
	if streaming {
		slog.Debug("ai stream", "run", runID, "msg", id, "role", string(role), "chunks", chunks,
			"text_runes", len([]rune(msg.Content)), "reasoning_runes", len([]rune(msg.ReasoningContent)), applog.Since(start))
	}
	if m := msg.ResponseMeta; m != nil {
		attrs := []any{"run", runID, "msg", id, "finish", m.FinishReason}
		if u := m.Usage; u != nil {
			attrs = append(attrs, "prompt_tokens", u.PromptTokens, "completion_tokens", u.CompletionTokens, "total_tokens", u.TotalTokens)
		}
		slog.Info("ai response", attrs...)
	}
	switch role {
	case schema.Assistant:
		for _, tc := range msg.ToolCalls {
			aiToolCalls.Store(tc.ID, time.Now())
			slog.Info("ai tool call", "run", runID, "tool", tc.Function.Name, "call", tc.ID,
				"args", applog.Trunc(tc.Function.Arguments, aiLogArgsMax))
		}
	case schema.Tool:
		attrs := []any{"run", runID, "tool", toolName, "call", msg.ToolCallID,
			"result", applog.Trunc(msg.Content, aiLogArgsMax)}
		if v, ok := aiToolCalls.LoadAndDelete(msg.ToolCallID); ok {
			attrs = append(attrs, "ms", time.Since(v.(time.Time)).Milliseconds())
		}
		slog.Info("ai tool result", attrs...)
	}
}

// logMCPCall wraps one in-process MCP tool handler call.
func logMCPCall(name string, input any, start time.Time, res *mcp.CallToolResult, err error) {
	attrs := []any{"tool", name, "args", applog.Trunc(fmt.Sprintf("%+v", input), aiLogArgsMax), applog.Since(start)}
	switch {
	case err != nil:
		slog.Error("mcp call error", append(attrs, "err", err)...)
	case res != nil && res.IsError:
		var txt string
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				txt += t.Text
			}
		}
		slog.Warn("mcp call tool error", append(attrs, "result", applog.Trunc(txt, aiLogArgsMax))...)
	default:
		slog.Debug("mcp call", attrs...)
	}
}

func init() {
	assistant.OnParsePanic = func(p any) { applog.LogPanic("openui parse", p) }
}

// settingsLogGroups is 设置 → 日志: level, open folder, export zip.
func (a *App) settingsLogGroups() []components.SettingsGroup {
	cur := logLevelSetting(a.store.settings())
	desc := "当前日志：" + applog.Path()
	if applog.Path() == "" {
		desc = "日志文件夹：" + applog.Dir()
	}
	lastLogExport.mu.Lock()
	exp := "把全部日志打包成 ZIP，保存到下载文件夹"
	if lastLogExport.err != "" {
		exp = "导出失败：" + lastLogExport.err
	} else if lastLogExport.path != "" {
		exp = "已导出：" + lastLogExport.path
	}
	lastLogExport.mu.Unlock()
	items := []components.SettingsItem{
		settingsSelectItem("level", "日志级别", "debug 会额外记录流式分片与每次工具调用；SUNVALE_LOG 环境变量优先", cur, applog.Levels, func(v string) {
			a.applySettings(func(s *appSettings) { s.LogLevel = v })
			slog.Warn("log level changed", "level", v)
		}),
		settingsActionItem("open", "日志文件夹", desc, "打开日志文件夹", openLogFolder),
		settingsActionItem("export", "导出日志", exp, "导出日志 (zip)", exportLogsAction),
	}
	return []components.SettingsGroup{{ID: "logs", Items: items}}
}
