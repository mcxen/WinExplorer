# 鼠标圈选（Rubber-band Selection）技术文档

> 状态：已实现（第一版）
> 相关代码：`marquee.go`（新增）、`state.go`、`files.go`、`gallery.go`
> 目标：在文件区的空白处按住左键拖动，画出选区，实时选中与选区相交的行/磁贴。

---

## 1. 目的与范围

为 Sunvale 增加 Windows 资源管理器 / macOS Finder 风格的**鼠标框选**能力：从空白处拖出一个矩形，凡与该矩形相交的文件项即被选中。

覆盖的视图：

| 视图 | 容器 | 项类型 |
| --- | --- | --- |
| 详细信息 | 虚拟化 `ui.List` | `detailsRow` |
| 列表 | 虚拟化 `ui.List` | `listRow` |
| 小 / 中 / 大 / 超大图标 | `ui.GridView`（分组时为 `ui.Scroll` + 每组的 `GridView`） | `iconTile` |
| 画廊（照片页） | `ui.Scroll` + 每月的 `GridView` | `galleryTile` |

**不在范围内**：树形侧边栏、主页磁贴、`此电脑` 页的卷行、回收站页。

---

## 2. 交互设计

| 手势 / 修饰键 | 语义 |
| --- | --- |
| 空白处拖动（无修饰键） | **替换**：选区成为唯一选择 |
| 空白处拖动 + `Shift` | **追加**：选区并入原选择，原选择保留 |
| 空白处拖动 + `Ctrl` / `Cmd` | **反选**：与选区相交的项在「选中/未选中」间翻转 |
| 空白处单击（未达拖动阈值） | 结束手势，**不改动选择**（与既有空白处单击行为一致） |
| 从文件项上拖动 | 不是圈选，走既有的**拖拽移动文件**（`dropops.go`） |

- 相交判定采用 **「碰到即选」**（overlap），与 Finder 一致，而非资源管理器「完全包住才选」。
- 拖动中框随指针实时更新，选择随之实时变化；松开即定型。
- 一个拖动阈值（4 DIP，`bandStart`，与 UI 工具包 `dragStart` 同值）区分「点击」与「拖动」。虚线框与选择都在越过阈值后才出现——两者同进同退。
- **阈值必须自己判**：`Element.Dragged` 从按下的第一帧起就以位移 0 报「正在拖动」，只看它的 `ok` 会把一次空白单击当成一个「零面积框」，从而把现有选择清空（见 `marqueeState.moved`）。

---

## 3. 架构总览

```
                        ┌─────────────────────────────┐
   指针按下（空白处）   │  bindMarquee（手势状态机）   │
        ───────────────▶│  Pressed → Dragged → 结束    │
                        └──────────────┬──────────────┘
                                       │ 每帧
                                       ▼
   itemBounds（上一帧各项的窗口矩形） ──▶ applyMarquee（命中测试 + 选择合成）
                                       │
                                       ▼
                              tabState.selected（既有选择模型）
                                       │
                                       ▼
                       paintSelect（既有高亮）+ requestPreview（预览刷新）

   tabState.marquee.rect() ──▶ marqueeOverlay（DrawOver 画虚框，PassThrough 不挡事件）
```

设计要点一句话：**不引入新的选择模型**。圈选只是「用批量方式改写既有的 `tabState.selected`」，因此高亮（`paintSelect`）、预览、状态栏合计、复制/删除等下游全部自动复用。

---

## 4. 状态模型（`state.go`）

在 `tabState` 上新增两个字段（`state.go:51`）：

```go
marquee    marqueeState
itemBounds map[string]ui.Rect
```

`marqueeState`（`state.go:61`）：

```go
type marqueeState struct {
	on     bool           // 手势是否进行中
	x0, y0 float32        // 起点，窗口 DIP
	x1, y1 float32        // 当前点，窗口 DIP
	mods   ui.Modifiers   // 按下时按住的修饰键（整个手势期间固定）
	base   map[string]bool// 拖动前的选择快照，Shift / Ctrl 在其上叠加
}
```

