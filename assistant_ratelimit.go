package main

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	openai "github.com/cloudwego/eino-ext/components/model/openai"
)

// assistant_ratelimit.go is what happens when a provider answers 429.
// Magpie and WorkBuddy name the reset time and tell the caller to switch
// models. A turn that has not painted anything yet tries another route of
// the same model; one that already showed text stops, so a retry cannot
// repeat a tool call the user has seen.

const maxModelFallbacks = 3

// providerModelIDs lists the active provider's models. Tests replace it.
var providerModelIDs = fetchProviderModels

// runFileAssistantOnce is the model loop. Tests of the fallback replace it
// so they do not open a socket.
var runFileAssistantOnce assistantRunner = runFileAssistant

var resetTimePattern = regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+UTC([+-]\d{1,2})\b`)

type modelRateLimit struct {
	message string
	until   time.Time
}

// runWithRateFallback is the production assistantRunner. The model loop
// itself stays in runFileAssistant.
func runWithRateFallback(ctx context.Context, run assistRun) (assistRunResult, error) {
	origin := run.prov.Model
	tried := map[string]bool{origin: true}
	var catalog []string
	var listed bool
	var limit modelRateLimit
	for attempt := 0; attempt <= maxModelFallbacks; attempt++ {
		if attempt > 0 {
			next, ok := nextModelRoute(ctx, run, origin, tried, &catalog, &listed)
			if !ok {
				break
			}
			tried[next] = true
			if run.onStatus != nil {
				run.onStatus(run.prov.Model + " 已限流，改用 " + next)
			}
			run.prov.Model = next
		}
		published := false
		call := run
		live := call.onLive
		call.onLive = func(id int, turn aiTurn, partial bool) {
			published = true
			if live != nil {
				live(id, turn, partial)
			}
		}
		result, err := runFileAssistantOnce(ctx, call)
		if err == nil {
			if attempt > 0 {
				noteModelSwitch(run)
			}
			return result, nil
		}
		if errors.Is(err, errAssistantStopped) || ctx.Err() != nil {
			return result, err
		}
		got, ok := asModelRateLimit(err)
		if !ok {
			return result, err
		}
		limit = got
		if published {
			return result, errors.New(rateLimitNotice(got))
		}
	}
	return assistRunResult{}, errors.New(rateLimitNotice(limit))
}

func noteModelSwitch(run assistRun) {
	if run.onSwitched != nil {
		run.onSwitched(run.prov.Model)
		return
	}
	persistProviderModel(run.prov.ID, run.prov.Model)
}

func nextModelRoute(ctx context.Context, run assistRun, origin string, tried map[string]bool, catalog *[]string, listed *bool) (string, bool) {
	if !*listed {
		*listed = true
		listCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		ids, err := providerModelIDs(listCtx, run.prov)
		cancel()
		if err == nil {
			*catalog = ids
		}
	}
	for _, id := range rankFallbackModels(origin, *catalog) {
		if !tried[id] {
			return id, true
		}
	}
	return "", false
}

func asModelRateLimit(err error) (modelRateLimit, bool) {
	if err == nil {
		return modelRateLimit{}, false
	}
	var api *openai.APIError
	if errors.As(err, &api) && (api.HTTPStatusCode == 429 || api.Type == "rate_limit_error") {
		return modelRateLimit{message: api.Message, until: parseResetTime(api.Message)}, true
	}
	text := err.Error()
	if !strings.Contains(text, "status code: 429") && !strings.Contains(text, "rate_limit_error") && !strings.Contains(text, "频率限制") && !strings.Contains(text, "Too Many Requests") {
		return modelRateLimit{}, false
	}
	message := text
	if i := strings.LastIndex(text, "message: "); i >= 0 {
		message = strings.TrimSpace(text[i+len("message: "):])
	}
	return modelRateLimit{message: message, until: parseResetTime(text)}, true
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

func rateLimitNotice(limit modelRateLimit) string {
	if !limit.until.IsZero() {
		return "当前模型已限流，" + limit.until.In(time.Local).Format("15:04") + " 后恢复。可以在输入框左侧换一个模型。"
	}
	return "当前模型已限流。可以在输入框左侧换一个模型。"
}

// rankFallbackModels orders other routes of the same model ahead of a
// looser family match. A routing group is first: it exists to skip a
// single upstream that has already said no.
func rankFallbackModels(current string, all []string) []string {
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
func persistProviderModel(id, model string) {
	model = strings.TrimSpace(model)
	if id == "" || model == "" {
		return
	}
	path := assistantConfigPath()
	if path == "" {
		return
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	disk, _, err := readAssistantDisk(path)
	if err != nil {
		return
	}
	changed := false
	for i := range disk.Providers {
		if disk.Providers[i].ID != id || disk.Providers[i].Model == model {
			continue
		}
		disk.Providers[i].Model = model
		changed = true
	}
	if !changed {
		return
	}
	if writeAssistantDisk(path, disk) == nil {
		markAssistantConfigDirty()
	}
}
