package assistant

import (
	"strings"
	"testing"
)

func TestEffectiveInstruction(t *testing.T) {
	if got := EffectiveInstruction(""); got != Instruction {
		t.Fatalf("empty extra should return built-in Instruction")
	}
	if got := EffectiveInstruction("   "); got != Instruction {
		t.Fatalf("whitespace extra should return built-in Instruction")
	}
	got := EffectiveInstruction("请用简洁中文回复")
	if !strings.HasPrefix(got, Instruction) {
		t.Fatalf("effective prompt must start with built-in Instruction")
	}
	if !strings.HasSuffix(got, "请用简洁中文回复") {
		t.Fatalf("effective prompt must end with extra: %q", got[len(got)-40:])
	}
	mid := got[len(Instruction) : len(Instruction)+2]
	if mid != "\n\n" {
		t.Fatalf("separator want \\n\\n, got %q", mid)
	}
	// Built-in body must stay intact so TestPromptListsAllTools keeps working.
	if !strings.Contains(EffectiveInstruction(""), "list_dir") {
		t.Fatal("built-in Instruction missing list_dir")
	}
}