- **窗口坐标**：`x0/y0/x1/y1` 均为窗口 DIP，与 `el.Bounds()` 同一坐标系，可直接比较。
- **修饰键在手势开始时固定**：拖动途中改按/松开修饰键不改变语义，与资源管理器一致，也避免中途改变合成方式造成选择抖动。
- **`base` 是快照**：因为 `applyMarquee` 每帧都会整体重写 `t.selected`，必须保留一份「拖动前」的选择，Ctrl 反选才有稳定基准。
- `itemBounds` 是**每项路径 → 窗口矩形**的映射，在渲染时记录（见 §7），供命中测试使用。

---

## 5. 手势状态机（`marquee.go:51` `bindMarquee`）

`bindMarquee` 被挂到**每一个可能的空白背景元素**上：文件区外层 `ui.Column`、`ui.List`、`ui.GridView`、`ui.Scroll`。这一设计依赖 UI 工具包的一条事实：

> 一次按下只交给**一个**元素——引擎的 press 目标。只有该元素的 `Pressed()` / `Dragged()` / `DragEnded()` / `Clicked()` 会返回 true，其余元素这几个方法一律 false，因而**不会互相干扰**。

状态机：

```
             ┌─ el.Pressed() 且不在文件项上（pointOnItem 为假）
             │   → on=true；记录 x0,y0、mods、base 快照
   空闲 ─────┤
             └─ 其余情况：无操作

   进行中 ───┬─ el.Dragged() → 更新 x1,y1；越过 bandStart（moved()）才 applyMarquee()
             ├─ el.DragEnded() 或 el.Clicked() → on=false；清 base（选择定型）
             └─ 其余：无操作（非 press 目标的元素永远走到这里，静默）
```

关键细节：

- **起点判定**：`wx, wy = el.Bounds().X + PointerPosition()`，再问 `pointOnItem(wx, wy)`。落在文件项上则不启动圈选——那次拖动是「拖拽移动文件」。
- **坐标换算**：`PointerPosition()` 返回元素**局部**坐标，即使指针已移出元素框仍然有效（`over` 标志只表示是否还在框内，圈选忽略它）。加回 `el.Bounds()` 原点即得窗口坐标，因此框可以自由拖出列表边界。
- **`Dragged()` 不等于「已拖动」**：工具包的 `Dragged` 只要求元素处于按下状态，第一帧就返回 `true`，位移为 0。因此状态机在更新 `x1,y1` 之后还要问 `moved()`：与起点相距不足 `bandStart`（4 DIP）就当作单击，既不改选择也不画框。
- **结束条件用 `Clicked()` 兜底**：未达拖动阈值的纯单击不会产生 `DragEnded()`，但会触发 `Clicked()`，用它收尾即可复位手势，避免 `on` 悬空。
- **`el.Clicked()` 的副作用**：它会置 `rt.consumed = true`，但空白单击本就是「无选择语义」，无碍。

---

## 6. 命中测试与几何（`marquee.go:9`、`21`、`26`、`33`、`102`）

```go
func (m marqueeState) rect() ui.Rect      // 归一化两角为矩形（支持向左/向上拖）
func (m marqueeState) moved() bool        // 是否已离开起点 bandStart，即「拖动」而非「点击」
func rectOverlaps(a, b ui.Rect) bool      // 任意重叠即为真
func (a *App) pointOnItem(x, y float32) bool // 起点是否落在某项上
```

- `rect()` 对 `x0/x1`、`y0/y1` 取 min/max，使任意方向的拖动都得到规范矩形。
- `moved()` 取两轴位移的绝对值与 `bandStart` 比较，任一轴越界即为拖动。
- `rectOverlaps` 是标准的轴对齐矩形相交测试（严格不等号，边贴边不算相交）。
- `pointOnItem` 遍历 `itemBounds`，用 `Rect.Contains` 判断起点是否在某一项内。

---

## 7. 项矩形的记录（渲染期）

`itemBounds` 在**每项构建时**写入，值是该元素**上一帧**的窗口矩形（`el.Bounds()` 的语义）：

