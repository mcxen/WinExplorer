# 07 — 定时任务（SCH1）

本机定时任务：设置页管理 + 进程内分钟节拍调度 + 助手 MCP 工具创建/改删。不依赖系统 cron。

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
| `file_rule` | 源目录非递归匹配（扩展名/glob）→ move 或 copy；`date_stamp` 时写入 `目标/YYYY-MM-DD/`（备份） |
| `script` | `.py`/`.sh`/`.bat` 路径或内联脚本；超时可配 |
| `assistant` | 自然语言 prompt + 可选文件夹；无头调用 `runAssistantHeadless`，默认全托管 |

## UI

设置 → **定时任务**：任务列表（启用、立即运行、编辑、删除）+ 表单。托盘菜单「定时任务…」打开同一页。

## 助手 MCP

`schedule_list` / `schedule_create` / `schedule_update` / `schedule_delete` / `schedule_run`，与设置页共用 `schedules.json`。系统提示词与 `TestPromptListsAllTools` 已列入。

## 示例：每晚 7 点移动下载里的文档

设置页点「填入『每晚整理下载文档』示例」并保存；或让助手调用：

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

## 非目标（v1）

云同步、复杂 cron 表达式编辑 UI、完整 Python IDE。
