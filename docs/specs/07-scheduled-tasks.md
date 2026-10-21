# 07 — 定时任务（SCH1 + SCH2）

本机定时任务：设置页管理 + 进程内分钟节拍调度 + 助手 MCP 工具创建/改删/立刻跑。不依赖系统 cron。

## 持久化

- `{UserConfigDir}/sunvale/schedules.json`（非密钥）
- 运行日志：`sunvale/schedule-logs/<id>-YYYYMMDD.log`
- 内联脚本：`sunvale/scripts/`

## 调度

- `startScheduleRunner`（`main.go` WhenReady）：每分钟扫描 `Due`，入队后串行执行。
- 时间为本机本地时区；UI/ MCP 用 `HH:MM` + 每天/工作日/周末。
- 生成并存储 5 字段 cron 字符串，供显示；真正判定用 Hour/Minute/Weekdays。

## 动作

| action | 说明 |
| --- | --- |
| `file_rule` | 源目录非递归匹配（扩展名/glob）→ move 或 copy；`date_stamp` 时写入 `目标/YYYY-MM-DD/`（备份）。**只按扩展名/匹配，不做内容嗅探。** |
| `script` | `.py`/`.sh`/`.bat` 路径或内联脚本；超时可配 |
| `assistant` | 自然语言 prompt + 可选文件夹；无头调用 `runAssistantHeadless`，默认全托管。可选 `preset`（见下） |

## SCH2 — 助手预设与立刻执行

### 内置预设（`Job.Preset`）

| preset id | 界面名 | 行为 |
| --- | --- | --- |
| `classify_downloads` | 分类下载 | 列出下载，读文本/PDF 摘要，按主题移到目标根下「文档/图片/安装包/其他」 |
| `inbox_triage` | 收件箱整理 | 对指定文件夹读内容后归入主题子文件夹 |
| `rename_by_content` | 按内容重命名 | 读文档后用 `rename_plan` 给出有意义文件名 |

实现：`internal/schedule/presets.go`。设置页「助手执行」可选预设并编辑提示词；`present_form` / `schedule_create` 可传 `preset`。

按内容分类**仅**走助手预设（或自定义助手 prompt），不要在 `file_rule` 里假装嗅探内容。

### 立刻跑

| 工具 | 用途 |
| --- | --- |
| `schedule_run` | 立即运行**已保存**的定时任务（「现在跑」「立刻整理」且已有任务） |
| `assistant_task_run` | 一次性全托管助手任务，**不写入** `schedules.json`（「现在把下载按内容分类」） |

二者共用 `executeScheduledJob` / `runScheduleAssistant`。`assistant_task_run` 异步执行，避免嵌套占用 `assistantRunMu`。

### 斜杠命令

- `/分类`：填入按内容分类提示（不发送）
- `/立即执行`：运行最近跑过的定时任务；若无任务则打开设置 → 定时任务

## UI

设置 → **定时任务**：任务列表（启用、立即运行、编辑、删除）+ 表单（助手预设、提示词、目录、目标根）。托盘菜单「定时任务…」打开同一页。

截图：`docs/assets/shots/SCH1/`（SCH1）、`docs/assets/shots/SCH2/`（预设与斜杠）。

## 助手 MCP

`schedule_list` / `schedule_create` / `schedule_update` / `schedule_delete` / `schedule_run` / `assistant_task_run`，与设置页共用 `schedules.json`（oneshot 除外）。系统提示词与 `TestPromptListsAllTools` 已列入。

## 示例

### 每晚 7 点按扩展名移动下载里的文档

```text
schedule_create
  name=每晚整理下载文档
  time=19:00
  days=每天
  action=file_rule
  source_dir=~/Downloads
  dest_dir=~/Documents/来自下载
  match=.pdf,.docx,.doc,.xlsx,.pptx,.txt,.md
  op=move
```

### 现在按内容分类下载（不保存任务）

```text
assistant_task_run
  preset=classify_downloads
```

### 每晚用助手按内容分类（保存为定时任务）

```text
schedule_create
  name=每晚分类下载
  time=19:00
  days=每天
  action=assistant
  preset=classify_downloads
```

## 非目标（v1）

云同步、复杂 cron 表达式编辑 UI、完整 Python IDE、在 `file_rule` 内做内容分类。
