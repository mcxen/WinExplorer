package assistant

import "strings"

const (
	ChoiceOpen  = "<<<choices"
	ChoiceClose = ">>>"
)

// ParseChoiceBlock reads the options the model placed in a choice block.
func ParseChoiceBlock(block string) *Choice {
	ch := &Choice{Multi: true}
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
