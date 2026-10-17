package main

import (
	"testing"
	"time"
)

// TestFloatDialogsRender boots the real window view and opens every glass
// dialog in turn — settings, properties, batch rename and the Quick Look
// panel — letting the entrance animation run its course. It guards the
// material styling in floatDialog: a panel that fails to lay out, or a
// transition that never settles, fails here.
func TestFloatDialogsRender(t *testing.T) {
	a, tt, dir := keynavWindow(t, 4)

	openings := []struct {
		name string
		open func()
	}{
		{"设置", func() { a.settingsOpen = true }},
		{"属性", func() {
			a.selectOnly(dir + "/f000.txt")
			a.showProperties()
		}},
		{"批量重命名", func() {
			a.selectOnly(dir + "/f000.txt")
			a.batch.open = true
		}},
		{"快速预览", func() { a.quickLook = true }},
	}
	for _, o := range openings {
		o.open()
		tt.Advance(300 * time.Millisecond) // the entrance motion finishes
	}
}
