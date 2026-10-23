# 晴谷（Sunvale）界面与动效规范

本文是界面的**现行规范**，所有数值均对照 `origin/main`（OpenUI 骨架占位合入后）的代码核实（`文件:函数/常量`）。代码与本文不一致时：若代码是有意的改动，同一提交更新本文；否则以本文为准修代码。标注“规划中”的条目尚未实现，规格见 `docs/specs/`。

截图（离屏渲染，均为浅色主题）位于 `docs/assets/ui/`。

## 1. 设计令牌

### 1.1 颜色（`theme.go:resolveTokens`）

只通过 `colorXxx()` 访问函数取色，不写死色值。外观切换后 `theme.go:repaintAppearance` 会重新解析令牌并重绘所有窗口。

| 令牌 | 访问函数 | 浅色 | 深色 | 用途 |
| --- | --- | --- | --- | --- |
| tokWindow | `colorWindow()` | #F3F3F3 | #202020 | 窗口底色；标题栏、侧栏、状态栏同值（`tokChrome/tokNav/tokStatus`） |
| tokContent | `colorContent()` | #FFFFFF | #272727 | 文件列表底色 |
| tokSel | `colorSelected()` | #CCE4F7 | #004C87 | 选中行/磁贴 |
| tokHover | `colorHover()` | rgba(0,0,0,.06) | rgba(255,255,255,.06) | 悬停底色 |
| tokText | `colorText()` | #1A1A1A | #F3F3F3 | 主文字 |
| tokMuted | `colorMuted()` | #5C5C5C | #A0A0A0 | 次要文字、图标 |
| tokAccent | `colorAccent()` | #0067C0 | #4CC2FF | 主操作、焦点、链接 |
| tokDivider | `colorDivider()` | #E5E5E5 | #383838 | 分隔线、次要边框 |
| tokFolder | `colorFolder()` | #F0C14D | #E8B84A | 文件夹图标 |
| tokAddress | `colorAddress()` | #FFFFFF | #2D2D2D | 地址栏、浮层表面 |
| tokBtnText | `colorBtnText()` | #FFFFFF | #1A1A1A | 主按钮文字 |
| tokFieldBorder | `colorFieldBorder()` | #D6D6D6 | #383838 | 输入框边框 |
| tokHit | `colorHit()` | #FFE28A | #8A6D00 | 搜索命中高亮 |
| tokSurface | （无访问函数，经 `appTheme` 作 `Background`） | #F9F9F9 | #2C2C2C | 应用自身弹层底色 |
| tokDanger | 经 `c.Theme().Danger` | #C42B1C | #FF5C5C | 危险操作、错误 |
| tokWarning | 经 `c.Theme().Warning` | #C77700 | #FFC83D | 警告 |
| tokSuccess | 经 `c.Theme().Success` | #0F7B0F | #6CCB5F | 正常/成功 |
| — | `colorCloseHover()` | #C42B1C | #C42B1C | 窗口关闭按钮悬停 |
| — | `addressbar.go:colorPressed()` | 文字色 α0.1 | 同左 | 按下态 |

mygo/Onevra 组件主题（`theme.go:appTheme`）：`SurfaceHover = tokAddress.Mix(tokText, .06)`，`SurfacePressed = …0.12`，`AccentHover = tokAccent.Mix(tokText, .12)`，`AccentPressed = …0.24`，`Focus = tokAccent α0.6`，`Scrollbar = tokText α0.32`，滚动条宽 6。高对比度偏好：边框与次要文字向主文字混合 0.45/0.4，焦点不透明。

### 1.2 字体与字号

- 字体由 mygo 按平台选择系统 UI 字体（Windows：Segoe UI Variable / 微软雅黑 UI；macOS：苹方；Linux：Noto Sans CJK SC）。不在代码里指定字体族。
- 基准字号 12（`appTheme.FontSize`），随系统文字缩放 `Preferences().TextScale` 等比放大。
- 常用字号：列表与侧栏 12；次要信息、搜索路径、页脚 11；搜索框输入 13；页面标题 18 / 粗细 600（如 `diskhealth.go:disksHeader`）。

### 1.3 圆角与间距

