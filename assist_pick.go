package main

import (
	"fmt"
	"os"
	"time"

	"sunvale/internal/forms"
)

// assist_pick.go shares the native open-dialog path for the assistant file
// picker and OpenUI / schedule PathField browse buttons. A missing
// DefaultPath makes GTK and some Windows choosers hang or return ENOENT;
// ExistingDir always hands them a real folder. picking is cleared on
// success, error, panic, or watchdog timeout so one hung dialog cannot
// permanently lock the assistant.

// assistPickWatchdog is how long a chooser may block before picking is
// forced clear. Native dialogs normally return on cancel; this only
// recovers from a stuck DefaultPath / portal. Tests may shorten it.
var assistPickWatchdog = 90 * time.Second

// assistDialogDir returns an existing directory for Dialog.Open's
// DefaultPath. Empty and special pages fall back through ExistingDir.
func assistDialogDir(path string) string {
	if path == "" || specialPath(path) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			path = home
		}
	}
	return forms.ExistingDir(path)
}

// runAssistPick runs open off the UI thread and always clears
// assist.picking afterwards. done runs on the UI thread with the result
// (or a timeout / panic error).
func (a *App) runAssistPick(open func() ([]string, error), done func(paths []string, err error)) {
	if a.assist.picking {
		return
	}
	a.assist.picking = true
	go func() {
		type outcome struct {
			paths []string
			err   error
		}
		ch := make(chan outcome, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					ch <- outcome{err: fmt.Errorf("选择对话框异常")}
				}
			}()
			paths, err := open()
			ch <- outcome{paths: paths, err: err}
		}()
		var out outcome
		select {
		case out = <-ch:
		case <-time.After(assistPickWatchdog):
			out.err = fmt.Errorf("选择超时，请重试")
		}
		a.postAssist(func() {
			a.assist.picking = false
			if done != nil {
				done(out.paths, out.err)
			}
		})
	}()
}
