package assistant

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

var ErrStopped = errors.New("已停止")

func StreamStopped(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, adk.ErrStreamCanceled))
}

// ReadMessageStream drains one model or tool stream. Cancellation keeps the
// partial message and reports ErrStopped.
func ReadMessageStream(ctx context.Context, stream *schema.StreamReader[*schema.Message], onText func(full, reasoning string), onTool func(name string)) (*schema.Message, error) {
	if stream == nil {
		return nil, nil
	}
	defer stream.Close()
	var chunks []*schema.Message
	var text, reasoning strings.Builder
	stopped := false
	for {
		if ctx.Err() != nil {
			stopped = true
			break
		}
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if StreamStopped(ctx, err) {
				stopped = true
				break
			}
			return nil, err
		}
		if chunk == nil {
			continue
		}
		chunks = append(chunks, chunk)
		if chunk.Content != "" {
			text.WriteString(chunk.Content)
		}
		if chunk.ReasoningContent != "" {
			reasoning.WriteString(chunk.ReasoningContent)
		}
		if onText != nil && (chunk.Content != "" || chunk.ReasoningContent != "") {
			onText(text.String(), reasoning.String())
		}
		if onTool != nil {
			for _, call := range chunk.ToolCalls {
				name := strings.TrimSpace(call.Function.Name)
				if name != "" {
					onTool(name)
				}
			}
		}
	}
	if len(chunks) == 0 {
		if stopped {
			return nil, ErrStopped
		}
		return nil, nil
	}
	msg, err := schema.ConcatMessages(chunks)
	if err != nil || msg == nil {
		msg = &schema.Message{Role: schema.Assistant, Content: text.String()}
	} else if msg.Content == "" {
		msg.Content = text.String()
	}
	if stopped {
		return msg, ErrStopped
	}
	return msg, nil
}

// StreamLive keeps a complete OpenUI block visible so the pane can hold its
// place, while hiding a half-written choice block.
func StreamLive(text string) string {
	cut, hide := len(text), false
	if i := strings.Index(text, ChoiceOpen); i >= 0 && !strings.Contains(text[i:], ChoiceClose) {
		cut, hide = i, true
	}
	if i := strings.Index(text, "```choices"); i >= 0 && strings.Count(text[i:], "```") < 2 && i < cut {
		cut, hide = i, true
	}
	if hide {
		return strings.TrimSpace(text[:cut])
	}
	return text
}

// StreamShown hides unfinished choice and OpenUI blocks until they close.
func StreamShown(text string) string {
	cut, hide := len(text), false
	if i := strings.Index(text, ChoiceOpen); i >= 0 && !strings.Contains(text[i:], ChoiceClose) && i < cut {
		cut, hide = i, true
	}
	if i := strings.Index(text, "```choices"); i >= 0 && strings.Count(text[i:], "```") < 2 && i < cut {
		cut, hide = i, true
	}
	if i := UnclosedFence(text); i >= 0 && i < cut {
		cut, hide = i, true
	}
	if i := strings.Index(text, OpenUIMarkerOpen); i >= 0 && !strings.Contains(text[i:], OpenUIMarkerEnd) && i < cut {
		cut, hide = i, true
	}
	if hide {
		return strings.TrimSpace(text[:cut])
	}
	return text
}

func ReasoningLabel(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.LastIndex(text, "\n"); i >= 0 {
		text = strings.TrimSpace(text[i+1:])
	}
	rs := []rune(text)
	if len(rs) > 36 {
		text = "…" + string(rs[len(rs)-36:])
	}
	if text == "" {
		return "正在思考…"
	}
	return "思考中 " + text
}

func ModelHistory(turns []Turn, maxMessages, maxRunes int) []*schema.Message {
	var msgs []*schema.Message
	for _, t := range turns {
		switch t.Role {
		case "user":
			body := t.Text
			if t.Model != "" {
				body = t.Model
			}
			msgs = append(msgs, schema.UserMessage(LimitRunes(body, maxRunes)))
		case "assistant":
			if t.Text != "" {
				msgs = append(msgs, schema.AssistantMessage(LimitRunes(t.Text, maxRunes), nil))
			}
		}
	}
	if maxMessages > 0 && len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	return msgs
}
