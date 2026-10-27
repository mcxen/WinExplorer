package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	assistant "sunvale/internal/assistant"
)

// assistant_memory.go keeps two kinds of disk state that assistant.json must
// not hold. A session is one transcript. Memory is a short list of facts the
// user asked to keep across sessions. API keys and passwords stay in
// assistant.json; file excerpts attached for one turn stay in memory only,
// because Model is not part of the session file.

const (
	assistMaxSessions = 30
	assistMaxTurns    = 80
	assistMaxMemory   = 40
	assistMemoryRunes = 120
	assistTurnRunes   = 8000
	assistHistMsgs    = 24
	assistHistRunes   = 6000
	assistTitleRunes  = 24
)

// assistantDiskMu serializes every assistant file in this process. Two
// windows share the directory and must not tear an index write.
var assistantDiskMu sync.Mutex

type aiMemory = assistant.Memory
type aiSessionMeta = assistant.SessionMeta
type assistIndex = assistant.SessionIndex
type aiSessionFile = assistant.SessionFile

func (u *assistantUI) assistRoot() string {
	if u.diskPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(u.diskPath), "assistant")
}

func (u *assistantUI) memoryPath() string {
	return filepath.Join(u.assistRoot(), "memory.json")
}

func (u *assistantUI) indexPath() string {
	return filepath.Join(u.assistRoot(), "index.json")
}

func (u *assistantUI) sessionPath(id string) string {
	return filepath.Join(u.assistRoot(), "sessions", id+".json")
}