- `appTheme.Radius = 6`（对话框由组件加倍为 12），`Spacing = 4`（组件内边距按此单位）。
- 行、按钮、标签：圆角 4；地址栏输入框 6；下拉/浮层 8；代码块与 OpenUI 占位 6。
- 行内边距 `files.go:rowInset = 4`。

### 1.4 尺寸

| 项 | 值 | 位置 |
| --- | --- | --- |
| 标题栏/标签条高 | 40；单个标签高 32，宽 96–220 | `chrome_tab.go:tabStrip`、`tabMinW/tabMaxW` |
| 窗口控制按钮 | 46×40 | `chrome_tab.go:winControl` |
| 命令栏 | 高 44，按钮高 32 | `chrome_tab.go:commandBar`、`commandMenu` |
| 导航栏（前进后退+地址栏+搜索） | 高 44；地址栏 32；面包屑按钮 24；文件夹搜索框 220×32 | `chrome_nav.go`、`addressbar.go:addressField`/`crumbButton` |
| 侧栏 | 宽 180–420，行高 32，树每级缩进 14，最多 96 | `sidebar.go:navMinWidth/navMaxWidth/navRowH`、`navtree.go:navTreeStep/navTreeMaxIndent` |
| 详细信息行 | 30，紧凑视图 22 | `files.go:detailsRowH` |
| 列表视图行 | 28，紧凑视图 22 | `files.go:listRowH` |
| 详细信息列 | 名称弹性（最小 160），修改日期 168，类型 120，大小 96，位置 200 | `files.go` 常量 |
| 图标视图 | 小 32 / 中 48 / 大 72 / 超大 96，可放大 1–4 倍 | `files_icon.go:iconBaseSize`、`tabs.go:iconZoomSteps` |
| 预览窗格 | 宽 220–460 | `preview.go:previewMinW/previewMaxW` |
| 状态栏 | 高 28，字号 12 | `status.go` |
| 助手面板 | 默认宽 380，最小 320 | `assistant.go:assistantPaneW/assistantMinW` |
| 快速搜索 | 960 宽（独立面板 960×560），列表高 120–384，结果行高 44：24px 图标、完整文件名与相对路径、150px 右对齐的“大小 · 类型 / 修改日期”；分组标题行高 24；右侧预览宽 270 | `searchbox.go:searchHitRowH`、`searchDisplayRowAt`、`searchHitMetaW`、`searchpreview.go` |
| 地址建议下拉 | 与地址栏同宽，最高 360，行高 28 | `addressbar.go:pathDropdown` |
| 助手斜杠菜单 | 与输入框同宽，最高 220，行高 28 | `assistant_slash.go:assistantSlashMenu` |
| 按钮最小宽 | 80 | `uikit.go:btnMinW` |

## 2. 布局区域

自上而下：标题栏/标签条 → 命令栏 → 导航栏 → 主体（侧栏 | 文件窗格 | 预览窗格 | 助手面板）→ 状态栏。

![详细信息视图](assets/ui/details.png)

- **标题栏/标签条**（`chrome_tab.go:tabStrip`、`tabItem`）：标签可拖动排序、中键关闭、双击空白新建、溢出时收窄并可横向滚动；标签悬停提示完整路径（`tabTooltip`）。特殊页的标签名见 §5。右侧空白区可拖动窗口（`DragWindow`）。
- **命令栏**（`commandBar`）：新建、剪切/复制/粘贴/重命名/分享/删除、排序、查看、更多（`moreButton`）。图标按钮不可用时置灰。
- **导航栏**：后退/前进/上一级/地址栏/刷新/文件夹搜索。地址栏两种状态：面包屑（`breadcrumbBar`，溢出折叠为「…」菜单 `crumbOverflow`，分隔箭头弹出子文件夹菜单 `crumbSeparator`）与编辑（`pathEditor`，补全下拉 `pathDropdown`，行内错误提示）。
- **侧栏**（`sidebar.go:sidebar`）：主文件夹、画廊、快速访问（可拖放固定）、最近位置、此电脑（卷 → 目录树）、回收站；虚拟列表。
- **文件窗格**：详细信息（`files_details.go`）、列表（`files_list.go`）、小/中/大/超大图标（`files_icon.go`）、画廊（`gallery.go`）。
  - 文件类型图标（`icons.go:fileTint` / `drawPDFSheet` / `drawHTMLSheet`）：PDF 红标、HTML/HTM 浏览器橙标，常见 Office 扩展独立配色；详情/列表 16px 只用自绘。
  - 中等及以上图标（`thumb.go:mediaThumb`，`box >= thumbMinBox`）：PDF 尽量显示首页（Quick Look / `pdftoppm`），HTML/HTM 尽量显示本地 favicon（`<link rel=icon>` 或同目录 `favicon.ico` / data URI）；失败回退自绘。

  ![大图标](assets/ui/large-icons.png) ![画廊](assets/ui/gallery.png)
