# 03 下一步重构

前几轮已完成：`internal/fsops`（复制、取名、粘贴、回收站元数据；主包 `fsops_glue.go` 薄封装）、`internal/remote`（协议信封、JWT、虚拟路径、网页令牌；处理函数仍是 App 方法）、`files.go` 按视图拆分。以下是当时推迟的步骤。

**总原则**
- 每一步一个分支 `refactor/<名称>`，**代码原样移动**为主，不顺手改行为；行为变更另开提交。
- 每个提交 < 1000 行；纯移动的大文件拆成“新建文件 + 删除旧代码”两个提交也可以。
- 子包不得引用主包（`AGENTS.md`）。主包通过薄封装或类型别名过渡，别名在全部调用点迁移后删除。
- 每步结束 `go build ./... && go vet ./... && go test -count=1 ./...` 与四平台交叉编译通过。

## 顺序

| 步 | 内容 | 依赖 | 估计 |
| --- | --- | --- | --- |
| 1 | `Entry` 移出 `state.go` | 无 | 0.5 天 |
| 2 | fsops 剩余：`freeName`/`placeItem`/`absPath`/undo 类型 | 无 | 0.5 天 |
| 3 | 拆分 `searchbox.go` | 建议在 01 搜索阶段 1 之前做 | 0.5 天 |
| 4 | `internal/search` | 1、3 | 1.5 天 |
| 5 | 拆分 `diskhealth.go` | 无 | 0.5 天 |
| 6 | `internal/assistant` 抽取 | 2 | 2–3 天 |
| 7 | 远程处理函数（以后） | 4、6 | 待定 |

> 与 01 全局搜索的协调：第 3 步只是文件拆分，应在搜索阶段 1 开工前合并，避免大冲突；第 4 步在搜索 8 个阶段完成后再做（否则搜索开发期间接口反复变动）。

## 1. `Entry` 移出 `state.go`

- 新包 `internal/fsentry`（名称可议，避免与 `fs` 冲突），内容：`state.go:Entry`（含 `Dir`、`VolFree/VolTotal`）。
- 主包 `state.go` 保留 `type Entry = fsentry.Entry`（类型别名），因此 38 个使用文件无需改动。
- `Crumb`、`Place`、`ViewMode`、`SortKey` 留在主包（UI 概念）。
- 验收：`git diff --stat` 只有 `state.go` 与新包；所有测试通过。

## 2. fsops 剩余部分

当前在 `assistant_fm.go`：`absPath(p string) (string, error)`（314 行）、`freeName(dir, name string, isDir bool)`（359 行）、`placeItem(src, dst string, move bool) error`（368 行）；在 `undo.go`：`type undoMove`、`type undoOp`。

- `freeName` → `fsops.FreeName`；`placeItem` → `fsops.PlaceItem`（跨卷移动回退到复制+删除的逻辑一起移）；`absPath` 依赖“当前文件夹”时保留在主包，只把纯函数部分（`fsops.AbsFrom(base, p string)`）移出。
- undo：`undoMove{From, To string}` 与 `undoOp` 的数据部分移到 `fsops`（导出字段），`pushUndo/doUndo` 仍是 App 方法（涉及 toast 和刷新）。
- 在 `fsops_glue.go` 增加薄封装，调用点不变；之后逐步替换为直接调用并删除封装。
- 测试：`internal/fsops` 增加 `FreeName`、`PlaceItem`（临时目录）单测，可从 `assistant_tools_test.go` 迁移相应断言。

## 3. 拆分 `searchbox.go`（1140 行）

纯移动，不改代码：
- `searchbox_panel.go`：面板窗口与热键 —— `toggleSearchBox`、`searchHostApp`、`appByWindow`、`registerSearchHotkey`、`applySearchHotkey`、`setSearchHotkey`、`searchShortcutDown`、`openSearchBox`、`searchBoxClose`、`showExplorer`、`focusSearchPanel`、`ensureSearchPanel`、`placeSearchPanel`、`searchPanelView`、`searchBoxCancel`。
- `searchbox_accel.go`：快捷键文本 —— `accelFromKey`、`parseSearchAccel`、`shortcutLabel`、`accelKeyToken`、`accelTokenKey`。
- `searchbox.go` 保留：状态类型、`searchBoxFrame`、命中合并与光标、视图（`searchBoxModal`、`searchBoxBody`、`searchBoxNotice`、`searchHitRow`、`searchBoxStatus`）、`searchBoxKeys`、范围换算。

