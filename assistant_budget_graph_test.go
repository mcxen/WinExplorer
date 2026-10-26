package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	assistant "sunvale/internal/assistant"
)

// The end-to-end half: a model that always calls a tool runs into
// MaxIterations. The graph must end with the wrap-up system message having
// reached the model on its final call — that is the marker the salvage path
// and the model's own last chance rely on.

// toolLoopModel answers every call with one tool call for the first n-1
// asks and a plain answer on the nth — unless forced, when it never stops.
type toolLoopModel struct {
	inner *countingModel
	force bool
}

func (m *toolLoopModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	_ = in
	if !m.force {
		return schema.AssistantMessage("done", nil), nil
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: "c1", Function: schema.FunctionCall{Name: "echo", Arguments: `"x"`}}}), nil
}

func (m *toolLoopModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *toolLoopModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

// echoTool returns its argument, the smallest tool the graph can call.
type echoTool struct{}

func (e *echoTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "echo", Desc: "echo its argument"}, nil
}

func (e *echoTool) InvokableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	return argumentsInJSON, nil
}

func TestBudgetModelReachesFinalCallInRealGraph(t *testing.T) {
	bm := newBudgetModel(&toolLoopModel{force: true}, 2)

	ctx := context.Background()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "loop",
		Model:         bm,
		MaxIterations: 2,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []tool.BaseTool{&echoTool{}}},
		},
		GenModelInput: assistant.GenModelInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("go")})
	var lastErr error
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			lastErr = ev.Err
		}
	}
	if lastErr == nil || !strings.Contains(lastErr.Error(), "max iterations") {
		t.Fatalf("want ErrExceedMaxIterations, got %v", lastErr)
	}
	// Both budgeted calls reached the model: the wrapper's count is exactly
	// the graph's own gate, so the note rides the last call the graph still
	// makes. (mark() appending and de-duplicating is covered in
	// TestBudgetModelMarksLastCall.)
	if bm.spentCalls() != 2 {
		t.Fatalf("model saw %d calls", bm.spentCalls())
	}
}
