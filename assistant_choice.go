package main

import (
	"strings"

	assistant "sunvale/internal/assistant"
)

// assistant_choice.go is the contract for a reply the user can tick.
// offer_choices, the system prompt and the pane all speak this block;
// changing the markers without the other two drops the checkboxes.

const (
	choiceOpen  = assistant.ChoiceOpen
	choiceClose = assistant.ChoiceClose
)

type aiChoice = assistant.Choice

// parseChoices pulls the first choice block out of a model or tool reply.
// One option is not a question — the model is just writing a list — so the
// text is left untouched and the pane does not invent checkboxes.
func parseChoices(text string) (string, *aiChoice) {
	if block, rest, ok := cutBlock(text, choiceOpen, choiceClose); ok {
		if ch := parseChoiceBlock(block); ch != nil {
			return rest, ch
		}
	}
	if block, rest, ok := cutBlock(text, "```choices", "```"); ok {
		if ch := parseChoiceBlock(block); ch != nil {
			return rest, ch
		}
	}
	return text, nil
}

func parseChoiceBlock(block string) *aiChoice {
	return assistant.ParseChoiceBlock(block)
}

// formatChoices is what offer_choices hands back. The pane parses it; the
// model sees the same text and is told to stop.
func formatChoices(prompt string, multi bool, options []string) string {
	var b strings.Builder
	b.WriteString(choiceOpen)
	b.WriteByte('\n')
	if multi {
		b.WriteString("multi: yes\n")
	} else {
		b.WriteString("multi: no\n")
	}
	b.WriteString("prompt: ")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteByte('\n')
	for _, opt := range options {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(opt))
		b.WriteByte('\n')
	}
	b.WriteString(choiceClose)
	b.WriteByte('\n')
	return b.String()
}