- **预览窗格**（`preview_pane.go`）：右侧可调宽。
- **状态栏**（`status.go`）：左侧项目数/选中数与大小，搜索时为搜索状态（`search.go:searchStatusLabel`）；右侧当前视图名。
- **助手面板**（`assistant.go`）：右侧，Markdown 回复与 OpenUI 卡片。
- **快速搜索 Ctrl+K**（`searchbox.go`、`searchpreview.go`）：有应用运行时为独立无边框磨砂面板（`ensureSearchPanel`，960×560），测试中为窗口内对话框（`searchBoxModal`）。查询行右侧是「相关度 / 按文件夹 / 按类型 / 名称」排序切换与「本文件夹 / 全部磁盘」范围切换；查询行下方是高 22、圆角 11、边框 `colorFieldBorder()` 的「类型 / 时间 / 大小 / 位置」筛选 chip，激活时背景 `colorSelected()`，右侧计数为「x / y 个结果」。按文件夹或按类型时，结果前插入高 24 的分组标题行，字号 11、粗细 600、文字 `colorMuted()`，显示组名与组内数量；标题不是命中，`↑`/`↓` 跳过它们。每行第一行显示完整文件名（扩展名用次要文字色）和 16 高、10 号字的命中原因标签（“文件名”为强调色，“路径”为次要色，背景 `colorHover()`），第二行显示相对搜索根的面包屑，悬停显示完整路径。路径分段是 16 高按钮：悬停背景 `colorHover()`、文字 `colorAccent()`，点击关闭面板并导航到该段；省略段不可点击，悬停提示被省略的文件夹。只有路径命中时，第二行路径中的命中片段用 `colorHit()` 标出。窗口宽 ≥900 时右侧为 270 宽预览列：顶部缩略图高 120（非媒体用 56px 文件图标），下方是高亮名称、类型/大小/修改/位置、11px 等宽完整路径，再放「打开 / 打开所在位置 / 复制路径」三个按钮与「排序依据（得分 n）」原因列表；窄于 900 时隐藏预览，结果列表独占面板。

  ![快速搜索现状](assets/ui/search-palette.png)
- **对话框**：统一走 `glass.go:floatDialog` / Onevra `components.Dialog`；底部按钮用 `uikit.go:uikitDialogButtons`（主按钮在右）。

## 3. 组件状态

| 状态 | 规则 |
| --- | --- |
| 悬停 | 底色 `colorHover()`（`files.go:paintSelect`、各按钮 `Hovered()`）；可点击的文字可改 `colorAccent()` |
| 按下 | 组件主题 `SurfacePressed` / `colorPressed()` |
| 选中 | 底色 `colorSelected()`；选中优先于悬停（选中行悬停不变色） |
| 焦点 | 键盘焦点环 `Theme.Focus`（强调色 α0.6）；输入框获焦边框为强调色 |
| 不可用 | 组件自带置灰，且不响应输入；不要用隐藏代替禁用（命令栏按钮保持位置） |
| 错误 | 文字/边框 `Theme.Danger`；地址栏错误在下拉位置行内显示，不弹窗 |
| 加载 | `components.Spinner(SpinnerRing)` + 次要文字（如“正在搜索…”、“正在生成界面…”） |

### 键盘

