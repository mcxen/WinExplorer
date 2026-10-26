package assistant

import (
	"fmt"
	"strings"
)

// modelsdev_fmt.go turns catalogue facts into the short strings the picker
// and the settings page show: a context size, a price, the capability
// badges. All formatting lives here so the tests can pin the wording and
// the UI files stay about layout.

// FormatContext is "200K", "1.2M" — the way model cards write it.
func FormatContext(tokens int) string {
	if tokens <= 0 {
		return ""
	}
	switch {
	case tokens >= 1_000_000 && tokens%1_000_000 == 0:
		return fmt.Sprintf("%dM", tokens/1_000_000)
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1000:
		return fmt.Sprintf("%dK", (tokens+499)/1000)
	}
	return fmt.Sprint(tokens)
}

// FormatPrice is "$2.5 进 $10 出"，per million tokens. A free model says
// 免费; an unknown price says nothing.
func FormatPrice(in, out float64) string {
	if in <= 0 && out <= 0 {
		return ""
	}
	if in <= 0 {
		return fmt.Sprintf("$%g 出", out)
	}
	if out <= 0 {
		return fmt.Sprintf("$%g 进", in)
	}
	return fmt.Sprintf("$%g 进 $%g 出", in, out)
}

// ModelBadges is the capability summary: 看图 推理 工具. Empty when the
// model does none of these — the common case says nothing.
func ModelBadges(m ModelInfo) string {
	var parts []string
	if m.SeesImages() {
		parts = append(parts, "看图")
	}
	if m.Reasoning {
		parts = append(parts, "推理")
	}
	if m.ToolCall {
		parts = append(parts, "工具")
	}
	return strings.Join(parts, "·")
}

// ModelDetail is the trailing text of one picker row: context and price,
// the numbers a choice is actually made on.
func ModelDetail(m ModelInfo) string {
	var parts []string
	if ctx := FormatContext(m.Context); ctx != "" {
		parts = append(parts, ctx)
	}
	if price := FormatPrice(m.CostIn, m.CostOut); price != "" {
		parts = append(parts, price)
	}
	return strings.Join(parts, " · ")
}

// ModelDescription is the second line: the catalogue's one-sentence pitch,
// capped so a row stays a row.
func ModelDescription(m ModelInfo) string {
	text := strings.TrimSpace(m.Description)
	if text == "" {
		return ""
	}
	r := []rune(text)
	if len(r) > 90 {
		text = string(r[:90]) + "…"
	}
	return text
}
