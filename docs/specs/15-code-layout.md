# 15 代码功能分布与可维护性布局

目标：把仍过大的主包文件按功能切开，能无 App/UI 依赖的逻辑继续下沉 `internal/`，并留下当前架构地图与下一步。本轮以**纯移动 / 薄封装**为主，不改用户可见行为；行为变更另提交。

与 [03 下一步重构](03-refactor-next.md) 的关系：R1–R6 已完成；本规格承接 **R7 启动**（远程文件按功能拆分 + 可下沉边界注释）以及设置页、壳层/占用图等与助手、并行冲突无关的拆分。完整「远程处理函数接口化并移入 `internal/remote`」仍记在 R7，本轮不强制做完。

## 总原则（沿用 03）

- 分支 `refactor/feature-layout`；每步独立提交，单次提交 < 1000 行。
- 子包不得反向引用主包（`AGENTS.md`）；新增 `internal/` 前确认不依赖 `App` / `ui`。
- 每步：`gofmt`、`go build ./...`、`go vet ./...`、`GOOS=windows` / `js,wasm`（改平台代码时加 darwin）构建、`go test -count=1 -timeout 900s ./...`。
- **刻意不做（并行冲突）**：不改 `assistant_*`、`internal/assistant`（留给 `fix/ai-stability`）。`archive*` / `bundle*` 在 macos-app-bundle 合入后可按需整理，但本轮优先 remote / settings / shell。

## 主包按功能的文件分组（约 291 个 `.go`，约 7.2 万行）

| 功能域 | 代表文件（主包） | 行数量级 | 说明 |
| --- | --- | --- | --- |
| 入口 / 状态 / 主题 | `main.go`、`state.go`、`statestore.go`、`theme.go`、`plugins.go`、`sys_*.go`、`tray.go`、`aifx.go` | ~6k | 接线、持久化、平台壳 |
| 资源管理器 UI | `files*.go`、`sidebar.go`、`chrome_tab.go`、`tabs.go`、`addressbar.go`、`preview*.go`、`gallery.go`、`thumb.go`、`docview.go`、`videoview.go`、`quicklook.go` 等 | ~10k | 列表/侧栏/标签/预览 |
| 助手 UI 与接线 | `assistant.go`、`assistant_run.go`、`assistant_http.go`、`assistant_tools.go`、`assistant_*paint*.go`、`assistant_opcard.go`、`assistant_mcp.go` 等 | ~16k | **本轮不碰**；逻辑多在 `internal/assistant` |
| 远程访问 | `remote.go`、`remote_manage*.go`、`remote_share.go`、`remote_preview.go`、`remote_storage.go`、`remote_fs.go`、`remote_admin.go`、`remote_archive.go`、`remote_*_test.go` | ~9k | 处理仍是 `(*App)`；协议已在 `internal/remote` |
| 搜索 UI | `searchbox.go`、`searchbox_panel.go`、`searchbox_accel.go`、`search.go`、`searchindex.go`、`searchpath.go`、`searchpreview.go` | ~4k | 引擎在 `internal/search` |
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
| `assistant.go` 等助手 UI | 800–1200+ | 按面板/运行/工具继续拆；逻辑下沉留给 AI 稳定性之后 | 不做 |
| `searchbox.go` | ~1290 | 已有 panel/accel；可再拆视图 vs 命中合并 | 低优 |
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

## 刻意不做

- 任何 `assistant_*`、`internal/assistant` 改动（并行 `fix/ai-stability`）。
- 用户可见行为变更、依赖升级、发版。
- force-push；不安装 post-commit hook。

## 验收

- 本规格入仓；`TASK.md` 有对应任务行。
- 至少若干真实结构提交（不仅文档）；测试全绿；分支 `refactor/feature-layout` 已推。
- 汇报：改了什么、行数变化、剩余（助手与可选归档整理）。
