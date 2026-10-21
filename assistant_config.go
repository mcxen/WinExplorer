package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	assistant "sunvale/internal/assistant"
)

// assistant_config.go is the headless half of assistant.json. The desktop
// pane keeps its own copy in assistantUI; A2A and HTTP MCP callers read and
// write through here so they see the same providers, preferences, and
// archive-password labels. API keys and archive-password values never leave
// this file through an HTTP response.

// assistantConfigMu orders a reader (the open pane) against a headless
// writer. assistantDiskMu alone is file-level: it cannot stop the pane from
// reading, editing and then saving a stale provider list.
var assistantConfigMu sync.Mutex

// assistantConfigEpoch increases on every assistant.json write. Unlike a
// dirty bool, an epoch survives several panes consuming it: a save still
// knows whether the disk changed after its own snapshot.
var assistantConfigEpochMu sync.Mutex
var assistantConfigEpoch uint64

func markAssistantConfigDirty() {
	assistantConfigEpochMu.Lock()
	assistantConfigEpoch++
	assistantConfigEpochMu.Unlock()
}

func assistantConfigStamp() uint64 {
	assistantConfigEpochMu.Lock()
	defer assistantConfigEpochMu.Unlock()
	return assistantConfigEpoch
}

type assistantConfigView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	BaseURL       string `json:"baseURL"`
	Model         string `json:"model"`
	KeyConfigured bool   `json:"keyConfigured"`
	Active        bool   `json:"active"`
}

type assistantConfigStatus struct {
	Providers []assistantConfigView `json:"providers"`
	ActiveID  string                `json:"activeID,omitempty"`
	Full      bool                  `json:"full"`
	Autopilot bool                  `json:"autopilot"`
	Width     float32               `json:"width,omitempty"`
	Sessions  int                   `json:"sessions"`
	Memories  int                   `json:"memories"`
	Passwords int                   `json:"passwords"`
}

type assistantConfigInput struct {
	ID      string  `json:"id,omitempty"`
	Name    string  `json:"name"`
	BaseURL string  `json:"baseURL"`
	Model   string  `json:"model"`
	APIKey  string  `json:"apiKey,omitempty"`
	Active  bool    `json:"active,omitempty"`
	Width   float32 `json:"width,omitempty"`
	Full    *bool   `json:"full,omitempty"`
	Auto    *bool   `json:"autopilot,omitempty"`
}

// assistantConfigPath is the file assistantUI.ensure uses. Tests can set
// the same path on a headless assistantUI without starting the UI.
func assistantConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, stateDirName, "assistant.json")
}

func readAssistantDisk(path string) (assistantDisk, bool, error) {
	var disk assistantDisk
	absent, err := assistant.ReadJSON(path, &disk)
	return disk, absent, err
}

func writeAssistantDisk(path string, disk assistantDisk) error {
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	return assistant.WritePrivate(path, data)
}

// loadAssistantConfig returns the provider list and preferences without
// constructing an assistantUI or touching global window state.
func loadAssistantConfig() (assistantDisk, error) {
	path := assistantConfigPath()
	if path == "" {
		return assistantDisk{}, fmt.Errorf("没有用户配置目录")
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return assistantDisk{}, err
	}
	if disk.Prefs.Width < assistantMinW {
		disk.Prefs.Width = assistantPaneW
	}
	return disk, nil
}

func assistantConfigStatusOf(disk assistantDisk) assistantConfigStatus {
	st := assistantConfigStatus{
		ActiveID: disk.ActiveID, Full: disk.Prefs.Full, Autopilot: disk.Prefs.Autopilot,
		Width: disk.Prefs.Width, Passwords: len(disk.ArchivePasswords),
	}
	for _, p := range disk.Providers {
		st.Providers = append(st.Providers, assistantConfigView{
			ID: p.ID, Name: p.Name, BaseURL: p.BaseURL, Model: p.Model,
			KeyConfigured: p.APIKey != "", Active: p.ID == disk.ActiveID,
		})
	}
	path := assistantConfigPath()
	if root := assistantSideRootFor(path); root != "" {
		assistantDiskMu.Lock()
		defer assistantDiskMu.Unlock()
		st.Memories = len(readAssistantMemories(root))
		st.Sessions = len(readAssistantSessions(root))
	}
	return st
}

// saveAssistantProvider creates or updates one provider. An omitted or
// empty apiKey keeps the stored key, which is how an MCP client can change
// the URL/model without ever reading the secret back.
func saveAssistantProvider(in assistantConfigInput) (aiProvider, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Model = strings.TrimSpace(in.Model)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.Name == "" || in.BaseURL == "" || in.Model == "" {
		return aiProvider{}, fmt.Errorf("name、baseURL 和 model 不能为空")
	}
	path := assistantConfigPath()
	if path == "" {
		return aiProvider{}, fmt.Errorf("没有用户配置目录")
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return aiProvider{}, fmt.Errorf("配置文件读不出来，没有覆盖它")
	}
	var old *aiProvider
	for i := range disk.Providers {
		if disk.Providers[i].ID == in.ID || (in.ID == "" && disk.Providers[i].Name == in.Name) {
			copy := disk.Providers[i]
			old = &copy
			break
		}
	}
	id := in.ID
	if old != nil {
		id = old.ID
	}
	if id == "" {
		id = newID()
	}
	if in.APIKey == "" && old != nil {
		in.APIKey = old.APIKey
	}
	if in.APIKey == "" {
		return aiProvider{}, fmt.Errorf("新供应商需要 apiKey")
	}
	p := aiProvider{ID: id, Name: in.Name, BaseURL: in.BaseURL, Model: in.Model, APIKey: in.APIKey}
	found := false
	for i := range disk.Providers {
		if disk.Providers[i].ID == id {
			disk.Providers[i] = p
			found = true
			break
		}
	}
	if !found {
		disk.Providers = append(disk.Providers, p)
	}
	if in.Active || disk.ActiveID == "" {
		disk.ActiveID = id
	}
	if in.Width >= assistantMinW {
		disk.Prefs.Width = in.Width
	}
	if in.Full != nil {
		disk.Prefs.Full = *in.Full
	}
	if in.Auto != nil {
		disk.Prefs.Autopilot = *in.Auto
	}
	if err := writeAssistantDisk(path, disk); err != nil {
		return aiProvider{}, err
	}
	markAssistantConfigDirty()
	return p, nil
}

