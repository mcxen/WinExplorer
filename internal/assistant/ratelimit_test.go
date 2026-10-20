package assistant

import (
	"errors"
	"strings"
	"testing"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

func TestRateLimitNoticeUsesResetTime(t *testing.T) {
	err := errors.New("error, status code: 429, status: 429 Too Many Requests, message: WorkBuddy: 您的使用量已超出频率限制，将在 2026-10-20 19:03:10 UTC+8 重置，您也可以切换其他模型继续使用。")
	limit, ok := AsRateLimit(err)
	if !ok {
		t.Fatal("429 was not recognized")
	}
	want := time.Date(2026, 10, 20, 19, 3, 10, 0, time.FixedZone("UTC+8", 8*3600))
	if !limit.Until.Equal(want) {
		t.Fatalf("reset %s, want %s", limit.Until, want)
	}
	api := &openai.APIError{HTTPStatusCode: 429, Type: "rate_limit_error", Message: "将在 2026-10-20 19:03:10 UTC+8 重置"}
	if _, ok := AsRateLimit(api); !ok {
		t.Fatal("APIError 429 was not recognized")
	}
	notice := RateLimitNotice(limit)
	if !strings.Contains(notice, limit.Until.In(time.Local).Format("15:04")) || strings.Contains(notice, "status code") {
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
	got := RankFallbackModels("workbuddy/deepseek-v4.1-flash", all)
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
