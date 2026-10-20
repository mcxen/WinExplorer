package main

import "strings"

// assistant_choice.go is the contract for a reply the user can tick.
// offer_choices, the system prompt and the pane all speak this block;
// changing the markers without the other two drops the checkboxes.

const (
	choiceOpen  = "<<<choices"
	choiceClose = ">>>"
)

// aiChoice is one unanswered (or already sent) question under a turn.
// On is parallel to Options and is what the checkboxes edit.
type aiChoice struct {
	Prompt  string   `json:"prompt,omitempty"`
	Multi   bool     `json:"multi"`
	Options []string `json:"options"`
	On      []bool   `json:"on,omitempty"`
	Sent    bool     `json:"sent,omitempty"`
}

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

func cutBlock(text, open, close string) (block, rest string, ok bool) {
	i := strings.Index(text, open)
	if i < 0 {
		return "", "", false
	}
	from := i + len(open)
	j := strings.Index(text[from:], close)
	if j < 0 {
		return "", "", false
	}
	block = text[from : from+j]
	rest = strings.TrimSpace(text[:i] + text[from+j+len(close):])
	return block, rest, true
}

func parseChoiceBlock(block string) *aiChoice {
	ch := &aiChoice{Multi: true}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "multi:"):
			v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "multi:")))
			ch.Multi = v == "yes" || v == "true" || v == "multi" || v == "多选"
		case strings.HasPrefix(line, "mode:"):
			v := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "mode:")))
			ch.Multi = v != "single" && v != "no" && v != "单选"
		case strings.HasPrefix(line, "prompt:"):
			ch.Prompt = strings.TrimSpace(strings.TrimPrefix(line, "prompt:"))
		case strings.HasPrefix(line, "- "):
			opt := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			if opt != "" {
				ch.Options = append(ch.Options, opt)
			}
		}
	}
	if len(ch.Options) < 2 {
		return nil
	}
	ch.On = make([]bool, len(ch.Options))
	return ch
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
