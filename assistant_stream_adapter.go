package main

import (
	"context"

	"github.com/cloudwego/eino/schema"

	assistant "sunvale/internal/assistant"
)

var errAssistantStopped = assistant.ErrStopped

func assistantStopped(ctx context.Context, err error) bool {
	return assistant.StreamStopped(ctx, err)
}

func readMessageStream(ctx context.Context, stream *schema.StreamReader[*schema.Message], onText func(full, reasoning string), onTool func(name string)) (*schema.Message, error) {
	return assistant.ReadMessageStream(ctx, stream, onText, onTool)
}

func streamLive(text string) string { return assistant.StreamLive(text) }

func streamShown(text string) string { return assistant.StreamShown(text) }

func reasoningLabel(text string) string { return assistant.ReasoningLabel(text) }

func modelHistory(turns []aiTurn) []*schema.Message {
	return assistant.ModelHistory(turns, assistHistMsgs, assistHistRunes)
}
