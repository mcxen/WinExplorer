# 16 命令入口与按钮排布梳理（CMD1）+ 经典外观 Fluent 2 对齐（CLASSIC-FLUENT）

分支 `feat/classic-fluent`（demo，未合 main）。截图 `docs/assets/shots/CLASSIC-FLUENT/`。

## 1. 原则

| # | 原则 | 来源 |
| --- | --- | --- |
| P1 | 高频命令外置为按钮，低频进「更多 …」；「更多」放在命令区最右 | WinUI CommandBar（Primary / Secondary commands）、Windows 11 资源管理器「查看更多」 |
| P2 | 一个主操作用强调色；同一动作的多个目标用分割按钮（左主、右 ▾ 列表） | WinUI SplitButton、QuickLook for Windows「用 … 打开」、Files 预览窗格 |
| P3 | 关闭永远在最右端，Esc 等效 | Windows 11 照片查看器、macOS Quick Look、QL-Win |
| P4 | 菜单按「打开 → 编辑 → 整理 → 扩展 → 属性」分组，组间分割线；属性永远最后 | Windows 11 右键菜单、Files、GNOME HIG Menus、KDE HIG |
| P5 | 可用性规则统一：**本类型永远不适用 → 隐藏**；**当前上下文暂不可用 → 置灰** | WinUI MenuFlyout、GNOME HIG |
| P6 | 有快捷键的菜单项标注快捷键（`MenuItem.Shortcut`，只显示，不重复绑定） | WinUI KeyboardAccelerator 提示 |
| P7 | 同一面板开关只保留一个菜单入口（按钮可另有一个） | Windows 11 资源管理器「查看 > 显示」 |

## 2. 现状（origin/main）

| 入口 | 现状 | 问题 |
| --- | --- | --- |
| 快速预览（空格） | 无标题栏；图片顶部一行 ‹ 文件名 ›；底部长提示「滚轮缩放 · 拖拽平移 · ←/→ … · 空格/Esc 关闭 · 100%」；无打开/打开方式/关闭按钮（仅不支持类型有「打开」） | 违反 P1/P2/P3：主操作只能靠 Enter；无关闭；说明文字冗长；清透玻璃下底层界面透出，难读 |
| 命令栏 | 新建 ｜ 剪切 复制 粘贴 重命名 分享 删除 ｜ 排序 查看 任务 助手 ｜ 更多 | 基本合规；任务/助手是本应用特有，保留 |
| 查看更多 | 预览窗格、助手 ｜ 插件项 ｜ 设置 | 预览窗格、助手与「查看」菜单和助手按钮重复（助手 3 处）；缺 Explorer 的撤销/全选/全部取消/属性 |
| 查看 ▸ | 视图×6、紧凑视图 ｜ 助手 ｜ 放大/缩小图标 ｜ 显示 ▸ 扩展名/隐藏/复选框 ｜ 分组依据 | 预览窗格不在「显示」里（Explorer 在） |
| 文件右键 | 打开、打开方式、固定 ｜ 剪切 复制 粘贴 复制为路径 重命名 ｜ 压缩 解压 ｜ 终端 刷新统计 插件 扩展 **删除** ｜ 属性 | 删除被插件/扩展项隔开、无分割线；无快捷键标注 |
| 空白处右键 | 新建 ▸ 粘贴 ｜ 终端 插件 ｜ 刷新 刷新统计 显示隐藏 ｜ 属性 | 无快捷键标注 |
| 标签右键 | 新建/复制/关闭/关闭其他/关闭右侧/重新打开 | 无快捷键标注 |
| 预览窗格 | 文件名 → 预览 → 属性；不可播放类型有「打开」 | 无「打开方式」；见 §5 |
| 状态栏 | 左计数，右「详细信息」与视图切换 | 合规 |
| 助手栏头 | 历史、新对话、全屏、关闭 | 合规（关闭在最右） |

## 3. 新排布（本分支已落地）

