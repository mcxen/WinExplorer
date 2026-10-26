package assistant

import "time"

// Choice is one unanswered, or already sent, question under a turn.
// On is parallel to Options and is what the checkboxes edit.
type Choice struct {
	Prompt  string   `json:"prompt,omitempty"`
	Multi   bool     `json:"multi"`
	Options []string `json:"options"`
	On      []bool   `json:"on,omitempty"`
	Sent    bool     `json:"sent,omitempty"`
	Custom  string   `json:"custom,omitempty"`
}

// Attach is a file named for one turn. Detail is preformatted so painting
// does not have to stat it on every frame.
type Attach struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

// Turn is one transcript line. UI-only fold and paint state stays outside.
type Turn struct {
	Role     string            `json:"role"`
	Text     string            `json:"text"`
	Model    string            `json:"-"`
	Plan     string            `json:"plan,omitempty"`
	Files    []Attach          `json:"files,omitempty"`
	Choice   *Choice           `json:"choice,omitempty"`
	OpenSent []string          `json:"openSent,omitempty"`
	OpenDone map[string]string `json:"openDone,omitempty"`
	Tool     string            `json:"tool,omitempty"`
	Stopped  bool              `json:"stopped,omitempty"`
	// Error marks a failed run's message: the transcript shows it as one
	// collapsible line instead of a reply, so a long error cannot push the
	// composer away.
	Error bool `json:"error,omitempty"`
	// At stamps the line for the chat's time label; Op and Steps turn a
	// tool line into a file-operation card and a plan list.
	At    time.Time  `json:"at,omitempty"`
	Op    *FileOp    `json:"op,omitempty"`
	Steps []PlanStep `json:"steps,omitempty"`
}

// Memory is one fact. Text is what the prompt and the panel show.
type Memory struct {
	ID      string    `json:"id"`
	Text    string    `json:"text"`
	Updated time.Time `json:"updated,omitempty"`
}

// SessionMeta is an index row. The transcript lives in its own file.
type SessionMeta struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
	Folder  string    `json:"folder,omitempty"`
	Preview string    `json:"preview,omitempty"`
	// Pinned sessions list first; Turns is the message count; Renamed marks
	// a user-chosen title that auto titling must keep.
	Pinned  bool `json:"pinned,omitempty"`
	Turns   int  `json:"turns,omitempty"`
	Renamed bool `json:"renamed,omitempty"`
}
