# 13 聊天记录显示（CHATHIST，演示）

状态：已选 B（侧列），已实现（`assistant_hist.go`）。截图 `docs/assets/shots/CHATHIST/10-b-real-light.png`、`11-b-real-dark.png`，由 `SUNVALE_SHOT_DIR=… go test -run TestChatHistShots .` 渲染真实界面（演示会话只写入临时目录）。A/C 仅为当时的候选，mock 已删除。

## 现状
- 存储：`<assistRoot>/sessions/<id>.json`（`SessionFile`：标题、创建/更新时间、文件夹、Turns），索引 `sessions` 列表为 `SessionMeta{ID,Title,Updated,Folder,Preview}` + `ActiveID`。空会话不落盘。
- UI：头部「会话」切换窗格内左侧历史列（140–200px），按 今天/昨天/更早 分组；行为标题 + 时间·文件夹·预览两行；同列还有记忆区和说明文字。点击恢复会话，可删除。

## 变体
| | 布局 | 适合 |
|---|---|---|
| A 翻转 | 历史替换整个对话区；搜索 + 全部/本文件夹；单行：置顶·标题·文件夹·条数·时间 | 历史多、需要检索 |
| B 侧列 | 现有左列收紧为 176px 单行（标题·时间），对话保持可见 | 频繁来回切换 |
| C 头部切换 | 点头部标题向下展开内联列表（推开对话，不浮动），最近 6 条 +「全部 N 个会话」进 A | 偶尔切换，最省空间 |

## 共同交互
- 行高 28，圆角 4；分组 置顶/今天/昨天/更早；当前会话选中底色，悬停显示 置顶/重命名/删除（替代时间列）。
- 标题：首条用户消息截断，回答完成后可由模型摘要覆盖；重命名后不再自动改。
- 搜索：标题 + Preview 即时过滤，回车搜全文（读会话文件）。
- 打开：`loadSession` 恢复消息，滚动到底；运行中的会话不可删除。
- 记忆区移出历史列，放到 设置 → 助手 → 记忆；历史列不再有说明文字。
- B 实现：列宽默认 176（140–320，拖右缘调整、双击复位），开合与宽度存 `Prefs.HistOpen/HistWidth`；打开会话时列保持展开；行内 置顶/重命名（回车确认、Esc 取消、清空恢复自动标题）/删除。

## 存储增量
`SessionMeta` 增 `pinned`、`turns`（消息数）、`renamed`；`SessionFile` 增 `renamed`。旧索引缺省为零值，兼容（`TestHistIndexBackwardCompat`）。保存会话时保留索引中的置顶。
