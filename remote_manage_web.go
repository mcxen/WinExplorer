package main

import (
	_ "embed"
	"net/http"
)

//go:embed remote_manage_web.html
var remoteManageHTML string

// remoteHandleManage serves Sunvale's built-in management page on /@manage
// and everything below it. The page is one self-contained document — no
// external assets, no build step — driving the same OpenList API the main
// page uses, plus the /api/sunvale/* endpoints; it carries no auth of its
// own because the page logs in through /api/auth/login/hash itself.
func (a *App) remoteHandleManage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(remoteManageHTML))
}
