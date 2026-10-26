# 11 视觉语言：macOS 26 风格（原 v2）

在保持 Explorer 密度的前提下，对齐 macOS 26 Tahoe / Liquid Glass：浮动毛玻璃侧栏、胶囊工具栏分组、同心圆角、系统蓝强调、内嵌字体。**默认**；经典（Windows 11 扁平）仍可切换。设置值保持 `"v2"`（兼容已保存配置），界面名称为 **macOS 26**。

## 令牌（`visual.go`）

| 常量 | 值 | 用途 |
| --- | --- | --- |
| `v2PaneRadius` | 14 | 浮动侧栏、内容卡、对话框（`Theme.Radius` 7 × 2） |
| `v2CardRadius` | 12 | 卡片内的卡（任务、磁盘、OpenUI） |
| `v2RowRadius` / `rowR()` | 7 | 行、选中高亮、焦点环、小按钮（经典 4） |
| `ctlR()` | 8 | 按钮、窗格内输入（经典 6） |
| `v2InputRadius` / `v2PillRadius` | 16 | 32 高的地址/搜索框与工具栏按钮组 = 胶囊 |
| `v2Gutter` | 8 | 窗口 → 浮动窗格内缩 |

同心规则：子形状内缩 d 时取 R−d（窗格 14，行内缩 6 → 7）。色板为 Apple 系统色（蓝 #007AFF / #0A84FF 等）。

## HIG 采用的度量（2026-10 抓取）

| 项 | 值 | 来源 |
| --- | --- | --- |
| 字号 | macOS Body 13/16、Callout 12、Subheadline 11、Footnote 10、Headline 13 Bold、Title3 15 → 列表/侧栏 13（`bodyFont`），表头/状态栏 11（`statusFont`） | [Typography](https://developer.apple.com/design/human-interface-guidelines/typography) |
| 系统色 | Blue #0088FF / #0091FF，Red #FF383C / #FF4245，Orange #FF8D28 / #FF9230，Green #34C759 / #30D158 | [Color](https://developer.apple.com/design/human-interface-guidelines/color) |
| 标签 / 分隔 | label 85%、secondaryLabel 50%（暗 55%）、separator 10% | AppKit 语义色（同上页面列出的 API） |
| 选中 | 列表有键盘焦点：强调色填充（selectedContentBackground #0064E1 / #0059D1）+ 白字；无焦点：灰（#DCDCDC / #464646）+ 常规字色 | [Focus and selection](https://developer.apple.com/design/human-interface-guidelines/focus-and-selection) |
| 侧栏 | 浮于内容之上；图标跟随强调色（单色符号，Lucide 近似 SF Symbols）；中号行高 28 | [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars) |
| 工具栏 | 控件圆角与栏同心；项目分组为胶囊；搜索在尾部 | [Toolbars](https://developer.apple.com/design/human-interface-guidelines/toolbars) |
| 表格 | 行高 24（紧凑 20），无网格、无交替行；日期/种类/大小用次级色，大小右对齐 | [Lists and tables](https://developer.apple.com/design/human-interface-guidelines/lists-and-tables) |

## 材质 / 模糊

- 窗口：`mygo.VibrancyTabbed`——Windows 11 为 Mica，macOS 为原生 vibrancy；无原生材质（Linux、旧 Windows）时绘制 mist 渐变。
- `macGlass`：MyGo `plugins/glass` Liquid Glass（真实背景模糊，GPU 着色器），用于侧栏（`macFloat`）、工具栏胶囊（`macPill`）；停靠助手窗格位于不透明内容卡内，仅着色；对话框为 Onevra 玻璃面板；任务翻转沿用自适应 `Blur`。
- 回退：高对比度、`glassOn=false`、或翻转测得超预算（`flipLowEnd`）→ 半透明色 `glassTint()`。
- 右键菜单为系统原生菜单（macOS NSMenu 自带模糊），不由应用绘制。

## 借鉴 Kero

`kero_elevation.go` 改编自 [egoist/kero](https://github.com/egoist/kero) `elevation.go`（MIT）：控件面阴影、浮层阴影、macOS 菜单外观、暗色亮边。

## 字体（`fonts.go`，`fonts/OFL-*.txt`）

内嵌 SIL OFL 1.1：Inter 可变（Latin 子集）、JetBrains Mono 可变（Latin + 制表符）、Noto Sans CJK SC 常规/粗体（GB2312 子集）。缺字回退系统字体。设置 → 界面字体：内嵌 / 系统。SF Pro 许可禁止分发，未内嵌。

## 截图

`docs/assets/shots/MACOS26/`（`TestMacOS26Shots`，`SUNVALE_SHOT_DIR`）；旧 v2 对比 `00-v2-before.png`；`VISUAL2/` 为历史。