### 3.1 快速预览标题栏（`quicklook_bar.go`，高 40）

```
[‹][›] [图标] 文件名（i / n）……………… [ 打开 |▾ ] [⋯] [×]
```

- ‹ › 32×32：图片在同目录图片间、其他类型在可见项间步进；两端置灰；助手图文卡打开的单文件置灰。
- 「打开」强调色分割按钮（高 32、圆角 4）：左＝默认应用打开并关闭预览（同 Enter）；右 ▾＝「打开方式」列表（与右键「打开方式 ▸」同一 `openWithMenu`：始终使用的应用、已登记应用、始终使用…、系统选择器）。
- 「更多」菜单：

| 组 | 项 | 快捷键 | 不可用规则 |
| --- | --- | --- | --- |
| 定位/复制 | 在文件夹中显示 | — | 始终可用（助手卡文件会切换到所在文件夹并选中） |
| | 复制路径 | Ctrl+Shift+C | 始终可用 |
| | 复制 | Ctrl+C | 预览对象不是当前选中项时置灰 |
| 分享 | 分享 | — | 同上 |
| 属性 | 属性 | Alt+Enter | 同上 |

- × 关闭（Esc），最右。
- 图片底部只留缩放百分比；手势说明移入按钮提示。
- 经典风格对话框改为不透明 Layer 面（v2 保留清透玻璃）。

### 3.2 命令栏更多（查看更多）

对照 Windows 11 资源管理器「…」（约 6–7 项）与 Files：顶层不超过约 8 项、最多 3 组分割线；插件多项进子菜单；与命令栏外置按钮、「查看」菜单重复的项不重复出现。

#### 改前（本分支上一轮，默认启用插件）

顶层 **7 项**、3 组分割线（插件项平铺）：

```
撤销（Ctrl+Z）
────────
全选（Ctrl+A）
全部取消
────────
远程访问          ← remote 插件
复制远程地址      ← remote 插件
────────
属性（Alt+Enter）
设置（Ctrl+,）
```

若日后再有插件往 `MoreItems` 塞项，顶层会继续变长。

#### 改后（默认启用插件）

顶层 **6 项**、3 组分割线；插件叶子进「插件 ▸」：

```
撤销（Ctrl+Z）
────────
全选（Ctrl+A）
全部取消
────────
插件 ▸
  ├ 远程访问
  └ 复制远程地址
────────
属性（Alt+Enter）
设置（Ctrl+,）
```

插件全关时顶层 5 项、2 组分割线（无「插件 ▸」）。

#### 被移走的项去向

| 项 | 去向 |
| --- | --- |
| 远程访问、复制远程地址（及今后一切 `MoreItems`） | 「查看更多 ▸ 插件」子菜单（设置里关插件则整组消失） |
| 预览窗格、助手（更早一轮已移出更多） | 「查看 ▸ 显示」；助手另有命令栏按钮 |

示意对比图：`docs/assets/shots/CLASSIC-FLUENT/06-more-menu.png`。`chrome_more_test.go` 断言默认顺序、项数上限（≤8 项 / ≤3 分割线）以及无插件时的收拢。

### 3.3 查看 ▸ 显示

预览窗格（Alt+P）、助手 ｜ 文件扩展名、隐藏的项目、项目复选框。「查看」顶层不再单列助手。

### 3.4 右键菜单

- 文件：打开 Enter … ｜ 剪切 Ctrl+X、复制 Ctrl+C、粘贴 Ctrl+V、复制为路径 Ctrl+Shift+C、重命名 F2、**删除 Delete** ｜ 压缩、解压 ｜ 终端、刷新统计、插件、扩展 ｜ 属性 Alt+Enter。
- 空白处：新建 ▸ 文件夹 Ctrl+Shift+N … ｜ 粘贴 Ctrl+V … 刷新 F5。
- 标签：新建标签页 Ctrl+T、关闭标签页 Ctrl+W、重新打开关闭的标签页 Ctrl+Shift+T。
- 放大/缩小图标标注 Ctrl+Shift+= / Ctrl+Shift+-。

