package main

import assistant "sunvale/internal/assistant"

func linkPath(url string) (string, bool) { return assistant.LinkPath(url) }

func normalizeOpenMarkers(src string) string { return assistant.NormalizeOpenMarkers(src) }

func linkMarkdownPaths(src string) string { return assistant.LinkMarkdownPaths(src) }
