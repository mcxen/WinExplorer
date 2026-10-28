package main

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
)

// startPprof 在设置了 SUNVALE_PPROF（如 127.0.0.1:6060）时开启 pprof
// HTTP 端点，用于排查卡顿与 CPU 占用；默认关闭，发布版不监听任何端口。
func startPprof() {
	addr := os.Getenv("SUNVALE_PPROF")
	if addr == "" {
		return
	}
	go func() {
		slog.Info("pprof listening", "addr", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Warn("pprof stopped", "err", err)
		}
	}()
}
