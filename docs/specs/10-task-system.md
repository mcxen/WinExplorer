# 10 — 统一任务系统（TASKSYS，设计稿）

状态：**设计**，未实现。实现按下方阶段拆分，每阶段一个提交（< 1000 行）。

## 1. 现状盘点

| 机制 | 位置 | 线程 | 进度 | 取消 | 可见性 |
|---|---|---|---|---|---|
| 压缩 / 解压任务 | `archive_progress.go`（ARCHIVE-PROG） | 后台 goroutine | 字节、当前成员、速度、剩余时间 | 有（reader 检查） | 状态栏条、助手任务卡 |
| 嵌套连解 / 密码重试 | `archive_nested.go`、`archive_password.go` | 同上 | 只算外层 | 每层检查 | 同上 |
| 粘贴 / 拖放 复制移动 | `ops.go:doPaste`、`dropops.go`、`internal/fsops/copy.go` | **UI 线程同步** | 无 | 无 | 完成后状态栏文字 |
| 删除 / 回收站 / 永久删除 / 清空 | `ops.go:doDelete`、`trash.go`、`deleteerr.go` | UI 线程同步 | 无 | 无 | 状态栏文字 |
| 批量重命名 | `batchrename.go:commitBatchRename`、`renamebridge.go` | UI 线程同步 | 无 | 无 | 对话框 |
| 撤销 | `undo.go`（创建 / 移动两种形状） | 同步 | — | — | Ctrl+Z |
| 插件后台任务 | `plugintask.go:taskRunner[T]`（单任务串行，后到替换排队） | goroutine | 插件自报文字 | 无 | 插件页状态行 |
| 文件夹大小 | `sizes.go`（4 个 walker，进程级队列） | goroutine | 无 | 无 | 大小列 |
| 缩略图 / PDF 缩略图 | `thumb.go`、`pdfthumb.go`（有界并发、LRU） | goroutine | 无 | 无 | 平铺即时 |
| 搜索索引 | `searchindex.go` | goroutine | 索引状态 | — | 搜索框状态 |
| 助手对话运行 / 排队 | `assistant_run.go`（`assistantRunMu`、排队消息） | goroutine | 活动行 | 停止 | 助手面板 |
| 助手一次性任务 / 定时任务 | `assistant_task_run.go`、`schedule_run.go`、`internal/schedule` | goroutine / 定时器 | 结束状态 + 详情 | 脚本超时 | 设置 → 定时任务记录 |
| 助手文件工具 | `assistant_mcp.go`（zip / unzip / move / copy / trash / rename） | MCP goroutine | 仅 zip/unzip 有进度 | 仅 zip/unzip | 行呼吸灯 `aifx.go`、活动行 |
| 行级反馈 | `aifx.go`（busy / leaving / renamed） | 共享 store | — | — | 文件行、状态栏「处理中 N 项」 |
| 远程存储 / 分享 / 远程归档 | `remote_*.go`、`internal/remote` | HTTP 服务 goroutine | 无 | 连接断开 | 无 |

结论：只有归档有真正的进度；复制、移动、删除、重命名仍在 UI 线程同步执行，大批量时界面卡住；每处各自报告状态，没有统一的查看、取消、重试和历史。

## 2. 目标与非目标

- 目标：用户操作、助手、定时任务发起的所有**用户可感知的长操作**走同一套 Task；统一进度、取消、暂停、重试、冲突处理、历史和界面。
- 非目标：缩略图、文件夹大小、搜索索引这类**隐式缓存工作**不进任务列表（只在 Manager 中共享 IO 并发配额，可选在「历史」里不出现）；远程 HTTP 请求不建任务，远程侧发起的复制 / 归档才建。

## 3. 包结构 `internal/tasks`

```
internal/tasks/
  task.go       Task、State、Progress、Item、Step、Result
  manager.go    Manager：排队、调度、并发槽、优先级、控制
  reporter.go   Reporter：操作代码唯一依赖的进度 API
  conflict.go   冲突策略与询问通道
  history.go    历史持久化（tasks.json，最近 500 条 / 30 天）
  events.go     订阅、节流
  ops/          copy.go move.go delete.go rename.go archive.go（把现有实现包成 Runner）
```

`internal/tasks` 不依赖 `ui`；主包的 `tasks_ui.go`、`tasks_pane.go` 负责绘制。

### 3.1 模型

```go
type Kind string    // copy move delete trash restore rename compress extract script assistant remote
type Source string  // user ai schedule remote
type IOClass string // disk-local disk-remote cpu net

type State int
const (Queued State = iota; Running; Paused; Done; Failed; Canceled; Partial) // Partial = 完成但有失败项

type Task struct {
    ID       string    // 时间序 ULID
    Kind     Kind
    Title    string    // 「解压 照片备份-2026.zip」
    Source   Source
    Origin   string    // 助手会话 id / 定时任务 id，可空
    Priority int       // 0 普通，1 用户前台，-1 后台
    IO       IOClass
    Created, Started, Ended time.Time
    State    State
    Progress Progress
    Steps    []Step    // 可选子步骤：扫描 → 复制 → 校验
    Items    []ItemErr // 只存失败 / 跳过项，上限 200，其余计数
    Result   Result
}

type Progress struct {
    Unit        Unit   // bytes | items | none（不确定）
    Done, Total int64
    ItemsDone, ItemsTotal int  // 字节任务也带项数
    Current     string
    Speed       float64        // 单位/秒，滑动窗口 3s
    ETA         time.Duration
}

type Step struct{ Name string; State State }
type ItemErr struct{ Path, Op, Err string; Skipped bool }
type Result struct {
    Created, Moved [][2]string // 供撤销（复用 undo.go 的两种形状）
    Summary        string      // 「已解压 3 个归档」
    Undo           *UndoInfo
}
```

