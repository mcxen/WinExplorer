package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pathfreq.go is the usage ledger: the assistant's 常用文件 memory, in the
// hermes style of an experience-fed store next to the user-told facts
// (memory.json) and the session history. Opening, visiting, pinning,
// attaching and the assistant's own reads all score points; the score
// decays on a half-life so last month's darlings sink by themselves.
//
// The ledger lives in state.json with the pins and the recent lists.

const (
	// maxFreqPaths caps the ledger; the least-scored entries go first.
	maxFreqPaths = 200
	// freqHalfLife is how long a path's score takes to halve.
	freqHalfLife = 7 * 24 * time.Hour
	// freqKeep is how many paths the prompt and the tool name at most.
	freqPromptTop = 12
)

// pathFreq is one path's tally: weighted hits and the last hit's time.
type pathFreq struct {
	N  int   `json:"n"`
	At int64 `json:"at"`
}

// freqScore is the ledger's ranking number: hits discounted by age, at a
// half-life of freqHalfLife.
func freqScore(f pathFreq, now time.Time) float64 {
	age := now.Sub(time.UnixMilli(f.At))
	if age < 0 {
		age = 0
	}
	return float64(f.N) * math.Pow(0.5, age.Hours()/freqHalfLife.Hours())
}

// notePath scores weight for path. Pins weigh most — they are a stated
// favorite; opening, visiting and the assistant's reads count one each.
func (st *persistedState) notePath(path string, weight int) {
	if path == "" || weight <= 0 {
		return
	}
	st.mu.Lock()
	st.noteFreqLocked(path, weight)
	st.saveLocked()
	st.mu.Unlock()
}

// noteFreqLocked scores with the lock already held, letting the visit,
// pin and recent writers fold the hit into their own save.
func (st *persistedState) noteFreqLocked(path string, weight int) {
	if st.Freq == nil {
		st.Freq = map[string]pathFreq{}
	}
	f := st.Freq[path]
	f.N += weight
	f.At = time.Now().UnixMilli()
	st.Freq[path] = f
	// Well over the cap? Evict the lowest-scored quarter. The comparison
	// runs per-note only past 2× the cap, so steady use stays cheap.
	if len(st.Freq) > 2*maxFreqPaths {
		now := time.Now()
		ranked := freqRankLocked(st.Freq, now)
		for _, dead := range ranked[maxFreqPaths/2:] {
			delete(st.Freq, dead.path)
		}
	}
}

// freqRankLocked sorts the ledger's paths, best first. Callers hold mu.
type freqRanked struct {
	path string
	f    pathFreq
}

func freqRankLocked(freq map[string]pathFreq, now time.Time) []freqRanked {
	ranked := make([]freqRanked, 0, len(freq))
	for p, f := range freq {
		ranked = append(ranked, freqRanked{path: p, f: f})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		si, sj := freqScore(ranked[i].f, now), freqScore(ranked[j].f, now)
		if si != sj {
			return si > sj
		}
		return ranked[i].f.At > ranked[j].f.At
	})
	return ranked
}

// frequentPaths is the ledger's top n, best first. Paths that no longer
// exist drop out of the answer and of the ledger — a renamed or trashed
// favorite should not linger as a ghost the model keeps suggesting.
func (st *persistedState) frequentPaths(n int) []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.Freq) == 0 {
		return nil
	}
	now := time.Now()
	var out []string
	pruned := false
	for _, r := range freqRankLocked(st.Freq, now) {
		if len(out) >= n {
			break
		}
		if _, err := os.Lstat(r.path); err != nil {
			delete(st.Freq, r.path)
			pruned = true
			continue
		}
		out = append(out, r.path)
	}
	if pruned {
		st.saveLocked()
	}
	return out
}

// clearFreq empties the ledger, the 设置 panel's reset for the usage
// memory.
func (st *persistedState) clearFreq() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	n := len(st.Freq)
	st.Freq = nil
	st.saveLocked()
	return n
}

// noteUsed is the window's one-call face of the ledger. attachDroppedPaths
// and the picker pass it their accepted local paths with a weight.
func (a *App) noteUsedPaths(weight int, paths ...string) {
	if a.store == nil {
		return
	}
	for _, p := range paths {
		a.store.notePath(p, weight)
	}
}

// assistNoteUsed is how the tools' reads feed the ledger: read_file and
// read_files score the paths they actually read. The window installs it at
// startup; tests install their own and restore it. It stays nil-headed and
// safe to leave unset for a run without a window, such as an A2A call or a
// scheduled task, so the remote and the cron habits do not pretend to be
// the user's.
var assistNoteUsed func(paths ...string)

// noteAssistRead scores the paths the assistant read, when a window
// installed the hook.
func noteAssistRead(paths ...string) {
	if assistNoteUsed != nil {
		assistNoteUsed(paths...)
	}
}

// frequentTop is the store's top-N guard for a window-less App.
func (a *App) frequentTop(n int) []string {
	if a.store == nil {
		return nil
	}
	return a.store.frequentPaths(n)
}

// storeFreqSnapshot counts the ledger's entries for the settings panel.
func (a *App) storeFreqSnapshot() int {
	if a.store == nil {
		return 0
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	return len(a.store.Freq)
}

// freqPromptText is the {frequent} section of the run: one line per
// path, or a quiet note there is nothing yet.
func freqPromptText(paths []string) string {
	if len(paths) == 0 {
		return "（暂无使用记录）"
	}
	var b strings.Builder
	for _, p := range paths {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// frequentFiles is the model's window onto the usage ledger: the paths the
// user actually works with, best first, so "我常看的那个表" is a lookup,
// not a guess. The snapshot was ranked when the run started.
func (s *sunvaleTools) frequentFiles(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	paths := append([]string(nil), s.frequent...)
	s.mu.Unlock()
	if len(paths) == 0 {
		return mcpText("还没有使用记录：用户打开、固定或拖给助手的文件会积累在这里。"), nil, nil
	}
	const maxShow = 20
	var b strings.Builder
	fmt.Fprintf(&b, "常用文件（按使用频率与最近程度，共 %d 条）：\n", min(len(paths), maxShow))
	for i, p := range paths[:min(len(paths), maxShow)] {
		fmt.Fprintf(&b, "%d. %s\n", i+1, p)
	}
	return mcpText(b.String()), nil, nil
}
