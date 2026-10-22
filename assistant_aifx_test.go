package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// assistant_aifx_test.go drives the breathing light between the assistant
// and the files it works on: a read lights the listing's row, a dropped
// file's chip breathes in the composer, and the sent message's chips keep
// breathing while the run reads them.

func swapTestFx(t *testing.T) (*aiFxStore, *fakeClock) {
	t.Helper()
	old := aiFx
	fx, clk := newTestFx()
	aiFx = fx
	t.Cleanup(func() { aiFx = old })
	return fx, clk
}

// TestReadToolsLightTheRow: read_file and read_files mark the rows they
// read busy, so the listing breathes over them until the run's endRun.
func TestReadToolsLightTheRow(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	deep := filepath.Join(dir, "deep.txt")
	for _, p := range []string{note, deep} {
		if err := os.WriteFile(p, []byte("alpha"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fx, clk := swapTestFx(t)
	sunvaleToolState.bind(dir, false, nil, nil, false)
	ctx := context.Background()
	sess, err := sunvaleSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	callToolText(t, sess, "read_file", map[string]any{"path": "note.txt"})
	clk.add(400 * time.Millisecond) // the breath fades in over its first 200 ms
	if l := fx.look(note); l.glow <= 0 || !l.live {
		t.Fatalf("read_file left the row dark: %+v", l)
	}
	if l := fx.look(deep); l.any() {
		t.Fatalf("an unread file glows: %+v", l)
	}
	callToolText(t, sess, "read_files", map[string]any{"paths": []string{"deep.txt"}})
	clk.add(400 * time.Millisecond)
	if l := fx.look(deep); l.glow <= 0 {
		t.Fatalf("read_files left the row dark: %+v", l)
	}
	fx.endRun()
	if l := fx.look(note); l.live && l.slide > 0 {
		t.Fatalf("endRun kept the row moving: %+v", l)
	}
	sunvaleToolState.bind(dir, false, nil, nil, false)
}

// TestAssistantAddFilesLightsChips: attaching a file — by drag, picker or
// paste, which all funnel into addFiles — starts its chip's breath, and
// removing the chip or starting a new session puts it out.
func TestAssistantAddFilesLightsChips(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	var u assistantUI
	u.chipLit = nil
	if n := u.addFiles([]string{note}); n != 1 {
		t.Fatalf("addFiles = %d", n)
	}
	at, ok := u.chipLit[note]
	if !ok || time.Since(at) > time.Second {
		t.Fatalf("chipLit = %+v", u.chipLit)
	}
	u.clearSession()
	if u.chipLit != nil {
		t.Fatal("a new session keeps old lights")
	}
}

// TestAssistantAttachmentChipsBreathe draws the composer's chips: each
// attached file is on screen, and the breath keeps asking for frames.
func TestAssistantAttachmentChipsBreathe(t *testing.T) {
	a := &App{}
	note := filepath.Join(t.TempDir(), "截图.png")
	a.assist.files = []aiAttach{{Path: note, Name: "截图.png", Detail: "12 KB"}}
	a.assist.litChip(note)
	tt := ui.NewTester(func(c *ui.Context) { a.assistantAttachments(c) }, 480, 120)
	tt.Frame()
	if !tt.HasText("截图.png") {
		t.Fatalf("chip missing: %v", tt.Texts())
	}
	if !tt.FramePending() {
		t.Fatal("the breath stopped asking for frames")
	}
	// Removal puts the light out with the chip gone.
	a.assist.files = nil
	delete(a.assist.chipLit, note)
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("an empty attachment row still animates")
	}
}

// TestSentChipsBreatheWhileReading draws a running turn whose message
// carries files: the chips breathe while the run reads them, and rest
// once it is over.
func TestSentChipsBreatheWhileReading(t *testing.T) {
	a := &App{}
	note := filepath.Join(t.TempDir(), "账单.xlsx")
	a.assist.chipLit = map[string]time.Time{note: time.Now()}
	a.assist.turns = []aiTurn{
		{Role: "user", Text: "看看这个", Files: []aiAttach{{Path: note, Name: "账单.xlsx", Detail: "4 KB"}}},
	}
	a.assist.running = true
	tt := ui.NewTester(func(c *ui.Context) { a.assistantTranscript(c) }, 520, 720)
	tt.Frame()
	if !tt.HasText("账单.xlsx") {
		t.Fatalf("sent chip missing: %v", tt.Texts())
	}
	if !tt.FramePending() {
		t.Fatal("the sent chip's breath stopped while the file is read")
	}
	a.assist.running = false
	a.assist.turns = append(a.assist.turns, aiTurn{Role: "assistant", Text: "读完了"})
	tt.Frame()
	if !tt.HasText("读完了") {
		t.Fatalf("finished turn missing: %v", tt.Texts())
	}
}

// TestSubmitLightsAttachedRows: the files a message carries are the run's
// first read, so their rows light the moment the message goes out.
func TestSubmitLightsAttachedRows(t *testing.T) {
	fx, clk := swapTestFx(t)
	note := filepath.Join(t.TempDir(), "note.txt")
	fx.markBusy(note)
	clk.add(400 * time.Millisecond)
	if l := fx.look(note); l.glow <= 0 {
		t.Fatalf("submit left the attached row dark: %+v", l)
	}
}
