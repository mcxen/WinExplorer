# 19 单实例与再次启动

## 问题

v0.3.6 用户反馈：关闭晴谷后再点图标，窗口不出来。

根因：Windows/macOS 上关闭窗口只是隐藏到托盘，进程继续运行并持有单实例锁。
第二次启动（不带参数）经 mygo 套接字把命令行转给它后立即退出；而运行中实例的
`OnSecondInstance` 只在**有路径参数**时调用 `trayMainApp()`，无参数时什么都不做，
隐藏的窗口一直不显示。mygo 的锁在读完数据后就关闭连接，不等对方显示窗口，
所以后启动的进程无法发现转发失败。Linux 下最小化（没有托盘时也会隐藏窗口）可复现。

## 设计（singleinstance.go）

- 自有协议：每用户一个 Unix 套接字 `sunvale-<hash>.sock`（`XDG_RUNTIME_DIR` 或临时目录）。
  客户端发一行 JSON `{args, workingDir}`，服务端在主线程（`mygo.RunOnMain`，不在 view 内）
  调 `trayMainApp()`（无窗口则新建，最小化则还原，隐藏则显示并聚焦），再在新标签页打开
  命令行路径，**之后**才回 `ok`。
- 客户端超时：连接 1 s，整体 2 s。结果：
  - `acked`：退出；
  - `no-listener`（无人监听 / kill -9 残留的套接字文件）：删除并接管；
  - `no-ack`（对方卡住、正在退出、没有窗口）：接管并正常启动。
- 退出时在 `OnBeforeQuit` 先关监听并删除自己的套接字文件，再关窗口。
- Windows 命名互斥量仍保留：套接字是自己的但互斥量被占（更新器重启时旧进程未退、
  或旧版本只监听 mygo 套接字），重试 3 s 后用 `FindWindow` 显示旧窗口；找不到窗口则照常启动。
  若已判定 `no-ack` 则直接接管，不再等互斥量。
- macOS：`OnActivate(hasVisibleWindows=false)`（点 Dock 图标、Finder 再次打开）调用 `trayMainApp()`。
- 没有托盘时最小化不再隐藏窗口（否则无处可点回）。
- 所有判断写入 sunvale.log，前缀 `single-instance:`。

## 测试

- `TestAcquireSingleInstance`：决策表（acked / no-ack 接管 / 互斥量）。
- `TestForwardStaleAndSilent`：真实套接字的残留文件、不回应、无窗口不 ack。
- Linux 真机（DISPLAY=:10）：隐藏后重启、带路径转发、关闭后重启、kill -9 后重启、SIGSTOP 卡住后重启。
- Windows / macOS 只做交叉编译与推理，未在真机运行。
