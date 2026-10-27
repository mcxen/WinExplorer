# 15 代码功能分布与可维护性布局

目标：把仍过大的主包文件按功能切开，能无 App/UI 依赖的逻辑继续下沉 `internal/`，并留下当前架构地图与下一步。本轮以**纯移动 / 薄封装**为主，不改用户可见行为；行为变更另提交。

与 [03 下一步重构](03-refactor-next.md) 的关系：R1–R6 已完成；本规格承接 **R7 启动**（远程文件按功能拆分 + 可下沉边界注释）以及设置页、壳层/占用图等与助手、并行冲突无关的拆分。完整「远程处理函数接口化并移入 `internal/remote`」仍记在 R7，本轮不强制做完。

## 总原则（沿用 03）

- 分支 `refactor/feature-layout`；每步独立提交，单次提交 < 1000 行。
- 子包不得反向引用主包（`AGENTS.md`）；新增 `internal/` 前确认不依赖 `App` / `ui`。
- 每步：`gofmt`、`go build ./...`、`go vet ./...`、`GOOS=windows` / `js,wasm`（改平台代码时加 darwin）构建、`go test -count=1 -timeout 900s ./...`。
- **LAY2**：`fix/ai-stability` 合入后已拆助手与 searchbox 大文件（纯移动）。`archive*` 等仍可按需整理。
- **本轮范围**：remote 文件/测试拆分、settings、shellcore/usage；R7 完整接口化仍以后做。

## 主包按功能的文件分组（约 291 个 `.go`，约 7.2 万行）

| 功能域 | 代表文件（主包） | 行数量级 | 说明 |
| --- | --- | --- | --- |
| 入口 / 状态 / 主题 | `main.go`、`state.go`、`statestore.go`、`theme.go`、`plugins.go`、`sys_*.go`、`tray.go`、`aifx.go` | ~6k | 接线、持久化、平台壳 |
| 资源管理器 UI | `files*.go`、`sidebar.go`、`chrome_tab.go`、`tabs.go`、`addressbar.go`、`preview*.go`、`gallery.go`、`thumb.go`、`docview.go`、`videoview.go`、`quicklook.go` 等 | ~10k | 列表/侧栏/标签/预览 |
| 助手 UI 与接线 | `assistant.go`、`assistant_transcript.go`、`assistant_attach.go`、`assistant_run.go`、`assistant_run_stream.go`、`assistant_http.go`、`assistant_a2a.go`、`assistant_tools*.go`、`assistant_*paint*`/`widgets`、`assistant_opcard.go`、`assistant_mcp.go` 等 | ~16k | **LAY2 已按功能切开**；逻辑多在 `internal/assistant` |
| 远程访问 | `remote.go`、`remote_manage*.go`、`remote_share.go`、`remote_preview.go`、`remote_storage.go`、`remote_fs.go`、`remote_admin.go`、`remote_archive.go`、`remote_*_test.go` | ~9k | 处理仍是 `(*App)`；协议已在 `internal/remote` |
| 搜索 UI | `searchbox.go`、`searchbox_frame.go`、`searchbox_view.go`、`searchbox_panel.go`、`searchbox_accel.go`、`search.go`、`searchindex.go`、`searchpath.go`、`searchpreview.go` | ~4k | 引擎在 `internal/search`；LAY2 已拆 frame/view |
| 设置 | `settingsdialog.go`、`settings_assistant.go`、`settings_schedule.go`、`settings_websearch.go` | ~2k | 按分区继续拆文件 |
| 压缩包 | `archive*.go`、`bundle.go` | ~3k | macos-app-bundle 已合入；可选再拆 |
| 磁盘健康 | `diskhealth*.go` | ~1.5k | R4 已拆 |
| 表单 / 定时 / 任务 | `forms_*.go`、`schedule*.go`、`tasks_*.go`、`task_run.go` | ~2.8k | 核心在 `internal/forms|schedule|tasks` |
| 文件操作 | `ops.go`、`dropops.go`、`sizes.go`、`dups.go`、`batchrename.go`、`undo.go`、`fsops_glue.go` | ~3.6k | `internal/fsops` + 薄封装 |
| 壳层 / 占用图 | `shellcore.go`、`usage.go` | ~2k | 本轮可按命令/扫描 vs UI 切开 |
| 杂项 | `pathinput.go`、`extensions.go`、`openwith.go`、`home.go`、`navtree.go` 等 | ~11k | 按需再归类 |

## `internal/` 包职责

| 包 | 职责 | 不得依赖 |
| --- | --- | --- |
| `applog` | 诊断日志 | App/ui |
| `assistant` | OpenUI 解析、提示词、限流、记忆/配置数据、流读取等（R6） | App/ui；**本轮不改** |
| `fileview` | 助手读文件/看图数据路径 | App/ui |
| `forms` | 规范表单 schema | App/ui |
| `fsentry` | `Entry`（R2） | App/ui |
| `fsops` | 复制/取名/粘贴/回收站/undo 数据（R3） | App/ui |
| `netfs` | 网络文件系统辅助 | App/ui |
| `remote` | 协议信封、JWT、虚路径、网页令牌、driver；**处理函数仍在主包** | App/ui |
| `rename` | 批量/正则重命名算法 | App/ui |
| `schedule` | 定时任务模型与调度 | App/ui |
| `search` / `searchquery` | 搜索引擎与查询解析（R5） | App/ui |
| `tasks` | 统一任务系统模型 | App/ui |

