package main

// assistant_a2a.go is the A2A JSON-RPC surface (message/send, tasks/get,
// tasks/cancel) and the in-memory task store. Admin-only; shares the same
// in-process sunvale tools as the pane. Split from assistant_http.go (LAY2).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	assistant "sunvale/internal/assistant"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	a2aVersion       = "1.0"
	a2aMaxTasks      = 200
	a2aTaskTextLimit = 64 * 1024
)

type a2aTask struct {
	ID        string
	ContextID string
	State     string
	Created   time.Time
	Updated   time.Time
	Text      string
	Events    []aiTurn
	Choice    *aiChoice
	Err       string
	cancel    context.CancelFunc
}

type a2aTaskStore struct {
	mu    sync.Mutex
	tasks map[string]*a2aTask
	order []string
}

var assistantA2ATasks = &a2aTaskStore{tasks: map[string]*a2aTask{}}

func newA2AID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *a2aTaskStore) add(t *a2aTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[t.ID] = t
	s.order = append(s.order, t.ID)
	for len(s.order) > a2aMaxTasks {
		id := s.order[0]
		s.order = s.order[1:]
		if old := s.tasks[id]; old != nil && old.cancel != nil && old.State == "working" {
			old.cancel()
		}
		delete(s.tasks, id)
	}
}

func (s *a2aTaskStore) view(id string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, false
	}
	return a2aTaskView(s.copyLocked(t)), true
}

func (s *a2aTaskStore) cancel(id string) (map[string]any, bool) {
	s.mu.Lock()
	t, ok := s.tasks[id]
	if ok {
		if t.cancel != nil && t.State == "working" {
			t.cancel()
		}
		if t.State == "working" {
			t.State = "canceled"
		}
		t.Updated = time.Now()
	}
	var view map[string]any
	if ok {
		view = a2aTaskView(s.copyLocked(t))
	}
	s.mu.Unlock()
	return view, ok
}

func (s *a2aTaskStore) copyLocked(t *a2aTask) *a2aTask {
	copy := *t
	copy.Events = append([]aiTurn(nil), t.Events...)
	if t.Choice != nil {
		ch := *t.Choice
		ch.Options = append([]string(nil), t.Choice.Options...)
		ch.On = append([]bool(nil), t.Choice.On...)
		copy.Choice = &ch
	}
	return &copy
}

func (s *a2aTaskStore) finish(id, text, errText string, events []aiTurn, choice *aiChoice, canceled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return
	}
	if t.State == "canceled" {
		if t.Text == "" && text != "" {
			t.Text = text
			t.Events = events
		}
		t.cancel = nil
		return
	}
	t.Updated = time.Now()
	switch {
	case canceled:
		t.State = "canceled"
	case errText != "":
		t.State = "failed"
		t.Err = errText
	case choice != nil:
		t.State = "input-required"
	default:
		t.State = "completed"
	}
	t.Text = text
	t.Events = events
	t.Choice = choice
	t.cancel = nil
}

func a2aTaskView(t *a2aTask) map[string]any {
	status := map[string]any{"state": t.State, "timestamp": t.Updated.UTC().Format(time.RFC3339Nano)}
	if t.Err != "" {
		status["message"] = map[string]any{
			"role": "agent", "messageId": t.ID + "-error",
			"parts": []any{map[string]any{"kind": "text", "text": t.Err}},
		}
	}
	artifacts := []any{}
	if t.Text != "" {
		artifacts = append(artifacts, map[string]any{
			"artifactId": t.ID + "-answer",
			"name":       "answer",
			"parts":      []any{map[string]any{"kind": "text", "text": t.Text}},
		})
	}
	for i, ev := range t.Events {
		if strings.TrimSpace(ev.Text) == "" {
			continue
		}
		artifacts = append(artifacts, map[string]any{
			"artifactId": fmt.Sprintf("%s-event-%d", t.ID, i),
			"name":       ev.Role,
			"parts":      []any{map[string]any{"kind": "text", "text": ev.Text}},
		})
	}
	history := []any{}
	statusPart := map[string]any{"kind": "text", "text": a2aStateText(t.State)}
	if t.Err != "" {
		statusPart["text"] = t.Err
	}
	history = append(history, map[string]any{
		"role": "agent", "messageId": t.ID + "-status",
		"parts": []any{statusPart},
	})
	out := map[string]any{
		"id":        t.ID,
		"contextId": t.ContextID,
		"status":    status,
		"artifacts": artifacts,
		"history":   history,
		"metadata":  map[string]any{"createdAt": t.Created.UTC().Format(time.RFC3339Nano)},
	}
	out["kind"] = "task"
	if t.Choice != nil {
		out["metadata"].(map[string]any)["choice"] = t.Choice
	}
	return out
}

