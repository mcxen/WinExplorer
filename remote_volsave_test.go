package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func volSaveResetForTest(t *testing.T) {
	volSaveConfFile = filepath.Join(t.TempDir(), "volsave.json")
	volSaveMu.Lock()
	volSaveRecent = map[string]*volSaveRec{}
	volSaveLast = nil
	volSaveNow = time.Now
	volSaveMu.Unlock()
	t.Cleanup(func() { volSaveConfFile = ""; volSaveNow = time.Now })
}

// TestRemoteVolSave covers save, name conflict, the one-minute dedupe,
// undo, and undo refusing a copy that changed or a save that never was.
func TestRemoteVolSave(t *testing.T) {
	e := newRemoteTestEnv(t)
	volSaveResetForTest(t)
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	volSaveNow = func() time.Time { return now }
	e.writeFile("pics/a.png", "A")
	e.writeFile("out/a.png", "old")
	tok := e.login()

	if r := e.api("POST", "/api/sunvale/volsave", "", map[string]string{"path": "/pics/a.png"}); r.Code != 401 {
		t.Fatalf("guest save code = %d", r.Code)
	}
	if r := e.api("POST", "/api/sunvale/volsave", tok, map[string]string{"path": "/pics/a.png"}); r.Code != 400 {
		t.Fatalf("no target code = %d %s", r.Code, r.Message)
	}
	if r := e.api("POST", "/api/sunvale/volsave/target", tok, map[string]string{"dir": "/../etc"}); r.Code == 200 {
		t.Fatal("target outside roots accepted")
	}
	if r := e.api("POST", "/api/sunvale/volsave/target", tok, map[string]string{"dir": "/out"}); r.Code != 200 {
		t.Fatalf("set target: %d %s", r.Code, r.Message)
	}
	save := func() remoteVolSaveResp {
		t.Helper()
		r := e.api("POST", "/api/sunvale/volsave", tok, map[string]string{"path": "/pics/a.png"})
		if r.Code != 200 {
			t.Fatalf("save: %d %s", r.Code, r.Message)
		}
		var v remoteVolSaveResp
		_ = json.Unmarshal(r.Data, &v)
		return v
	}
	v := save()
	if v.Name != "a (1).png" || v.Dup {
		t.Fatalf("first save = %+v", v)
	}
	copyPath := filepath.Join(e.home, "out", "a (1).png")
	if b, _ := os.ReadFile(copyPath); string(b) != "A" {
		t.Fatalf("copy content %q", b)
	}
	now = now.Add(59 * time.Second)
	if v := save(); !v.Dup || v.Msg != "已保存过" {
		t.Fatalf("within a minute = %+v", v)
	}
	if _, err := os.Stat(filepath.Join(e.home, "out", "a (2).png")); err == nil {
		t.Fatal("dedupe still copied")
	}
	now = now.Add(2 * time.Second)
	if v := save(); v.Dup || v.Name != "a (2).png" {
		t.Fatalf("after a minute = %+v", v)
	}
	// Undo takes back only the last copy.
	if r := e.api("POST", "/api/sunvale/volsave/undo", tok, map[string]string{}); r.Code != 200 {
		t.Fatalf("undo: %d %s", r.Code, r.Message)
	}
	if _, err := os.Stat(filepath.Join(e.home, "out", "a (2).png")); err == nil {
		t.Fatal("undo left the copy")
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatal("undo removed an older copy")
	}
	if r := e.api("POST", "/api/sunvale/volsave/undo", tok, map[string]string{}); r.Code == 200 {
		t.Fatal("second undo succeeded")
	}
	// After undo the same image saves again at once; a changed copy is kept.
	if v := save(); v.Dup || v.Name != "a (2).png" {
		t.Fatalf("save after undo = %+v", v)
	}
	if err := os.WriteFile(filepath.Join(e.home, "out", "a (2).png"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.api("POST", "/api/sunvale/volsave/undo", tok, map[string]string{}); r.Code == 200 {
		t.Fatal("undo deleted a changed file")
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "out", "a (2).png")); string(b) != "edited" {
		t.Fatal("changed copy lost")
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "out", "a.png")); string(b) != "old" {
		t.Fatal("existing file touched")
	}
}
