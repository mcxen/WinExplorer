package main

import (
	_ "embed"
	"net/http"
	"strings"

	"sunvale/internal/remote"
)

//go:embed remote_manage_web.html
var remoteManageHTMLRaw string

// remoteManageHTML is the management page with the shared design tokens and
// script helpers (webtokens.go) inlined over the /*__TOKENS__*/ and
// /*__SHARED_JS__*/ placeholders, so the served document stays one
// self-contained file.
var remoteManageHTML = strings.NewReplacer(
	"/*__TOKENS__*/", remote.WebUITokensCSS,
	"/*__SHARED_JS__*/", remote.WebUISharedJS,
).Replace(remoteManageHTMLRaw)

// remoteHandleManage serves Sunvale's built-in management page on /@manage
// and everything below it. The page is one self-contained document — no
// external assets, no build step — driving the same OpenList API the main
// page uses, plus the /api/sunvale/* endpoints; it carries no auth of its
// own because the page logs in through /api/auth/login/hash itself.
func (a *App) remoteHandleManage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(remoteManageHTML))
}
