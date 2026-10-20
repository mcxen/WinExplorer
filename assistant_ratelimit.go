package main

import (
	"context"
	"errors"
	"strings"
	"time"
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
