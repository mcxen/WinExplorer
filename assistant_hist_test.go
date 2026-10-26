package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	assistant "sunvale/internal/assistant"
)

func histUI(t *testing.T) *assistantUI {
	t.Helper()
	u := &assistantUI{}
	u.loaded = true
	u.diskPath = filepath.Join(t.TempDir(), "assistant.json")
	return u
}

// TestHistIndexBackwardCompat: an index written before pinned/turns/renamed
// loads, and the new fields default to zero.
func TestHistIndexBackwardCompat(t *testing.T) {
	u := histUI(t)
	_ = os.MkdirAll(u.assistRoot(), 0o755)
	old := `{"activeID":"a1","sessions":[{"id":"a1","title":"旧会话","updated":"2026-10-01T10:00:00Z","folder":"/tmp","preview":"p"}]}`
	if err := os.WriteFile(u.indexPath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, ok := readIndexFile(u.indexPath())
	if !ok || len(idx.Sessions) != 1 {
		t.Fatalf("old index: ok %v %+v", ok, idx)
	}
	if s := idx.Sessions[0]; s.Title != "旧会话" || s.Pinned || s.Renamed || s.Turns != 0 {
		t.Fatalf("old entry %+v", s)
	}
}

// TestHistPinRenamePersist: pin and rename survive a re-read and a later
// persist; the renamed title is not replaced by auto titling.
func TestHistPinRenamePersist(t *testing.T) {
	u := histUI(t)
	u.turns = []aiTurn{{Role: "user", Text: "整理下载目录"}, {Role: "assistant", Text: "好"}}
	u.persistSession("/tmp")
	id := u.sessionID
	if len(u.sessions) != 1 || u.sessions[0].Title != "整理下载目录" || u.sessions[0].Turns != 2 {
		t.Fatalf("auto title/turns: %+v", u.sessions)
	}
	u.pinSession(id, true)
	u.renameSession(id, "下载归档")
	u.turns = append(u.turns, aiTurn{Role: "user", Text: "再来"})
	u.persistSession("/tmp")

	v := histUI(t)
	v.diskPath = u.diskPath
	v.loadSide()
	if len(v.sessions) != 1 {
		t.Fatalf("sessions %+v", v.sessions)
	}
	s := v.sessions[0]
	if !s.Pinned || !s.Renamed || s.Title != "下载归档" || s.Turns != 3 {
		t.Fatalf("after reload %+v", s)
	}
	if v.sessionTitle != "下载归档" || !v.sessionRenamed {
		t.Fatalf("session file title %q renamed %v", v.sessionTitle, v.sessionRenamed)
	}
}

// TestChatHistShots renders the real history column (CHATHIST B) when
// SUNVALE_SHOT_DIR is set. Demo sessions are seeded into a temp dir only.
func TestChatHistShots(t *testing.T) {
	dir := os.Getenv("SUNVALE_SHOT_DIR")
	if dir == "" {
		t.Skip("SUNVALE_SHOT_DIR not set")
	}
	t.Cleanup(func() { visualV2 = false; testDark = nil; resolveTokens() })
	now := time.Now()
	day := func(d int, h, m int) time.Time {
		y := now.AddDate(0, 0, -d)
		return time.Date(y.Year(), y.Month(), y.Day(), h, m, 0, 0, now.Location())
	}
	seed := []struct {
		title string
		when  time.Time
		pin   bool
	}{
		{"按月份归档下载目录", day(3, 9, 42), true},
		{"批量重命名京都照片", now.Add(-20 * time.Minute), false},
		{"找出重复的 4K 视频", now.Add(-75 * time.Minute), false},
		{"季度报告转 PDF 并压缩", day(1, 17, 30), false},
		{"清理 node_modules 占用", day(1, 14, 2), false},
		{"NAS 备份目录结构建议", day(9, 10, 0), false},
		{"预算表按部门拆分", day(14, 10, 0), false},
		{"照片按拍摄日期分文件夹", day(40, 10, 0), false},
	}
	for _, dark := range []bool{false, true} {
		d := dark
		testDark = &d
		resolveTokens()
		a, tt, _ := keynavWindow(t, 4)
		a.applySettings(func(x *appSettings) { x.VisualStyle = "v2" })
		u := &a.assist
		u.loaded = true
		u.diskPath = filepath.Join(t.TempDir(), "assistant.json")
		_ = os.MkdirAll(filepath.Join(u.assistRoot(), "sessions"), 0o755)
		idx := assistIndex{}
		for i, s := range seed {
			id := "demo" + itoa(i)
			f := aiSessionFile{ID: id, Title: s.title, Updated: s.when, Turns: []aiTurn{{Role: "user", Text: s.title}}}
			if i == 1 {
				f.Turns = []aiTurn{
					{Role: "user", Text: "把京都的照片按拍摄日期重命名，格式 2026-09-18 京都 001"},
					{Role: "assistant", Text: "找到 86 张照片（HEIC 62，JPG 24），拍摄日期跨 9/16–9/21。按 EXIF 时间排序后重命名，同日内顺序编号。"},
					{Role: "assistant", Text: "已完成 86 / 86 项，可在操作卡上撤销。"},
					{Role: "user", Text: "再把视频也按同样规则处理"},
				}
			}
			writeSessionFile(t, u.sessionPath(id), f)
			idx.Sessions = append(idx.Sessions, assistant.SessionMeta{ID: id, Title: s.title, Updated: s.when, Pinned: s.pin, Turns: len(f.Turns)})
		}
		idx.ActiveID = "demo1"
		if err := writeIndexFile(u.indexPath(), idx); err != nil {
			t.Fatal(err)
		}
		u.loadSide()
		a.assistantOpen = true
		u.width = 560
		u.showHist = true
		for range 4 {
			tt.Frame()
		}
		name := map[bool]string{false: "10-b-real-light.png", true: "11-b-real-dark.png"}[dark]
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		_ = png.Encode(f, tt.Image())
		f.Close()
	}
}

func writeSessionFile(t *testing.T, path string, f aiSessionFile) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(path, data); err != nil {
		t.Fatal(err)
	}
}
