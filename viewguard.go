package main

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// viewguard.go 守住「视图里不做阻塞调用」：App.view 逐帧在 UI 线程上跑，
// 一次剪贴板往返、exec.LookPath 或网络请求就会让窗口停止重绘、吞掉输入
// （v0.3.5 的卡顿）。会阻塞的 helper 开头调用 viewBlocked：在视图构建期间被调
// 时，测试里直接失败，发布版记一次日志并让调用方返回缓存值。

// inView 是正在构建的视图层数（多个窗口在同一 UI 线程上依次构建）。
var inView atomic.Int32

// enterView / leaveView 包住 App.view。
func enterView() { inView.Add(1) }
func leaveView() { inView.Add(-1) }

// viewGuardFail 在测试里被替换为 t.Errorf；为 nil 时只记日志。
var viewGuardFail func(msg string)

var viewGuardLogged sync.Map

// viewBlocked 报告 name 这个阻塞调用是否发生在视图构建期间；为真时调用方
// 必须放弃本次阻塞操作，改用缓存。
func viewBlocked(name string) bool {
	if inView.Load() <= 0 {
		return false
	}
	msg := fmt.Sprintf("blocking call %s during view build", name)
	if f := viewGuardFail; f != nil {
		f(msg)
	}
	if _, seen := viewGuardLogged.LoadOrStore(name, true); !seen {
		slog.Warn("view guard: " + msg + "; served from cache")
	}
	return true
}