## 仍过大的文件与建议拆分

| 文件 | 约行数 | 建议 | 本轮 |
| --- | --- | --- | --- |
| `assistant.go` 等助手 UI | 原 800–1200+ → 现大多 <600 | LAY2：按面板/对话/附件、运行/流式、A2A、工具、OpenUI 控件、记忆工具拆文件 | **LAY2 已做** |
| `searchbox.go` | 原 ~1290 → ~264 + frame/view | LAY2：类型/查询 vs 帧循环命中合并 vs 绘制按键 | **LAY2 已做**（view 仍略 >600） |
| `remote_manage.go` | ~840 | 重命名类 handler vs 批处理 move/copy/remove/put | 做 |
| `remote_share.go` | ~850 | 管理 CRUD vs 访客 FS/下载/访问计数 | 做 |
| `remote_preview.go` | ~700 | sniff/文本 vs Office vs 归档列表；纯预览构造可再下沉 `internal/remote` | 做文件拆；下沉可选 |
| `remote_storage.go` | ~690 | 挂载/driver 解析 vs HTTP CRUD | 做 |
| `remote_test.go` / `remote_manage_test.go` | 1000 / 1600 | 按 auth / fsapi / share / admin 拆测试文件 | 做（控提交行数） |
| `settingsdialog.go` | ~730 | 外观/视图/托盘 vs 扩展编辑 | 做 |
| `settings_assistant.go` / `settings_schedule.go` | ~585 / ~520 | 列表 vs 表单/预设；助手设置仅文件切开、不改 `internal/assistant` | 做 |
| `shellcore.go` | ~1016 | 读命令 vs 写/文本工具命令 | 做 |
| `usage.go` | ~804 | 扫描/布局数学 vs 对话框绘制 | 做 |
| `archive.go` | ~690 | 检测/列出 vs 解压流程（`bundle.go` 已独立） | 可选 |

## 与 R7 的关系

03 §7：HTTP 处理仍是 `(*App)` 方法；完整移入需 `Host` 一类接口（List/Read/…）。本轮：

1. **主包内按功能拆文件**（纯移动），降低单文件认知负荷。
2. 在拆出的文件顶部注明「R7 边界」：哪些是纯协议/路径/预览构造（可下沉），哪些绑定 `App`/state store（需接口化）。
3. 已明显无 App 依赖的纯函数（如部分 preview 构造、batch 规划）**能安全移则移**到 `internal/remote`；完整 handler 接口化留待争议小、助手并行结束后再做。

## LAY2 完成摘要（`refactor/assistant-layout`）

纯移动、不改行为。新增/切开：

| 原文件 | 约行数（前） | 拆后 |
| --- | --- | --- |
| `assistant.go` | ~1165 | ~580 + `assistant_transcript` (~432) + `assistant_attach` (~178) |
| `assistant_run.go` | ~923 | ~549 + `assistant_run_stream` (~392) |
| `assistant_http.go` | ~780 | ~215 + `assistant_a2a` (~574) |
| `assistant_tools.go` | ~763 | ~396 + `assistant_tools_more` (~384) |
| `assistant_openui_paint.go` | ~871 | ~413 + `assistant_openui_widgets` (~472) |
| `assistant_memory.go` | ~645 | ~508 + `assistant_memory_tools` (~148) |
| `searchbox.go` | ~1293 | ~264 + `searchbox_frame` (~424) + `searchbox_view` (~628) |

稳定性：`assistant_run_stream.go` 承接 stop / halted / liveEpoch；`assistant_guard_test.go` 仍 Walk 全树检查 `NewChatModelAgent` + `assistant.GenModelInput`。

## 刻意不做（LAY1 当时 / 仍适用）

- `internal/assistant` 再下沉（非本轮主目标；LAY2 以纯拆文件为主）。
- R7 完整 `Host` 接口化（仅文件拆分 + 边界注释）。
- 用户可见行为变更、依赖升级、发版。
- force-push；不安装 post-commit hook。

## 下一步

1. 可选：`archive.go`（~690）按检测/列出 vs 解压流程再拆；`searchbox_view.go`（~628）若需可再切过滤器条。
2. R7：为 remote handlers 设计 `Host` 接口并迁入 `internal/remote`（单独评审）。
3. 其它仍略大的主包文件（`forms_paint`、`sidebar`、`statestore`、`main` 等）按需整理。

## 验收

- 本规格入仓；`TASK.md` 有对应任务行（LAY1 / LAY2）。
- LAY1：真实结构提交；测试全绿；分支 `refactor/feature-layout` 已合入 main。
- LAY2：助手与 searchbox 拆分提交；测试全绿；分支 `refactor/assistant-layout`。
