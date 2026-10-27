# 14 助手 AI 稳定性审计

对照 `cloudwego/eino` v0.9.11 ADK（ChatModelAgent / Runner / ModelRetry / middleware）与
`eino-ext` openai/mcp 官方用法，审计本仓库助手栈的稳定性。基线：`origin/main` @
`b2c52c5`（CHATHIST B）。不做 UI / macOS26 打磨。

## 1. 现状（已有防护）

| 区域 | 实现 | 证据 |
| --- | --- | --- |
| FString 花括号 | 自定义 `GenModelInput` + AST guard | `internal/assistant/genmodelinput.go`、`assistant_guard_test.go` |
| 工具 IsError 不杀轮次 | `ResultPolicy{ErrorAsError: false}` | `assistant_mcp.go:sunvaleRunTools`、`TestSunvaleRunToolsToolErrorFlowsBack` |
| 迭代预算收尾 | `budgetModel` + `budgetFinishTurn` | `assistant_budget.go`、BUDGET-FINISH |
| 限流换模型 | 未出字前换路由，已出字则停 | `assistant_ratelimit.go` |
| 取消 / 流式部分保留 | `WithCancel` + `ReadMessageStream` → `ErrStopped` | `assistant_run.go`、`internal/assistant/stream.go` |
| panic | `runFileAssistant` recover + applog | `assistant_run.go` |
| 错误后 composer | `failAssist` 立刻清 `running` | AST1 / `assistant_error_test.go` |
| 上下文裁剪 | 最近 24 条、每条 6000 runes；落盘另裁 | `assistHistMsgs/Runes`、`ModelHistory` |
| 会话持久化 | `WritePrivate` 原子写 | `internal/assistant/store.go` |
| shell / 读文件预算 | 30s / 64KiB；文本 12000 runes | `assistant_shell.go`、`fileview` |
| 日志 | run id、HTTP 状态/耗时、脱敏 | LOG1 / `applog_glue.go` |

## 2. 风险（按严重度）

### P0 — 已修或本轮必修
### P0 — 停止/取消（用户反馈「暂停 ai 能力很差」，本轮最高优先）

11. **bash 工具丢弃 run ctx**（已修）  
    `bash` 曾调用 `shellCtx() = context.Background()`，Stop 无法打断最长 30s 的脚本。  
    **修复**：`bash(ctx, …)` 把 MCP/Agent 传入的 ctx 交给 `runBash`；取消时返回「已停止」。

12. **停止后 UI 仍像在跑**（已修）  
    仅 `cancel()` +「正在停止…」，`running` 一直为 true，composer 卡在 Stop。  
    **修复**：`assistantRequestStop` 立刻 `halted=true`、清 pending/status/queue、标记末条 `Stopped`、丢掉 coalesced live；composer `Running = running && !halted`，可立即打字；清理结束后再 `running=false`。停止后 Send 走队列，不与仍持锁的旧 run 并行。

13. **停止后迟到事件**（已修）  
    流式/工具回调在 cancel 后仍可能 `onLive`。  
    **修复**：`liveEpoch` 在 submit 递增；回调携带 epoch；`halted` 时丢弃 paint；defer 仅在 epoch 仍归属本 run 时收尾 UI。

14. **回归**  
    `assistant_stop_test.go`：停止释放 composer、流式停止 <1s、bash cancel <1s、限流等待中停止不二次尝试、迟到 live 丢弃、bash 工具使用 call ctx。



1. **MCP 进程级会话用 run ctx 初始化**  
   `sunvaleSession(ctx)` 在 `sync.Once` 里把**第一次**调用的 ctx 传给
   `Connect`。若首轮在握手中被取消，`mcpConnectErr` 会永久是
   `context.Canceled`，之后助手全部起不来。  
   **对照**：会话应与单次 run 生命周期解耦。  
   **修复**：`Connect` 使用 `context.Background()`。

5. **幻觉工具名直接打挂整轮**  
   `ToolsNodeConfig` 未设 `UnknownToolsHandler`；Eino 默认对未知工具返回
   error，整轮失败。官方文档明确该钩子用于 graceful handling。  
   **修复**：返回说明文字，让模型改用已有工具。

### P1 — 本轮修

