package assistant

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

// RateLimit describes a provider 429 and the reset time it advertised.
type RateLimit struct {
	Message string
	Until   time.Time
}

var resetTimePattern = regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+UTC([+-]\d{1,2})\b`)

func AsRateLimit(err error) (RateLimit, bool) {
	if err == nil {
		return RateLimit{}, false
	}
	var api *openai.APIError
	if errors.As(err, &api) && (api.HTTPStatusCode == 429 || api.Type == "rate_limit_error") {
		return RateLimit{Message: api.Message, Until: parseResetTime(api.Message)}, true
	}
	text := err.Error()
	if !strings.Contains(text, "status code: 429") && !strings.Contains(text, "rate_limit_error") && !strings.Contains(text, "频率限制") && !strings.Contains(text, "Too Many Requests") {
		return RateLimit{}, false
	}
	message := text
	if i := strings.LastIndex(text, "Message: "); i >= 0 {
		message = strings.TrimSpace(text[i+len("Message: "):])
	}
	return RateLimit{Message: message, Until: parseResetTime(text)}, true
}

func parseResetTime(text string) time.Time {
	m := resetTimePattern.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}
	}
	hours, err := strconv.Atoi(m[2])
	if err != nil {
		return time.Time{}
	}
	loc := time.FixedZone("UTC"+m[2], hours*3600)
	t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1], loc)
	if err != nil {
		return time.Time{}
	}
	return t
}

func RateLimitNotice(limit RateLimit) string {
	if !limit.Until.IsZero() {
		return "当前模型已限流，" + limit.Until.In(time.Local).Format("15:04") + " 后恢复。可以在输入框左侧换一个模型。"
	}
	return "当前模型已限流。可以在输入框左侧换一个模型。"
}

// RankFallbackModels orders other routes of the same model ahead of a
// looser family match. A routing group is first: it exists to skip a
// single upstream that has already said no.
func RankFallbackModels(current string, all []string) []string {
	core := modelCore(current)
	if core == "" {
		return nil
	}
	type cand struct {
		id   string
		rank int
	}
	var ranked []cand
	seen := map[string]bool{current: true}
	word := strings.Split(core, "-")[0]
	for _, id := range all {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		other := modelCore(id)
		rank := 3
		switch {
		case other == core && strings.HasPrefix(strings.ToLower(id), "group/"):
			rank = 0
		case other == core:
			rank = 1
		case strings.Contains(other, core) || (len(other) >= 8 && strings.Contains(core, other)):
			rank = 2
		case len(word) >= 4 && strings.Contains(other, word):
			rank = 3
		default:
			continue
		}
		ranked = append(ranked, cand{id, rank})
	}
	// Stable by rank, and within a rank by the provider's own order.
	out := make([]string, 0, len(ranked))
	for want := 0; want <= 3; want++ {
		for _, c := range ranked {
			if c.rank == want {
				out = append(out, c.id)
			}
		}
	}
	return out
}

func modelCore(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	id = strings.TrimPrefix(id, "auto-")
	id = strings.TrimPrefix(id, "sn-")
	id = strings.ReplaceAll(id, ".", "-")
	id = strings.ReplaceAll(id, "_", "-")
	return id
}

// persistProviderModel keeps a headless run on the route that answered,
// when no window is around to call useModel.
