package main

import (
	"strings"
	"testing"
)

func TestParseChoices(t *testing.T) {
	text := formatChoices("下一步？", false, []string{"留下", "移走"}) + "尾注"
	display, ch := parseChoices(text)
	if display != "尾注" || ch == nil || ch.Multi || len(ch.Options) != 2 || ch.Options[0] != "留下" || ch.Prompt != "下一步？" {
		t.Fatalf("display %q choice %+v", display, ch)
	}
	fenced := "说明\n```choices\nmulti: yes\nprompt: 挑几项\n- 甲\n- 乙\n```\n完"
	display, ch = parseChoices(fenced)
	if ch == nil || !ch.Multi || len(ch.Options) != 2 || ch.Prompt != "挑几项" || display != "说明\n\n完" {
		t.Fatalf("fenced display %q choice %+v", display, ch)
	}
	if _, ch = parseChoices("<<<choices\nprompt: 不够\n- 只有一个\n>>>"); ch != nil {
		t.Fatal("a single bullet is a list, not a question")
	}
}

func TestChoiceOptionValues(t *testing.T) {
	for _, raw := range []any{
		[]string{"按类型", "按日期"},
		[]any{"按类型", "按日期"},
		"- 按类型\n- 按日期",
		"按类型、按日期",
		`["按类型","按日期"]`,
		[]any{map[string]any{"label": "按类型"}, map[string]any{"text": "按日期"}},
	} {
		if got := choiceOptionValues(raw); len(got) != 2 || got[0] != "按类型" || got[1] != "按日期" {
			t.Fatalf("choiceOptionValues(%#v) = %v", raw, got)
		}
	}
}

func TestOfferChoicesShortListDoesNotError(t *testing.T) {
	res, _, err := sunvaleToolState.offerChoices(t.Context(), nil, choiceArgs{
		Prompt:  "下一步？",
		Options: []any{"只留这一个"},
	})
	if err != nil || res == nil || res.IsError {
		t.Fatalf("short list must not fail the run: err=%v res=%+v", err, res)
	}
	text := mcpTextOf(res)
	if strings.Contains(text, "需要至少") || strings.Contains(text, choiceOpen) {
		t.Fatalf("short list should ask for a real list, got %q", text)
	}
	ok, _, err := sunvaleToolState.offerChoices(t.Context(), nil, choiceArgs{
		Prompt:  "怎么整理？",
		Options: []any{map[string]any{"label": "按类型"}, map[string]any{"name": "按日期"}},
	})
	if err != nil || ok == nil || ok.IsError {
		t.Fatalf("object options: err=%v res=%+v", err, ok)
	}
	_, ch := parseChoices(mcpTextOf(ok))
	if ch == nil || len(ch.Options) != 2 || ch.Options[0] != "按类型" {
		t.Fatalf("object options did not become a choice: %q", mcpTextOf(ok))
	}
}
