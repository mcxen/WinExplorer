# 08 — 规范表单组件（FORM1）

设置「定时任务」与助手（OpenUI / `present_form`）共用同一套声明式表单模型，避免设置页手写一排 TextBox、助手侧又另起一套控件。

## 共享模型

包：`internal/forms`

| 字段 kind | 控件 | 说明 |
| --- | --- | --- |
| `text` | TextBox | 单行 |
| `textarea` | TextBox multiline | 多行 |
| `password` | TextBox + Password | 密文 |
| `number` | TextBox | 数字（超时等） |
| `path` | TextBox + 浏览 | `path_mode`: folder / file，调 `mygo.Dialog.Open` |
| `select` | ui.Select | 下拉 |
| `multi_select` | 开关组 | 多选，值用逗号拼接 |
| `switch` | ui.Switch | 布尔，存 `"true"` / `"false"` |
| `time` | TextBox | HH:MM |
| `days` | ui.Segmented | 默认每天 / 工作日 / 周末 |
| `segmented` | ui.Segmented | 互斥分段（移动 / 复制） |

`Form`：`id`、`title`、`description`、`fields[]`、`submit_actions[]`。校验提示：`required`、`pattern`、`placeholder`。

## 绘制一次

- Settings「添加任务」：`forms.ScheduleAdd` → `paintFormField`（`forms_paint.go`）
- 助手：扩展 OpenUI 组件 `TimeField` / `PathField` / `DayChips` / `Number` / `SwitchField` / `Segmented` / `MultiSelect`；也接受 Markdown ````form` JSON 围栏（转成同一套 OpenUI 再画）

## AI 编排

- 工具 **`present_form`**：传入 `fields_json` 或 `preset=schedule`（可 `prefill_json`）→ 返回 ````openui` Form；调用后停止本轮，等用户点提交（字段以「名称：值」附在下一句用户消息，与现有 Form 按钮相同）
- 系统提示：缺路径/时间时先 `present_form` / OpenUI Form，再 `schedule_create`

## 定时任务表单 v2

字段：名称、时间、重复、动作、源/目标路径、匹配、操作（分段）、按日期开关、脚本路径或助手提示词（textarea）。保留「填入示例」按钮。

## 非目标

不引入外部 MCP 表单服务器；不做完整可视化表单设计器。


## OPENUI2 — 扩展控件与演示场景

新增 field kind：`date`、`filetypes`、`tags`、`slider`、`progress`、`table_select`（绘制见 `forms_paint.go`，OpenUI 组件见 `internal/assistant/openui_fields.go`）。

`present_form` 预设：`schedule` | `rename` | `share` | `search` | `classify` | `backup`（`internal/forms/scenes.go`）。助手斜杠 `/表单-…` 与 `SUNVALE_OPENUI_DEMO=1` 关键词可离线注入同一套表单。

### 演示路径沙箱

无 `~/Downloads` / `~/Documents` 的环境（CI、部分 Linux 主机）上，`PreferPath` / `EnsureDemoSandbox`（`internal/forms/sandbox.go`）在 `TempDir/sunvale-openui-demo` 创建真实目录与样例文件，定时任务与场景表单默认路径指向它们。`formPickPath` 用 `ExistingDir` 避免原生对话框对缺失 DefaultPath 报 `no such file or directory`。

截图：`docs/assets/shots/OPENUI-DEMO/`。
