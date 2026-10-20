# TASK.md — 编码代理任务清单

本文件是**任务状态的唯一来源**。`docs/specs/README.md` 只做规格索引，不再维护状态。状态取值：待开发 / 进行中 / 已完成。开始任务时把状态改为“进行中”，合并时改为“已完成”，与代码同一提交或同一 PR。

## 项目目标

晴谷（Sunvale，模块 `sunvale`）是 Go + mygo v0.5.5（immediate-mode）自绘的 Windows 11 资源管理器风格文件管理器，跨 Windows / macOS / Linux / Web（wasm）。目标是在外观和交互上贴近 Windows 资源管理器，同时提供 Everything 式的全局搜索和右侧文件助手。

## 工作方式

1. 开工前阅读：`AGENTS.md`、`docs/UI.md`（界面与动效规范）、对应的 `docs/specs/*.md`。
2. **一个任务一个分支**，从最新 `origin/main` 拉出，分支名见任务表。不在一个分支里混做多个任务。
3. **单次提交少于 1000 行**（新增 + 删除，`git show --shortstat`）。需要更多时拆提交。
4. **永远不要 force-push**，不改写已推送历史。
5. **不要安装 post-commit hook**（不运行 `scripts/install-git-hooks.sh`），避免自动推送/发版。
6. 每次提交前必须全部通过：
   ```sh
   gofmt -l $(git diff --name-only --diff-filter=AM HEAD -- '*.go')   # 无输出
   go build ./... && go vet ./...
   GOOS=windows go build -o /tmp/sv.exe .
   GOOS=js GOARCH=wasm go build -o /tmp/sv.wasm .
   GOOS=darwin go build -o /tmp/sv-mac .        # 涉及平台代码时
   go test -count=1 ./...
   ```
   **已知的既有失败**：无。
7. **离屏截图**：UI 改动必须附截图。在包内临时建 `zz_shot_test.go`：
   ```go
   a := newStoreApp(t, dir)
   a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
   tt := ui.NewTester(a.view, 1200, 800)
   tt.Frame(); tt.Frame()
   f, _ := os.Create("/tmp/shots/<任务ID>/01-xxx.png"); png.Encode(f, tt.Image()); f.Close()
   ```
   参考 `/workspace/winexplorer-demo/search/current/searchshot_test.go.txt`。**截图测试不要提交**，看完删除；截图放 `docs/assets/shots/<任务ID>/`（每任务 ≤ 6 张，PNG）随 PR 提交。
8. 界面文字简体中文、不用 emoji，颜色只用 `theme.go` 令牌（见 `docs/UI.md` §1、§7）。
9. 提交信息格式：`<模块>：<做了什么>`（中文，与历史一致，如 `搜索：结果显示完整文件名与相对路径`）；文档提交 `docs: …`。PR 标题同首个提交，正文列出：任务 ID、改动文件、测试、截图路径、已知问题。
10. 用户可见行为变化需同步更新 `README.md` 对应段落；界面/动效数值变化同步更新 `docs/UI.md`。

## 任务表

