package schedule

import "time"

// Action kinds stored on a Job. Values are stable JSON strings.
const (
	ActionFileRule  = "file_rule"
	ActionScript    = "script"
	ActionAssistant = "assistant"
)

// FileOps are the file_rule Op values.
const (
	OpMove = "move"
	OpCopy = "copy"
)

// Job is one scheduled task. Schedule is daily-at local clock; Weekdays
// empty means every day, otherwise Go weekday numbers (Sunday = 0).
type Job struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	Hour     int   `json:"hour"`               // 0–23 local
	Minute   int   `json:"minute"`             // 0–59 local
	Weekdays []int `json:"weekdays,omitempty"` // empty = every day

	// Cron is a generated 5-field string (min hour dom mon dow) for
	// display and future importers; the runner uses Hour/Minute/Weekdays.
	Cron string `json:"cron,omitempty"`

	Action string `json:"action"` // ActionFileRule | ActionScript | ActionAssistant

	// File rule
	SourceDir string `json:"source_dir,omitempty"`
	DestDir   string `json:"dest_dir,omitempty"`
	Match     string `json:"match,omitempty"` // glob, .ext, or comma list; empty = *
	Op        string `json:"op,omitempty"`    // move|copy
	DateStamp bool   `json:"date_stamp,omitempty"`

	// Script: path to .py/.sh/.bat, or Inline saved under sunvale/scripts/.
	ScriptPath   string `json:"script_path,omitempty"`
	InlineScript string `json:"inline_script,omitempty"`
	TimeoutSec   int    `json:"timeout_sec,omitempty"`

	// Assistant: natural-language prompt + optional folder.
	// Preset is a built-in template id (classify_downloads, …); empty = custom prompt.
	Preset string `json:"preset,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	Folder string `json:"folder,omitempty"`

	LastRunAt  time.Time `json:"last_run_at,omitempty"`
	LastStatus string    `json:"last_status,omitempty"` // ok | error | skipped
	LastError  string    `json:"last_error,omitempty"`
	LastDetail string    `json:"last_detail,omitempty"` // short summary for UI
	NextRunAt  time.Time `json:"next_run_at,omitempty"`
}

// File is the on-disk root document.
type File struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}

// Normalize fills derived fields and clamps schedule numbers.
func (j *Job) Normalize() {
	if j.Hour < 0 {
		j.Hour = 0
	}
	if j.Hour > 23 {
		j.Hour = 23
	}
	if j.Minute < 0 {
		j.Minute = 0
	}
	if j.Minute > 59 {
		j.Minute = 59
	}
	switch j.Action {
	case ActionFileRule, ActionScript, ActionAssistant:
	default:
		j.Action = ActionFileRule
	}
	if j.Action == ActionFileRule {
		if j.Op != OpMove && j.Op != OpCopy {
			j.Op = OpMove
		}
	}
	if j.TimeoutSec <= 0 {
		j.TimeoutSec = 300
	}
	j.Cron = CronFromDaily(j.Minute, j.Hour, j.Weekdays)
}
