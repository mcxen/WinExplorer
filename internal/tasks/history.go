package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// HistoryMax and HistoryAge bound the persisted history.
const (
	HistoryMax = 500
	HistoryAge = 30 * 24 * time.Hour
)

func trimHistory(h []Snapshot, now time.Time) []Snapshot {
	cut := 0
	for cut < len(h) && now.Sub(h[cut].Ended) > HistoryAge {
		cut++
	}
	h = h[cut:]
	if len(h) > HistoryMax {
		h = h[len(h)-HistoryMax:]
	}
	return h
}

func loadHistory(path string, now time.Time) []Snapshot {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var h []Snapshot
	if json.Unmarshal(b, &h) != nil {
		return nil
	}
	for i := range h {
		// A run the app quit under ended canceled.
		if !h[i].State.Finished() {
			h[i].State, h[i].Err = Canceled, ErrCanceled.Error()
		}
	}
	return trimHistory(h, now)
}

func saveHistory(path string, h []Snapshot) {
	if path == "" {
		return
	}
	b, err := json.Marshal(h)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}
