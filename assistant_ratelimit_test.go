package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestRateLimitSwitchesModelBeforeAnyText(t *testing.T) {
	oldRun := runFileAssistantOnce
	oldList := providerModelIDs
	t.Cleanup(func() {
		runFileAssistantOnce = oldRun
		providerModelIDs = oldList
	})
	var models []string
	runFileAssistantOnce = func(_ context.Context, run assistRun) (assistRunResult, error) {
		models = append(models, run.prov.Model)
		if run.prov.Model == "workbuddy/deepseek-v4.1-flash" {
			return assistRunResult{}, errors.New("error, status code: 429, message: 将在 2026-10-20 19:03:10 UTC+8 重置")
		}
		return assistRunResult{Text: "ok"}, nil
	}
	providerModelIDs = func(context.Context, aiProvider) ([]string, error) {
		return []string{"group/auto-deepseek-v4-1-flash", "raccoon/sn-deepseek-v4-1-flash"}, nil
	}
	var switched string
	var status string
	run := assistRun{
		prov:       aiProvider{ID: "p", Model: "workbuddy/deepseek-v4.1-flash"},
		onStatus:   func(s string) { status = s },
		onSwitched: func(model string) { switched = model },
	}
	result, err := runWithRateFallback(context.Background(), run)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "ok" || switched != "group/auto-deepseek-v4-1-flash" {
		t.Fatalf("result %q switched %q models %v", result.Text, switched, models)
	}
	if !strings.Contains(status, "group/auto-deepseek-v4-1-flash") {
		t.Fatalf("status %q", status)
	}
}

func TestRateLimitDoesNotRetryAfterText(t *testing.T) {
	oldRun := runFileAssistantOnce
	oldList := providerModelIDs
	t.Cleanup(func() {
		runFileAssistantOnce = oldRun
		providerModelIDs = oldList
	})
	calls := 0
	runFileAssistantOnce = func(_ context.Context, run assistRun) (assistRunResult, error) {
		calls++
		run.onLive(1, aiTurn{Role: "assistant", Text: "半句"}, true)
		return assistRunResult{}, &openai.APIError{HTTPStatusCode: 429, Type: "rate_limit_error", Message: "频率限制"}
	}
	providerModelIDs = func(context.Context, aiProvider) ([]string, error) {
		t.Fatal("listed models after text was already shown")
		return nil, nil
	}
	_, err := runWithRateFallback(context.Background(), assistRun{prov: aiProvider{Model: "m"}})
	if err == nil || !strings.Contains(err.Error(), "已限流") || calls != 1 {
		t.Fatalf("calls %d err %v", calls, err)
	}
}
