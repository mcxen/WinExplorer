package assistant

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// GenModelInput replaces Eino's default, which formats the instruction as an
// FString template whenever session values exist and so fails on literal
// braces such as the OpenUI docs' {style: "gauge"}. Only exact {key}
// placeholders for present session values are substituted; every other brace
// is passed through verbatim.
func GenModelInput(ctx context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemMessage(FillInstruction(instruction, adk.GetSessionValues(ctx))))
	}
	return append(msgs, input.Messages...), nil
}

// FillInstruction substitutes {key} for each session value, leaving any
// other braces untouched.
func FillInstruction(instruction string, vs map[string]any) string {
	if len(vs) == 0 {
		return instruction
	}
	keys := make([]string, 0, len(vs))
	for k := range vs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		pairs = append(pairs, "{"+k+"}", fmt.Sprint(vs[k]))
	}
	return strings.NewReplacer(pairs...).Replace(instruction)
}