菜单是原生菜单，截图截不到，结构以本节为准；`quicklook_bar_test.go` 断言快速预览「更多」的顺序与分组。

## 4. 建议但未动（改变习惯较大或需设计确认）

1. **地址行与命令栏换位**：Windows 11 22H2+ 顺序是 标签 → 地址/搜索 → 命令栏。会改变肌肉记忆，暂不动。
2. **右键菜单顶部图标行**（剪切/复制/重命名/分享/删除横排，Windows 11 与 Files 同款）：mygo 原生菜单不支持，需自绘 MenuFlyout。
3. **右键加「分享」**：现在只在命令栏与快速预览「更多」。
4. **预览窗格加标题栏操作**（打开 ▾ / 更多），与快速预览共用 `quickLookOpenSplit`；窗格较窄，需确认宽度下的折叠规则。
5. **视频/文档/不支持类型的正文「打开」按钮**与标题栏「打开」重复：保留一版观察，再决定是否移除正文按钮。
6. **命令栏随宽度溢出**：窄窗口时把分享/删除等折入「更多」（CommandBar 动态溢出）。
7. **字号 Body 14**：Fluent 2 正文 14，当前基准 12（Caption）。整体放大影响信息密度，与「信息密集」偏好冲突，保持 12。

## 5. 经典外观：对照 Fluent 2 的调整

| 项 | 旧 | 新 | 规范 |
| --- | --- | --- | --- |
| 主题 Radius / 对话框 | 6 / 12 | 4 / 8 | ControlCornerRadius 4、OverlayCornerRadius 8 |
| 地址栏/搜索 `fieldRadius` | 6 | 4 | TextBox 圆角 4 |
| 页级卡 `cardRadius` | 12 | 8 | Card / Flyout 8 |
| 卡内软面 `innerRadius` | 6 | 4 | — |
| 内容层 | 白底直接贴窗口 | 左上圆角 8 + 顶/左 1px 描边（`classicContentFrame`） | NavigationView 内容框 |
| 导航选中 | 整行选中色 | + 左侧 3×16 强调色指示条 | NavigationViewItem SelectionIndicator |
| 强调色 | #0067C0 / #4CC2FF | #005FB8 / #60CDFF | Windows 默认强调色 AccentDark1 / AccentLight2 |
| 次要文字（深色） | #A0A0A0 | #C5C5C5 | TextFillColorSecondary（白 78.6%） |
| 选中色（深色） | #004C87 | #26415C | 降低饱和，接近资源管理器深色选中 |
| 快速预览底 | 清透玻璃 | 不透明 Layer（tokSurface） | ContentDialog 不透明 |

## 参考

- Fluent 2 / Windows 设计：<https://learn.microsoft.com/windows/apps/design/signature-experiences/geometry>、<https://learn.microsoft.com/windows/apps/design/signature-experiences/typography>、<https://learn.microsoft.com/windows/apps/design/signature-experiences/color>、<https://learn.microsoft.com/windows/apps/design/style/spacing>
- 控件：<https://learn.microsoft.com/windows/apps/design/controls/command-bar>、<https://learn.microsoft.com/windows/apps/design/controls/buttons#create-a-split-button>、<https://learn.microsoft.com/windows/apps/design/controls/menus-and-context-menus>、<https://learn.microsoft.com/windows/apps/design/controls/navigationview>、<https://learn.microsoft.com/windows/apps/design/controls/dialogs-and-flyouts/dialogs>
- Files（WinUI 开源文件管理器）：<https://github.com/files-community/Files>
- QuickLook for Windows：<https://github.com/QL-Win/QuickLook>
- macOS Quick Look：<https://support.apple.com/guide/mac-help/view-and-edit-files-with-quick-look-mh14119/mac>
- GNOME HIG 菜单：<https://developer.gnome.org/hig/patterns/controls/menus.html>；KDE HIG：<https://develop.kde.org/hig/>