func (u *assistantUI) loadSide() {
	if u.diskPath == "" {
		return
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	items, err := readMemoryFile(u.memoryPath())
	if err != nil {
		u.memoryBroken = true
	} else {
		u.memories = items
	}
	idx, ok := readIndexFile(u.indexPath())
	if !ok {
		u.indexBroken = true
		return
	}
	u.sessions = idx.Sessions
	id := idx.ActiveID
	if id == "" && len(u.sessions) > 0 {
		id = u.sessions[0].ID
	}
	if id == "" {
		return
	}
	_ = u.loadSessionLocked(id)
}

func (u *assistantUI) loadSession(id string) error {
	if !safeSessionID(id) {
		return fmt.Errorf("会话不存在")
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	return u.loadSessionLocked(id)
}

func (u *assistantUI) loadSessionLocked(id string) error {
	data, err := os.ReadFile(u.sessionPath(id))
	if err != nil {
		return err
	}
	var file aiSessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	if file.ID == "" {
		file.ID = id
	}
	u.sessionID = file.ID
	u.sessionTitle = file.Title
	u.sessionRenamed = file.Renamed
	u.created = file.Created
	u.folder = file.Folder
	u.turns = normalizeTurns(file.Turns)
	u.halted = false
	u.resetLive()
	return nil
}

func (u *assistantUI) clearSession() {
	u.sessionID = ""
	u.sessionTitle = ""
	u.sessionRenamed = false
	u.created = time.Time{}
	u.folder = ""
	u.turns = nil
	u.halted = false
	u.chipLit = nil
	u.resetLive()
}

// persistSession writes the current transcript. An empty unsaved chat is not
// a session yet: quitting on a blank "新会话" should reopen the previous one.
func (u *assistantUI) persistSession(folder string) {
	if u.diskPath == "" || len(u.turns) == 0 {
		return
	}
	if folder != "" {
		u.folder = folder
	}
	if len(u.turns) > assistMaxTurns {
		u.turns = append([]aiTurn(nil), u.turns[len(u.turns)-assistMaxTurns:]...)
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	if u.sessionID == "" {
		u.sessionID = newID()
	}
	if !safeSessionID(u.sessionID) {
		return
	}
	if u.created.IsZero() {
		u.created = time.Now()
	}
	if !u.sessionRenamed && (strings.TrimSpace(u.sessionTitle) == "" || u.sessionTitle == "新会话") {
		u.sessionTitle = sessionTitleFrom(u.turns)
	}
	now := time.Now()
	file := aiSessionFile{
		ID: u.sessionID, Title: u.sessionTitle, Created: u.created, Updated: now,
		Folder: u.folder, Renamed: u.sessionRenamed, Turns: diskTurns(u.turns),
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	if writePrivate(u.sessionPath(u.sessionID), data) != nil {
		return
	}
	if u.indexBroken {
		return
	}
	u.upsertIndexLocked(aiSessionMeta{
		ID: u.sessionID, Title: u.sessionTitle, Updated: now,
		Folder: u.folder, Preview: sessionPreview(u.turns),
		Turns: len(u.turns), Renamed: u.sessionRenamed,
	})
}

func (u *assistantUI) upsertIndexLocked(meta aiSessionMeta) {
	idx, ok := readIndexFile(u.indexPath())
	if !ok {
		u.indexBroken = true
		return
	}
	next := make([]aiSessionMeta, 0, len(idx.Sessions)+1)
	for _, s := range idx.Sessions {
		if s.ID == meta.ID {
			meta.Pinned = s.Pinned
		} else if safeSessionID(s.ID) {
			next = append(next, s)
		}
	}
	next = append(next, meta)
	sort.Slice(next, func(i, j int) bool { return next[i].Updated.After(next[j].Updated) })
	keep, drop := capSessions(next, meta.ID, assistMaxSessions)
	for _, s := range drop {
		_ = os.Remove(u.sessionPath(s.ID))
	}
	idx.ActiveID = meta.ID
	idx.Sessions = keep
	if writeIndexFile(u.indexPath(), idx) != nil {
		return
	}
	u.sessions = keep
}

func (u *assistantUI) touchActive() {
	if u.diskPath == "" || u.sessionID == "" || u.indexBroken {
		return
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	idx, ok := readIndexFile(u.indexPath())
	if !ok {
		u.indexBroken = true
		return
	}
	idx.ActiveID = u.sessionID
	if writeIndexFile(u.indexPath(), idx) != nil {
		return
	}
}

func (u *assistantUI) deleteSession(id string) {
	if u.diskPath == "" || !safeSessionID(id) || u.indexBroken {
		return
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	_ = os.Remove(u.sessionPath(id))
	idx, ok := readIndexFile(u.indexPath())
	if !ok {
		u.indexBroken = true
		return
	}
	next := make([]aiSessionMeta, 0, len(idx.Sessions))
	for _, s := range idx.Sessions {
		if s.ID != id {
			next = append(next, s)
		}
	}
	idx.Sessions = next
	if idx.ActiveID == id {
		idx.ActiveID = ""
		if len(next) > 0 {
			idx.ActiveID = next[0].ID
		}
	}
	if writeIndexFile(u.indexPath(), idx) != nil {
		return
	}
	u.sessions = next
}

func (u *assistantUI) sessionLabel() string {
	if title := strings.TrimSpace(u.sessionTitle); title != "" && title != "新会话" {
		return title
	}
	if title := sessionTitleFrom(u.turns); title != "新会话" {
		return title
	}
	return "新会话"
}

func (u *assistantUI) historyWhere() string {
	root := u.assistRoot()
	if root == "" {
		return "没有用户配置目录，这段对话只会留在本次运行里。"
	}
	return "会话和记忆在 " + homeish(root) + "，和密钥分开。解压密码请到设置 → 助手管理。"
}

func (u *assistantUI) addMemory(text string) (string, error) {
	clean, err := cleanMemory(text)
	if err != nil {
		return "", err
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	if u.hasMemoryLocked(clean) {
		return "已经记得", nil
	}
	if u.memoryBroken {
		return "", fmt.Errorf("记忆文件读不出来，没有覆盖它")
	}
	u.memories = append(u.memories, aiMemory{ID: newID(), Text: clean, Updated: time.Now()})
	u.trimMemories()
	if err := u.saveMemoryLocked(); err != nil {
		return "", err
	}
	return "已记住", nil
}

func (u *assistantUI) deleteMemory(id string) {
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	next := u.memories[:0]
	for _, m := range u.memories {
		if m.ID != id {
			next = append(next, m)
		}
	}
	u.memories = next
	if u.memoryBroken {
		return
	}
	_ = u.saveMemoryLocked()
}

func (u *assistantUI) applyMemory(add []aiMemory, drop []string) {
	if len(add) == 0 && len(drop) == 0 {
		return
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	if len(drop) > 0 {
		next := u.memories[:0]
		for _, m := range u.memories {
			if containsStr(drop, m.ID) {
				continue
			}
			next = append(next, m)
		}
		u.memories = next
	}
	for _, m := range add {
		if strings.TrimSpace(m.Text) == "" || u.hasMemoryLocked(m.Text) {
			continue
		}
		u.memories = append(u.memories, m)
	}
	u.trimMemories()
	if u.memoryBroken {
		return
	}
	_ = u.saveMemoryLocked()
}

func (u *assistantUI) trimMemories() {
	if len(u.memories) > assistMaxMemory {
		u.memories = append([]aiMemory(nil), u.memories[len(u.memories)-assistMaxMemory:]...)
	}
}

func (u *assistantUI) hasMemoryLocked(text string) bool {
	for _, m := range u.memories {
		if strings.EqualFold(m.Text, text) {
			return true
		}
	}
	return false
}

func (u *assistantUI) saveMemoryLocked() error {
	return assistant.WriteMemories(u.memoryPath(), u.memories)
}

func readMemoryFile(path string) ([]aiMemory, error) {
	return assistant.ReadMemories(path, assistMaxMemory)
}

func readIndexFile(path string) (assistIndex, bool) {
	return assistant.ReadIndex(path)
}

func writeIndexFile(path string, idx assistIndex) error {
	return assistant.WriteIndex(path, idx)
}

func writePrivate(path string, data []byte) error {
	return assistant.WritePrivate(path, data)
}

func normalizeTurns(turns []aiTurn) []aiTurn {
	return assistant.NormalizeTurns(turns, assistMaxTurns, sunvaleToolState.hasPlan)
}

func diskTurns(in []aiTurn) []aiTurn {
	return assistant.DiskTurns(in, assistTurnRunes)
}

func capSessions(list []aiSessionMeta, active string, max int) (keep, drop []aiSessionMeta) {
	return assistant.CapSessions(list, active, max)
}

func sessionTitleFrom(turns []aiTurn) string {
	return assistant.SessionTitle(turns, assistTitleRunes)
}

func sessionPreview(turns []aiTurn) string {
	return assistant.SessionPreview(turns, 42)
}

func memoryPrompt(items []aiMemory) string {
	return assistant.MemoryPrompt(items)
}

func cleanMemory(text string) (string, error) {
	return assistant.CleanMemory(text, assistMemoryRunes)
}

func memorySecret(text string) bool { return assistant.MemorySecret(text) }

func limitRunes(s string, n int) string { return assistant.LimitRunes(s, n) }

func safeSessionID(id string) bool { return assistant.SafeSessionID(id) }

func containsStr(list []string, s string) bool { return assistant.Contains(list, s) }

func homeish(path string) string { return assistant.Homeish(path) }

func assistWhen(t time.Time) string { return assistant.When(t) }

// editIndexLocked rewrites one index entry in place (no reorder, no touch).
func (u *assistantUI) editIndex(id string, edit func(*aiSessionMeta)) bool {
	if u.diskPath == "" || !safeSessionID(id) || u.indexBroken {
		return false
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	idx, ok := readIndexFile(u.indexPath())
	if !ok {
		u.indexBroken = true
		return false
	}
	found := false
	for i := range idx.Sessions {
		if idx.Sessions[i].ID == id {
			edit(&idx.Sessions[i])
			found = true
		}
	}
	if !found || writeIndexFile(u.indexPath(), idx) != nil {
		return false
	}
	u.sessions = idx.Sessions
	return true
}

func (u *assistantUI) pinSession(id string, on bool) {
	u.editIndex(id, func(m *aiSessionMeta) { m.Pinned = on })
}

// renameSession sets a user title that auto titling keeps; an empty title
// hands the session back to auto titling.
func (u *assistantUI) renameSession(id, title string) {
	title = limitRunes(strings.TrimSpace(title), 60)
	renamed := title != ""
	if !u.editIndex(id, func(m *aiSessionMeta) {
		m.Renamed = renamed
		if renamed {
			m.Title = title
		}
	}) {
		return
	}
	if id == u.sessionID {
		u.sessionRenamed = renamed
		if renamed {
			u.sessionTitle = title
		} else {
			u.sessionTitle = ""
		}
		u.persistSessionFileOnly()
		return
	}
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	data, err := os.ReadFile(u.sessionPath(id))
	if err != nil {
		return
	}
	var file aiSessionFile
	if json.Unmarshal(data, &file) != nil {
		return
	}
	file.Renamed = renamed
	if renamed {
		file.Title = title
	}
	if out, err := json.MarshalIndent(file, "", "  "); err == nil {
		_ = writePrivate(u.sessionPath(id), out)
	}
}

// persistSessionFileOnly rewrites the open session's title fields without
// bumping Updated, so a rename does not reorder the list.
func (u *assistantUI) persistSessionFileOnly() {
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	data, err := os.ReadFile(u.sessionPath(u.sessionID))
	if err != nil {
		return
	}
	var file aiSessionFile
	if json.Unmarshal(data, &file) != nil {
		return
	}
	file.Renamed = u.sessionRenamed
	if u.sessionTitle != "" {
		file.Title = u.sessionTitle
	}
	if out, err := json.MarshalIndent(file, "", "  "); err == nil {
		_ = writePrivate(u.sessionPath(u.sessionID), out)
	}
}
