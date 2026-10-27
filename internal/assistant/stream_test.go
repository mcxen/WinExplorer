package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestReadMessageStreamAndHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var seen []string
	msg, err := ReadMessageStream(ctx, schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "你", ReasoningContent: "先看目录"},
		{Role: schema.Assistant, Content: "好"},
		{Role: schema.Assistant, Content: "呀"},
	}), func(full, reasoning string) {
		seen = append(seen, full)
		if full == "你好" {
			cancel()
		}
		if reasoning != "" && !strings.Contains(ReasoningLabel(reasoning), "先看目录") {
			t.Fatalf("reasoning label %q", ReasoningLabel(reasoning))
		}
	}, nil)
	if !errors.Is(err, ErrStopped) || msg == nil || msg.Content != "你好" {
		t.Fatalf("partial message %+v %v", msg, err)
	}
	if len(seen) != 2 || seen[1] != "你好" {
		t.Fatalf("text callbacks %v", seen)
	}

	history := ModelHistory([]Turn{
		{Role: "user", Text: "visible", Model: "model text"},
		{Role: "assistant", Text: "answer"},
		{Role: "tool", Text: "ignored"},
	}, 2, 10)
	if len(history) != 2 || history[0].Content != "model text" || history[1].Content != "answer" {
		t.Fatalf("history %+v", history)
	}
}

func TestStreamingDisplayHidesIncompleteBlocks(t *testing.T) {
	if got := StreamLive("前文\n" + ChoiceOpen + "\n- a"); got != "前文" {
		t.Fatalf("live choice leaked: %q", got)
	}
	if got := StreamShown("前文\n```openui\nroot = Card([h])"); got != "前文" {
		t.Fatalf("unfinished OpenUI leaked: %q", got)
	}
	if got := StreamShown("前文\n" + OpenUIMarkerOpen + "\nroot = Card([])"); got != "前文" {
		t.Fatalf("unfinished OpenUI marker leaked: %q", got)
	}
}

func TestReadMessageStreamSkipsWillRetryAttempt(t *testing.T) {
	sr, sw := schema.Pipe[*schema.Message](4)
	go func() {
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "坏"}, nil)
		sw.Send(nil, &adk.WillRetryError{ErrStr: "503", RetryAttempt: 1})
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "好"}, nil)
		sw.Close()
	}()
	var status []string
	var texts []string
	msg, err := ReadMessageStream(context.Background(), sr, func(full, reasoning string) {
		if full == "" && reasoning != "" {
			status = append(status, reasoning)
			return
		}
		texts = append(texts, full)
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg == nil || msg.Content != "好" {
		t.Fatalf("want accepted attempt only, got %+v", msg)
	}
	if len(status) == 0 || !strings.Contains(status[0], "重试") {
		t.Fatalf("retry status missing: %v", status)
	}
	// The rejected "坏" may have been painted briefly; the final concat must not keep it.
	for _, s := range texts {
		if strings.Contains(s, "坏") && !strings.Contains(s, "好") {
			// interim paint of the failed attempt is ok; ensure final message is clean (checked above)
			_ = s
		}
	}
}
