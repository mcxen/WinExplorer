package assistant

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WritePrivate writes JSON-like state through a private temporary file so a
// crash cannot leave readable secrets half-written.
func WritePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadJSON decodes a private configuration file and reports a missing file
// separately so callers can preserve their in-memory state.
func ReadJSON(path string, out any) (absent bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, json.Unmarshal(data, out)
}

func CleanMemory(text string, maxRunes int) (string, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", fmt.Errorf("没有要记的内容")
	}
	if len([]rune(text)) > maxRunes {
		return "", fmt.Errorf("太长，请收成一句话")
	}
	if MemorySecret(text) {
		return "", fmt.Errorf("密码和密钥请放进密码本，不要记在记忆里")
	}
	return text, nil
}

func MemorySecret(text string) bool {
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

func LimitRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func SafeSessionID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func Contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func Homeish(path string) string {
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

func When(t time.Time) string {
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
		return fmt.Sprintf("%d 分钟前", max(1, int(d.Minutes())))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", max(1, int(d.Hours())))
	default:
		return t.Format("1月2日 15:04")
	}
}
