# 12 诊断日志（LOG1）

目标：在 Windows（GUI 子系统，无控制台）上也能调试，尤其是 AI 助手。

## 位置

| 系统 | 目录 |
|------|------|
| Windows | `%LOCALAPPDATA%\Sunvale\logs` |
| macOS | `~/Library/Logs/Sunvale` |
| Linux | `$XDG_STATE_HOME/sunvale/logs`（默认 `~/.local/state/sunvale/logs`） |

`SUNVALE_LOG_DIR` 覆盖目录。文件：`sunvale.log`（当前）、`sunvale.log.1…5`（轮转，5 MB × 5）、`crash.log`（Go 运行时致命错误，`debug.SetCrashOutput`）。

## 实现

- `internal/applog`：`log/slog` 文本格式 → 按大小轮转的文件；非 Windows 或 `SUNVALE_LOG_STDERR=1` 时同时写 stderr（写失败忽略）。标准库 `log.Printf` 同样落到日志。
- 所有记录经过脱敏处理器：`Authorization`、`Bearer …`、`api_key/token/secret/password=…`、URL 中 `key=`/`token=`、`sk-…`、`ghp_…`、`AIza…`、私钥块；键名含 apiKey/token/secret/password/cookie 的属性整体替换为 `[REDACTED]`。路径保留。
- `main()` 第一行初始化日志，`defer applog.Recover("main", true)` 记录顶层 panic 后再抛出。
- 级别：error / warn / info / debug，设置 → 日志 → 日志级别（`appSettings.LogLevel`），环境变量 `SUNVALE_LOG` 优先。

## 记录内容

- 启动：pid、OS、GOARCH、Go 版本、可执行路径、日志路径；第一帧绘制（`since_boot_ms`，无条件）；就绪后版本号、各显示器缩放（`mygo.Screen.Displays`）。
- 托盘与全局搜索快捷键初始化失败：warn（其余行为不受影响）。
- 单实例：获得锁 / 已在运行并唤起。
- 更新：每条镜像线路成功 / 失败（耗时、错误），全部失败。
- 任务内核：`runTask` 包装，task start / end / failed（kind、标题、来源、路径、耗时），panic 带栈。
- 助手（每次运行一个 `run` id，来源 pane / schedule:<id> / a2a / mcp）：
  - run start：供应商名、模型、Base URL（脱敏）、根目录、全托管、历史条数；run end / stopped / failed（耗时、错误截断 2000 字）；运行中 panic 记录栈并转为错误。
  - HTTP（自定义 `http.Client` Transport）：方法、URL（脱敏）、状态码、Content-Type、首包耗时；≥400 时附响应体片段（脱敏，截断 1500 字）；网络错误。不记录请求头。
  - 每条消息：finish reason、token 用量（若供应商返回）；debug 级别记录流式分片数与字数。
  - 工具调用：名称、call id、参数（截断 800 字）；结果（截断）与从调用到结果的耗时。
  - MCP 工具处理函数：参数、耗时、错误或工具错误（debug 记录成功调用）。
  - OpenUI：解析失败（同一程序只记一次）、解析 panic。
  - 定时任务触发的助手运行。

## 界面

- 设置 → 日志：日志级别下拉、打开日志文件夹、导出日志 (zip)（`makeZip`，保存到下载文件夹并在文件管理器中选中）。
- 助手面板错误行下方“查看日志”：打开 `sunvale.log`，失败时打开文件夹。

## 限制

- 流式响应只记录首包耗时，总耗时见 run end。
- 脱敏基于规则，非常规格式的密钥可能漏网；导出前可自行检查。
- 原生（cgo / Win32）崩溃不经 Go 运行时时不会写入 crash.log。
