package assistant

import "testing"

func TestCleanMemory(t *testing.T) {
	if got, err := CleanMemory("  下载按月份归档  ", 40); err != nil || got != "下载按月份归档" {
		t.Fatalf("clean = %q, %v", got, err)
	}
	for _, text := range []string{"密码是 hunter2", "sk-abcdefghijklmnop"} {
		if _, err := CleanMemory(text, 40); err == nil {
			t.Fatalf("accepted secret %q", text)
		}
	}
}

func TestSafeSessionID(t *testing.T) {
	for _, id := range []string{"", "abc", "0123456789abcdef", "../x"} {
		want := id == "abc" || id == "0123456789abcdef"
		if got := SafeSessionID(id); got != want {
			t.Fatalf("SafeSessionID(%q) = %v, want %v", id, got, want)
		}
	}
}