6. **无 ModelRetryConfig**  
   瞬时网络错误 / 5xx / 空响应会立刻失败。eino v0.9 提供
   `ModelRetryConfig` + 流式 `WillRetryError`。限流 429 仍走现有换模型逻辑，
   **不要**在 ModelRetry 里重试 429（避免与 `runWithRateFallback` 叠加成风暴）。  
   **修复**：`MaxRetries=2`，仅 `TransientModelError`；流读取忽略
   `WillRetryError` 并清空失败尝试缓冲。

7. **并行工具调用共享 bind 状态**  
   默认并行执行；写类工具（move/copy/write/trash）共享 `sunvaleToolState`。  
   **修复**：`ExecuteSequentially: true`。

8. **HTTP 客户端无 ResponseHeaderTimeout**  
   注释故意不设整请求 `Timeout`（避免切断长流），但握手挂死时只能靠用户点停止；
   部分阶段对 ctx 响应不完整。  
   **修复**：克隆 Transport，设 `ResponseHeaderTimeout=60s`，仍不设 Client.Timeout。

9. **budgetFinishAsk 无截断**  
   预算用尽收尾时把本轮全部 tool/assistant 事件拼进一次 Generate，超长会话可能
   再次触发上下文错误。  
   **修复**：单条与整体 runes 上限。

### P2 — 差距（记录，不在本轮大改）

10. **未用官方 middleware**：`PatchToolCalls` / `Reduction` / `Summarization`。  
   当前历史只回放 user/assistant 文本（`ModelHistory` 丢弃 tool 角色且不带
   tool_calls），悬挂 tool_call 风险较低；超长 tool 结果已有 shell/读文件上限。
   若以后把完整 tool transcript 回灌模型，应加 PatchToolCalls + Reduction。

11. **未用 CheckPointStore / Interrupt-Resume / TurnLoop**。  
   `offer_choices` 用结束本轮 + 用户下一句的方式实现 HITL，能用但不是 ADK 标准
   Interrupt。大改成本高，列为后续。

12. **全局 `assistantRunMu`** 串行化所有窗口/A2A 运行——正确保证 bind 安全，但
   多窗口会互相排队；可接受。

13. **无 FailoverConfig**（跨供应商）；已有同供应商模型换路由，足够。

## 3. 对照最佳实践摘要

| 实践 | Eino / 业界 | 本仓库 |
| --- | --- | --- |
| 安全 GenModelInput / 指令模板 | 自定义或小心 FString | 已有 |
| Runner + Streaming | 推荐 | 已有 |
| ModelRetry（瞬时错误） | v0.9 推荐 | 本轮补 |
| UnknownToolsHandler | ToolsNodeConfig | 本轮补 |
| 工具失败回灌模型 | MCP IsError / ErrorAsError=false | 已有 |
| 可取消 ctx | WithCancel / CancelMode | 基本有；MCP init 本轮修 |
| 输出 / 工具预算 | Reduction middleware | 局部有；收尾截断本轮补 |
| Interrupt/Resume HITL | CheckPointStore | 未用（choices 自研） |
| 幂等写工具 | 应用层 | 部分（rename plan 确认） |

## 4. 本轮落地

见同分支提交（「助手：…」）：

- MCP 初始化与 run 取消解耦
- UnknownToolsHandler + 顺序执行工具
- ModelRetryConfig + 流式 WillRetry 处理
- HTTP ResponseHeaderTimeout
- budgetFinishAsk 截断
- 停止/取消链路（bash ctx、composer halted、迟到事件、1s 回归）
- 单测覆盖瞬时错误分类、未知工具、WillRetry 流、MCP 不继承 cancel、停止回归

## 5. 建议下一步

1. 评估 `offer_choices` 迁到 ADK Interrupt/Resume（需 CheckPointStore）。
2. 若回灌 tool 历史，接入 `patchtoolcalls` + `reduction`。
3. 可选：空回复的 output-based ShouldRetry（当前只按 err 重试）。
4. 「暂停后继续」：若需要，建议用 ADK Interrupt/Resume + CheckPointStore，而不是自研半暂停；当前 Stop 是硬取消，保留部分回复。
5. 真实模型 eval（`assistanteval`）加「幻觉工具名」「断网重试」用例。
