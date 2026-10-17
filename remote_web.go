package main

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed remote_web.html
var remoteWebHTMLRaw string

// remoteWebHTML is the guest page with the shared design tokens and script
// helpers (webtokens.go) inlined over the /*__TOKENS__*/ and
// /*__SHARED_JS__*/ placeholders, so the served document stays one
// self-contained file.
var remoteWebHTML = strings.NewReplacer(
	"/*__TOKENS__*/", WebUITokensCSS,
	"/*__SHARED_JS__*/", WebUISharedJS,
).Replace(remoteWebHTMLRaw)

func (a *App) remoteHandleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(remoteWebHTML))
}

func (a *App) remoteHandlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("pong"))
}