- 文件窗格（`main.go:handleKeys`、`keynav.go`）：方向键移动、Shift 扩选、Enter 打开、F2 重命名、Delete 删除到回收站、Ctrl+C/X/V、Ctrl+Z 撤销、Alt+←/→ 后退前进、Alt+↑ 上一级、Ctrl+F 文件夹搜索、Ctrl+K 快速搜索。获焦输入框优先消费按键；模态对话框打开时按键归对话框。
- 标签页（`tabkeys.go`）：Ctrl+T 新建、Ctrl+W 关闭、Ctrl+Tab/Ctrl+Shift+Tab 切换、Ctrl+Shift+T 重新打开、Ctrl+1–9。
- 地址栏编辑（`addressbar.go:pathEditorKey`）：↑↓ 选建议、Tab/→ 接受、Enter 前往、Esc 取消；失焦即退出编辑。
- 快速搜索（`searchbox.go:searchBoxKeys`）：↑↓、PageUp/PageDown、Enter 打开、Ctrl+Enter 定位、Esc 关闭。
- 助手输入框（Onevra `chat.GlassComposer`，`assistant.go:assistantComposer` / `assistant_slash.go`）：Enter 发送、Shift+Enter 换行；草稿为单行 `/…` 时上方弹出命令菜单（含 `/分类` `/立即执行`），`↑`/`↓` 选择、`Enter` 选用、`Esc` 关闭（关闭后同一草稿不再弹出，直到离开斜杠查询）。回答中且草稿非空时 Trailing 提供停止。
- 侧栏方向键导航（`navkeys.go`）：点击侧栏后 `↑`/`↓`、`Home`/`End` 移动焦点，`→` 展开或进入子级，`←` 折叠或回到父级，`Enter`/空格导航，`Tab` 交还焦点；移动不改变当前标签（`docs/specs/02-tree-polish.md` C）。
- 空格快速预览（`quicklook.go`、图片见 `imageview.go`、视频见 `videoview.go`）：`空格` 打开/关闭；`Esc` 或点外部关闭。图片：滚轮缩放（1×–8×，适配为 1×）、拖拽平移、`←`/`→` 或顶栏按钮切换**同目录图片**；坏图显示 `ImageOff` 占位。视频：关键帧 + 细进度条 scrub 抽帧（`ffmpeg`），按钮「用系统播放器打开」（无内嵌播放器）；点画面播放提示同样走系统播放器。非图片/非视频：`←`/`→` 步进可见项；音频打开时为快退/快进 5 秒。侧栏预览窗格（`preview_pane.go`）选中图片时只做 Contain 适配大图；选中视频时显示关键帧海报 +「用系统播放器打开」，不重复 scrub 控件。

## 4. 中文文案

- 全部界面文字为简体中文，术语对齐 Windows 11 资源管理器：新建、剪切、复制、粘贴、重命名、删除、排序、查看、详细信息、列表、大图标、紧凑视图、属性、打开所在文件夹、在文件夹中显示、复制路径。
- 标点用全角（，。：；“”（）），数字与单位之间空格（`181 KB`）；省略号用“…”。
- 计数：“找到 11 个项目”“已选择 3 个项目”。
- 时间格式 `2026/10/20 14:00`（`formatModTime`）。
- 错误提示说清原因和下一步，不出现英文原始错误串或错误码；需要时放在“详细信息”中。
- 按钮用动词短语（“应用方案”“重建索引”），不用“确定/OK”泛称，除非是纯确认。

## 5. 平台命名

| 对象 | Windows | macOS | Linux | 位置 |
| --- | --- | --- | --- | --- |
| 根卷 | `本地磁盘 (C:)`，有卷标时用卷标（“新卷”视为无卷标） | `Macintosh HD` | `文件系统`（`/`） | `sys_windows.go`、`sys_darwin.go:sysRootName`、`sys_linux.go` |
| 特殊文件夹 | 主文件夹、桌面、下载、文档、图片、音乐、视频 | 同 | 同 | `fs.go`（显示名映射） |
| 特殊页 | 此电脑、主文件夹、画廊、回收站、磁盘健康 | 同 | 同 | `chrome_tab.go:tabTitle`、`fs.go` 面包屑 |

磁盘名、文件名保持原样，不翻译。

## 6. 动效

通用规则：动画在绘制时读状态并按需请求帧（`Painter.AnimationFrame` / `c.After`），**不得**用循环 `repaint()` 驱动；遵守系统“减少动画”（`c.Preferences().ReduceMotion`）——只保留终态。

### 6.1 已实现

