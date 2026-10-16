package main

import (
	_ "embed"
	"net/http"
)

//go:embed remote_web.html
var remoteWebHTML string

func (a *App) remoteHandleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(remoteWebHTML))
}

func (a *App) remoteHandlePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("pong"))
}