状态机：`Queued → Running ⇄ Paused → {Done | Partial | Failed | Canceled}`；`Failed/Partial/Canceled → Queued`（重试，只重试失败项或从头，由 Runner 声明 `Resumable`）。非法转移返回错误。

### 3.2 Reporter（操作侧 API）

```go
type Reporter interface {
    Context() context.Context        // 取消 = ctx.Done
    SetTotal(unit Unit, total int64, items int)
    Add(n int64)                     // 字节或项
    ItemDone(path string)
    Current(name string)
    Step(i int, s State)
    Fail(path, op string, err error) // 记失败项，继续
    Skip(path, reason string)
    Wait() error                     // 暂停点：暂停时阻塞，取消时返回 ctx.Err()
    Conflict(c Conflict) Resolution  // 见 3.4
    Reader(r io.Reader) io.Reader    // 计数 + 暂停 + 取消
    Writer(w io.Writer) io.Writer
}
type Runner func(r Reporter) (Result, error)
```

现有代码只改「拿到 Reporter 并在循环里调用」：`fsops.CopyFile` 增加 `io.Reader` 包装点；`archive_progress.go` 的 `archiveTask` 变成 Reporter 的适配器后删除。

### 3.3 Manager

- `Submit(spec Spec, run Runner) *Task`；`Get(id)`、`List(filter)`、`Pause/Resume/Cancel/Retry(id)`、`PauseAll/ResumeAll`、`ClearFinished()`。
- **并发槽**：按 IOClass：`disk-local` 每个卷 2 个（同盘复制串行更快）、`disk-remote` 2、`cpu` = `GOMAXPROCS/2`、`net` 4；按 Kind 额外上限：`assistant` 1（沿用 `assistantRunMu`）、`script` 2。缩略图 / 文件夹大小通过 `Manager.Slot(IOClass)` 借用同一配额但不建 Task。
- **优先级**：用户前台（1）> 普通（0）> 后台 / 定时（-1）；同级 FIFO。用户可在行菜单「置顶」。
- **路径锁**：同一路径树上的写任务互斥（移动 A 与删除 A/b 不能并行），读任务共享；冲突时后者排队并显示「等待：复制 214 项」。
- **事件**：`Subscribe(func(Event)) (cancel func())`；Event = `{TaskID, Kind: added|state|progress|removed}`。进度事件节流到每任务 ≥ 150ms 一次，并合并为一次窗口 Invalidate。UI 读快照（`Snapshot()`），不持锁绘制。
- **持久化**：结束的任务写 `tasks.json`（与 `state.json` 同目录），上限 500 条 / 30 天；运行中的任务在退出时标记为 `Canceled（退出）`，可在历史中重试。退出时有运行中任务：沿用托盘逻辑，关窗不退出；真正退出时一行确认（窗口内横幅，不是弹窗）。

### 3.4 冲突策略（复制 / 移动 / 重命名 / 解压）

- `Spec.OnConflict`：`Ask`（默认，用户来源）、`KeepBoth`（「- 副本」，助手与定时默认）、`Replace`、`Skip`、`ReplaceIfNewer`。
- `Ask` 时任务进入 `Paused(等待选择)`，任务行内出现一行选项「替换 / 跳过 / 保留两者 / 对其余 N 项相同」——**在任务面板内联**，不弹窗；状态栏指示变为提醒色。
- 重命名批次先整体预检（`batchHasClash` 已有），冲突在提交前解决；执行中出现的外部冲突按 `Skip` 记失败项。

### 3.5 撤销

`Result.Created/Moved` 直接生成 `undoOp`，任务完成时 `pushUndo`；历史中的任务行有「撤销」直到它被更晚的操作覆盖。

## 4. 助手集成

- MCP 工具：
  - `task_list(state?, kind?, limit?)` → 精简 JSON（id、title、state、percent、current、失败数）。
  - `task_get(id)` → 含失败项。
  - `task_control(id, action: pause|resume|cancel|retry)`；助手只能控制 `Source=ai` 的任务，控制用户任务需确认模式下用户同意。
  - 现有 `zip_files/unzip/copy_items/move_items/trash_items/rename_item` 改为 `Submit` 后**等待**完成（默认）或 `async: true` 立即返回 task id。
