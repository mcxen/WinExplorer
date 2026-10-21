# 界面组件：先复用，再手写

晴谷的界面是 mygo 的 immediate-mode UI，加上 [Onevra](https://github.com/oneo-me/onevra) 的 `components`。新界面先在这里找。两边都没有、或者行为对不上文件管理器时，再手写。

包：

- `github.com/egoist/mygo/ui` —— 布局、基础控件、窗口内的菜单和列表
- `github.com/egoist/mygo` —— 窗口、托盘、全局快捷键、通知、更新
- `github.com/oneo-me/onevra/source/components` —— 已经画好的产品控件
- `github.com/oneo-me/onevra/source/icons/lucide` —— 图标

主题用 `theme.go` 的 `appTheme`。Onevra 控件读的是 `c.Theme()`，不要再给每个按钮写一套颜色。

## 设置

设置对话框用 `components.Settings`。新的一页是一个 `SettingsCategory`，新的一行是一个 `SettingsItem`：`Title`、`Description`，控件放在 `Build` 里。不要再写左侧导航、分组标题、分割线和「左文右控」的行。

开关、下拉、按钮行用 `settingsdialog.go` 里的 `settingsSwitchItem`、`settingsSelectItem`、`settingsActionItem`。整行都要自己排（例如扩展命令那一行有多个按钮）才用 `Content`。

## 直接用的控件

| 要做的事 | 用这个 | 不要再写 |
| --- | --- | --- |
| 模态对话框（遮罩、圆角、Esc、点外面关闭） | `components.Dialog`，本仓库包了一层 `floatDialog` | 自己铺一层半透明再居中一个盒子 |
| 确认框 | `mygo` 的消息框（删除确认已经在用） | 再做一个「确定 / 取消」对话框 |
| 按钮 | `components.Button` 的 `ButtonPrimary` / `ButtonSecondary` / `ButtonGhost` | `uikit.go` 那种手写底色的按钮；对话框底栏的一对可以继续用 `uikitDialogButtons`，新的单个按钮用 Onevra |
| 图标按钮 | `components.IconButton` | 28×28 的 `ButtonBase` 里塞一个 `Icon` |
| 开关 | `ui.Switch`；设置行用 `settingsSwitchItem` | 自己画滑块 |
| 分段选择 | `ui.Segmented` | 一排互斥按钮 |
| 下拉 | `ui.Select` | 自己做的弹出列表 |
| 单行 / 多行输入 | `components.TextBox`，或已经在用的 `ui.TextInputBase` / `ui.TextArea` | 自己画输入框边框 |
| 面包屑（只显示、点击上一级） | `components.Breadcrumbs` | 地址栏的路径段。编辑、历史、补全仍在 `chrome_nav.go` |
| 侧栏里的固定项 | `components.Sidebar`、`SidebarItem`、`SidebarSection` | 主文件夹、此电脑、回收站这种纯导航行 |
| 左右分栏 | `components.Split`（文件区已经在用） | 自己算拖拽宽度 |
| 图标 | `components.Icon` + `lucide` | 再内嵌一套 SVG |
| 角标、提示、加载 | `components.Badge`、`Tooltip`、`Spinner` | |
| 进度 | `components.Progress`、`ProgressRing` | |
| 图表 | `components/chart`（磁盘健康已经在用） | |
| 轻提示 | `c.Toast` / `c.ToastAction`（`main.go` 的 `view`） | 自己做的状态气泡 |
| 长列表 | `ui.List`（只构建可见行） | `ui.Column` 里把每一行都建出来 |
| 图标网格 | `ui.GridView` | |
| 菜单 | `ui` 的菜单（右键）和 `mygo.Menu`（托盘） | |
| 毛玻璃窗口 | `WindowOptions.Vibrancy`（`VibrancyAcrylic` 是磨砂，`VibrancyTabbed` 是主窗口） | 自己模糊一块背景 |
| 浮在所有应用之上的面板 | `WindowOptions.Panel`（快速搜索已经是这样） | 在主窗口里再盖一层假窗口 |
| 全局快捷键 | `mygo.GlobalShortcut` | 只在窗口有焦点时才生效的快捷键，用 `c.Shortcut` |
| 托盘 | `mygo.Tray`（`tray.go`） | |
| 自动更新 | `mygo/plugins/updater` | |

## 不要换成现成组件的

这些看起来像通用控件，但现成的那一版没有文件管理器要的行为。换了会丢功能。

| 现在的代码 | 为什么不换 |
| --- | --- |
| `files.go` 的详细信息、列表、图标 | `components.Table` / `TableVirtualized` 有列排序和拖列宽，没有框选、拖放、行内改名、分组、缩略图 |
| `navtree.go` 的文件夹树 | `components.Tree` 不虚拟化，展开就会把每一行都建出来。这里是按层懒加载，用 `ui.List` 滚动 |
| `chrome_tab.go` 的标签条 | `components.Tabs` 只是一排文字，没有关闭、拖动、把文件拖到标签上 |
| `searchbox.go` 的快速搜索 | `components/command` 是在一份固定命令里过滤。这里是流式的文件结果，带路径、大小、日期 |
| 无边框窗口的最小化 / 最大化 / 关闭 | mygo 和 Onevra 都没有这三颗标题按钮 |

`components/chat` 只用于右侧助手：`FloatingConversation` 和 `GlassComposer` 画对话页；草稿以 `/` 开头时由主包 `assistant_slash.go` 在输入框上方画命令菜单（不改 Onevra 按键），`Turn` 在回复进行时展开工具调用、完成后折叠，`Picker` 选择模型。回复正文用 mygo 的 `ui/markdown` 画：进行中 `markdown.Stream`，完成后 `markdown.View`，同一位置续接解析（`assistant_markdown.go`）。代码块用自己的「复制」头，回复里的路径变成链接，点开在本窗口定位。回复里的 OpenUI 是 `openui` 代码块，经 `markdown.Options.Code` 原位画成卡片；表单字段与 Settings 共用 `internal/forms`（TextBox、Select、Switch、Segmented、TimeField、PathField、DayChips 等，解析在 `internal/assistant/openui.go`，绘制在 `assistant_openui_paint.go` / `forms_paint.go`），也接受 `form` JSON 围栏。不嵌入 OpenUI 的 Web 运行时。卡片只回答一次：按下任一按钮后锁定，写「已选择「…」」；destructive 按钮先在原处再确认。offer_choices 的单选是单选按钮、点一下就发送，多选是复选框加「发送所选」，都带「其他」输入；rename_plan 是一张列出原名称和新名称的确认卡（`assistant_choice_paint.go`）。`components/command`、`calendar`、`color` 拾色器、`time` 选择器、`rating`、`honeycomb`、`graph`、`shaders`，以及 mygo 的 `game`、`terminal`、`sqlite` 插件，文件管理器目前用不上。不要为了用而用。

## 加一个设置项

在对应的 `settings*Groups` 里追加一项即可，例如：

```go
settingsSwitchItem("id", "标题", "一句说明",
    func() bool { return a.store.settings().SomeFlag },
    func(v bool) { a.applySettings(func(x *appSettings) { x.SomeFlag = v }) }),
```

`ID` 在同一组里不能重复。标题和说明会进设置对话框顶部的搜索，不要只写在控件自己的文字里。