## 4. `internal/search`

前提：第 1 步完成（子包能引用 `Entry`）。
- 移入：`search.go` 的 `streamMatches`、`walkRoot`、`searchPruneDir`、`walkMatches`、`searchEntry`、`frameQueue`（若不依赖 mygo，可移；否则留主包）；`searchindex.go` 的索引结构、构建、查询、持久化（`walkSearchIndex`、`indexWalkRoot`、`searchIndexQuery`、`underRoot`、`loadSearchIndex`、`saveSearchIndex` 等）。
- 留主包：所有 `(*App)` 方法（`updateSearch`、`searchRoots`、`activeSearchQuery`、`searchStatusLabel`）、`globalSearchRoots`（依赖 `sysVolumes`，改为把根列表作为参数传入子包）、UI 文案。
- 子包 API 草案：
  ```go
  package search
  type Options struct{ Hidden bool; Deadline time.Time; Max int }
  func Stream(roots []string, q searchquery.Query, o Options, emit func(batch []fsentry.Entry, more bool))
  type Index struct{ /* 私有 */ }
  func (ix *Index) Query(roots []string, q searchquery.Query, hidden bool) ([]fsentry.Entry, bool)
  func (ix *Index) Status() Status
  ```
- 全局变量 `searchIndex` 改为主包持有的 `*search.Index`。
- `search_stream_test.go`、`searchindex_test.go` 中不涉及 UI 的用例随之移入子包。

## 5. 拆分 `diskhealth.go`（1180 行）

纯移动：
- `diskhealth_page.go`：插件注册与页面骨架 `registerDiskPlugin`…`diskRefreshButton`、`disksSummary` 及 summary 系列。
- `diskhealth_card.go`：`diskCard`、`diskIdentityColumn`、`diskScoreRing`/`drawScoreRing`、gauge 系列、`diskChecksColumn`、`diskFactsRow`。
- `diskhealth_chart.go`：`diskChart`、`diskChartDetail`、`diskChartState`、`diskChartData`、`liveRateLabel`、`diskVolumeComposition`、`volumeGroups`、`volumePalette`、`volumeChartState`。
- `diskhealth.go` 保留：空态、提示、tone/徽章、`diskReport` 与格式化函数。
- 注意：`TestDiskPageRenders` 与 Linux `smartKeys` 为既有失败，拆分后失败情况应保持一致（不多不少）。

## 6. `internal/assistant`

范围最大，单独评审。边界：
- **移入**（与 UI 无关）：OpenUI 解析（`assistant_openui.go` 的 `splitOpenUI`…`openTextStyle`，`ouiExpr/ouiView/ouiCol` 导出）、Markdown 路径链接化（`assistant_markdown.go:linkPath`、`linkMarkdownPaths`、`linkLinePaths`、`linkBarePaths`、`pathURL`、`escapeMarkdown`、`normalizeOpenMarkers`、`winAbs`）、提示词（`assistant_prompt.go`）、限流（`assistant_ratelimit.go`）、记忆存储（`assistant_memory.go` 中非 UI 部分）、模型配置（`assistant_config.go`、`assistant_models.go` 的数据部分）、流读取（`assistant_run.go:readMessageStream`、`streamLive`、`streamShown`、`reasoningLabel`、`modelHistory`）。
- **留主包**：所有绘制（`*_paint.go`、`assistant.go` 的面板）、`sunvaleTools`（依赖 App 状态；`assistant_tools.go`、`assistant_fm.go`、`assistant_mcp.go`）、`runAssistant` 与 `publishAssist`。
- 子步骤：6a OpenUI 解析；6b Markdown 工具函数；6c 提示词与限流；6d 记忆与配置。每个子步骤独立提交并通过测试。

## 7. 远程处理函数（以后再做）

`remote_*.go` 中的 HTTP 处理函数仍是 `(*App)` 方法。待第 4、6 步完成后，评估将处理函数改为依赖接口（`type Host interface { List(path string) ([]fsentry.Entry, error); ... }`）后移入 `internal/remote`。

文件级拆分与边界注释见 [15 代码功能分布](15-code-layout.md)（任务 LAY1 / R7 启动）；完整接口化仍待单独评审。
