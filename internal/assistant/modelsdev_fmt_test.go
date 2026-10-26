package assistant

import (
	"strings"
	"testing"
)

func TestFormatContextAndPrice(t *testing.T) {
	for _, tc := range []struct {
		tokens int
		want   string
	}{
		{128000, "128K"}, {200000, "200K"}, {1000000, "1M"},
		{1048576, "1.0M"}, {400000, "400K"}, {0, ""}, {512, "512"},
	} {
		if got := FormatContext(tc.tokens); got != tc.want {
			t.Fatalf("FormatContext(%d) = %q, want %q", tc.tokens, got, tc.want)
		}
	}
	if got := FormatPrice(2.5, 10); got != "$2.5 进 $10 出" {
		t.Fatalf("FormatPrice = %q", got)
	}
	if got := FormatPrice(0, 3); got != "$3 出" {
		t.Fatalf("free-in price = %q", got)
	}
	if got := FormatPrice(0, 0); got != "" {
		t.Fatalf("free model price = %q", got)
	}
}

func TestModelBadgesAndDetail(t *testing.T) {
	m := ModelInfo{Context: 128000, CostIn: 1.25, CostOut: 10, Input: []string{"text", "image"}, Reasoning: true, ToolCall: true}
	if got := ModelBadges(m); got != "看图·推理·工具" {
		t.Fatalf("badges = %q", got)
	}
	if got := ModelDetail(m); got != "128K · $1.25 进 $10 出" {
		t.Fatalf("detail = %q", got)
	}
	plain := ModelInfo{}
	if ModelBadges(plain) != "" || ModelDetail(plain) != "" {
		t.Fatalf("plain model should be silent: %q %q", ModelBadges(plain), ModelDetail(plain))
	}
	long := ModelInfo{Description: strings.Repeat("字", 120)}
	if got := ModelDescription(long); len([]rune(got)) != 91 || !strings.HasSuffix(got, "…") {
		t.Fatalf("description cap: %d %q", len([]rune(got)), got)
	}
}
