package main

import "strings"

type assistSegKind uint8

const (
	assistSegText assistSegKind = iota
	assistSegTools
	assistSegCard
	assistSegError
)

// assistSeg is one run of a reply in the order it streamed: prose, a row
// of tool calls, or a file/plan card. Turns are transcript indexes.
type assistSeg struct {
	Kind  assistSegKind
	Turns []int
	Text  string
}

// assistSegments splits an exchange's turns into ordered segments.
// Adjacent prose merges into one Markdown run and adjacent tool calls into
// one block, so a ReAct turn reads text, calls, text, calls, answer.
// Transcripts saved before this kept the same flat order, so they load as is.
func assistSegments(turns []aiTurn, rest []int) []assistSeg {
	var segs []assistSeg
	add := func(kind assistSegKind, i int, text string) {
		if n := len(segs); n > 0 && segs[n-1].Kind == kind && (kind == assistSegTools || kind == assistSegText) {
			sg := &segs[n-1]
			sg.Turns = append(sg.Turns, i)
			if text != "" {
				sg.Text += "\n\n" + text
			}
			return
		}
		segs = append(segs, assistSeg{Kind: kind, Turns: []int{i}, Text: text})
	}
	for _, i := range rest {
		t := turns[i]
		switch {
		case t.Role == "tool" && (t.Op != nil || len(t.Steps) > 0):
			add(assistSegCard, i, "")
		case t.Role == "tool":
			add(assistSegTools, i, "")
		case t.Error:
			// Errors stay in the footer as one collapsible line.
			segs = append(segs, assistSeg{Kind: assistSegError, Turns: []int{i}})
		default:
			text := t.Text
			if t.Choice != nil && strings.TrimSpace(text) == t.Choice.Prompt {
				text = ""
			}
			if text = strings.TrimSpace(text); text != "" {
				add(assistSegText, i, text)
			}
		}
	}
	return segs
}
