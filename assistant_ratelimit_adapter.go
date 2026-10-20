package main

import assistant "sunvale/internal/assistant"

type modelRateLimit = assistant.RateLimit

func asModelRateLimit(err error) (modelRateLimit, bool) { return assistant.AsRateLimit(err) }

func rateLimitNotice(limit modelRateLimit) string { return assistant.RateLimitNotice(limit) }

func rankFallbackModels(current string, all []string) []string {
	return assistant.RankFallbackModels(current, all)
}
