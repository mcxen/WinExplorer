package assistant

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type captureModel struct{ sys *string }

func (m captureModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	*m.sys = in[0].Content
	return schema.AssistantMessage("ok", nil), nil
}
func (m captureModel) Stream(ctx context.Context, in []*schema.Message, o ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, _ := m.Generate(ctx, in, o...)
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}
func (m captureModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

// AST-FSTRING: literal braces with session values must not hit FString.
func TestGenModelInputLiteralBraces(t *testing.T) {
	ins := `当前文件夹是 {folder}。用法 {style: "gauge"} {cols: n} {media,...}`
	var sys string
	ctx := context.Background()
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "t", Description: "t", Instruction: ins,
		Model: captureModel{&sys}, GenModelInput: GenModelInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	it := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent}).Run(ctx,
		[]adk.Message{schema.UserMessage("总结一下这个文件夹里的文档")},
		adk.WithSessionValues(map[string]any{"folder": `C:\docs`, "mode": "x"}))
	for {
		ev, ok := it.Next()
		if !ok {
			break
		}
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
	}
	want := `当前文件夹是 C:\docs。用法 {style: "gauge"} {cols: n} {media,...}`
	if sys != want {
		t.Fatalf("system = %q", sys)
	}
	if got := FillInstruction(EffectiveInstruction(""), map[string]any{"style": "BAD"}); got == "" {
		t.Fatal("empty")
	}
}