- 对话内展示：当前已有的实时任务卡改为订阅 Manager，按 `Origin = 会话 id` 过滤，样式跟随「进度样式」设置（条形 / 仪表盘）。OpenUI `TaskProgress` 增加 `task: "<id>"` 参数，绑定实时任务（无 id 时仍为静态）。
- 定时任务：`schedule_run.go` 的每次执行建 `Source=schedule` 的 Task，运行记录与任务历史合并显示。

## 5. 界面位置

约束：Explorer 式布局、高信息密度、**不要浮层弹出**（不做 Windows 那种浮动复制对话框，也不做右下角 toast 堆）。

### 方案 A（推荐）：状态栏指示 + 底部停靠「任务」面板

- 状态栏右侧常驻紧凑指示：细条 + 「3 个任务 · 55% · 10.4 MB/s · 剩余 1:40」+ 失败数红点 + 展开箭头；无任务时不占位。
- 点击指示或快捷键 `Ctrl+Shift+J` 在文件区与状态栏之间展开**底部停靠面板**（像 VS Code 面板 / 资源管理器详细信息窗格的横向版），高度可拖（默认 6 行 ≈ 240 DIP，记忆），展开 / 收起高度过渡 180ms ease-out，不覆盖内容而是挤压文件区。
- 面板头：分段「进行中 / 排队 / 失败 / 历史」带计数，右侧总吞吐、全部暂停、清除已完成、收起。
- 每任务一行 34 DIP：图标（状态色）、标题、来源标签（我 / AI / 定时）、110 DIP 细条（或设置为仪表盘时行首 28 DIP 迷你环）、数字行、当前项 / 错误、暂停、取消；点行展开失败项列表与「重试失败项 / 复制日志 / 打开目标 / 撤销」。
- 双行宽：横向空间充足，一行能放下全部数字，最符合高密度；与右侧助手面板互不争抢，两者可同时开。
- 完成通知：只在状态栏指示上闪一次（成功绿 / 失败红，600ms）并更新计数；窗口不在前台时才发系统通知。

截图：`docs/assets/shots/TASKSYS/01-option-a-bottom-dock.png`、`03-failed-detail-and-status.png`。

### 方案 B：右侧停靠区「助手 | 任务」页签

- 复用助手停靠面板的位置、宽度拖拽和全屏，顶部页签切换「助手 / 任务 N」。
- 优点：不新增停靠区，代码复用多；与助手发起的任务天然同处。
- 缺点：宽度 360–440 时一行放不下数字行与当前项，只能两行或截断（见截图）；与助手对话互斥，边看对话边看任务需来回切换。

截图：`docs/assets/shots/TASKSYS/02-option-b-side-tab.png`。

**推荐 A**：信息密度高、不与助手抢位置、与 Explorer「详细信息窗格」心智一致；B 作为窄窗口（宽 < 900）时的自动回退：底部面板在窗口过矮（< 560）时改为右侧页签。

## 6. 迁移计划

| 阶段 | 内容 | 验收 |
|---|---|---|
| T1 内核 | `internal/tasks`：模型、状态机、Manager（槽、优先级、路径锁）、Reporter、事件节流；纯 Go 单测 | 状态机非法转移、槽上限、优先级、路径锁互斥、暂停阻塞 / 取消返回、节流 ≤ 1 次 / 150ms 的单测；`go test -race` 通过 |
| T2 归档迁移 | `archive_progress.go` 改为 Reporter 适配；状态栏指示改读 Manager；助手任务卡订阅 Manager | ARCHIVE-PROG / PROG-STYLE 现有测试全部保留通过；暂停 / 继续 zip 解压 |
| T3 复制 / 移动 / 删除后台化 | `doPaste`、拖放、`doDelete`、回收站操作改为 Task；`fsops` 增加 Reader 包装与逐项回调；冲突策略 Ask 内联 | 1 万个小文件复制期间 UI 帧时间 < 16ms；冲突内联选择；失败项记录；撤销可用 |
| T4 任务面板 | 方案 A 底部停靠面板、状态栏指示、历史持久化、失败详情、重试 | 无头截图；面板高度记忆；历史重启后仍在；不出现任何浮层 |
| T5 重命名与助手工具 | 批量重命名、助手文件工具、`task_list/get/control`、`TaskProgress(task:)` | 助手可查询 / 取消自己发起的任务；控制用户任务需确认 |
| T6 定时与插件 | `schedule_run` 建 Task；`plugintask.taskRunner` 改为 Manager 的薄封装；缩略图 / 大小借用 IO 槽 | 定时任务记录在任务历史可见；插件页状态不回退 |
| T7 远程 | 远程侧发起的复制 / 归档建 `Source=remote` 任务 | 远程管理页可见同一任务 |

## 7. 待定问题

1. 推荐方案 A，是否同意？窄窗口回退到右侧页签是否需要？
2. 冲突默认：用户操作 `Ask`、助手 / 定时 `KeepBoth`，是否合适？
3. 历史保留 500 条 / 30 天够不够？是否需要「完成后自动清除」开关？
4. 关闭窗口时有运行任务：留托盘继续，还是窗口内横幅确认后取消？
5. 同盘复制串行（每卷 2 槽）是否接受，还是需要设置项？
6. 系统通知：仅窗口在后台时发，是否需要可关闭？