| ID | 标题 | 优先级 | 状态 | 规格 | 依赖 | 分支 |
| --- | --- | --- | --- | --- | --- | --- |
| F6 | gofmt gifplay.go | 高 | 已完成 | 本文 | — | `chore/gofmt-gifplay` |
| F1 | 修复 TestDiskPageRenders | 高 | 已完成 | 本文 | — | `fix/test-diskpage` |
| F2 | 修复 TestAppDeclaredExtsBundle | 高 | 已完成 | 本文 | — | `fix/test-extsbundle` |
| F3 | 修复 TestBatchRenameDialogRowsAndDrag | 高 | 已完成 | 本文 | — | `fix/test-batchrename` |
| F4 | 修复 diskhealth smartKeys（Linux） | 高 | 已完成 | 本文 | — | `fix/test-smartkeys` |
| F5 | 消除 TestRemoteExtRun 偶发失败 | 中 | 已完成 | 本文 | — | `fix/test-remoteext` |
| R1 | 拆分 searchbox.go | 高 | 已完成 | [03 §3](docs/specs/03-refactor-next.md) | — | `refactor/searchbox-split` |
| S1 | 搜索：完整文件名 + 相对路径 + 两行行 | 高 | 已完成 | [01 阶段 1](docs/specs/01-global-search.md) | R1 | `feat/search-p1` |
| S2 | 搜索：路径分段可点击 | 中 | 已完成 | 01 阶段 2 | S1 | `feat/search-p2` |
| S3 | 搜索：相关度排序 | 高 | 已完成 | 01 阶段 3 | S1 | `feat/search-p3` |
| S4 | 搜索：匹配原因标签 | 中 | 已完成 | 01 阶段 4 | S3 | `feat/search-p4` |
| S5 | 搜索：筛选条 | 中 | 已完成 | 01 阶段 5 | S3 | `feat/search-p5` |
| S6 | 搜索：分组切换 | 低 | 已完成 | 01 阶段 6 | S3 | `feat/search-p6` |
| S7 | 搜索：右侧预览 | 中 | 已完成 | 01 阶段 7 | S1 | `feat/search-p7` |
| S8 | 搜索：页脚索引进度 | 低 | 已完成 | 01 阶段 8 | S1 | `feat/search-p8` |
| W1 | Windows 隐藏属性用于地址栏补全 | 中 | 已完成 | 本文 | — | `fix/win-hidden` |
| T1 | 侧栏目录树润色 | 中 | 已完成 | [02](docs/specs/02-tree-polish.md) | — | `feat/tree-polish` |
| TB1 | 拖动文件悬停标签自动切换 | 中 | 待开发 | [04 A](docs/specs/04-tabs-followups.md) | — | `feat/tab-drag-hover` |
| TB2 | 会话恢复后退/前进历史 | 中 | 待开发 | 04 B | — | `feat/session-history` |
| TB3 | 切换标签时保存进行中的重命名 | 高 | 已完成 | 04 C | — | `fix/rename-on-tab-switch` |
| A1 | 提示词与工具同步检查 + 真实模型回归 | 中 | 待开发 | [05 A、C6](docs/specs/05-assistant-followups.md) | — | `feat/assistant-eval` |
| A2 | OpenUI/Markdown 遗留修复 | 中 | 待开发 | 05 C1–C5 | — | `fix/assistant-render` |
| A3 | 回复中的目录树组件 | 低 | 待开发 | 05 B | A2 | `feat/assistant-tree` |
| R2 | Entry 移出 state.go | 中 | 待开发 | 03 §1 | — | `refactor/entry` |
| R3 | fsops 剩余：freeName/placeItem/absPath/undo 类型 | 中 | 待开发 | 03 §2 | — | `refactor/fsops-rest` |
| R4 | 拆分 diskhealth.go | 低 | 待开发 | 03 §5 | F1 | `refactor/diskhealth-split` |
| R5 | internal/search | 低 | 待开发 | 03 §4 | R2、S1–S8 全部 | `refactor/internal-search` |
| R6 | internal/assistant（6a–6d） | 低 | 待开发 | 03 §6 | R3、A1–A3 | `refactor/internal-assistant` |
| R7 | 远程处理函数移入 internal/remote | 低 | 待开发（以后） | 03 §7 | R5、R6 | — |
| — | 地址栏修复 | — | 已完成 | — | — | `fix/addressbar` |

建议顺序：F6 → F1–F4 → R1 → S1 → S3 → TB3 → 其余并行；R5 必须在 S1–S8 全部完成后。

---

## 任务详情

通用交付要求（每个任务都适用，下文不再重复）：代码 + 测试、通过工作方式第 6 条、UI 任务附截图到 `docs/assets/shots/<ID>/`、更新本表状态、必要时更新 `README.md` 与 `docs/UI.md`。

### F6 gofmt gifplay.go
- **目标**：`gofmt -l .` 无输出。
- **范围**：仅 `gofmt -w gifplay.go`，不改逻辑。
- **验收**：`gofmt -l .` 为空；`git diff --stat` 只有 `gifplay.go`。

