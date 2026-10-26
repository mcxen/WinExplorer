package main

import (
	"encoding/json"
	"testing"
)

func TestAssistSegmentsInterleave(t *testing.T) {
	u := &assistantUI{}
	// Streamed order: text, tool, text, tool, tool, text (ReAct steps).
	seq := []aiTurn{
		{Role: "assistant", Text: "先看看目录"},
		{Role: "tool", Tool: "list_dir", Text: "a.md\nb.md"},
		{Role: "assistant", Text: "有两个文档，逐个读取"},
		{Role: "tool", Tool: "read_file", Text: "# A"},
		{Role: "tool", Tool: "read_file", Text: "# B"},
		{Role: "assistant", Text: "总结如下"},
	}
	u.turns = append(u.turns, aiTurn{Role: "user", Text: "总结文档"})
	for id, turn := range seq {
		u.applyLive(id, aiTurn{Role: turn.Role, Tool: turn.Tool, Text: turn.Text[:len(turn.Text)/2]}, true)
		u.applyLive(id, turn, false)
	}
	segs := assistSegments(u.turns, []int{1, 2, 3, 4, 5, 6})
	want := []assistSegKind{assistSegText, assistSegTools, assistSegText, assistSegTools, assistSegText}
	if len(segs) != len(want) {
		t.Fatalf("segments = %+v", segs)
	}
	for i, k := range want {
		if segs[i].Kind != k {
			t.Fatalf("segment %d kind %d, want %d: %+v", i, segs[i].Kind, k, segs)
		}
	}
	if len(segs[3].Turns) != 2 || segs[4].Text != "总结如下" {
		t.Fatalf("segments = %+v", segs)
	}
}

func TestAssistSegmentsOldTranscript(t *testing.T) {
	// A session saved before segments: flat turns, tools then reply.
	raw := `[{"role":"user","text":"q"},{"role":"tool","text":"x","tool":"list_dir"},{"role":"assistant","text":"done"}]`
	var turns []aiTurn
	if err := json.Unmarshal([]byte(raw), &turns); err != nil {
		t.Fatal(err)
	}
	segs := assistSegments(turns, []int{1, 2})
	if len(segs) != 2 || segs[0].Kind != assistSegTools || segs[1].Kind != assistSegText || segs[1].Text != "done" {
		t.Fatalf("segments = %+v", segs)
	}
}
