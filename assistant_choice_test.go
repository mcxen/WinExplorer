package main

import "testing"

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
	} {
		if got := choiceOptionValues(raw); len(got) != 2 || got[0] != "按类型" || got[1] != "按日期" {
			t.Fatalf("choiceOptionValues(%+v) = %v", raw, got)
		}
	}
}