func a2aStateText(state string) string {
	switch state {
	case "working":
		return "正在处理。"
	case "input-required":
		return "需要用户选择后继续。"
	case "canceled":
		return "任务已取消。"
	case "failed":
		return "任务失败。"
	default:
		return "任务已完成。"
	}
}

func (a *App) assistantA2ACard() map[string]any {
	base := strings.TrimSuffix(a.remoteLANURL(), "/")
	disk, _ := loadAssistantConfig()
	var skills []any
	if !assistant.SkillDisabled(disk.Prefs, assistant.SkillFiles) {
		skills = append(skills, map[string]any{
			"id":          "files",
			"name":        "文件助手",
			"description": "读取指定文件，搜索和整理文件夹，生成重命名方案，压缩、解压并调用受控的本地文件工具。",
			"tags":        []string{"files", "search", "rename", "archive"},
		})
	}
	if !assistant.SkillDisabled(disk.Prefs, assistant.SkillMemory) {
		skills = append(skills, map[string]any{
			"id":          "memory",
			"name":        "会话与记忆",
			"description": "使用助手的历史会话和跨会话短记忆。",
			"tags":        []string{"memory", "history"},
		})
	}
	return map[string]any{
		"protocolVersion": a2aVersion,
		"name":            "Sunvale 文件助手",
		"description":     "读取指定文件、搜索、整理、重命名并执行受控的文件操作。",
		"url":             base + "/a2a",
		"version":         remoteAdminVersion,
		"capabilities": map[string]any{
			"streaming":              false,
			"pushNotifications":      false,
			"stateTransitionHistory": true,
		},
		"defaultInputModes":  []string{"text/plain", "application/json"},
		"defaultOutputModes": []string{"text/plain", "application/json"},
		"skills":             skills,
	}
}

func (a *App) remoteHandleA2ACard(w http.ResponseWriter, r *http.Request) {
	// Discovery stays public so a client can find the endpoint before it
	// has the token. The card contains no account data or provider keys.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(a.assistantA2ACard())
}

