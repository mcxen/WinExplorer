# 02 侧栏目录树打磨

基于 bf9ae80 的 `sidebar.go` 与 `navtree.go`。现状：
- 树行由 `sidebar.go:(*App).navFolderRow` 绘制；展开箭头是 `sidebar.go:(*App).navTwist`，内部调用 `icons.go:chevronIcon(c, open, 12)`，**open 时直接换成 `lucide.ChevronDown`**，没有过渡。
- 缩进：`navtree.go:navIndent(depth)`，每级 `navTreeStep = 14`，最多 `navTreeMaxIndent = 96`；`navFolderRow` 用一个空 `ui.Box` 作缩进。没有缩进引导线。
- 行收集：`sidebar.go:(*App).navRows` / `navTreeRows` 生成 `[]navRow`，虚拟列表 `navTree.list ui.ListState` 渲染。
- 跟随：`navtree.go:(*App).navSync`、`navTreeFollow`、`navTreeReveal`（`navTree.reveal`/`row` + `ScrollIntoView`）。
- 键盘：`main.go:(*App).handleKeys` 只服务文件窗格，侧栏没有焦点概念与方向键导航。

## A. 箭头旋转动画（150ms）

**改动**
- `icons.go`：新增 `func chevronIconAnimated(c *ui.Context, key string, open bool, px float32) ui.Element`：始终使用 `lucide.ChevronRight`，用 `el.AnimateWith(key, target, 150*time.Millisecond, ui.EaseOut)`（mygo v0.5.5 `Element.AnimateWith`；若无合适 easing 常量用 `Animate`）把角度从 0 过渡到 90，再 `.Rotate(deg)`。
- `sidebar.go:navTwist` 改用 `chevronIconAnimated(c, "twist-"+r.path, r.open, 12)`。保留 `chevronIcon` 给详细信息表头等其他调用点。
- 动画进行中 mygo 会自行请求帧；不要手动 `repaint()` 循环。

**验收**：展开/折叠时箭头 150ms 内平滑旋转；离屏测试中第一帧角度在 0–90 之间、跑过 200ms（`tt.Advance` 或等待真实时间）后为 90。
**边界**：系统“减少动画”设置若 mygo 暴露则直接跳到终值；树滚动出视图再滚回（虚拟列表重建元素）不应重新播放——key 用路径，`Animate` 状态以 key 为准。

## B. 悬停显示缩进引导线

**目标**：鼠标在导航窗格内时，每一级缩进处画一条 1px 竖线（`colorDivider()`），像 VS Code；鼠标离开窗格时不画。当前选中行所在的祖先链那一级的线使用 `colorMuted()` 以示路径。

**改动**
- `sidebar.go:navFolderRow`：把缩进 `ui.Box` 换成 `navIndentGuides(c, depth int, hovered bool, active int)`，在 `Draw(func(p *ui.Painter, r ui.Rect))` 中按 `navTreeStep` 间隔画线，线位于每级箭头中心（`x = navRowPad + i*navTreeStep + 9`）。
- `navRow` 增加 `depth int`（当前只有 `indent float32`；由 `navTreeRows` 填写），超过 `navTreeMaxIndent` 的层级不画（与缩进被压缩一致）。
- 窗格悬停：`sidebar.go:(*App).sidebar` 外层元素 `Hovered()` 存到 `a.navTree.hover bool`（本帧用上一帧的值即可）。线条透明度用 `AnimateColor` 在 120ms 内淡入淡出。

**验收/测试**：截图对比悬停与否；单测 `navRows` 的 `depth` 正确。

## C. 方向键导航

**目标**：点击侧栏任一行后侧栏获得“键盘焦点”，直到点击文件窗格或按 Tab：
- ↑/↓：上一/下一可选行（跳过 `navRowSection` 标题行）；Home/End 到首/尾。
- →：折叠的文件夹展开；已展开则移到第一个子行。
- ←：展开的文件夹折叠；否则移到父文件夹行。
- Enter/空格：导航到该行（`navFromPane` + `navigate`，与点击一致）。
- 移动只改变焦点行（画 1px `colorAccent()` 焦点环），**不立即导航**，与 Explorer 一致。

**改动**
- `navtree.go:navTree` 增加 `focused bool`、`cursor string`（行 key）。
- 新文件 `navkeys.go`：`func (a *App) navTreeKeys(c *ui.Context) bool`，在 `main.go:handleKeys` 开头当 `a.navTree.focused && !a.fieldFocused` 时调用，返回 true 表示已消费。
- 父行查找：在 `navRows()` 的结果里向上找第一个 `depth < 当前 depth` 的行。
- 焦点行滚动入视：`a.navTree.list.ScrollIntoView(i)`。
- 点击文件窗格（`files.go` 的空白/行点击处）置 `a.navTree.focused = false`。

**测试**（`navtree_keys_test.go`，参照 `keynav_ui_test.go` 的窗口）：点击一行 → ↓ 两次 → 焦点 key 正确且 `a.cur().path` 未变；→ 展开；← 回父；Enter 导航。文件窗格的方向键行为不受影响（现有 keynav 测试全部通过）。

## D. 平滑滚动（可选，能做则做）

`navTreeReveal` 现在用 `ScrollIntoView` 瞬间跳转。mygo v0.5.5 的 `ListState` 只有 `ScrollTo/ScrollIntoView/ScrollToEnd`，没有动画参数。方案：先查 `ListState` 是否暴露当前偏移（`Offset()` 一类）；若有，用 `Animate` 在 180ms 内插值偏移并每帧 `ScrollTo`；若没有，**不做**，在 PR 中记录“mygo 不支持”，不要 fork 依赖。

## 不做
- 不改树的数据加载（`navChildren` 的懒读）。
- 不改快速访问拖放（`bindDropFolder`/`bindDragSource`）。