| 动效 | 触发 | 时长 | 曲线 | 数值 | 实现 |
| --- | --- | --- | --- | --- | --- |
| AI 处理呼吸光 | 助手工具正在处理某路径（`aiFx.markBusy`）；`endRun` 后仍亮满 `aiFxMinGlow` | 淡入 200ms，周期 1.6s（`aiFxPulse`），至少亮 900ms（`aiFxMinGlow`） | 余弦呼吸 | 强度 0.35–1；贴文件行**右侧内缘**（列表区内、助手窗格左侧）宽 64 DIP 的渐变带，距行右缘内缩 10 DIP（`aiFxGlowBand`/`aiFxGlowInset`），浅色端点 `#4CC2FF` α 0.55×强度（深色为强调色）；文件窗格 `Clip` 防止行溢出盖住助手 | `aifx.go:breath`、`aiFxRow`、`paintAIGlow`；截图 `docs/assets/shots/FX1/` |
| 重命名高亮 | 助手改名完成（`markRenamed`） | 1.2s（`aiFxFlash`） | 线性衰减 | 名称颜色由强调色线性回到原色 | `aifx.go:look`、`flashColor` |
| 移出（删除/移动） | 项目被移走（`markLeaving`） | 滑出 340ms（`aiFxSlide`）+ 折叠 320ms（`aiFxFold`） | 滑出 easeIn（t²），折叠 easeOut | 左移 1.05×行宽、透明度 1→0；行高 → 0（磁贴保留格位） | `aifx.go:look`、`aiFxRow`；`pendingLeave` 让刷新等动画结束 |
| 减少动画 | `ReduceMotion` 开 | — | — | 呼吸光静止为 0.8；无滑出/折叠，到时直接消失 | `aifx.go:aiFxRow` |
| 回复流式显示 | 模型输出 token | 合并重绘最多每 33ms 一次（`assistLiveEvery`） | — | 未闭合的 choices/OpenUI/代码围栏先隐藏，避免标记闪现 | `assistant_run.go:noteAssistLive`、`flushAssistLive`、`streamLive`、`streamShown` |
| OpenUI 生成占位（骨架） | 流式中出现未闭合 `openui`/`form` 围栏（`CodeBlockView.Open`） | 围栏闭合为止；闭合后交叉淡入淡出 `assistOpenSkelFade`=180ms | 条带：`Loop("pulse", aiFxPulse=1.6s, Bounce(EaseInOut))`；替换：`EaseOut`（Transition 默认） | 最小高 64、圆角 8、`colorSurface()` 底、1px `colorDivider()` **虚线**边框（`BorderStyle(BorderDashed)`，同 mygo gallery 放置区）；内 3 条骨架条（高 8、圆角 4、间距 8、上下内边距 10，宽约 100%/68%/42%），`colorMuted()` α 0.10–0.24 随脉冲；`ReduceMotion` 条带停在 α≈0.19；闭合后骨架 `Exit` 淡出、卡片 `assistOpenReveal` `Enter` 淡入，避免硬切 | `assistant_markdown.go:assistOpenPlaceholder`、`assistOpenSkelBar`、`assistOpenReveal`；截图 `docs/assets/shots/OPENUI-SKEL/` |
| 标签拖动 | 按住标签拖动 | 实时 | 无插值 | 越过半个标签宽（标签宽+2）与相邻标签交换 | `chrome_tab.go:tabDragFrame` |
| 地址建议下拉 | 编辑地址时有建议或错误 | 即时出现 | — | 浮层圆角 8、阴影 (0,8,16) α0.14、上边距 4 | `addressbar.go:pathDropdown` |
| GIF 播放 | 预览/缩略图中的 GIF | 按帧延时，默认 100ms，最小 10ms | — | — | `gifplay.go` |
| 搜索索引进度条 | 索引正在构建 | 不定进度 1.4s 往返 | 余弦往返 | 80×4、`colorAccent()` 填充；有估计时定值填充并显示百分比；`ReduceMotion` 下静止 | `searchbox.go:searchIndexProgress`、Onevra `components.Progress` |
| 树展开箭头旋转 | 展开/折叠目录树节点 | 150ms | easeOut | 始终使用右箭头，0°→90°；`ReduceMotion` 直接到终值 | `icons.go:chevronIconAnimated`、`sidebar.go:navTwist` |
| 树缩进引导线淡入淡出 | 鼠标进入/离开导航窗格 | 120ms | easeOut | 每级 1px `colorDivider()`；当前选中行祖先链用 `colorMuted()`；`ReduceMotion` 直接到终值 | `sidebar.go:navIndentGuides` |
| 拖动文件悬停标签自动切换 | 文件拖动在非当前标签上持续悬停 | 600ms | 到时切换 | 离开或放下清零；等待期间用 `colorHover()`，用 `c.After` 只请求到时帧 | `chrome_tab.go:tabDropHoverFrame` |