type a2aRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type a2aMessage struct {
	Role      string          `json:"role"`
	MessageID string          `json:"messageId"`
	ContextID string          `json:"contextId,omitempty"`
	Parts     []a2aPart       `json:"parts"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type a2aPart struct {
	Kind     string          `json:"kind"`
	Text     string          `json:"text,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	File     *a2aFilePart    `json:"file,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type a2aFilePart struct {
	Name     string `json:"name,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Bytes    string `json:"bytes,omitempty"`
	URI      string `json:"uri,omitempty"`
}

type a2aSendParams struct {
	Message   a2aMessage     `json:"message"`
	Metadata  a2aRunMetadata `json:"metadata"`
	ContextID string         `json:"contextId,omitempty"`
}

type a2aGetParams struct {
	ID string `json:"id"`
}

type a2aCancelParams struct {
	ID string `json:"id"`
}

type a2aRunMetadata struct {
	Folder    string   `json:"folder,omitempty"`
	Autopilot bool     `json:"autopilot,omitempty"`
	Files     []string `json:"files,omitempty"`
}

func (a *App) remoteHandleA2A(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req a2aRPCRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		a2aWriteError(w, req.ID, -32700, "parse error")
		return
	}
	if req.JSONRPC != "2.0" {
		a2aWriteError(w, req.ID, -32600, "invalid request")
		return
	}
	switch req.Method {
	case "message/send":
		a.a2aMessageSend(w, req)
	case "tasks/get":
		var p a2aGetParams
		if json.Unmarshal(req.Params, &p) != nil || p.ID == "" {
			a2aWriteError(w, req.ID, -32602, "task id required")
			return
		}
		view, ok := assistantA2ATasks.view(p.ID)
		if !ok {
			a2aWriteError(w, req.ID, -32001, "task not found")
			return
		}
		a2aWriteResult(w, req.ID, view)
	case "tasks/cancel":
		var p a2aCancelParams
		if json.Unmarshal(req.Params, &p) != nil || p.ID == "" {
			a2aWriteError(w, req.ID, -32602, "task id required")
			return
		}
		view, ok := assistantA2ATasks.cancel(p.ID)
		if !ok {
			a2aWriteError(w, req.ID, -32001, "task not found")
			return
		}
		a2aWriteResult(w, req.ID, view)
	default:
		a2aWriteError(w, req.ID, -32601, "method not found")
	}
}

func (a *App) a2aMessageSend(w http.ResponseWriter, req a2aRPCRequest) {
	var p a2aSendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		a2aWriteError(w, req.ID, -32602, "invalid params")
		return
	}
	text, files, err := a2aTextAndFiles(p.Message.Parts)
	if err != nil {
		a2aWriteError(w, req.ID, -32602, err.Error())
		return
	}
	files = append(files, p.Metadata.Files...)
	if strings.TrimSpace(text) == "" && len(files) == 0 {
		a2aWriteError(w, req.ID, -32602, "message has no text or files")
		return
	}
	root, err := a.assistantRemoteRoot(p.Metadata.Folder)
	if err != nil {
		a2aWriteError(w, req.ID, -32602, err.Error())
		return
	}
	disk, err := loadAssistantConfig()
	if err != nil {
		a2aWriteError(w, req.ID, -32000, err.Error())
		return
	}
	prov, ok := activeAssistantProvider(disk)
	if !ok || prov.APIKey == "" {
		a2aWriteError(w, req.ID, -32000, "请先配置并激活一个供应商")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &a2aTask{
		ID: newA2AID(), ContextID: p.Message.ContextID, State: "working",
		Created: time.Now(), Updated: time.Now(), cancel: cancel,
	}
	if task.ContextID == "" {
		task.ContextID = p.ContextID
	}
	if task.ContextID == "" {
		task.ContextID = newA2AID()
	}
	assistantA2ATasks.add(task)
	memories := readAssistantMemories(assistantSideRootFor(assistantConfigPath()))
	run := assistRun{
		origin: "a2a",
		prov:   prov, root: root, book: disk.ArchivePasswords,
		files: files, autopilot: p.Metadata.Autopilot || disk.Prefs.Autopilot,
		prefs: disk.Prefs,
		hist:  []*schema.Message{schema.UserMessage(text)}, memories: memories,
	}
	if a.store != nil {
		run.hidden = a.store.remoteShowHidden()
		run.freqPaths = a.store.frequentPaths(20)
	}
	go func(taskID string) {
		assistantRunMu.Lock()
		result, runErr := runAssistantHeadless(ctx, run)
		saved := sunvaleToolState.drainPending()
		sunvaleToolState.drainUndos()
		sunvaleToolState.drainNav()
		memAdd, memDrop := sunvaleToolState.drainMemory()
		assistantRunMu.Unlock()
		if len(saved)+len(memAdd)+len(memDrop) > 0 {
			a.assistantApplyHeadlessSideEffects(saved, memAdd, memDrop)
		}
		canceled := ctx.Err() != nil || errors.Is(runErr, errAssistantStopped)
		if runErr != nil && !canceled {
			assistantA2ATasks.finish(taskID, "", runErr.Error(), nil, nil, false)
			return
		}
		assistantA2ATasks.finish(taskID, result.Text, "", result.Events, result.Choice, canceled)
	}(task.ID)
	view, _ := assistantA2ATasks.view(task.ID)
	a2aWriteResult(w, req.ID, view)
}

// assistantApplyHeadlessSideEffects persists only state that is safe
// without a window: archive-password additions and memory changes. It does
// not reload a tab, push an undo entry into a closed window, or navigate.
func (a *App) assistantApplyHeadlessSideEffects(saved []archivePassword, add []aiMemory, drop []string) {
	path := assistantConfigPath()
	if path == "" {
		return
	}
	assistantConfigMu.Lock()
	assistantDiskMu.Lock()
	disk, _, err := readAssistantDisk(path)
	if err == nil {
		if len(saved) > 0 {
			disk.ArchivePasswords = append(disk.ArchivePasswords, saved...)
			if len(disk.ArchivePasswords) > assistMaxArchivePasswords {
				disk.ArchivePasswords = disk.ArchivePasswords[len(disk.ArchivePasswords)-assistMaxArchivePasswords:]
			}
			if writeAssistantDisk(path, disk) == nil {
				markAssistantConfigDirty()
			}
		}
	}
	assistantDiskMu.Unlock()
	assistantConfigMu.Unlock()
	if err != nil {
		return
	}
	root := assistantSideRootFor(path)
	u := &assistantUI{loaded: true, diskPath: path, memories: readAssistantMemories(root)}
	u.applyMemory(add, drop)
}

func a2aTextAndFiles(parts []a2aPart) (string, []string, error) {
	var text strings.Builder
	var files []string
	for _, p := range parts {
		if p.Kind == "" || p.Kind == "text" {
			if p.Text != "" {
				if text.Len() > 0 {
					text.WriteByte('\n')
				}
				text.WriteString(p.Text)
			}
			continue
		}
		if p.Kind == "data" && len(p.Data) > 0 {
			var v struct {
				Path  string   `json:"path"`
				Paths []string `json:"paths"`
			}
			if json.Unmarshal(p.Data, &v) == nil {
				if v.Path != "" {
					files = append(files, v.Path)
				}
				files = append(files, v.Paths...)
			}
		}
		if p.Kind == "file" && p.File != nil {
			if p.File.URI != "" {
				path, err := fileURIPath(p.File.URI)
				if err != nil {
					return "", nil, err
				}
				files = append(files, path)
			} else if p.File.Name != "" {
				files = append(files, p.File.Name)
			}
		}
	}
	if len(files) > assistReadFiles {
		return "", nil, fmt.Errorf("一次最多指定 %d 个文件", assistReadFiles)
	}
	return strings.TrimSpace(text.String()), files, nil
}

// fileURIPath maps an A2A file URI to a local path. Only file:// URIs are
// accepted: remote HTTP URLs would make the local model fetch a network
// resource the administrator did not name.
func fileURIPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("只支持 file:// 文件 URI")
	}
	path := u.Path
	if u.Host != "" && u.Host != "localhost" {
		return "", fmt.Errorf("不支持远程主机上的 file URI")
	}
	if path == "" {
		return "", fmt.Errorf("文件 URI 没有路径")
	}
	return filepath.Clean(path), nil
}

func (a *App) assistantRemoteRoot(folder string) (string, error) {
	if folder == "" {
		mount, ok := a.remoteRootMount()
		if !ok {
			return "", fmt.Errorf("远程根目录已停用")
		}
		return storageDiskRoot(mount), nil
	}
	path, ok := a.remoteDiskPath(folder)
	if !ok {
		return "", fmt.Errorf("folder 不在已启用的存储中")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("folder 不是可读文件夹")
	}
	return path, nil
}

func a2aWriteResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func a2aWriteError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

// a2aTaskFilesForTest gives tests a deterministic view of the named file
// list without invoking the model.
func a2aTaskFilesForTest(root string, files []string) []string {
	out := make([]string, 0, len(files))
	for _, p := range files {
		if filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
			continue
		}
		out = append(out, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "/"))))
	}
	sort.Strings(out)
	return out
}
