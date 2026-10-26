package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// assistant_budget_test.go pins the two halves of the wrap-up guarantee: the
// wrapper tells the model the budget is ending on the last call only, and a
// turn cut off anyway is salvaged into a published answer, not an error.

// countingModel records what it was asked and answers with what the test set.
type countingModel struct {
	asks  [][]*schema.Message
	reply string
}

var _ model.ToolCallingChatModel = (*countingModel)(nil)

func (m *countingModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	cp := append([]*schema.Message(nil), in...)
	m.asks = append(m.asks, cp)
	return schema.AssistantMessage(m.reply, nil), nil
}

func (m *countingModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, in, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func (m *countingModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

// The iters-th call carries the wrap-up note; earlier ones do not; a second
// injected note never stacks.
func TestBudgetModelMarksLastCall(t *testing.T) {
	inner := &countingModel{reply: "ok"}
	bm := newBudgetModel(inner, 3)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := bm.Generate(ctx, []*schema.Message{schema.UserMessage("q")}); err != nil {
			t.Fatal(err)
		}
	}
	if bm.spentCalls() != 3 {
		t.Fatalf("spent %d", bm.spentCalls())
	}
	for i, ask := range inner.asks {
		last := ask[len(ask)-1]
		has := last.Role == schema.System && strings.Contains(last.Content, finishNowMark)
		if i < 2 && has {
			t.Fatalf("call %d got the wrap-up note early: %s", i, last.Content)
		}
		if i == 2 && !has {
			t.Fatalf("final call missing the wrap-up note")
		}
	}
	// The WithTools copy shares the counter and keeps marking once.
	wrapped, err := bm.WithTools(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.Generate(ctx, []*schema.Message{schema.UserMessage("q")}); err != nil {
		t.Fatal(err)
	}
	if bm.spentCalls() != 4 {
		t.Fatalf("WithTools copy does not share the counter: %d", bm.spentCalls())
	}
	last := inner.asks[3][len(inner.asks[3])-1]
	if !strings.Contains(last.Content, finishNowMark) {
		t.Fatalf("post-WithTools final call not marked: %s", last.Content)
	}
}

// A cut-off turn's events become one salvage prompt; the answer lands as the
// transcript's final assistant line and the headless result's Text.
func TestBudgetFinishTurnPublishesAnswer(t *testing.T) {
	inner := &countingModel{reply: "结果都在 ~/Downloads/south-plus-app/decompiled/sources，共 2825 个 java。"}
	run := assistRun{runID: "t"}
	result := assistRunResult{Events: []aiTurn{
		{Role: "assistant", Text: "我来查一下 java 文件的分布。"},
		{Role: "tool", Tool: "search_names", Text: "共找到 2825 个\n命中按第一层位置分布：\n  south-plus-app  2825"},
	}}
	if err := budgetFinishTurn(context.Background(), run, inner, 7, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 3 {
		t.Fatalf("events %d", len(result.Events))
	}
	final := result.Events[2]
	if final.Role != "assistant" || !strings.Contains(final.Text, "south-plus-app") {
		t.Fatalf("final turn: %+v", final)
	}
	if result.Text != final.Text {
		t.Fatalf("result.Text %q", result.Text)
	}
	ask := inner.asks[0]
	if len(ask) != 1 || ask[0].Role != schema.User {
		t.Fatalf("salvage asks: %+v", ask)
	}
	body := ask[0].Content
	if !strings.Contains(body, "[工具 search_names]") || !strings.Contains(body, "south-plus-app  2825") {
		t.Fatalf("transcript missing tool evidence: %s", body)
	}
	if !strings.Contains(body, "[助手] 我来查一下") {
		t.Fatalf("transcript missing assistant turns: %s", body)
	}
}

// A salvage call that fails or returns nothing is an error the run loop
// turns into the ordinary failure path — never a silent empty answer.
func TestBudgetFinishTurnRejectsEmptyAnswer(t *testing.T) {
	result := assistRunResult{Events: []aiTurn{{Role: "tool", Tool: "search_names", Text: "x"}}}
	if err := budgetFinishTurn(context.Background(), assistRun{runID: "t"}, &countingModel{reply: "  "}, 0, &result); err == nil {
		t.Fatal("empty answer accepted")
	}
	boomErr := errors.New("provider down")
	result2 := assistRunResult{Events: []aiTurn{{Role: "tool", Tool: "search_names", Text: "x"}}}
	err := budgetFinishTurn(context.Background(), assistRun{runID: "t"}, failingModel{err: boomErr}, 0, &result2)
	if !errors.Is(err, boomErr) {
		t.Fatalf("want provider error, got %v", err)
	}
}

// failingModel always errors, standing in for a provider outage during the
// salvage call.
type failingModel struct{ err error }

func (m failingModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, m.err
}

func (m failingModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, m.err
}

func (m failingModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}
