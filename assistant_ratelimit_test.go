package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestRateLimitNoticeUsesResetTime(t *testing.T) {
	err := errors.New("error, status code: 429, status: 429 Too Many Requests, message: WorkBuddy: 您的使用量已超出频率限制，将在 2026-10-20 19:03:10 UTC+8 重置，您也可以切换其他模型继续使用。")
	limit, ok := asModelRateLimit(err)
	if !ok {
		t.Fatal("429 was not recognized")
	}
	want := time.Date(2026, 10, 20, 19, 3, 10, 0, time.FixedZone("UTC+8", 8*3600))
	if !limit.until.Equal(want) {
		t.Fatalf("reset %s, want %s", limit.until, want)
	}
	api := &openai.APIError{HTTPStatusCode: 429, Type: "rate_limit_error", Message: "将在 2026-10-20 19:03:10 UTC+8 重置"}
	if _, ok := asModelRateLimit(api); !ok {
		t.Fatal("APIError 429 was not recognized")
	}
	notice := rateLimitNotice(limit)
	if !strings.Contains(notice, limit.until.In(time.Local).Format("15:04")) || strings.Contains(notice, "status code") {
		t.Fatalf("notice %q", notice)
	}
}

func TestRankFallbackPrefersSameRoute(t *testing.T) {
	all := []string{
		"grok/grok-4.7",
		"lobsterai/deepseek-flash",
		"raccoon/sn-deepseek-v4-1-flash",
		"group/auto-deepseek-v4-1-flash",
		"workbuddy/deepseek-v4-pro",
		"workbuddy/deepseek-v4.1-flash",
	}
	got := rankFallbackModels("workbuddy/deepseek-v4.1-flash", all)
	want := []string{
		"group/auto-deepseek-v4-1-flash",
		"raccoon/sn-deepseek-v4-1-flash",
		"lobsterai/deepseek-flash",
		"workbuddy/deepseek-v4-pro",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", got, want)
	}
}

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
