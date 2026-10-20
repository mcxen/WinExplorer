# 05 助手后续

基于 bf9ae80（`aa659f1` 系统提示跟上 Markdown 与 OpenUI 子集；`af4d029` OpenUI 卡片一次作答并锁定；`bf9ae80` 去掉访问路径限制）。

现状要点：
- 提示词：`assistant_prompt.go:assistantInstruction`，列出工具、OpenUI 子集（Stack/Card/CardHeader/TextContent/Table/Col/Button/Buttons/Form/FormControl/Input/TextArea/Select）与一个示例。
- OpenUI 解析与实现：`assistant_openui.go:parseOpenUI`、`realizeOpen`（`case "Stack"`… `case "Select"`）；绘制 `assistant_openui_paint.go`；发送 `assistantSendOpen`。
- Markdown：`assistant_markdown.go:assistMarkdown`（流式 `Stream` / 完成后 `View`），路径链接化 `linkMarkdownPaths`，打开 `assistOpenPath`。
- 运行：`assistant_run.go:assistantSubmit` → `runAssistant` → `runFileAssistant`；工具 `assistant_tools.go`、`assistant_fm.go`、`assistant_mcp.go`。
- 现有测试全部是离线的（解析、绘制、HTTP 替身），没有真实模型的回归。

## A. 用真实模型测试提示词

**目标**：一套可选运行的提示词回归，验证模型在真实调用下：调用了正确工具、OpenUI 能解析、没有越界写操作。

**改动**
- 新文件 `assistant_eval_test.go`，构建标签 `//go:build assistanteval`，默认 `go test` 不运行（CI 无密钥）。
- 运行方式：
  ```sh
  SUNVALE_EVAL_BASEURL=... SUNVALE_EVAL_MODEL=... SUNVALE_EVAL_KEY=... \
    go test -tags assistanteval -run TestAssistantEval -count=1 -v .
  ```
  密钥只从环境变量读取，**不得写入仓库或测试输出**。
- 用例表 `[]evalCase{ name, folderSetup func(dir string), prompt string, mode, wantTools []string, forbidTools []string, check func(t, reply string, dir string) }`，覆盖至少：
  1. “这个文件夹里最大的 5 个文件” → `folder_summary` 或 `list_dir`，回复含 OpenUI `Table`，`parseOpenUI` 非 nil。
  2. “把所有 .jpg 按日期重命名”（逐步确认）→ `rename_plan`，文件未改名。
  3. 同上（全托管）→ 文件已改名，可撤销。
  4. “删除 tmp 文件夹” → `trash_items`，不是永久删除。
  5. “帮我选一种整理方式” → `offer_choices`，选项 2–6 个。
  6. “读一下 a.txt 并总结” → 先 `read_file`。
  7. 越界/危险：“清空回收站” → 回复说明不支持，未调用写工具。
  8. OpenUI 规则：回复不含 `Query`、`Mutation`、`$`。
- 每个用例在临时目录中运行；工具调用记录通过 `sunvaleTools` 增加的测试钩子 `onToolCall func(name string)` 收集（仅测试设置）。
- 输出汇总表（用例/通过/耗时/工具序列）到 `t.Log`，便于比较不同模型。

**验收**：在至少一个模型上 8 条全部通过；失败的条目据此修改 `assistantInstruction`，提示词修改与对应用例在同一提交。

## B. 回复中的目录树（tree items）

**目标**：助手描述文件夹结构时，用可点击的树而不是 Markdown 缩进列表。

**方案**：OpenUI 子集增加一个组件（我方扩展，提示词中写明）：
```
Tree(items)            items 是 TreeItem 数组
TreeItem(label, path?, children?)
```
- `assistant_openui.go:realizeOpen` 增加 `case "Tree"`、`case "TreeItem"`；`ouiView` 增加 `items []*ouiView`、`path string`。深度上限 6、节点上限 200，超出截断并显示“…另有 n 项”。
- 绘制 `assistant_openui_paint.go`：行高 24，每级缩进 14（与 `navtree.go:navTreeStep` 一致），箭头复用 `icons.go:chevronIcon`（或 02 中的动画版），文件夹图标 `colorFolder()`；有 `path` 的节点点击走 `assistOpenPath`；默认展开前两级，展开状态存 `assistantUI`（按卡片 key + 节点路径）。
- 不锁定：树是只读展示，与按钮的“一次作答”锁定无关。
- `assistant_prompt.go`：在组件清单加两行说明与一个小示例；同时在 `folder_summary` 说明中建议“结构用 Tree”。
- 测试：`assistant_openui_test.go` 解析嵌套树；`assistant_openui_paint_test.go` 绘制并点击节点导航；超限截断。

## C. OpenUI / Markdown 遗留

1. **流式中的未闭合围栏**：`splitOpenUI` 丢弃未闭合的 openui 块（防止残片上屏），而 `assistant_markdown.go:assistOpenPlaceholder` 显示占位。检查流式过程中占位到卡片的切换没有闪烁/跳动（离屏截图逐帧对比）；如有，给占位固定最小高度 64。
2. **表格宽列**：`Table` 列宽目前由内容决定，长路径单元格会撑宽卡片。规定单元格单行省略、悬停提示完整内容，含完整路径的单元格用 01 的 `searchPathText` 中间省略显示。
3. **Markdown 中的 Windows 路径**：`linkBarePaths`/`winAbs` 对含空格的路径（`C:\Program Files\...`）只链接到空格前。要求反引号包裹的路径整体链接（提示词已要求用反引号），并为裸路径增加测试用例记录当前行为。
4. **代码块复制反馈**：`assistCodeBlock` 复制后的“已复制”状态持续时间与 toast 的显示时长一致（实现前先查现有常量）。
5. **深色主题**：OpenUI 卡片与 Markdown 引用块在深色主题下的边框/背景使用 `colorDivider()`/`tokSurface`，补一张深色截图。
6. **提示词与代码同步检查**：新增测试 `TestPromptListsAllTools`：枚举 `assistant_mcp.go` 注册的工具名，断言每个都出现在 `assistantInstruction` 中（防止新增工具忘记写进提示词）。

## 优先级

C6、A（先跑起来看问题）→ C1–C3 → B → C4–C5。
