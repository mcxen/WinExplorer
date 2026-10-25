package main

import (
	"strings"
	assistant "sunvale/internal/assistant"
)

const (
	openUIMarkerOpen = assistant.OpenUIMarkerOpen
	openUIMarkerEnd  = assistant.OpenUIMarkerEnd
)

// These aliases keep the immediate-mode painter and its tests on their
// established vocabulary while the parser itself is reusable.
type (
	ouiExpr = assistant.OUIExpr
	ouiView = assistant.OUIView
	ouiCol  = assistant.OUICol
)

func splitOpenUI(text string) (string, []string) { return assistant.SplitOpenUI(text) }

func parseOpenUI(src string) *ouiView {
	v := assistant.ParseOpenUI(src)
	if v == nil && strings.TrimSpace(src) != "" {
		logOpenUIParseFail(src)
	}
	return v
}

func unclosedFence(text string) int { return assistant.UnclosedFence(text) }

func openTextStyle(size string) (float32, bool, bool) { return assistant.OpenTextStyle(size) }

func cutBlock(text, open, close string) (string, string, bool) {
	return assistant.CutBlock(text, open, close)
}

func openShort(s string, n int) string { return assistant.OpenShort(s, n) }

func openIsField(kind string) bool { return assistant.OpenIsField(kind) }
