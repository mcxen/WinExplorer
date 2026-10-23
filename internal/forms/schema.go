package forms

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Field kinds match OpenUI components and Settings controls.
const (
	KindText        = "text"
	KindTextArea    = "textarea"
	KindPassword    = "password"
	KindNumber      = "number"
	KindPath        = "path"
	KindSelect      = "select"
	KindMultiSelect = "multi_select"
	KindSwitch      = "switch"
	KindTime        = "time"
	KindDate        = "date"
	KindDays        = "days"
	KindSegmented   = "segmented"
	KindFileTypes   = "filetypes"
	KindTags        = "tags"
	KindSlider      = "slider"
	KindProgress    = "progress"
	KindTableSelect = "table_select"
	KindList        = "list"
	KindImage       = "image"
	KindVideo       = "video"
)

// PathMode selects what a path field's browse button opens.
const (
	PathFolder = "folder"
	PathFile   = "file"
	ListMulti  = "multi"
	ListSingle = "single"
)

// Field is one control in a Form.
type Field struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Label       string   `json:"label,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	Value       string   `json:"value,omitempty"`
	Options     []string `json:"options,omitempty"`
	PathMode    string   `json:"path_mode,omitempty"` // path: folder|file; list: single|multi
	Description string   `json:"description,omitempty"`
	// Min/Max/Step drive slider (and optional progress scale). Zero Max
	// means the painter picks a default (0–100 for slider, 0–1 for progress).
	Min  float64 `json:"min,omitempty"`
	Max  float64 `json:"max,omitempty"`
	Step float64 `json:"step,omitempty"`
}

// Action is a submit button. Message is what the assistant receives
// (same role as OpenUI @ToAssistant); empty Message uses Label.
type Action struct {
	ID      string `json:"id,omitempty"`
	Label   string `json:"label"`
	Variant string `json:"variant,omitempty"` // primary | secondary | destructive
	Message string `json:"message,omitempty"`
}

// Form is a titled group of fields and submit actions.
type Form struct {
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Fields      []Field  `json:"fields"`
	Actions     []Action `json:"submit_actions,omitempty"`
}

// ParseJSON reads a Form from JSON (present_form / ```form fences).
func ParseJSON(raw string) (*Form, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("表单为空")
	}
	var f Form
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return nil, fmt.Errorf("表单 JSON 无效：%v", err)
	}
	f.Normalize()
	if len(f.Fields) == 0 {
		return nil, fmt.Errorf("表单没有填写项")
	}
	return &f, nil
}

// Normalize fills defaults and clamps kinds the painter understands.
func (f *Form) Normalize() {
	if f == nil {
		return
	}
	f.ID = strings.TrimSpace(f.ID)
	f.Title = strings.TrimSpace(f.Title)
	f.Description = strings.TrimSpace(f.Description)
	out := f.Fields[:0]
	for _, field := range f.Fields {
		field.ID = strings.TrimSpace(field.ID)
		field.Kind = normalizeKind(field.Kind)
		field.Label = strings.TrimSpace(field.Label)
		field.Placeholder = strings.TrimSpace(field.Placeholder)
		field.Pattern = strings.TrimSpace(field.Pattern)
		field.PathMode = strings.TrimSpace(field.PathMode)
		if field.ID == "" || field.Kind == "" {
			continue
		}
		if field.Label == "" {
			field.Label = field.ID
		}
		if field.Kind == KindPath && field.PathMode == "" {
			field.PathMode = PathFolder
		}
		if field.Kind == KindList && field.PathMode == "" {
			field.PathMode = ListMulti
		}
		if field.Kind == KindDays && len(field.Options) == 0 {
			field.Options = []string{"每天", "工作日", "周末"}
		}
		out = append(out, field)
	}
	f.Fields = out
	acts := f.Actions[:0]
	for _, a := range f.Actions {
		a.Label = strings.TrimSpace(a.Label)
		if a.Label == "" {
			continue
		}
		if a.ID == "" {
			a.ID = a.Label
		}
		if a.Message == "" {
			a.Message = a.Label
		}
		acts = append(acts, a)
	}
	f.Actions = acts
}

func normalizeKind(k string) string {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case KindText, "input", "string":
		return KindText
	case KindTextArea, "multiline":
		return KindTextArea
	case KindPassword, "passwd", "secret":
		return KindPassword
	case KindNumber, "int", "float":
		return KindNumber
	case KindPath, "folder", "file", "dir":
		return KindPath
	case KindSelect, "dropdown":
		return KindSelect
	case KindMultiSelect, "checkboxes":
		return KindMultiSelect
	case KindSwitch, "toggle", "bool", "checkbox":
		return KindSwitch
	case KindTime, "hhmm", "clock":
		return KindTime
	case KindDays, "weekdays", "repeat":
		return KindDays
	case KindSegmented, "segment", "chips":
		return KindSegmented
	case KindDate, "day", "calendar":
		return KindDate
	case KindFileTypes, "filetype", "ext", "extensions":
		return KindFileTypes
	case KindTags, "tag", "labels":
		return KindTags
	case KindSlider, "range":
		return KindSlider
	case KindProgress, "progress_bar", "bar":
		return KindProgress
	case KindTableSelect, "table", "rows":
		return KindTableSelect
	case KindList, "file_list", "checklist", "item_list":
		return KindList
	case KindImage, "img", "picture", "photo":
		return KindImage
	case KindVideo, "movie", "clip":
		return KindVideo
	default:
		return ""
	}
}

// ValuesFromMap returns a stable "标签：值" block for the next user message,
// matching OpenUI Form button behavior.
func (f *Form) FormatSubmit(values map[string]string) (body, missing string) {
	if f == nil {
		return "", ""
	}
	var lines []string
	for _, field := range f.Fields {
		label := field.Label
		if label == "" {
			label = field.ID
		}
		val := strings.TrimSpace(values[field.ID])
		if val == "" && field.Required {
			return "", label
		}
		if val != "" {
			lines = append(lines, label+"："+val)
		}
	}
	return strings.Join(lines, "\n"), ""
}

// KnownKinds is the vocabulary listed in the system prompt and docs.
func KnownKinds() []string {
	return []string{
		KindText, KindTextArea, KindPassword, KindNumber, KindPath,
		KindSelect, KindMultiSelect, KindSwitch, KindTime, KindDate, KindDays,
		KindSegmented, KindFileTypes, KindTags, KindSlider, KindProgress, KindTableSelect,
		KindList, KindImage, KindVideo,
	}
}
