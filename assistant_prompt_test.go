package main

import (
	"context"
	"strings"
	"testing"
)

// TestPromptListsAllTools keeps the system prompt synchronized with the
// in-process MCP registry. It runs offline and fails when a newly registered
// tool is missing from assistantInstruction.
func TestPromptListsAllTools(t *testing.T) {
	sess, err := sunvaleSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range result.Tools {
		if !strings.Contains(assistantInstruction, tool.Name) {
			t.Errorf("tool %q is registered but absent from assistantInstruction", tool.Name)
		}
	}
}
