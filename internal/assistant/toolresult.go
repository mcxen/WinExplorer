package assistant

import (
	"encoding/json"
	"strings"
)

// ToolText unwraps the official MCP JSON result envelope while leaving
// ordinary model text untouched. The adapter returns the whole CallToolResult
// as one JSON string, so parsing plan IDs and choice blocks from the raw
// envelope would see escaped newlines instead of the tool's actual text.
func ToolText(raw string) string {
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || len(envelope.Content) == 0 {
		return raw
	}
	var parts []string
	for _, item := range envelope.Content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) == 0 {
		return raw
	}
	return strings.Join(parts, "\n")
}
