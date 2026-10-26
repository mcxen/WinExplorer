package main

import (
	"context"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// assistant_budget.go keeps the ReAct loop from dying without an answer.
// Eino's react graph ends with ErrExceedMaxIterations once the iteration
// budget runs out — mid-work, after tool results the user never saw explained.
// The wrapper here counts the model calls it forwards and, on the last one,
// tells the model the budget is ending and it must wrap up now. Even then a
// model may answer with more tool calls; the run loop catches that case and
// asks once more for a plain answer (budgetFinishTurn).

// finishNowInstruction is injected as a system message on the final model
// call. It rides last so it outranks the instruction block in recency.
const finishNowInstruction = "系统：这已是本轮最后一次思考机会，不会再有下一轮工具调用。立即根据已获得的信息作答：给出结论与必要的路径、数字；未完成的部分明确说明做到了哪一步、下一步该做什么。不要再调用任何工具。"

// finishNowMark identifies a previously injected wrap-up note, so retries
// never stack copies.
var finishNowMark = "这已是本轮最后一次思考机会"

// budgetState is the call counter shared between a wrapper and the copy the
// agent's WithTools path makes: the budget belongs to the run, not to one
// immutable view of the model.
type budgetState struct {
	mu    sync.Mutex
	left  int
	spent int
}

// budgetModel wraps a tool-calling model and injects the wrap-up instruction
// on the last iteration the agent will spend.
type budgetModel struct {
	inner model.ToolCallingChatModel
	st    *budgetState
}

// newBudgetModel returns the model whose last iters calls carry the wrap-up
// instruction. iters mirrors the agent's MaxIterations.
func newBudgetModel(inner model.ToolCallingChatModel, iters int) *budgetModel {
	return &budgetModel{inner: inner, st: &budgetState{left: iters}}
}

var _ model.ToolCallingChatModel = (*budgetModel)(nil)

func (m *budgetModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, m.mark(in), opts...)
}

func (m *budgetModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, m.mark(in), opts...)
}

// WithTools forwards tool binding. The copy shares the counter: the graph
// binds tools once before iterating, and the budget is the run's.
func (m *budgetModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	inner, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &budgetModel{inner: inner, st: m.st}, nil
}

// mark counts one model call and appends the wrap-up system message when
// this call is the budget's last. The react graph checks-and-decrements the
// same way: the iters-th call still runs, one more would not.
func (m *budgetModel) mark(in []*schema.Message) []*schema.Message {
	m.st.mu.Lock()
	defer m.st.mu.Unlock()
	m.st.spent++
	if m.st.spent < m.st.left {
		return in
	}
	out := make([]*schema.Message, 0, len(in)+1)
	for _, msg := range in {
		if msg != nil && msg.Role == schema.System && strings.Contains(msg.Content, finishNowMark) {
			continue
		}
		out = append(out, msg)
	}
	return append(out, schema.SystemMessage(finishNowInstruction))
}

// spentCalls reports how many model calls went through. Tests only.
func (m *budgetModel) spentCalls() int {
	m.st.mu.Lock()
	defer m.st.mu.Unlock()
	return m.st.spent
}