![呼吸光](assets/ui/motion-glow.png) ![移出](assets/ui/motion-slideout.png) ![改名高亮](assets/ui/motion-rename-flash.png)

助手打开时的验证截图：![助手旁呼吸光](assets/shots/FX1/01-glow-assist-open.png) ![移出中](assets/shots/FX1/02-slideout-mid.png) ![改名高亮](assets/shots/FX1/03-rename-flash.png)

![流式回复](assets/ui/assistant-streaming.png) ![内联卡片](assets/ui/assistant-card.png) ![地址补全](assets/ui/address-autocomplete.png)

### 6.2 规划中

| 动效 | 时长/数值 | 规格 |
| --- | --- | --- |
| 树平滑滚动到当前位置 | 跳过 | mygo v0.5.5 `ListState` 只有 `ScrollTo/ScrollIntoView/ScrollToEnd`，没有当前偏移读写 API，无法在不 fork 依赖的情况下插值；按规格 D 记录并保留瞬时滚动 |

## 7. 反模式 / 禁止

1. 界面中使用 emoji 或彩色符号代替图标（统一 Lucide 图标 `components.Icon`）。
2. 写死色值（`ui.Hex("#...")`）而不走令牌——唯一例外是 `paintAIGlow` 浅色渐变端点与 `colorCloseHover`。
3. 在左对齐内容上方放居中的分节标题；标题与内容同一左边线。
4. 在界面上显示内部 id、哈希、键名、原始英文错误、Go 类型名或调试路径（如 `tab-3`、`hit-/path`、`context deadline exceeded`）。
5. 隐藏扩展名导致同名文件无法区分的场合（搜索结果、确认对话框）不显示扩展名。
6. 从头截断路径（丢掉盘符/主文件夹）；长路径用中间省略并提供悬停完整路径。
7. 用循环 `repaint()` 或 goroutine 定时驱动动画；忽略 `ReduceMotion`。
8. 动画改变布局时不请求重建（只重绘），导致跳帧。
9. 弹窗报告可在行内显示的错误（地址栏、重命名冲突）。
10. 英文或中英混杂的按钮/菜单文案；“OK/Cancel”。
11. 用隐藏代替禁用，使命令栏按钮位置跳动。
12. 点击后列表或侧栏发生非用户触发的滚动跳动（见 `f69b01d` 修复）。



## 规范表单（FORM1）

设置「定时任务」添加/编辑与助手 OpenUI Form / `present_form` 共用 `internal/forms` 字段种类与 `forms_paint.go` 绘制：TextBox、Select、Switch、Segmented、路径浏览。新表单优先声明 schema，不要再手写一排无标签 TextBox。规格见 `docs/specs/08-forms.md`；截图 `docs/assets/shots/FORM1/`。

## OpenUI 场景演示（OPENUI2）

在 FORM1 之上扩展日期、文件类型 chips、标签、滑块、进度、表格多选等控件；`present_form` 提供 rename/share/search/classify/backup 等预设。助手斜杠「表单·…」可离线打开。无家目录下载/文档文件夹时，默认路径落在临时沙箱（见 `docs/specs/08-forms.md` OPENUI2）。截图 `docs/assets/shots/OPENUI-DEMO/`。

## 设置 · 助手（A4）

设置对话框在「插件」与「更新」之间增加 **助手** 分类（`settingsdialog.go:settingsCategories`，实现见 `settings_assistant.go`）：

- 侧栏标题「助手」，图标 `lucide.Bot()`，ID `assistant`。
- 分组：模型与密钥、系统提示词、Skills、MCP、行为与记忆、解压密码本。
- 附加提示词为多行输入（`TextBox` Multiline，`Lines(1, 6)`）；密钥与解压密码输入为 Password，保存后列表不回显正文。
- 助手窗格顶栏用「助手设置」打开本分类；不再提供供应商 / 解压密码抽屉。会话抽屉保留；记忆区在 Skills 关闭 memory 时隐藏。

截图：`docs/assets/shots/A4/`。

## 助手对话窗口（CHAT1）

