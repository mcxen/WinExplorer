package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	fileName    = "schedules.json"
	dirName     = "sunvale"
	logDirName  = "schedule-logs"
	scriptsName = "scripts"
	fileVersion = 1
)

// Store is the process-wide schedules.json. Safe for concurrent use.
type Store struct {
	mu   sync.Mutex
	path string
	file File
}

// DefaultPath is UserConfigDir/sunvale/schedules.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dirName, fileName), nil
}

// LogDir is UserConfigDir/sunvale/schedule-logs.
func LogDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dirName, logDirName), nil
}

// ScriptsDir is UserConfigDir/sunvale/scripts for inline scripts.
func ScriptsDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dirName, scriptsName), nil
}

// NewEmpty returns a store that will save to path, with no jobs yet.
func NewEmpty(path string) *Store {
	return &Store{path: path, file: File{Version: fileVersion}}
}

// Open loads path (or DefaultPath). Missing file yields an empty store.
func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	s := &Store{path: path, file: File{Version: fileVersion}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &s.file); err != nil {
		return nil, fmt.Errorf("schedules.json: %w", err)
	}
	if s.file.Version == 0 {
		s.file.Version = fileVersion
	}
	for i := range s.file.Jobs {
		s.file.Jobs[i].Normalize()
	}
	return s, nil
}

// Path returns the on-disk path.
func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// Jobs returns a copy of all jobs.
func (s *Store) Jobs() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, len(s.file.Jobs))
	copy(out, s.file.Jobs)
	return out
}

// Get returns a job by id.
func (s *Store) Get(id string) (Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.file.Jobs {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}

// Upsert inserts or replaces a job and persists. Recalculates NextRunAt.
func (s *Store) Upsert(j Job, now time.Time, loc *time.Location) error {
	j.Normalize()
	j.NextRunAt = NextRun(j, now, loc)
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for i := range s.file.Jobs {
		if s.file.Jobs[i].ID == j.ID {
			s.file.Jobs[i] = j
			found = true
			break
		}
	}
	if !found {
		s.file.Jobs = append(s.file.Jobs, j)
	}
	return s.saveLocked()
}

// Delete removes a job by id.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.file.Jobs[:0]
	for _, j := range s.file.Jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	s.file.Jobs = out
	return s.saveLocked()
}

// SetEnabled flips the switch and refreshes NextRunAt.
func (s *Store) SetEnabled(id string, on bool, now time.Time, loc *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.file.Jobs {
		if s.file.Jobs[i].ID == id {
			s.file.Jobs[i].Enabled = on
			s.file.Jobs[i].Normalize()
			s.file.Jobs[i].NextRunAt = NextRun(s.file.Jobs[i], now, loc)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("任务不存在")
}

// RecordRun writes last-run fields and next fire time.
func (s *Store) RecordRun(id string, at time.Time, status, detail, errMsg string, loc *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.file.Jobs {
		if s.file.Jobs[i].ID == id {
			s.file.Jobs[i].LastRunAt = at
			s.file.Jobs[i].LastStatus = status
			s.file.Jobs[i].LastDetail = detail
			s.file.Jobs[i].LastError = errMsg
			s.file.Jobs[i].NextRunAt = NextRun(s.file.Jobs[i], at, loc)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("任务不存在")
}

// RefreshNext recomputes NextRunAt for every job.
func (s *Store) RefreshNext(now time.Time, loc *time.Location) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.file.Jobs {
		s.file.Jobs[i].NextRunAt = NextRun(s.file.Jobs[i], now, loc)
	}
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	s.file.Version = fileVersion
	data, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// WriteLog appends a run log under schedule-logs/<id>-YYYYMMDD.log.
func WriteLog(jobID, text string) error {
	dir, err := LogDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.log", jobID, time.Now().Format("20060102"))
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "[%s]\n%s\n\n", time.Now().Format(time.RFC3339), text)
	return err
}

// NewID returns a short unique id.
func NewID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
