package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// aiMemory is one fact. Text is what the prompt and the panel show.
type aiMemory struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Updated time.Time `json:"updated,omitempty"`
}

type memoryFile struct {
	Items []aiMemory `json:"items"`
}

// aiSessionMeta is the index row. The transcript lives in its own file so
// rewriting the index does not rewrite every conversation.
type aiSessionMeta struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
	Folder  string    `json:"folder,omitempty"`
	Preview string    `json:"preview,omitempty"`
}

type assistIndex struct {
	ActiveID string          `json:"activeID,omitempty"`
	Sessions []aiSessionMeta `json:"sessions,omitempty"`
}

type aiSessionFile struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Created time.Time `json:"created,omitempty"`
	Updated time.Time `json:"updated"`
	Folder  string    `json:"folder,omitempty"`
	Turns   []aiTurn  `json:"turns,omitempty"`
}

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
	u.created = time.Time{}
	u.folder = ""
	u.turns = nil
	u.halted = false
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
	if strings.TrimSpace(u.sessionTitle) == "" || u.sessionTitle == "新会话" {
		u.sessionTitle = sessionTitleFrom(u.turns)
	}
	now := time.Now()
	file := aiSessionFile{
		ID: u.sessionID, Title: u.sessionTitle, Created: u.created, Updated: now,
		Folder: u.folder, Turns: diskTurns(u.turns),
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
		if s.ID != meta.ID && safeSessionID(s.ID) {
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
	return "会话和记忆在 " + homeish(root) + "，和密钥分开。密码请放密码本。"
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
	data, err := json.MarshalIndent(memoryFile{Items: u.memories}, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(u.memoryPath(), data)
}

func readMemoryFile(path string) ([]aiMemory, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var file memoryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	if len(file.Items) > assistMaxMemory {
		file.Items = file.Items[len(file.Items)-assistMaxMemory:]
	}
	return file.Items, nil
}

func readIndexFile(path string) (assistIndex, bool) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return assistIndex{}, true
	}
	if err != nil {
		return assistIndex{}, false
	}
	var idx assistIndex
	if json.Unmarshal(data, &idx) != nil {
		return assistIndex{}, false
	}
	return idx, true
}

func writeIndexFile(path string, idx assistIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, data)
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeTurns(turns []aiTurn) []aiTurn {
	if len(turns) > assistMaxTurns {
		turns = append([]aiTurn(nil), turns[len(turns)-assistMaxTurns:]...)
	}
	for i := range turns {
		turns[i].Model = ""
		if turns[i].Plan != "" && !sunvaleToolState.hasPlan(turns[i].Plan) {
			turns[i].Plan = ""
		}
		if ch := turns[i].Choice; ch != nil && len(ch.On) != len(ch.Options) {
			on := make([]bool, len(ch.Options))
			copy(on, ch.On)
			ch.On = on
		}
	}
	return turns
}

func diskTurns(in []aiTurn) []aiTurn {
	out := make([]aiTurn, len(in))
	for i, t := range in {
		t.Model = ""
		t.Text = limitRunes(t.Text, assistTurnRunes)
		t.Files = append([]aiAttach(nil), t.Files...)
		if t.Choice != nil {
			ch := *t.Choice
			ch.Options = append([]string(nil), t.Choice.Options...)
			ch.On = append([]bool(nil), t.Choice.On...)
			t.Choice = &ch
		}
		t.OpenSent = append([]string(nil), t.OpenSent...)
		out[i] = t
	}
	return out
}

func capSessions(list []aiSessionMeta, active string, max int) (keep, drop []aiSessionMeta) {
	if max < 1 {
		max = 1
	}
	for _, s := range list {
		if s.ID == active {
			keep = append(keep, s)
			continue
		}
		if len(keep) < max {
			keep = append(keep, s)
			continue
		}
		drop = append(drop, s)
	}
	if len(keep) > max {
		for i := len(keep) - 1; i >= 0; i-- {
			if keep[i].ID != active {
				drop = append(drop, keep[i])
				keep = append(keep[:i], keep[i+1:]...)
				break
			}
		}
	}
	return keep, drop
}

func sessionTitleFrom(turns []aiTurn) string {
	for _, t := range turns {
		if t.Role != "user" {
			continue
		}
		text := strings.Join(strings.Fields(t.Text), " ")
		if text == "" || text == "请阅读这些文件" {
			if len(t.Files) > 0 && t.Files[0].Name != "" {
				return trimRunes(t.Files[0].Name, assistTitleRunes)
			}
			continue
		}
		return trimRunes(text, assistTitleRunes)
	}
	return "新会话"
}

func sessionPreview(turns []aiTurn) string {
	for i := len(turns) - 1; i >= 0; i-- {
		text := strings.Join(strings.Fields(turns[i].Text), " ")
		if text != "" {
			return trimRunes(text, 42)
		}
	}
	return ""
}

func memoryPrompt(items []aiMemory) string {
	if len(items) == 0 {
		return "无"
	}
	var b strings.Builder
	for i, m := range items {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(sessionPlain(m.Text))
	}
	return b.String()
}