### F1 修复 TestDiskPageRenders
- **目标**：该用例在 Linux CI（无 SMART、虚拟机）下稳定通过。
- **范围**：先定位原因（多半依赖真实磁盘探测或 smartctl）。修法优先“测试注入假的 `diskhealth.Report`”，不得让测试依赖硬件（`AGENTS.md`）。只有当失败暴露真实缺陷时才改 `diskhealth.go`。
- **交付**：提交信息写明根因。
- **验收**：`go test -count=5 -run TestDiskPageRenders .` 连续通过；`TestDiskPageRenders` 从已知失败列表删除（更新本文件与 `docs/specs/README.md`）。

### F2 修复 TestAppDeclaredExtsBundle
- **目标**：用例通过。
- **范围**：定位为测试数据过期（扩展声明与打包清单不一致）还是 `extensions.go` 缺陷；两者择一修，说明理由。
- **验收**：`go test -count=3 -run TestAppDeclaredExtsBundle .` 通过；已知失败列表删除该项。

### F3 修复 TestBatchRenameDialogRowsAndDrag
- **目标**：用例通过。
- **范围**：`batchrename.go` 与其测试；若因对话框宽度/行高在近期界面打磨（ac058d7）后变化，更新断言并在测试注释说明新布局依据。
- **验收**：`go test -count=3 -run TestBatchRenameDialogRowsAndDrag .` 通过；附一张批量重命名对话框截图。

### F4 修复 diskhealth smartKeys（Linux）
- **目标**：`go test ./diskhealth/` 在 Linux 通过。
- **范围**：`diskhealth/linux.go`、`smartctl.go` 与测试；不得要求真实 smartctl，用固定 JSON 夹具。
- **验收**：Linux 下 `go test -count=3 ./diskhealth/` 通过；Windows/macOS 交叉编译通过。

### F5 消除 TestRemoteExtRun 偶发失败
- **目标**：不再偶发失败。
- **范围**：`remote_ext_test.go:TestRemoteExtRun` 与相关 `remote_ext.go`。查找固定端口、sleep 计时、共享全局状态；用可等待的信号替代 sleep。
- **验收**：`go test -count=50 -run TestRemoteExtRun .` 全部通过；`-race` 下无数据竞争报告。

### R1 拆分 searchbox.go
- **目标**：S1 开工前把 1140 行的 `searchbox.go` 拆成面板/快捷键/主体三个文件，减少后续冲突。
- **范围**：纯移动，按 `docs/specs/03-refactor-next.md` §3 的函数清单；不改任何语句。
- **交付**：提交信息 `重构：searchbox.go 拆分为面板、快捷键与主体，代码原样移动`。
- **验收**：`git diff -M --stat` 显示仅移动；`git show --color-moved` 无非移动改动；全部测试与改前结果相同。

### S1 搜索：完整文件名 + 相对路径 + 两行行
- **目标/范围**：见 01 阶段 1（`searchpath.go:searchPathSegments`、`searchPathText`、`searchNameSpans`，`searchHitRow` 两行 44px，`files_details.go:searchLocation` 相对路径）。
- **交付**：`searchpath_test.go`；更新 `searchbox_test.go` 行距断言；截图：快速搜索结果 + 窗格搜索详细信息视图（使用 01 规格中的嵌套样例数据）。
- **验收**：
  - 样例中 `report.docx/.md/.txt/.pdf` 显示完整扩展名；
  - `~/Library/Caches/com.app/very/deep/nested/folder/structure` 显示为 `主文件夹 › Library › … › folder › structure`；
  - 悬停行显示完整路径；
  - 窗格搜索“位置”列显示相对搜索根的路径而非单个文件夹名；
  - 截图与 `docs/specs/assets/search-01-current-vs-improved.png` 右侧的行结构一致。

### S2 搜索：路径分段可点击
- **目标/范围**：01 阶段 2。
- **验收**：Tester 点击分段后 `a.cur().path` 为该段路径且面板关闭；点击分段不改变光标；省略段不可点击但有提示。

