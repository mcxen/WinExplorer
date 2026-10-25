// Package tasks is Sunvale's task kernel (TASKSYS, docs/specs/10-task-system.md):
// one model for every long, user-visible operation — compress, extract,
// copy, move, delete, rename, scripts — whoever started it (the user, the
// assistant, a schedule). A Manager queues tasks by priority, runs them
// within per-IO-class slots (two per disk by default), pauses, resumes,
// cancels and retries them, keeps a persisted history, and tells
// subscribers what changed, progress throttled. Operations see only a
// Reporter.
//
// The package does not import the UI; the app paints Snapshots.
package tasks
