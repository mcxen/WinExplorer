package main

import (
	"sync"
	"time"

	"sunvale/internal/schedule"
)

// schedule.go is the process-wide timed-job service: one Store, one
// minute ticker, and a single-flight queue so jobs never overlap. The
// settings page and tray menu call into this file; the pure clock and
// file-rule logic live in internal/schedule.

var (
	schedOnce  sync.Once
	schedStore *schedule.Store
	schedMu    sync.Mutex // serializes job runs
	schedQueue chan string
)

// scheduleLoc is the wall clock the UI and ticker share. Tests may replace
// it; production uses time.Local (the user's OS timezone).
var scheduleLoc = func() *time.Location { return time.Local }

// ensureScheduleStore opens schedules.json once. A corrupt or unreadable
// file yields an empty in-process store so the settings page still works;
// the next successful save overwrites the bad file.
func ensureScheduleStore() *schedule.Store {
	schedOnce.Do(func() {
		s, err := schedule.Open("")
		if err != nil {
			path, _ := schedule.DefaultPath()
			s = schedule.NewEmpty(path)
		}
		schedStore = s
		_ = schedStore.RefreshNext(time.Now(), scheduleLoc())
	})
	return schedStore
}

// startScheduleRunner launches the minute ticker and the single-job
// worker. Called once from WhenReady.
func startScheduleRunner() {
	st := ensureScheduleStore()
	schedQueue = make(chan string, 32)
	go scheduleWorker()
	go scheduleTicker(st)
}

func scheduleTicker(st *schedule.Store) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	scheduleScan(st)
	for range t.C {
		scheduleScan(st)
	}
}

func scheduleScan(st *schedule.Store) {
	now := time.Now()
	loc := scheduleLoc()
	for _, j := range st.Jobs() {
		if schedule.Due(j, now, loc) {
			scheduleEnqueue(j.ID)
		}
	}
}

func scheduleEnqueue(id string) {
	if schedQueue == nil {
		return
	}
	select {
	case schedQueue <- id:
	default:
		// Queue full: drop; next minute retries if still due.
	}
}

func scheduleWorker() {
	for id := range schedQueue {
		runScheduledJob(id, false)
	}
}

// runScheduledJobNow is the settings「立即运行」entry: bypasses Due.
func runScheduledJobNow(id string) {
	go runScheduledJob(id, true)
}

func runScheduledJob(id string, manual bool) {
	schedMu.Lock()
	defer schedMu.Unlock()
	st := ensureScheduleStore()
	j, ok := st.Get(id)
	if !ok {
		return
	}
	if !manual && !j.Enabled {
		return
	}
	now := time.Now()
	status, detail, errMsg := executeScheduledJob(j)
	_ = st.RecordRun(id, now, status, detail, errMsg, scheduleLoc())
	logBody := status + " " + detail
	if errMsg != "" {
		logBody += "\n" + errMsg
	}
	_ = schedule.WriteLog(id, logBody)
	repaint()
}
