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

## OPENUI4 — 展示组件（数据看板）

回复不再只有表单与表格：新增一组展示组件，磁盘、空间、统计类回复直接画成看板。

| 组件 | OpenUI 写法 | 绘制 |
| --- | --- | --- |
| `Note` | `Note(text, tone?, icon?)`，tone 为 `info`/`success`/`warning`/`danger` | Onevra 提示条：色调左边条 + 图标（tone 未指定图标时自动选）；icon 取白名单 lucide 名（约 35 个），不认识的名字丢弃而非丢组件 |
| `Badge` | `Badge(text, tone?)` | Onevra Badge 胶囊，状态用 |
| `KeyValue` | `KeyValue(label, value, tone?)` | 一行摘要：左灰标签、右加重值（可着色） |
| `Chart` | `Chart(labels, [Series(name, [v…])…])`，也接受 `{labels: […], series: [{label, values}]}` | Onevra chart.Bar，PlotHeight 140，最多 4 系列 / 60 类目，选择状态按卡片键存在 `assistOpenState.charts` |
| `DonutChart` | `DonutChart([Series(name, [v])…])` | chart.Donut（128 高）+ 自绘数值图例（色点、值、百分比），比例制：百分比或绝对数都直接可用 |
| `Accordion` | `Accordion([AccordionItem(title, children?, open?)])` | Onevra Accordion；首项默认展开，`open` 可指定；折叠状态记在 `assistOpenState.planNo` |
| `RatingField` | `RatingField(name, label?, max?, "required"?)` | Onevra Rating 星级（1–max，max 默认 5 上限 10），值以数字字符串进表单消息；`rating` 加入 `OpenIsField` |
| `Divider` | `Divider()` | 一条分隔线 |

规则与实现：

- `FormControl` 现在也接受展示组件：字段保持原样，展示组件前垫一行小字标题（`internal/assistant/openui.go`）；`present_form` 的 ToOpenUI 把每个字段都包在 FormControl 里，两条路由此合一。
- `internal/forms` 新 kind：`note`/`badge`/`keyvalue`/`rating`/`chart`/`donut`/`accordion`（`Field.Tone` 着色；图表数据用 `Options` 里的 `「标签: v1, v2」` 行表示系列，见 `chartFieldData`）。展示类 kind 不要求 `id`，也不进提交消息。
- 演示场景 `disk`（磁盘概览看板）：Badge 健康 + KeyValue 容量 + Chart 近三月 + DonutChart 构成 + Note 建议 + Accordion SMART + RatingField 打分；`SUNVALE_OPENUI_DEMO=1` 说「磁盘 / 看板 / 概览」或斜杠 `/表单-磁盘概览看板` 注入。
- 系统提示词列出全部新组件与看板优先规则；颜色始终由绘制层从主题 token 取（`openToneColor` + `openChartPalette`），程序里不携带颜色。

## OPENUI-MEDIA — 图文列表 MediaList

只读展示组件（不是表单字段），类似 ChatGPT 的图文步骤列表。

```
root = MediaList([m1, m2, m3], "剪辑步骤", "numbered")
m1 = MediaItem("/abs/cover.png", "选封面", "用第二张做封面。", "image")
m2 = MediaItem("/abs/clip.mp4", "剪开场", "保留前 5 秒。")
m3 = {media: "https://example.com/a.png", kind: "image", title: "参考", text: "构图参考。", n: "3"}
```

| 参数 | 说明 |
| --- | --- |
| `MediaList(items, title?, style?)` | `items` 为 MediaItem/对象数组（最多 30 行）；`title` 可选；`style` 为 `"numbered"` 时标题前自动编号 `1.` `2.`。别名 `MediaCards`。 |
| `MediaItem(media, title, text, kind?, n?)` | 别名 `MediaCard`。对象写法键：`media`（`src`/`path`/`url`）、`kind`（`type`）、`title`、`text`（`body`/`desc`）、`n`（显式编号优先）。 |
| `kind` | `image` / `video` / `doc`；省略或无法识别时按扩展名推断，`data:image/` 视为图片，其余为 `doc`。 |

绘制：左侧 136×92 圆角缩略图卡（图片 Cover；视频走 `mediaThumb` 关键帧 + 播放角标；PDF/HTML 走首页/图标缩略；其余或加载失败显示文件图标），右侧 13px 粗体标题 + 12px 正文自动换行，行间 1px 分隔线。点击缩略图以该文件打开空格快速预览（`openQuickLookPath`，不改变选中项；←/→ 不切换）。`http(s)` 地址后台下载（≤16 MB、20 s 超时）到 `TempDir/sunvale-openui-media` 后显示与预览，下载前显示文件图标。流式未闭合时沿用 OPENUI-SKEL 骨架。

截图：`docs/assets/shots/OPENUI-MEDIA/04-medialist.png`（列表）、`05-medialist-quicklook.png`（点缩略图打开快速预览）。

## OPENUI-MORE — Files / Steps / StatCards / Gallery

已有组件不重复：`Note`/`Callout`、`KeyValue`、`Badge`、`Table`、`Tree`、`Chart`、`DonutChart`、`Accordion`、`ListField`（`FileList` 为其别名，表单内勾选）、`MediaList`。本批只补缺口。

| 组件 | 别名 | 语法 | 说明 |
|---|---|---|---|
| `Files` | `FileCards` | `Files(items, title?)`；项 `FileCard(path, note?, size?, mtime?, name?)` / `{path, name, size, mtime, note}` / 路径字符串 | 行：36px 图标或缩略图 + 名称 + 「大小 · 修改时间 · 备注」；本地文件缺省的大小、时间自动读取。点图标快速预览（文件夹图标单击即进入），双击行系统打开（文件夹进入）。本地 stat 结果按路径缓存 2 秒。 |
| `Steps` | `Timeline` | `Steps(items, title?)`；项 `Step(title, text?, status?, time?)` / `{title, text, status, time}` | 竖向时间线；status `done`/`active`/`pending`/`error`（complete、doing、failed 等同义词归一）。 |
| `StatCards` | `Stats`、`Metrics` | `StatCards(items)`；项 `StatCard(label, value, delta?, tone?)` / `{label, value, delta, tone}` | 自动换行的指标卡；tone 着色数值与 delta。 |
| `Gallery` | `ImageGrid` | `Gallery(items, title?, cols?)`，或 `{cols: n}`；项为路径字符串、`MediaItem(media, caption)` 或 `{media, title}` | 缩略图网格（4:3，默认 120×90），视频带播放角标；`cols` 1–8 列平分宽度，瓦片窄于 72 时退回自动排；单击快速预览（延迟 300ms 判双击），双击系统打开；悬停/聚焦描边，提示显示说明与文件名。 |

每组最多 60 项；空项丢弃，全空则整条语句不画。流式时沿用 OPENUI-SKEL 骨架。离线演示：`SUNVALE_OPENUI_DEMO=1` 下发送含「更多组件」或 `openui-more` 的消息。截图：`docs/assets/shots/OPENUI-MORE/`（OPENUI-MORE2 续 `05-gallery-cols.png`、`06-gallery-narrow.png`）。