func cleanMemory(text string) (string, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", fmt.Errorf("没有要记的内容")
	}
	if len([]rune(text)) > assistMemoryRunes {
		return "", fmt.Errorf("太长，请收成一句话")
	}
	if memorySecret(text) {
		return "", fmt.Errorf("密码和密钥请放进密码本，不要记在记忆里")
	}
	return text, nil
}

func memorySecret(text string) bool {
	s := strings.ToLower(text)
	for _, part := range []string{
		"sk-", "api_key", "apikey", "api-key", "bearer ",
		"-----begin", "password=", "passwd=", "secret=",
		"密码是", "密码:", "密码：", "密钥是", "密钥:", "密钥：",
		"令牌是", "令牌:", "令牌：",
	} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func limitRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func safeSessionID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func containsStr(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func homeish(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return "~" + string(filepath.Separator) + rel
}

func assistWhen(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return t.Format("1月2日 15:04")
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		n := int(d.Minutes())
		if n < 1 {
			n = 1
		}
		return fmt.Sprintf("%d 分钟前", n)
	case d < 24*time.Hour:
		n := int(d.Hours())
		if n < 1 {
			n = 1
		}
		return fmt.Sprintf("%d 小时前", n)
	default:
		return t.Format("1月2日 15:04")
	}
}

func (s *sunvaleTools) hasPlan(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans == nil {
		return false
	}
	_, ok := s.plans[id]
	return ok
}

func (s *sunvaleTools) bindMemory(items []aiMemory) {
	s.mu.Lock()
	s.memory = append([]aiMemory(nil), items...)
	s.memAdd = nil
	s.memDrop = nil
	s.mu.Unlock()
}

func (s *sunvaleTools) drainMemory() (add []aiMemory, drop []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	add, drop = s.memAdd, s.memDrop
	s.memAdd, s.memDrop = nil, nil
	return add, drop
}

type memoryArgs struct {
	Text string `json:"text" jsonschema:"一句要记住或忘掉的话，不要包含密码或密钥"`
}

func (s *sunvaleTools) remember(_ context.Context, _ *mcp.CallToolRequest, args memoryArgs) (*mcp.CallToolResult, any, error) {
	text, err := cleanMemory(args.Text)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.memory {
		if strings.EqualFold(m.Text, text) {
			return mcpText("已经记得：" + m.Text), nil, nil
		}
	}
	item := aiMemory{ID: newID(), Text: text, Updated: time.Now()}
	s.memory = append(s.memory, item)
	s.memAdd = append(s.memAdd, item)
	for len(s.memory) > assistMaxMemory {
		old := s.memory[0]
		s.memory = s.memory[1:]
		fresh := false
		next := s.memAdd[:0]
		for _, m := range s.memAdd {
			if m.ID == old.ID {
				fresh = true
				continue
			}
			next = append(next, m)
		}
		s.memAdd = next
		if !fresh {
			s.memDrop = append(s.memDrop, old.ID)
		}
	}
	return mcpText("已记住：" + text), nil, nil
}

func (s *sunvaleTools) forget(_ context.Context, _ *mcp.CallToolRequest, args memoryArgs) (*mcp.CallToolResult, any, error) {
	q := strings.Join(strings.Fields(args.Text), " ")
	if q == "" {
		return mcpErr(fmt.Errorf("没有要忘掉的内容")), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var hit []aiMemory
	ql := strings.ToLower(q)
	for _, m := range s.memory {
		if m.ID == q || strings.EqualFold(m.Text, q) || strings.Contains(strings.ToLower(m.Text), ql) {
			hit = append(hit, m)
		}
	}
	if len(hit) == 0 {
		return mcpErr(fmt.Errorf("没有这条记忆")), nil, nil
	}
	if len(hit) > 1 {
		var exact []aiMemory
		for _, m := range hit {
			if m.ID == q || strings.EqualFold(m.Text, q) {
				exact = append(exact, m)
			}
		}
		if len(exact) != 1 {
			return mcpErr(fmt.Errorf("有 %d 条相似的记忆，请说完整的一句", len(hit))), nil, nil
		}
		hit = exact
	}
	id := hit[0].ID
	next := s.memory[:0]
	for _, m := range s.memory {
		if m.ID != id {
			next = append(next, m)
		}
	}
	s.memory = next
	fresh := false
	added := s.memAdd[:0]
	for _, m := range s.memAdd {
		if m.ID == id {
			fresh = true
			continue
		}
		added = append(added, m)
	}
	s.memAdd = added
	if !fresh {
		s.memDrop = append(s.memDrop, id)
	}
	return mcpText("已忘掉：" + hit[0].Text), nil, nil
}

func (s *sunvaleTools) memoryList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	items := append([]aiMemory(nil), s.memory...)
	s.mu.Unlock()
	if len(items) == 0 {
		return mcpText("无"), nil, nil
	}
	var b strings.Builder
	for _, m := range items {
		b.WriteString("- ")
		b.WriteString(m.Text)
		b.WriteByte('\n')
	}
	return mcpText(strings.TrimRight(b.String(), "\n")), nil, nil
}
