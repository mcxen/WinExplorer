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

无 `~/Downloads` / `~/Documents` 的环境（CI、部分 Linux 主机）上，`PreferPath` / `EnsureDemoSandbox`（`internal/forms/sandbox.go`）在 `TempDir/sunvale-openui-demo` 创建真实目录与样例文件，定时任务与场景表单默认路径指向它们。`formPickPath` 与助手「指定文件」(`assistantPickFiles`) 经 `assistDialogDir` → `ExistingDir` 选起始目录，避免原生对话框对缺失 DefaultPath 报 ENOENT 或挂起；`runAssistPick` 在成功/失败/超时后复位 `assist.picking`。

截图：`docs/assets/shots/OPENUI-DEMO/`。

## OPENUI3 — 列表 / 图片 / 视频

新增 field kind：

| kind | OpenUI 组件 | 说明 |
| --- | --- | --- |
| `list` | `ListField(name, options, mode?)` | 可勾选/单选文件清单。options 每项 `文件名\|大小\|缩略图路径`（后两段可空）；`path_mode`/`mode` 为 `multi`（默认）或 `single`；空 options 显示空状态（placeholder 或「暂无项目」）。提交值为所选**文件名**的逗号拼接。 |
| `image` | `ImageField(name, src)` | 表单内嵌图片。`value`/`src` 为本地路径或 `data:image/…;base64,…`；缩放 `Contain`；坏图占位（图标 + 原因）。 |
| `video` | `VideoField(name, path)` | 关键帧预览（`ffmpeg` 抽帧，与预览窗格相同）+「打开播放」走系统播放器。**无内嵌视频解码器**（mygo/Onevra 限制）。 |

`present_form` 预设追加：`filelist` | `image` | `video`（`internal/forms/scenes.go`）。

截图：`docs/assets/shots/OPENUI-MEDIA/`。
