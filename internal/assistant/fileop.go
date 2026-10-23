package assistant

import "strings"

// fileop.go is the transcript's record of one file operation the assistant
// proposed or ran. The chat paints it as a card — kind, path, a diff or a
// preview, a status and an undo — instead of a line of tool output, so a
// reader sees what changed on disk without unfolding the tool log. The undo
// itself is not stored here: it lives with the window that ran the step.

// OpKind names what the card does to the disk.
type OpKind string

const (
	OpCreate OpKind = "create"
	OpEdit   OpKind = "edit"
	OpRename OpKind = "rename"
	OpMove   OpKind = "move"
	OpDelete OpKind = "delete"
	OpBatch  OpKind = "batch"
	OpRead   OpKind = "read"
)

// OpStatus is the card's lifecycle. Pending waits for the user's
// confirmation; Undone is a finished step the user has reversed.
type OpStatus string

const (
	OpPending   OpStatus = "pending"
	OpRunning   OpStatus = "running"
	OpDone      OpStatus = "done"
	OpFailed    OpStatus = "failed"
	OpUndone    OpStatus = "undone"
	OpCancelled OpStatus = "cancelled"
)

// OpItem is one file inside a batch, a delete or a read. To is empty for
// items that are not renamed or moved.
type OpItem struct {
	From string `json:"from"`
	To   string `json:"to,omitempty"`
	Done bool   `json:"done,omitempty"`
}

// FileOp is one card. Before/After carry an edit's two texts or a new
// file's content (After only). Done counts finished Items while running.
type FileOp struct {
	ID     string   `json:"id"`
	Kind   OpKind   `json:"kind"`
	Path   string   `json:"path,omitempty"`
	To     string   `json:"to,omitempty"`
	Before string   `json:"before,omitempty"`
	After  string   `json:"after,omitempty"`
	Items  []OpItem `json:"items,omitempty"`
	Status OpStatus `json:"status"`
	Danger bool     `json:"danger,omitempty"`
	Err    string   `json:"err,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// Done counts finished items.
func (o *FileOp) Done() int {
	n := 0
	for _, it := range o.Items {
		if it.Done {
			n++
		}
	}
	return n
}

// PlanStep is one line of a multi-step plan shown above the cards.
type PlanStep struct {
	Text   string   `json:"text"`
	Status OpStatus `json:"status"`
}

// DiffLine is one line of a line diff: Kind is ' ', '-' or '+'.
type DiffLine struct {
	Kind byte
	Text string
}

// diffMaxLines bounds the LCS table: an edit card previews a note or a
// config file, not a log. Longer texts fall back to remove-all/add-all.
const diffMaxLines = 400

// DiffLines is a plain LCS line diff, enough for the card's preview.
func DiffLines(before, after string) []DiffLine {
	a := splitLines(before)
	b := splitLines(after)
	if len(a) > diffMaxLines || len(b) > diffMaxLines {
		out := make([]DiffLine, 0, len(a)+len(b))
		for _, l := range a {
			out = append(out, DiffLine{'-', l})
		}
		for _, l := range b {
			out = append(out, DiffLine{'+', l})
		}
		return out
	}
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []DiffLine
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, DiffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{'-', a[i]})
			i++
		default:
			out = append(out, DiffLine{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{'-', a[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{'+', b[j]})
	}
	return out
}

// DiffHunks keeps the changed lines with ctx lines of context around them,
// marking a gap with a single "…" context line.
func DiffHunks(lines []DiffLine, ctx int) []DiffLine {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Kind == ' ' {
			continue
		}
		for k := max(0, i-ctx); k <= min(len(lines)-1, i+ctx); k++ {
			keep[k] = true
		}
	}
	var out []DiffLine
	gap := false
	for i, l := range lines {
		if !keep[i] {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, DiffLine{' ', "…"})
		}
		gap = false
		out = append(out, l)
	}
	return out
}

func splitLines(s string) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
