package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAssistantMemoryAndSessions(t *testing.T) {
	if _, err := cleanMemory("  下载按月份归档  "); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanMemory("密码是 hunter2"); err == nil {
		t.Fatal("accepted a password")
	}
	if _, err := cleanMemory("sk-abcdefghijklmnop"); err == nil {
		t.Fatal("accepted a token")
	}
	if _, err := cleanMemory(strings.Repeat("甲", assistMemoryRunes+1)); err == nil {
		t.Fatal("accepted a long fact")
	}
	if got := memoryPrompt([]aiMemory{{Text: "目录 {folder}"}}); got != "- 目录 (folder)" {
		t.Fatalf("prompt %q", got)
	}

	dir := t.TempDir()
	u := &assistantUI{loaded: true, diskPath: filepath.Join(dir, "assistant.json")}
	msg, err := u.addMemory("下载按月份归档")
	if err != nil || msg != "已记住" {
		t.Fatalf("%s %v", msg, err)
	}
	if msg, err = u.addMemory("下载按月份归档"); err != nil || msg != "已经记得" || len(u.memories) != 1 {
		t.Fatalf("%s %v %d", msg, err, len(u.memories))
	}
	info, err := os.Stat(u.memoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("memory mode %v", info.Mode())
	}

	u.turns = []aiTurn{
		{Role: "user", Text: "把下载按月份归档", Model: "EXCERPT-should-not-land", Files: []aiAttach{{Path: "/tmp/a.txt", Name: "a.txt", Detail: "1 B"}}},
		{Role: "assistant", Text: "可以", Plan: "missing-plan", Choice: &aiChoice{Prompt: "怎么做", Multi: true, Options: []string{"按月", "按年"}, On: []bool{true, false}, Sent: true}},
	}
	u.persistSession(filepath.Join(dir, "下载"))
	raw, err := os.ReadFile(u.sessionPath(u.sessionID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "EXCERPT-should-not-land") {
		t.Fatal("file excerpt was stored")
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(u.sessionPath(u.sessionID))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("session mode %v", st.Mode())
		}
	}
	v := &assistantUI{loaded: true, diskPath: u.diskPath}
	v.loadSide()
	if v.sessionID != u.sessionID || len(v.turns) != 2 || len(v.memories) != 1 {
		t.Fatalf("id %s turns %d mem %d", v.sessionID, len(v.turns), len(v.memories))
	}
	if v.turns[0].Model != "" || v.turns[0].Files[0].Name != "a.txt" {
		t.Fatalf("turn0 %+v", v.turns[0])
	}
	if v.turns[1].Choice == nil || !v.turns[1].Choice.Sent || !v.turns[1].Choice.On[0] || v.turns[1].Plan != "" {
		t.Fatalf("turn1 %+v plan %q", v.turns[1].Choice, v.turns[1].Plan)
	}
	if v.sessionTitle != "把下载按月份归档" {
		t.Fatalf("title %q", v.sessionTitle)
	}

	u.width, u.full, u.autopilot = 420, true, true
	u.save()
	prefs := &assistantUI{diskPath: u.diskPath, width: 100}
	prefs.readConfig()
	if prefs.width != 420 || !prefs.full || !prefs.autopilot {
		t.Fatalf("prefs %+v", prefs)
	}
	if err := os.WriteFile(u.diskPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := &assistantUI{diskPath: u.diskPath, width: 500, autopilot: true}
	broken.readConfig()
	broken.providers = nil
	broken.save()
	if data, err := os.ReadFile(u.diskPath); err != nil || string(data) != "{" {
		t.Fatalf("corrupt config was overwritten: %s %v", data, err)
	}
}

func TestAssistantSessionEvictsOldest(t *testing.T) {
	dir := t.TempDir()
	u := &assistantUI{loaded: true, diskPath: filepath.Join(dir, "assistant.json")}
	var first string
	for i := 0; i < assistMaxSessions+1; i++ {
		u.clearSession()
		u.sessionID = fmt.Sprintf("%02x", i)
		u.turns = []aiTurn{{Role: "user", Text: fmt.Sprintf("会话 %d", i)}}
		u.persistSession("/data")
		if i == 0 {
			first = u.sessionID
		}
	}
	if _, err := os.Stat(u.sessionPath(first)); !os.IsNotExist(err) {
		t.Fatalf("oldest session still present: %v", err)
	}
	if len(u.sessions) != assistMaxSessions {
		t.Fatalf("index len %d", len(u.sessions))
	}
	if u.sessions[0].ID != fmt.Sprintf("%02x", assistMaxSessions) {
		t.Fatalf("newest %+v", u.sessions[0])
	}
}

func TestArchivePasswordsPreserveRetiredField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assistant.json")
	raw := []byte(`{"passwords":[{"id":"old","label":"workspace","name":"alice","user":"alice","note":"retired"}],"prefs":{"width":410}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	u := &assistantUI{diskPath: path}
	u.readConfig()
	if len(u.passwords) != 0 || len(u.legacyPasswords) == 0 {
		t.Fatalf("retired data became candidates: %+v", u.passwords)
	}
	u.pwSecret = "archive-secret"
	u.addArchivePassword()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk assistantDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if len(disk.ArchivePasswords) != 1 || disk.ArchivePasswords[0].Secret != "archive-secret" {
		t.Fatalf("archive book %+v", disk.ArchivePasswords)
	}
	var legacy []map[string]any
	if err := json.Unmarshal(disk.Passwords, &legacy); err != nil {
		t.Fatal(err)
	}
	if len(legacy) != 1 || legacy[0]["user"] != "alice" || legacy[0]["note"] != "retired" {
		t.Fatalf("retired field changed: %s", disk.Passwords)
	}
}

func TestModelHistoryTail(t *testing.T) {
	var turns []aiTurn
	for i := 0; i < 30; i++ {
		turns = append(turns,
			aiTurn{Role: "user", Text: fmt.Sprintf("q%d", i), Model: fmt.Sprintf("m%d", i)},
			aiTurn{Role: "assistant", Text: fmt.Sprintf("a%d", i)},
		)
	}
	msgs := modelHistory(turns)
	if len(msgs) != assistHistMsgs {
		t.Fatalf("len %d", len(msgs))
	}
	if msgs[0].Content != "m18" || msgs[len(msgs)-1].Content != "a29" {
		t.Fatalf("span %q .. %q", msgs[0].Content, msgs[len(msgs)-1].Content)
	}
}

func TestAssistantMemoryTools(t *testing.T) {
	sunvaleToolState.bindMemory(nil)
	t.Cleanup(func() { sunvaleToolState.bindMemory(nil) })
	ctx := context.Background()
	sess, err := sunvaleSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := callToolText(t, sess, "remember", map[string]any{"text": "下载按月份归档"})
	if !strings.Contains(got, "已记住") {
		t.Fatal(got)
	}
	again := callToolText(t, sess, "remember", map[string]any{"text": "下载按月份归档"})
	if !strings.Contains(again, "已经记得") {
		t.Fatal(again)
	}
	bad := callTool(t, sess, "remember", map[string]any{"text": "密码是 hunter2"})
	if bad == nil || !bad.IsError {
		t.Fatalf("secret remembered: %+v", bad)
	}
	callToolText(t, sess, "remember", map[string]any{"text": "照片按年归档"})
	vague := callTool(t, sess, "forget", map[string]any{"text": "归档"})
	if vague == nil || !vague.IsError {
		t.Fatal("ambiguous forget was accepted")
	}
	gone := callToolText(t, sess, "forget", map[string]any{"text": "下载按月份归档"})
	if !strings.Contains(gone, "已忘掉") {
		t.Fatal(gone)
	}
	add, drop := sunvaleToolState.drainMemory()
	if len(drop) != 0 || len(add) != 1 || add[0].Text != "照片按年归档" {
		t.Fatalf("add %+v drop %v", add, drop)
	}

	sunvaleToolState.bindMemory([]aiMemory{{ID: "aa", Text: "窗口默认详细信息", Updated: time.Now()}})
	callToolText(t, sess, "forget", map[string]any{"text": "窗口默认详细信息"})
	add, drop = sunvaleToolState.drainMemory()
	if len(add) != 0 || len(drop) != 1 || drop[0] != "aa" {
		t.Fatalf("old add %+v drop %v", add, drop)
	}
	if list := callToolText(t, sess, "memory_list", map[string]any{}); list != "无" {
		t.Fatalf("list %q", list)
	}
}
