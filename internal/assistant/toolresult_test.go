package assistant

import "testing"

func TestToolTextUnwrapsMCPContent(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"plan_id: abc\n文件夹: /tmp"}]}`
	if got := ToolText(raw); got != "plan_id: abc\n文件夹: /tmp" {
		t.Fatalf("ToolText() = %q", got)
	}
	if got := ToolText("plain text"); got != "plain text" {
		t.Fatalf("plain text changed: %q", got)
	}
}