实现：`assistant.go:assistantExchange`、`assistant_opcard.go`；离线演示 `assistant_demo.go`（`SUNVALE_ASSIST_DEMO=1`）。截图 `docs/assets/shots/CHAT-DEMO/`（`before-*` 为改版前）。

- **层次**：用户消息沿用 Onevra `chat.Bubble`（右对齐、圆角 12、`Background.Mix(Text,.06)`），气泡底部一行 11 号 `colorMuted()` 时间 + 复制按钮（`chat.IconAction`，复制后 1.5s 内显示对勾）。助手回复不加气泡，左对齐，首行为发言人行：20×20 圆形头像（强调色 α0.14 底、`lucide.Bot` 12）、「晴谷助手」12/600、时间 11；演示模式加 10.5 号「演示」标签。回复完成后 Turn 的 Actions 槽放复制按钮。
- **时间**：`Turn.At`；当天显示 `15:04`，更早显示 `1/2 15:04`（`assistClock`）。
- **文件操作卡片**（`assistOpCard`）：圆角 8，`colorAddress()` 底，1px `colorDivider()` 边框；执行中边框强调色 α0.5 且整卡呼吸（`breathOverlay`，与文件行同一节拍），待确认的危险操作与失败为 Danger α0.6。头部：24×24 圆角 6 的图标块（强调色 α0.12，删除为 Danger），标题 12.5/600（新建文件 / 修改内容 / 重命名 / 移动 / 删除到回收站 / 批量操作 / 读取内容，多项时加「· n 项」），路径 11 号相对当前文件夹显示、超长中间省略；右侧状态胶囊高 20、圆角 10、对应色 α0.12 底：待确认（Warning）、执行中（强调色呼吸点）、完成（Success）、失败（Danger）、已撤销 / 已取消（`colorMuted()`）。
- **卡片正文**：新建给前 8 行内容预览；修改给行级对比（`internal/assistant.DiffLines` + 1 行上下文，最多 12 行，删除行 Danger α0.1 底、新增行 Success α0.1 底）；重命名/移动给「旧名 → 新位置」（新位置强调色 600）；批量/删除/读取给逐项列表（最多 6 项，其余「…以及另外 n 项」），执行中上方是 `components.Progress` 与「已完成 / 总数」。
- **确认与撤销**：危险操作待确认时正文下方是 Danger α0.08 的提示条（`TriangleAlert` + 说明），底部右对齐「取消」（`uikitSecondaryButton`）与 Danger 底的主按钮（删除为「移到回收站」）。完成且本窗口持有撤销记录时底部为「已记录，可撤销」+ 26 高「撤销」按钮（`lucide.Undo2`）；撤销后状态变「已撤销」，逐项图标变撤销箭头。
- **执行计划**（`assistPlanCard`）：同卡片外观，标题「执行计划」与「完成数 / 总数」；每行 22 高：完成 `CircleCheck`（Success）、进行中呼吸点且文字 600、未开始 `Circle`（muted）、失败 `CircleX`（Danger）。
- **进行中提示**：有卡片的一轮把「正在…」行放在卡片下方（呼吸点 + 12.5 号 muted 文字），而不是 Turn 默认的卡片上方。
- **当前目录条**（`assistContextBar`）：输入框正上方，高 22：文件夹图标（`colorFolder()`）、「当前目录」11 muted、目录名 11.5/600、「n 个项目 · 已选择 m 个」；悬停提示完整路径。特殊页不显示。
- **空状态**（`assistantEmpty`）：左对齐头像 + 「晴谷助手」18/600 + 一句能力说明；「试试这样问」11/600 小标题下六张示例卡（新建、修改、重命名并归类、删除临时文件、批量、总结），卡片 `colorAddress()` 底、图标块 26×26、第二行是会填入输入框的原话；点击只填入不发送，悬停提示说明。
- **与文件列表动效对应**：卡片执行中 ↔ 行右缘呼吸光（`aiFx.markBusy`）；重命名完成 ↔ 新名称强调色高亮（`markRenamed`）；移动/删除完成 ↔ 行滑出收起（`markLeaving`）；一轮结束 ↔ `aiFx.endRun()` + `reloadAfterFx`。
- **布局修复**：对话列在横向 Row 中需 `FillHeight()`，否则长对话把顶栏覆盖、输入框挤出窗格（`assistantPane` 的 `conv`）。

