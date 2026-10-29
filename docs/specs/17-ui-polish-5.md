# 17 — 界面打磨第五轮（UI5）

参照：修前截图 `/workspace/sunvale-review-shots/`，修后 `/workspace/sunvale-ui5-shots/`。默认外观（v2）不变；暗色对比度一组（选中态、分段按钮、暗色图标、侧栏焦点框）不在本轮。

## A 菜单统一

- 命令栏下拉（新建、排序、查看、查看更多）与右键菜单在两种外观下都用窗口内自绘 flyout（`menuflyout.go`），不再用系统菜单。右键菜单经 `menuctx.go`：`xMenu` 与 `ui.Menu` 同形（`Item/Separator/Submenu/Disabled/Checked/Shortcut/Chosen`，另加 `Radio`、`Icon`），右键仍走 `ui.ContextMenu` 以保留列表行的右键选中、Shift+F10 与菜单键，但收集时不往系统菜单放项，系统菜单因此不弹出。
- flyout：快捷键右对齐次级色；单选项用圆点、开关项用勾；子菜单箭头用 10px 细 chevron；超过 12 行时滚动（面包屑下拉等），方向键会把高亮行滚入视野。
- 右键菜单在点击处弹出，靠近窗口边缘时由 `AttachTo` 翻到点的另一侧；级联子菜单与 `↑↓←→ Enter Esc` 照常。
- v2 取 v2 的面（白 / `#26262C`）、`tokFieldBorder` 边、`tokHover` 悬停；分隔线用新令牌 `tokMenuSep`（暗色 `#4A4A4A` / v2 `#44444C`），暗色下可见。
- 插件菜单项只有 `Toggle` 为真（开关型，如「远程访问」）才显示勾，其余动作项不再带复选框。
- 新建：命令栏和右键「新建」都带图标、文字左对齐，列出文件夹、文本文档、Markdown 文档、JSON 文件、压缩(zipped)文件夹（`ops.go:newFileKinds`，均能真正创建）。
- 面包屑分隔箭头、溢出「…」、最近访问下拉的每项带文件夹图标。
