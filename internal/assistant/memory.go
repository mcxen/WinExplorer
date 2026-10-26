package assistant

import (
	"encoding/json"
	"strings"
	"time"
)

type MemoryFile struct {
	Items []Memory `json:"items"`
}

// SessionIndex is the small navigation file beside the transcript archive.
type SessionIndex struct {
	ActiveID string        `json:"activeID,omitempty"`
	Sessions []SessionMeta `json:"sessions,omitempty"`
}

// SessionFile is one persisted conversation. Turn.Model is deliberately
// transient and never serialized.
type SessionFile struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Created time.Time `json:"created,omitempty"`
	Updated time.Time `json:"updated"`
	Folder  string    `json:"folder,omitempty"`
	Renamed bool      `json:"renamed,omitempty"`
	Turns   []Turn    `json:"turns,omitempty"`
}

func ReadMemories(path string, max int) ([]Memory, error) {
	var file MemoryFile
	if _, err := ReadJSON(path, &file); err != nil {
		return nil, err
	}
	if max > 0 && len(file.Items) > max {
		file.Items = file.Items[len(file.Items)-max:]
	}
	return file.Items, nil
}

func WriteMemories(path string, items []Memory) error {
	data, err := json.MarshalIndent(MemoryFile{Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(path, data)
}

func ReadIndex(path string) (SessionIndex, bool) {
	var idx SessionIndex
	absent, err := ReadJSON(path, &idx)
	return idx, absent || err == nil
}

func WriteIndex(path string, idx SessionIndex) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(path, data)
}

func NormalizeTurns(turns []Turn, max int, planExists func(string) bool) []Turn {
	if max > 0 && len(turns) > max {
		turns = append([]Turn(nil), turns[len(turns)-max:]...)
	}
	for i := range turns {
		turns[i].Model = ""
		if turns[i].Plan != "" && (planExists == nil || !planExists(turns[i].Plan)) {
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

func DiskTurns(in []Turn, maxRunes int) []Turn {
	out := make([]Turn, len(in))
	for i, t := range in {
		t.Model = ""
		t.Text = LimitRunes(t.Text, maxRunes)
		t.Files = append([]Attach(nil), t.Files...)
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

func CapSessions(list []SessionMeta, active string, max int) (keep, drop []SessionMeta) {
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

func SessionTitle(turns []Turn, maxRunes int) string {
	for _, t := range turns {
		if t.Role != "user" {
			continue
		}
		text := strings.Join(strings.Fields(t.Text), " ")
		if text == "" || text == "请阅读这些文件" {
			if len(t.Files) > 0 && t.Files[0].Name != "" {
				return TruncateRunes(t.Files[0].Name, maxRunes)
			}
			continue
		}
		return TruncateRunes(text, maxRunes)
	}
	return "新会话"
}

func SessionPreview(turns []Turn, maxRunes int) string {
	for i := len(turns) - 1; i >= 0; i-- {
		text := strings.Join(strings.Fields(turns[i].Text), " ")
		if text != "" {
			return TruncateRunes(text, maxRunes)
		}
	}
	return ""
}

func MemoryPrompt(items []Memory) string {
	if len(items) == 0 {
		return "无"
	}
	var b strings.Builder
	for i, m := range items {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(SessionPlain(m.Text))
	}
	return b.String()
}

// SessionPlain keeps path text out of f-string braces.
func SessionPlain(s string) string {
	s = strings.ReplaceAll(s, "{", "(")
	return strings.ReplaceAll(s, "}", ")")
}

func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
