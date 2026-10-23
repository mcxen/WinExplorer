package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pathfreq_test.go drives the usage ledger: scoring and decay, ranking,
// the pruning of gone paths, the cap's eviction, and the model's
// frequent_files window onto it.

func freqTestStore(t *testing.T) *persistedState {
	t.Helper()
	return &persistedState{} // in-memory: no state.json writes
}

// TestFreqLedgerRanksAndDecays: pins outrank visits, and an old favorite
// sinks under a freshly used one.
func TestFreqLedgerRanksAndDecays(t *testing.T) {
	dir := t.TempDir()
	pin := filepath.Join(dir, "pin")
	visit := filepath.Join(dir, "visit")
	old := filepath.Join(dir, "old")
	for _, p := range []string{pin, visit, old} {
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st := freqTestStore(t)
	st.pin(pin)
	st.pushVisit(visit)
	st.Freq[old] = pathFreq{N: 30, At: time.Now().Add(-60 * 24 * time.Hour).UnixMilli()}
	got := st.frequentPaths(3)
	if len(got) != 3 || got[0] != pin {
		t.Fatalf("ranking = %v", got)
	}
	// 30 hits eight+ half-lives old score under a single fresh visit.
	if got[1] != visit || got[2] != old {
		t.Fatalf("decay did not sink the old favorite: %v", got)
	}
	if f := st.Freq[pin]; f.N != 3 {
		t.Fatalf("pin weight = %d", f.N)
	}
}

// TestFreqLedgerPrunesGonePaths: a trashed favorite leaves the answer and
// the ledger.
func TestFreqLedgerPrunesGonePaths(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "live.txt")
	gone := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(live, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := freqTestStore(t)
	st.notePath(gone, 9)
	st.notePath(live, 1)
	got := st.frequentPaths(5)
	if len(got) != 1 || got[0] != live {
		t.Fatalf("pruning = %v", got)
	}
	if _, ok := st.Freq[gone]; ok {
		t.Fatal("the ghost stayed in the ledger")
	}
}

// TestFreqLedgerCapEvicts: past twice the cap, the lowest quarter goes.
func TestFreqLedgerCapEvicts(t *testing.T) {
	st := freqTestStore(t)
	st.Freq = map[string]pathFreq{}
	keep := filepath.Join(t.TempDir(), "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	for i := 0; i < 2*maxFreqPaths+1; i++ {
		st.Freq[filepath.Join("gone", itoa(i))] = pathFreq{N: 1, At: old}
	}
	st.Freq[keep] = pathFreq{N: 2, At: old}
	st.notePath(keep, 1) // trips the eviction
	if len(st.Freq) > maxFreqPaths {
		t.Fatalf("ledger keeps %d", len(st.Freq))
	}
	if _, ok := st.Freq[keep]; !ok {
		t.Fatal("the eviction took the freshest")
	}
}

// TestFreqClear: the settings panel's reset empties the ledger.
func TestFreqClear(t *testing.T) {
	st := freqTestStore(t)
	st.notePath("/d/a.txt", 1)
	if n := st.clearFreq(); n != 1 || st.Freq != nil {
		t.Fatalf("clearFreq = %d, %v", n, st.Freq)
	}
}

// TestFrequentFilesTool: the model sees the ranked snapshot, and an empty
// one reads as an invitation rather than an error.
func TestFrequentFilesTool(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sunvaleToolState.bind(dir, false, nil, nil, false)
	sunvaleToolState.bindFrequent([]string{note})
	t.Cleanup(func() { sunvaleToolState.bindFrequent(nil) })
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "frequent_files", map[string]any{})
	if !strings.Contains(got, note) || !strings.Contains(got, "常用文件") {
		t.Fatalf("frequent_files: %s", got)
	}
	sunvaleToolState.bindFrequent(nil)
	got = callToolText(t, sess, "frequent_files", map[string]any{})
	if !strings.Contains(got, "还没有使用记录") {
		t.Fatalf("empty ledger: %s", got)
	}
}

// TestAssistReadScoresInLedger: the window's hook scores the paths the
// assistant reads, feeding what it reads back into what it knows.
func TestAssistReadScoresInLedger(t *testing.T) {
	note := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := freqTestStore(t)
	old := assistNoteUsed
	assistNoteUsed = func(paths ...string) {
		for _, p := range paths {
			st.notePath(p, 1)
		}
	}
	t.Cleanup(func() { assistNoteUsed = old })
	sunvaleToolState.bind(filepath.Dir(note), false, nil, nil, false)
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	callToolText(t, sess, "read_file", map[string]any{"path": note})
	if f, ok := st.Freq[note]; !ok || f.N != 1 {
		t.Fatalf("ledger = %v", st.Freq)
	}
}