### S3 搜索：相关度排序
- **目标/范围**：01 阶段 3（`searchquery.Query.Score`，`sortHitsByScore`，相关度/名称切换）。
- **验收**：样例数据顺序前三为 `report-final.docx`、`report.md`、`report.txt`（或与规格权重一致的确定顺序，写进测试）；`Library/Caches` 下结果排在主文件夹普通结果之后；流式批次到达时已选中的结果不移位；切换“名称”恢复旧顺序。

### S4 搜索：匹配原因标签
- **验收**：名称命中显示“文件名”，只有路径命中显示“路径”且高亮落在第二行路径中；不出现“内容”标签。

### S5 搜索：筛选条
- **验收**：`dm:`、`size:`、`kind:` 修饰符有解析与匹配单测；点击“类型：文档”后计数显示 `x / y 个结果` 且 x 正确；切换筛选不重新遍历磁盘（测试中计数遍历次数）；“位置：当前文件夹”与本文件夹范围一致。

### S6 搜索：分组切换
- **验收**：按文件夹/按类型分组标题数与组内数量正确；↑↓ 跳过标题行；1 万条结果下切换分组一帧内完成（基准测试 < 16ms）。

### S7 搜索：右侧预览
- **验收**：面板 960 宽；窗口宽 < 900 时隐藏预览；选中第二条后预览显示其完整路径；“复制路径”与 Ctrl+C（查询框无选区时）写入剪贴板 `e.Path`；“打开所在位置”等同 Ctrl+Enter；排序依据列出 `Score` 返回的原因。截图对照 `docs/specs/assets/search-02-preview-filters.png`。

### S8 搜索：页脚索引进度
- **验收**：构建中页脚显示 `索引 n 项` 与进度条，`estimate` 为上次 `total`；首次构建为不定进度；数字有千分位；`ReduceMotion` 下不定进度条静止。

### W1 Windows 隐藏属性用于地址栏补全
- **目标**：地址栏补全（`pathinput.go:readFolderNames`）在 Windows 上把带 `FILE_ATTRIBUTE_HIDDEN` 的文件夹视为隐藏（目前只认点开头）。
- **范围**：新增平台函数 `sysHiddenName(dir string, e os.DirEntry) bool`：`sys_windows.go` 读属性（`syscall.Win32FileAttributeData` 或 `e.Info().Sys()`），其他平台返回点开头判断；`readFolderNames` 调用。评估文件列表（`fs.go:visibleEntries`）是否沿用同一函数——若沿用，单独提交。
- **交付**：Linux 下单测点开头规则；Windows 专用测试加 `//go:build windows`（用 `attrib +h` 或 `SetFileAttributes` 建隐藏目录）。
- **验收**：`GOOS=windows` 与 `GOOS=js` 编译通过；Windows 上隐藏的 `AppData` 在“不显示隐藏项目”时不出现在补全中，开启后出现。

### T1 侧栏目录树打磨
- **目标/范围**：02 规格 A（箭头 150ms 旋转）、B（悬停缩进引导线）、C（方向键导航）、D（平滑滚动，可选；mygo 不支持则在 PR 说明并跳过）。可拆为 4 个提交。
- **交付**：`navtree_keys_test.go`；截图：箭头旋转中间帧、引导线、焦点环；更新 `docs/UI.md` §6.2 → §6.1。
- **验收**：展开后 ≥200ms 箭头为 90°；`ReduceMotion` 下直接到终值；方向键行为符合规格表且不触发导航（Enter 才导航）；现有 `keynav_ui_test.go`、`navtree_test.go`、`navtree_jump_test.go` 全部通过。

### TB1 拖动文件悬停标签自动切换
- **验收**：纯函数 `tabHoverSwitch` 单测覆盖 <600ms 不切换、≥600ms 切换、当前标签不切换、离开清零；放下仍为移动到该标签文件夹。

### TB2 会话恢复后退/前进历史
- **验收**：`session_test.go` 往返测试恢复 `history/histIdx`；每标签最多 50 条；不存在的目录被剔除且 `histIdx` 仍指向当前路径；无新字段的旧会话文件正常加载。