## 图片快速查看（IMAGE1）

交互分工：侧栏预览窗格选中即显大图（Contain）；`空格` 弹出轻量层做缩放/平移/同目录翻页，二者不重复同一套控件。

- **格式**：JPEG/PNG/GIF/WebP/BMP（`decodeBytesScaled`，与 OpenUI `ImageField`/`formDecodeBitmap` 同路径）；HEIC/TIFF/AVIF/ICO 等走系统缩略图兜底（非 macOS 可能仅占位）。
- **快捷键**：见 §3「空格快速预览」。
- **截图**：`docs/assets/shots/IMAGE1/`。

## 视频关键帧预览（VIDEO1）

交互分工与 NOTE（`assistant-float-mockups/preview-demo/NOTE.md`）一致：空格看一眼（关键帧 + scrub）、侧栏看详情海报、回车/双击/「用系统播放器打开」真正播放。不做内嵌完整播放器。

- **格式**：mp4/m4v/mov/mkv/avi/webm/wmv/flv（`kindByExt` → `previewVideo`）；抽帧走 `media.go:videoFrameAt`（`ffmpeg`），无 ffmpeg 时侧栏/空格优雅降级为占位 + 系统打开。
- **空格层**（`videoview.go`）：海报/当前帧、细进度 scrub（防抖约 120ms 再抽帧）、底部「用系统播放器打开」；`←`/`→` 步进相邻可见项。
- **侧栏**：Contain 海报 +「用系统播放器打开」提示时长；不重复 scrub。
- **截图**：`docs/assets/shots/VIDEO1/`；方案示意亦见 `docs/assets/shots/PREVIEW-DEMO/preview-video.png`。

## 文档快速预览（DOCPREV1）

与 NOTE 和 `PREVIEW-DEMO/preview-doc.png` 一致：空格看几页、侧栏看首页、回车/双击交给系统默认应用。

- **格式**：pdf；doc/docx/xls/xlsx/ppt/pptx/odt/ods/odp/rtf（`previewDoc`）。PDF 直接栅格化（`pdfthumb.go` 的 pdftoppm，macOS 首页可走 Quick Look）；Office 先用 `soffice --headless --convert-to pdf` 转 PDF，按路径+大小+修改时间缓存在临时目录。
- **空格层**（`docview.go`）：每屏 3 页白底页卡，左右圆形 ‹ › 按钮或 PageUp/PageDown 翻页，最多预览前 30 页；下方「第 a–b 页 / 共 N 页」、「打开」按钮与提示「首页栅格预览 · PageUp/PageDown 翻页 · ←/→ 相邻文件 · 回车 系统默认打开 · 空格/Esc 关闭」。叠层内回车用系统默认应用打开并关闭叠层。
- **降级**：无 LibreOffice 时显示文件图标 +「未安装 LibreOffice，无法在应用内预览 Office 文档」+「打开」；单页渲染失败显示「此页无法渲染」。
- **文本/代码**：等宽字体（`monoFont`），制表符展开为 4 空格；超过 64 KB 截断并提示「文件较大（约 X），仅显示前 64 KB」。html/htm 按文本显示。
- **侧栏**：首页缩略图 +「打开」+「首页缩略图 · 空格翻页预览」；生成失败时写明原因。
- **截图**：`docs/assets/shots/DOCPREV1/`（01 PDF 首屏、02 PageDown 翻页、03 docx 经 LibreOffice、04 代码等宽、05 侧栏首页、06 无 LibreOffice 降级）。

## OpenUI 媒体控件（OPENUI3）

在 OPENUI2 之上增加：

- **列表 `list` / `ListField`**：文件名 + 可选大小与 32px 缩略图；多选开关或单选；空 options 显示空状态。
- **图片 `image` / `ImageField`**：卡片内嵌预览（本地路径或 data URL），高度约 160、`Fit(Contain)`；坏图占位。
- **视频 `video` / `VideoField`**：关键帧（`ffmpeg`，见 `media.go:videoPosterImage`）+「打开播放」；无内嵌播放。限制写在本段与 `docs/specs/08-forms.md` OPENUI3。

演示预设：`filelist` / `image` / `video`。截图 `docs/assets/shots/OPENUI-MEDIA/`。