| 位置 | 代码 | 记录 |
| --- | --- | --- |
| 详细信息 / 列表 / 图标（共用） | `files.go:967` (`bindItemInput`) | `t.itemBounds[e.Path] = el.Bounds()` |
| 画廊 | `gallery.go:333` (`galleryTile`) | `t.itemBounds[p.path] = tile.Bounds()` |

**每帧重建**：`filePane` 在帧首清空映射（`files.go:52`），除非手势进行中（`!t.marquee.on`）。手势期间不清空，是为了让拖动中因滚动/重排而暂未渲染的项仍保有上一帧的矩形。

```go
t := a.cur()
if t.itemBounds == nil {
    t.itemBounds = map[string]ui.Rect{}
} else if !t.marquee.on {
    clear(t.itemBounds)
}
```

---

## 8. 选择合成（`marquee.go:114` `applyMarquee`）

每帧由 `Dragged()` 触发，从当前框重建选择：

```
hit    = { 路径 | itemBounds[路径] 与框相交 }
next   = 依据 mods 在 base 上合成：
         无修饰键  → next = hit                         // 替换
         Shift     → next = base ∪ hit                  // 追加
         Ctrl/Cmd  → next = base △ hit                  // 对称差（反选）
若 selectionEqual(t.selected, next) → 直接返回（避免无谓刷新）
t.selected = next
锚点 anchor/cursor 对齐到 next 中视图顺序的第一项（替换模式必做；追加模式仅在原锚点失效时）
a.requestPreview()
```

- **`selectionEqual` 短路**（`marquee.go:171`）：框没有新进/新出项时不改选择、不刷新预览，节省无谓重绘。
- **锚点维护**：`anchor`/`cursor` 是既有 Shift 范围选择与键盘移动的基准。替换模式下手势接管锚点，指向框内视图顺序的第一项；追加模式尽量保留原锚点。
- **`copySelection`**（`marquee.go:185`）：`base` 必须深拷贝，否则会被 `applyMarquee` 的整体重写波及。

---

## 9. 渲染（`marquee.go:196` `marqueeOverlay`）

在文件区 `Column` 末尾挂一个绝对定位覆盖层，用 `DrawOver` 在**所有子元素之后**绘制，保证虚框压在最上层：

```go
overlay := ui.Box(c).Absolute().Left(0).Top(0).Fill().PassThrough()
overlay.DrawOver(func(p *ui.Painter, r ui.Rect) {
	// 画布坐标 == 窗口坐标：band 原样绘制，且夹在文件区盒内
	p.Clip(r, 0, func() {
		p.Fill(band, colorAccent().Alpha(0.12), 0)          // 半透明填充
		p.StrokeDashed(band, colorAccent().Alpha(0.9), 0, 1) // 虚线边框
	})
})
```

- **直接用窗口坐标**：引擎 `place` 之后所有元素的 `x/y/w/h` 都是窗口坐标，`world` 只承载视觉变换（`Transform`），所以 `Paint`/`DrawOver` 的画布坐标就是窗口坐标，回调参数 `r` 也是窗口坐标。`band` 与 `itemBounds` 都在这一坐标系里，**原样绘制**即可。
- **不要用 `WindowToLocal` 换算**：`WindowToLocal` 把窗口点变成「元素自身盒内」的点（`world⁻¹` 再减去 `st.x/st.y`），而画布并不在那个坐标系里；用它会把框再往左上偏移一个「文件区在窗口中的位置」——正是此前框位置不对的原因（框比实际拖动区偏左一个侧边栏宽、偏上一个工具栏高）。
- **`p.Clip(r, 0, …)`**：`DrawOver` 不受元素自身 `Clip()` 影响（引擎在调用 `paintAfterFn` 之前已弹出自身裁剪），因此显式用 `Painter.Clip` 把框夹在文件区盒内：拖到边缘外时框止步于文件区，不会压到侧边栏和工具栏。
- **`PassThrough()`**：覆盖层不接管任何指针事件，行/磁贴的点击、拖拽、右键菜单不受影响。
- **只画「已是拖动」的框**：`marqueeOverlay` 与选择同用一个 `moved()` 判据，单击不留残框。
- **配色**：复用主题令牌 `colorAccent()`，自动适配亮/暗外观。
- **`filePane` 中的调用点**：`files.go:85`（常规视图）与 `files.go:68`（gallery 分支，因 gallery 提前 return）。

