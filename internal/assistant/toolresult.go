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
		return StripMedia(raw)
	}
	var parts []string
	for _, item := range envelope.Content {
		if item.Type == "text" {
			parts = append(parts, item.Text)
		}
	}
	if len(parts) == 0 {
		return StripMedia(raw)
	}
	return StripMedia(strings.Join(parts, "\n"))
}

// Media markers name the pictures a tool result carries. The bytes live
// beside the turn (they are far too big for a transcript on disk); the
// marker is what the model-side rewrite looks for, and what ToolText erases
// before anything is shown to a person.
const (
	MediaOpen  = "[[sunvale-media:"
	MediaClose = "]]"
)

// MediaMarker names one batch of pictures.
func MediaMarker(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return MediaOpen + strings.Join(ids, ",") + MediaClose
}

// MediaIDs lists the picture ids in a message body, marker or raw envelope.
func MediaIDs(raw string) []string {
	var out []string
	rest := raw
	for {
		i := strings.Index(rest, MediaOpen)
		if i < 0 {
			break
		}
		rest = rest[i+len(MediaOpen):]
		j := strings.Index(rest, MediaClose)
		if j < 0 {
			break
		}
		for _, id := range strings.Split(rest[:j], ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				out = append(out, id)
			}
		}
		rest = rest[j+len(MediaClose):]
	}
	return out
}

// StripMedia removes the markers from a body. It is idempotent and safe on
// ordinary text.
func StripMedia(raw string) string {
	if !strings.Contains(raw, MediaOpen) {
		return raw
	}
	var b strings.Builder
	rest := raw
	for {
		i := strings.Index(rest, MediaOpen)
		if i < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		rest = rest[i+len(MediaOpen):]
		j := strings.Index(rest, MediaClose)
		if j < 0 {
			break
		}
		rest = rest[j+len(MediaClose):]
	}
	return b.String()
}
