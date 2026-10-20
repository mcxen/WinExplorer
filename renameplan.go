package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// renameplan.go is the rename engine behind every batch rename surface: the
// desktop's 批量重命名 dialog (batchrename.go) and the remote server's
// /api/fs/batch_rename (remote_manage.go). It owns what a mode expands to,
// what makes a target invalid, which targets collide, and how a plan is
// applied to disk — including the two-pass move a name swap needs — so the
// two surfaces can never disagree about what will happen.
//
// Three modes cover what the OpenList frontend offers plus the template this
// app had first:
//
//	模板      {name} {stem} {ext} {n} {n:3} — one pattern for the lot
//	正则      a Go regexp, $1 replacements, date tokens
//	查找替换  literal text, replaced everywhere
//
// Every mode expands the {created_*} / {modified_*} date tokens afterwards,
// as OpenList's frontend does for its regex mode. Rows may also be edited by
// hand: such a row keeps what the user typed until it is reset.

// renameMode is how a plan names its items.
type renameMode int

const (
	renameModeTemplate renameMode = iota
	renameModeRegex
	renameModeFindReplace
)

// renameModeLabel names a mode for the dialog's chips, in the order the chips
// show them.
func renameModeLabel(m renameMode) string {
	switch m {
	case renameModeRegex:
		return "正则"
	case renameModeFindReplace:
		return "查找替换"
	}
	return "模板"
}

// renameOptions is the dialog's inputs, as the engine sees them. Pattern is
// the template, the regexp or the literal to find, depending on Mode;
// Replace is only used by the two replacement modes.
type renameOptions struct {
	Mode    renameMode
	Pattern string
	Replace string
	Start   int
}

// renameRow is one item of a plan: where it is now, what it should be
// called, and why that name cannot be used. Marking the row — not dropping
// it — is what lets the dialog show every conflict before anything moves.
type renameRow struct {
	Name    string
	Path    string
	IsDir   bool
	Ext     string // lower, no dot; empty for folders
	ModTime time.Time
	Created time.Time

	// To is the target name. Manual rows were typed by the user and the
	// pattern leaves them alone until the row is reset.
	To     string
	Manual bool
	// Err is why To cannot be used: an invalid name, or a collision. Empty
	// when the row is ready, and also when To equals Name (nothing to do).
	Err string
}

// renameFixableRows reports how many rows would actually be renamed: those
// without an error whose name really changes.
func renameFixableRows(rows []renameRow) int {
	n := 0
	for _, r := range rows {
		if r.Err == "" && r.To != "" && r.To != r.Name {
			n++
		}
	}
	return n
}

// renameRowsFromEntries makes a plan out of the selection, in view order —
// which is the order the numbers run in, and the order a drag changes.
func renameRowsFromEntries(entries []Entry) []renameRow {
	rows := make([]renameRow, len(entries))
	for i, e := range entries {
		rows[i] = renameRow{
			Name:    e.Name,
			Path:    e.Path,
			IsDir:   e.IsDir,
			Ext:     e.Ext,
			ModTime: e.ModTime,
		}
		if !e.IsDir && e.Path != "" {
			if bt, err := birthTime(e.Path); err == nil {
				rows[i].Created = bt
			}
		}
	}
	return rows
}

// renameTarget expands the options over one row, n being its number.
func renameTarget(o renameOptions, r renameRow, n int) string {
	var to string
	switch o.Mode {
	case renameModeRegex:
		re, err := regexp.Compile(o.Pattern)
		if err != nil {
			// An invalid regexp is reported by renameReplan, which sees the
			// error; every row keeps its name here.
			return r.Name
		}
		to = re.ReplaceAllString(r.Name, o.Replace)
	case renameModeFindReplace:
		if o.Pattern == "" {
			return r.Name
		}
		to = strings.ReplaceAll(r.Name, o.Pattern, o.Replace)
	default:
		to = expandRenamePattern(o.Pattern, n, Entry{Name: r.Name, IsDir: r.IsDir, Ext: r.Ext})
	}
	return renameDateTokens(to, r)
}

