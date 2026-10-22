package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Provider is one OpenAI-compatible model endpoint. APIKey is persisted
// privately and must never enter a response or model message.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
	// Vision says whether this model receives pictures. Empty means auto:
	// the model name decides, because /models endpoints report no
	// capabilities and asking the user is worse than a good guess.
	Vision string `json:"vision,omitempty"`
}

// Prefs contains the assistant pane's model-independent settings.
// ExtraInstruction is appended to the built-in Instruction at run time.
// DisabledSkills lists capability ids (files, memory, archive) the user turned off.
type Prefs struct {
	Width            float32   `json:"width,omitempty"`
	Full             bool      `json:"full,omitempty"`
	Autopilot        bool      `json:"autopilot,omitempty"`
	ExtraInstruction string    `json:"extraInstruction,omitempty"`
	DisabledSkills   []string  `json:"disabledSkills,omitempty"`
	WebSearch        WebSearch `json:"webSearch,omitempty"`
}

// WebSearch configures the web_search and web_read tools. Provider is
// "tavily" or "bocha"; BaseURL overrides the provider's endpoint, which
// mainly serves gateways and tests. The key stays in the local
// assistant.json, as the providers' keys do.
type WebSearch struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Provider string `json:"provider,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
	BaseURL  string `json:"baseURL,omitempty"`
}

// SkillFiles, SkillMemory, SkillArchive and SkillWeb are
// Prefs.DisabledSkills ids.
const (
	SkillFiles   = "files"
	SkillMemory  = "memory"
	SkillArchive = "archive"
	SkillWeb     = "web"
)

// Vision modes for Provider.Vision.
const (
	VisionAuto = ""
	VisionOn   = "on"
	VisionOff  = "off"
)

// VisionEnabled reports whether this route may carry pictures. Auto mode
// trusts the model name: a wrong guess degrades to text at request time, so
// guessing is cheaper than asking every user to know their model.
func (p Provider) VisionEnabled() bool {
	switch strings.TrimSpace(p.Vision) {
	case VisionOn:
		return true
	case VisionOff:
		return false
	}
	return ModelSeesImages(p.Model)
}

// ModelSeesImages is the auto-detection heuristic. Positive markers win
// over the negative ones: "qwen2.5-vl-72b-instruct" is a vision model even
// though its name ends in -instruct. A wrong guess degrades to a text retry
// at request time, so guessing is cheaper than asking every user to know
// what their model does.
func ModelSeesImages(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	for _, yes := range []string{
		"vision", "-vl", "vl-", "vl:", "4o", "gpt-4.1", "gpt-5", "gpt-4-turbo",
		"gemini", "gemma-3", "claude", "pixtral", "llava", "internvl", "minicpm-v",
		"cogvlm", "qvq", "glm-4v", "glm-4.5v", "glm-4.1v", "grok", "llama-4",
		"doubao", "step-1v", "moonshot-v1-vision", "kimi-vl", "mimo-vl", "seed-1",
	} {
		if strings.Contains(m, yes) {
			return true
		}
	}
	for _, no := range []string{"embed", "whisper", "tts", "dall-e", "moderation", "completion"} {
		if strings.Contains(m, no) {
			return false
		}
	}
	switch {
	case strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return !strings.Contains(m, "preview")
	}
	return false
}

// SkillDisabled reports whether id is listed in prefs.DisabledSkills.
func SkillDisabled(prefs Prefs, id string) bool {
	for _, s := range prefs.DisabledSkills {
		if s == id {
			return true
		}
	}
	return false
}

// SetSkillDisabled returns a copy of skills with id added or removed.
func SetSkillDisabled(skills []string, id string, disabled bool) []string {
	out := make([]string, 0, len(skills)+1)
	for _, s := range skills {
		if s == id {
			continue
		}
		out = append(out, s)
	}
	if disabled {
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Config is assistant.json. The generic secret rows belong to the file but
// are interpreted by the application, not by model routing.
type Config[T any] struct {
	Providers        []Provider      `json:"providers,omitempty"`
	ActiveID         string          `json:"activeID,omitempty"`
	ArchivePasswords []T             `json:"archivePasswords,omitempty"`
	Passwords        json.RawMessage `json:"passwords,omitempty"`
	Prefs            Prefs           `json:"prefs,omitempty"`
}

// ModelChoice is the UI-independent form of one provider/model row.
type ModelChoice struct {
	Value    string
	Label    string
	Group    string
	Provider string
	Model    string
}

// ActiveProvider resolves the active route and falls back to the first one.
func ActiveProvider(providers []Provider, activeID string) (Provider, bool) {
	for _, p := range providers {
		if p.ID == activeID {
			return p, true
		}
	}
	if len(providers) > 0 {
		return providers[0], true
	}
	return Provider{}, false
}

// MergeProviders reconciles a pane snapshot with a newer disk copy.
func MergeProviders(base, local, disk []Provider) []Provider {
	baseByID := make(map[string]Provider, len(base))
	for _, p := range base {
		baseByID[p.ID] = p
	}
	diskByID := make(map[string]Provider, len(disk))
	for _, p := range disk {
		diskByID[p.ID] = p
	}
	localByID := make(map[string]bool, len(local))
	out := make([]Provider, 0, len(local)+len(disk))
	for _, p := range local {
		localByID[p.ID] = true
		old, hadOld := baseByID[p.ID]
		fresh, hadFresh := diskByID[p.ID]
		switch {
		case !hadOld:
			out = append(out, p)
		case p == old:
			if hadFresh {
				out = append(out, fresh)
			}
		default:
			out = append(out, p)
		}
	}
	for _, p := range disk {
		if _, hadOld := baseByID[p.ID]; !hadOld && !localByID[p.ID] {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ModelChoices keeps the current model first while retaining provider order.
func ModelChoices(providers []Provider, lists map[string][]string) []ModelChoice {
	var out []ModelChoice
	for _, p := range providers {
		models := lists[p.ID]
		if len(models) == 0 && p.Model != "" {
			models = []string{p.Model}
		}
		seen := make(map[string]bool, len(models)+1)
		if p.Model != "" && !Contains(models, p.Model) {
			models = append([]string{p.Model}, models...)
		}
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, ModelChoice{
				Value: p.ID + "\x1f" + model, Label: model, Group: p.Name,
				Provider: p.ID, Model: model,
			})
		}
	}
	return out
}

// FetchProviderModels reads a bounded, deduplicated model list from an
// OpenAI-compatible endpoint and returns it in stable name order.
func FetchProviderModels(ctx context.Context, p Provider) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型列表 %d", res.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload.Data) == 0 {
		return nil, fmt.Errorf("模型列表没有内容")
	}
	seen := make(map[string]bool, len(payload.Data))
	var models []string
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
		if len(models) >= 100 {
			break
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表没有内容")
	}
	return models, nil
}