### TB3 切换标签时保存进行中的重命名
- **验收**：`rename_ui_test.go`：开始重命名 → 输入 → 切换标签 → 文件已改名且可撤销；重名时给出与 Enter 相同的提示且切换不受阻；地址栏草稿仍被丢弃。

### A1 提示词与工具同步检查 + 真实模型回归
- **范围**：05 C6 `TestPromptListsAllTools`（默认运行）；05 A `assistant_eval_test.go`（`//go:build assistanteval`，只从环境变量读密钥）。
- **交付**：README 增加“助手回归”小节说明运行方法；PR 附一次运行汇总表（不含密钥）。
- **验收**：默认 `go test` 不访问网络；`TestPromptListsAllTools` 对 `assistant_mcp.go` 注册的每个工具名都通过；8 条评测用例在至少一个模型上通过，失败项附提示词修改。

### A2 OpenUI/Markdown 遗留修复
- **范围**：05 C1–C5。
- **验收**：流式占位到卡片无高度跳变（逐帧截图高度差 ≤ 2px）；表格长路径单元格单行中间省略并有提示；反引号中含空格的 Windows 路径整体可点击；深色主题截图一张。

### A3 回复中的目录树组件
- **验收**：`Tree/TreeItem` 解析与绘制测试；深度 > 6 或节点 > 200 截断并显示“…另有 n 项”；点击带路径节点在窗口打开；提示词已包含组件说明。

### R2 Entry 移出 state.go
- **验收**：新包仅含 `Entry`；主包 `type Entry = …` 别名；除 `state.go` 与新包外无文件改动；测试结果与改前相同。

### R3 fsops 剩余
- **验收**：`fsops.FreeName`、`fsops.PlaceItem`、`fsops.AbsFrom` 与 undo 数据类型有子包单测；主包行为不变（`assistant_tools_test.go`、撤销相关测试通过）。

### R4 拆分 diskhealth.go
- **验收**：纯移动（同 R1 的检查方法）；`TestDiskPageRenders`（F1 后）仍通过。

### R5 internal/search
- **前置**：S1–S8 全部“已完成”。
- **验收**：子包不 import 主包；`globalSearchRoots` 以参数传根；搜索相关测试全部通过，非 UI 用例移入子包；无行为变化（截图对比 S8 后的快速搜索一致）。

### R6 internal/assistant
- **验收**：按 6a–6d 四个分支或四个提交；每步测试通过；绘制与 `sunvaleTools` 留在主包。

### R7 远程处理函数
- 暂不执行；R5、R6 完成后由方案维护者补规格。

---

## 已完成（main）

| 内容 | 提交 |
| --- | --- |
| 助手处理文件的行内动效：呼吸光、改名高亮、滑出收起 | `914a6a3` |
| 界面打磨：标签中文名、平台根卷名、详细信息列自适应、空文件夹大小留空等 | `ac058d7` |
| 动效改为绘制时取状态、按帧重建，补窗口级测试 | `9fff3ce` |
| 导航树点击快速访问/最近位置不再跳动 | `f69b01d` |
| 重构第一轮：`internal/fsops`、`internal/remote`、`files.go` 按视图拆分 | `8c9cd2d`、`d724845`、`5b6e67a`（合并 `76aae4e`） |
| 助手 Markdown 流式渲染、OpenUI 卡片原位与一次作答锁定、系统提示更新 | `17482c0`、`af4d029`、`aa659f1`（合并 `dfa1ffb`） |
| 搜索后台批次按本次范围与查询比对 | `26fbad4` |
| 标签页：切换重读、关闭选邻近、重新打开、拖动排序、中键关闭、溢出滚动等 | `5dd344e`、`e01aa73` |
| 助手去掉访问路径限制 | `bf9ae80` |
| 地址栏：输入解析与补全、修复无法输入、面包屑菜单、行内错误 | `affef9a`、`d4bd849`、`e53b077` |
| 技术方案文档 `docs/specs/` | `6c23c7c` |