// expandRenamePattern fills the template's placeholders for one item:
//
//	{name}  the full original name        {stem} the name without extension
//	{ext}   the extension without a dot   {n}    the item's number
//	{n:3}   the number padded to 3 digits
func expandRenamePattern(pattern string, n int, e Entry) string {
	stem := e.Name
	ext := ""
	if !e.IsDir {
		ext = strings.TrimPrefix(filepath.Ext(e.Name), ".")
		stem = strings.TrimSuffix(e.Name, filepath.Ext(e.Name))
	}
	pattern = replaceNumberToken(pattern, n)
	pattern = strings.ReplaceAll(pattern, "{name}", e.Name)
	pattern = strings.ReplaceAll(pattern, "{stem}", stem)
	pattern = strings.ReplaceAll(pattern, "{ext}", ext)
	return strings.TrimSpace(pattern)
}

// replaceNumberToken handles {n} and {n:width}, the only placeholder whose
// value differs per row.
func replaceNumberToken(pattern string, n int) string {
	for {
		i := strings.Index(pattern, "{n:")
		if i < 0 {
			break
		}
		end := strings.Index(pattern[i:], "}")
		if end < 0 {
			break
		}
		width := pattern[i+3 : i+end]
		pattern = pattern[:i] + fmt.Sprintf("%0*d", atoiOr(width, 1), n) + pattern[i+end+1:]
	}
	return strings.ReplaceAll(pattern, "{n}", fmt.Sprintf("%d", n))
}

// atoiOr reads a width, falling back when it is not a positive number.
func atoiOr(s string, fallback int) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return fallback
	}
	return n
}

// renameDateTokens fills the {created_*} and {modified_*} placeholders,
// OpenList's frontend tokens, zero-padded as it pads them.
func renameDateTokens(s string, r renameRow) string {
	if !strings.Contains(s, "{created_") && !strings.Contains(s, "{modified_") {
		return s
	}
	pairs := []struct {
		token string
		value string
	}{
		{"{created_year}", fmt.Sprintf("%04d", r.Created.Year())},
		{"{created_month}", fmt.Sprintf("%02d", int(r.Created.Month()))},
		{"{created_date}", fmt.Sprintf("%02d", r.Created.Day())},
		{"{created_hour}", fmt.Sprintf("%02d", r.Created.Hour())},
		{"{created_minute}", fmt.Sprintf("%02d", r.Created.Minute())},
		{"{created_second}", fmt.Sprintf("%02d", r.Created.Second())},
		{"{modified_year}", fmt.Sprintf("%04d", r.ModTime.Year())},
		{"{modified_month}", fmt.Sprintf("%02d", int(r.ModTime.Month()))},
		{"{modified_date}", fmt.Sprintf("%02d", r.ModTime.Day())},
		{"{modified_hour}", fmt.Sprintf("%02d", r.ModTime.Hour())},
		{"{modified_minute}", fmt.Sprintf("%02d", r.ModTime.Minute())},
		{"{modified_second}", fmt.Sprintf("%02d", r.ModTime.Second())},
	}
	for _, p := range pairs {
		s = strings.ReplaceAll(s, p.token, p.value)
	}
	return s
}