---

## 10. 与既有机制的边界

- **拖拽移动文件不冲突**：文件项自身注册为拖拽源（`bindItemInput` 里 `el.Drag(...)`），从项上拖动被它消费，`bindMarquee` 的 `pointOnItem` 二次拦截，双保险。
- **选择模型复用**：圈选只写 `tabState.selected` 并调 `requestPreview()`，高亮、预览、状态栏、复制/删除/压缩等全部沿用既有路径，无需改动。
- **按标签隔离**：`marquee` 与 `itemBounds` 都在 `tabState` 上，天然按标签独立，切换标签不会串味。
- **预览刷新**：`applyMarquee` 复用既有 `requestPreview()`，与单击/范围选择同一入口。

---

## 11. 代码索引

| 位置 | 说明 |
| --- | --- |
| `state.go:51` | `tabState.marquee` / `itemBounds` 字段 |
| `state.go:61` | `marqueeState` 类型 |
| `marquee.go:9` | `marqueeState.rect()` 归一化矩形 |
| `marquee.go:21` | `bandStart` 拖动阈值 |
| `marquee.go:26` | `marqueeState.moved()` 点击/拖动判定 |
| `marquee.go:33` | `rectOverlaps()` 相交测试 |
| `marquee.go:51` | `bindMarquee()` 手势状态机 |
| `marquee.go:102` | `pointOnItem()` 起点判定 |
| `marquee.go:114` | `applyMarquee()` 选择合成 |
| `marquee.go:171` | `selectionEqual()` 相等判断 |
| `marquee.go:185` | `copySelection()` 快照深拷贝 |
| `marquee.go:196` | `marqueeOverlay()` 虚框绘制 |
| `files.go:52` | 每帧重建 `itemBounds` |
| `files.go:68` / `85` | 挂载覆盖层（gallery / 常规） |
| `files.go:967` | 记录行矩形（`bindItemInput`） |
| `files.go:1011` | 挂载 `bindMarquee`（`bindPaneMenu`） |
| `gallery.go:333` | 记录画廊磁贴矩形 |
| `marquee_test.go` | 圈选测试：命中与修饰键合成、两个视图里框与选区的位置、空白单击不改选择 |

---

## 12. 已知限制与后续可做

- **只覆盖当前已渲染的项**：`ui.List` 是虚拟化的，只有渲染出来的行才有 `itemBounds`。因此框只能选中屏幕上真实出现的项——与「框住可见内容」的直觉一致，但**不包含滚出视口的项**。
- **无边缘自动滚动**：拖到列表边缘时不会自动滚动（资源管理器有）。需要驱动列表的 `ListState`/`GridState` 滚动，属增强项。
- **手势期间不清空 `itemBounds`**：好处是拖动中重排仍可命中；代价是极少数情况下会引用到「上一帧还在、这一帧已消失」的矩形。当前目录切换会重建整表，影响可忽略。
- **不覆盖侧边栏 / 主页磁贴 / 卷行**。
- **滚动条区域**：在列表滚动条上按下且未落在项上时，理论上可能误启动圈选；实践中滚动条自己消费按下，暂未处理。

---

## 13. 手工验收清单

1. 详细信息视图——空白处左下→右上拖，框住的行高亮；框越界不崩溃。
2. 图标视图——斜向拖，磁贴「碰到即选」。
3. 画廊页——同上。
4. `Shift` + 拖动 → 原选择保留，选区并入。
5. `Ctrl/Cmd` + 拖动 → 与选区相交的项翻转选中态。
6. 空白处单击（不拖）→ 选择不变，无残留虚框。
7. 从文件项上拖动 → 仍是拖拽移动文件，不出虚框。
8. 拖框松开后 → 预览窗格/状态栏合计随新选择更新。
9. 切换标签页 / 切换目录后再圈选 → 无旧矩形残留导致的误选。