func activateAssistantProvider(id string) error {
	path := assistantConfigPath()
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return fmt.Errorf("配置文件读不出来")
	}
	found := false
	for _, p := range disk.Providers {
		if p.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("供应商不存在")
	}
	disk.ActiveID = id
	if err := writeAssistantDisk(path, disk); err != nil {
		return err
	}
	markAssistantConfigDirty()
	return nil
}

func deleteAssistantProvider(id string) error {
	path := assistantConfigPath()
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return fmt.Errorf("配置文件读不出来")
	}
	next := disk.Providers[:0]
	found := false
	for _, p := range disk.Providers {
		if p.ID == id {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return fmt.Errorf("供应商不存在")
	}
	disk.Providers = next
	if disk.ActiveID == id {
		disk.ActiveID = ""
		if len(disk.Providers) > 0 {
			disk.ActiveID = disk.Providers[0].ID
		}
	}
	if err := writeAssistantDisk(path, disk); err != nil {
		return err
	}
	markAssistantConfigDirty()
	return nil
}

// activeAssistantProvider resolves the stored active provider, falling
// back to the first one exactly as the pane does.
func activeAssistantProvider(disk assistantDisk) (aiProvider, bool) {
	return assistant.ActiveProvider(disk.Providers, disk.ActiveID)
}

func assistantSideRootFor(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "assistant")
}

func readAssistantMemories(root string) []aiMemory {
	if root == "" {
		return nil
	}
	items, _ := readMemoryFile(filepath.Join(root, "memory.json"))
	return items
}

func readAssistantSessions(root string) []aiSessionMeta {
	if root == "" {
		return nil
	}
	idx, _ := readIndexFile(filepath.Join(root, "index.json"))
	return idx.Sessions
}

// updateAssistantPrefs mutates prefs on disk and bumps the epoch so an open
// pane merges the change. Callers must not hold assistantConfigMu.
func updateAssistantPrefs(fn func(*assistPrefs)) error {
	path := assistantConfigPath()
	if path == "" {
		return fmt.Errorf("没有用户配置目录")
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return fmt.Errorf("配置文件读不出来")
	}
	if disk.Prefs.Width < assistantMinW {
		disk.Prefs.Width = assistantPaneW
	}
	fn(&disk.Prefs)
	if err := writeAssistantDisk(path, disk); err != nil {
		return err
	}
	markAssistantConfigDirty()
	return nil
}

// saveAssistantArchivePassword appends one password book entry from settings.
func saveAssistantArchivePassword(label, secret string) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("解压密码不能为空")
	}
	label = strings.TrimSpace(label)
	path := assistantConfigPath()
	if path == "" {
		return fmt.Errorf("没有用户配置目录")
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return fmt.Errorf("配置文件读不出来")
	}
	if label == "" {
		label = fmt.Sprintf("解压密码 %d", len(disk.ArchivePasswords)+1)
	}
	disk.ArchivePasswords = append(disk.ArchivePasswords, archivePassword{ID: newID(), Label: label, Secret: secret})
	if len(disk.ArchivePasswords) > assistMaxArchivePasswords {
		disk.ArchivePasswords = disk.ArchivePasswords[len(disk.ArchivePasswords)-assistMaxArchivePasswords:]
	}
	if err := writeAssistantDisk(path, disk); err != nil {
		return err
	}
	markAssistantConfigDirty()
	return nil
}

func deleteAssistantArchivePassword(id string) error {
	path := assistantConfigPath()
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return fmt.Errorf("配置文件读不出来")
	}
	next := disk.ArchivePasswords[:0]
	found := false
	for _, p := range disk.ArchivePasswords {
		if p.ID == id {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return fmt.Errorf("解压密码不存在")
	}
	disk.ArchivePasswords = next
	if err := writeAssistantDisk(path, disk); err != nil {
		return err
	}
	markAssistantConfigDirty()
	return nil
}

// clearAssistantMemories truncates memory.json under the assistant side root.
func clearAssistantMemories() error {
	path := assistantConfigPath()
	root := assistantSideRootFor(path)
	if root == "" {
		return fmt.Errorf("没有用户配置目录")
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	return assistant.WriteMemories(filepath.Join(root, "memory.json"), nil)
}

// sunvaleBuiltinToolNames matches the in-process MCP registry in assistant_mcp.go.
// Settings lists them without starting a session on every paint.
func sunvaleBuiltinToolNames() []string {
	return []string{
		"list_dir", "read_file", "read_files", "list_outside", "read_outside",
		"properties", "mkdir", "new_text_file", "copy_items", "move_items",
		"trash_items", "rename_item", "open_item", "open_terminal", "copy_as_path",
		"rename_plan", "zip_files", "unzip", "password_list", "password_save",
		"search_names", "grep_text", "write_text", "folder_summary", "disk_space",
		"current_selection", "navigate_to", "offer_choices",
		"remember", "forget", "memory_list",
		"schedule_list", "schedule_create", "schedule_update", "schedule_delete", "schedule_run",
	}
}