// renameValidateName reports why name cannot be a target of its folder, or
// "" when it can. The rule is the protocol's checkRelativePath: one path
// component, never dot or dot-dot, no separators, not empty.
func renameValidateName(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "名称不能为空"
	case name == "." || name == "..":
		return "名称无效"
	case strings.ContainsAny(name, `/\`):
		return "名称不能包含路径分隔符"
	}
	return ""
}

// renameReplan recomputes every pattern-driven target and re-marks the rows
// as invalid or colliding. Manually edited rows keep what the user typed;
// dir is the folder the targets land in, and empty skips the on-disk clash
// check (the remote endpoint validates against the disk it will write to).
func renameReplan(rows []renameRow, o renameOptions, dir string) {
	badRegex := false
	if o.Mode == renameModeRegex {
		if _, err := regexp.Compile(o.Pattern); err != nil {
			badRegex = true
		}
	}
	for i := range rows {
		r := &rows[i]
		r.Err = ""
		if !r.Manual {
			if badRegex {
				r.Err = "正则表达式无效"
				continue
			}
			r.To = renameTarget(o, *r, o.Start+i)
		}
		if r.Err != "" {
			continue
		}
		if r.To == r.Name {
			continue // unchanged: neither invalid nor a clash
		}
		if why := renameValidateName(r.To); why != "" {
			r.Err = why
		}
	}
	// Two rows wanting one name collide; the first keeps it, the rest are
	// marked, so the dialog shows which of them got it.
	taken := make(map[string]int, len(rows))
	for i := range rows {
		r := &rows[i]
		if r.Err != "" || r.To == r.Name {
			continue
		}
		key := strings.ToLower(r.To)
		if j, ok := taken[key]; ok {
			r.Err = fmt.Sprintf("与第 %d 项重名", j+1)
			continue
		}
		taken[key] = i
	}
	if dir == "" {
		return
	}
	// A target that something else already holds in this folder is a clash,
	// unless the holder is one of the sources that is itself moving away.
	sources := make(map[string]bool, len(rows))
	for i := range rows {
		if rows[i].Err == "" && rows[i].To != rows[i].Name {
			sources[strings.ToLower(rows[i].Name)] = true
		}
	}
	for i := range rows {
		r := &rows[i]
		if r.Err != "" || r.To == r.Name {
			continue
		}
		if sources[strings.ToLower(r.To)] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(dir, r.To)); err == nil {
			r.Err = "已存在同名项目"
		}
	}
}

// renameApply performs the plan's clean rows in dir and reports what moved,
// plus the original names that failed. A target naming another row's source
// (a swap, or a rotation) goes through a temporary name first, so a plan can
// reorder names without a half-done state.
//
// The caller owns the undo record: the desktop dialog pushes moved as one
// undo op, the remote endpoint forgets it.
func renameApply(dir string, rows []renameRow) (moved []undoMove, failed []string) {
	type move struct{ from, to, name string }
	var moves []move
	sources := make(map[string]bool, len(rows))
	for _, r := range rows {
		if r.Err == "" && r.To != "" && r.To != r.Name {
			sources[strings.ToLower(r.Name)] = true
		}
	}
	for _, r := range rows {
		if r.Err != "" || r.To == "" || r.To == r.Name {
			continue
		}
		moves = append(moves, move{
			from: filepath.Join(dir, r.Name),
			to:   filepath.Join(dir, r.To),
			name: r.Name,
		})
	}
	// A move landing on a name another move vacates waits for that move: it
	// goes to a temporary name first, then to its target once everything is
	// out of the way.
	var deferred []move
	for _, m := range moves {
		if sources[strings.ToLower(filepath.Base(m.to))] {
			deferred = append(deferred, m)
			continue
		}
		if err := os.Rename(m.from, m.to); err != nil {
			failed = append(failed, m.name)
			continue
		}
		moved = append(moved, undoMove{From: m.from, To: m.to})
	}
	if len(deferred) == 0 {
		return moved, failed
	}
	type parked struct {
		m   move
		tmp string
	}
	var temps []parked
	for i, m := range deferred {
		tmp := m.from + fmt.Sprintf(".sunvale-rename-%d", i)
		if err := os.Rename(m.from, tmp); err != nil {
			failed = append(failed, m.name)
			continue
		}
		temps = append(temps, parked{m: m, tmp: tmp})
	}
	for _, p := range temps {
		if err := os.Rename(p.tmp, p.m.to); err != nil {
			// Put it back where it came from rather than leave a temp file.
			_ = os.Rename(p.tmp, p.m.from)
			failed = append(failed, p.m.name)
			continue
		}
		moved = append(moved, undoMove{From: p.m.from, To: p.m.to})
	}
	return moved, failed
}
