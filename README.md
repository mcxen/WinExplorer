# WinExplorer

一个用 [MyGo](https://github.com/egoist/mygo) 纯原生 UI 实现的文件管理器，完全参考 Windows 11 文件资源管理器的布局与排版。界面用 Go 绘制，没有 HTML、没有 WebView、没有 cgo。

## 运行

本项目需要 Go 1.27 或更高版本（MyGo 的要求）。

```bash
cd /Users/mcx/Documents/OpenSpring/WinExplorer
go run .
```

如果你的 Go 低于 1.27，但已通过 `GOTOOLCHAIN` 可用新版，可以这样：

```bash
GOTOOLCHAIN=go1.27.2 go run .
```

构建单文件二进制：

```bash
go build -o winexplorer .
```

> 这是一个 macOS 上运行、但外观仿照 Windows 11 资源管理器的应用。侧边栏的“此电脑”会列出本机的磁盘（macOS 上即 `/` 与各 `/Volumes` 挂载），“快速访问”列出家目录下的桌面、下载、文档、图片、音乐、视频。

## 界面布局

从上到下，完全对齐 Windows 11 资源管理器：

1. **标签条** —— 每个打开的目录一个标签，当前标签高亮，每个标签带关闭按钮，`+` 新建标签。
2. **命令栏** —— `新建 | 剪切 复制 粘贴 重命名 分享 删除 | 排序 查看`，图标加中文标签，分组之间用 1px 细分隔线。
3. **地址导航行** —— 后退、前进、向上，可点击的面包屑地址栏（点击空白处或编辑按钮可输入路径），以及当前文件夹内的搜索框。
4. **主体** —— 左侧 Windows 风格导航窗格（可拖动调整宽度，180–420），中间文件区，右侧预览窗格（`Alt+P` 切换，可拖动 220–460）。
5. **状态栏** —— 项目数量、选中数量与体积，以及当前视图模式。

## 功能

- **多标签**：新建（`Ctrl+T`）、关闭（`Ctrl+W`）、切换（`Ctrl+Tab` / `Ctrl+Shift+Tab`），每个标签独立的导航历史与选择。
- **导航**：双击进入文件夹、面包屑跳转、地址栏直输路径、前进/后退/向上、刷新（`F5`）。
- **选择**：单击、`Cmd/Ctrl+单击` 多选、`Shift+单击` 范围选、`Cmd+A` 全选。
- **查看**：详细信息、列表、小/中/大/超大图标；按名称/修改日期/类型/大小排序，可升序降序。
- **操作**：新建文件夹、重命名（`F2`）、复制/剪切/粘贴（Windows 风格“名称 - 副本”）、删除（移入废纸篓而非直接删除）、内联重命名。
- **键盘**：`Enter` 打开、`Delete` 删除、`Backspace`/`Alt+↑` 向上、退格/方括号 前进后退。
- **显示**：可切换显示隐藏项目（以 `.` 开头的文件）。
- **预览窗格**：选中单个文件时在右侧显示其预览与属性，随选择实时切换——
  - 图片：PNG/JPEG/GIF/WebP/BMP 走 MyGo 的 `ui.DecodeBitmap`（含 JPEG EXIF 方向修正）；HEIC/AVIF/TIFF/SVG 走系统 Quick Look（`qlmanage -t`）出 PNG 后解码。
  - 视频：`ffmpeg` 抽 1 秒处的海报帧 + `ffprobe` 读时长/分辨率/编码，`qlmanage` 兜底；面板提供“打开”按钮调用系统播放器（与 Explorer 的预览窗格一致：静态帧 + 元数据，不内嵌播放）。
  - 音频：WAV/MP3/Ogg Vorbis 用 MyGo 的 `audio` 包（纯 Go、无 cgo）**内置播放**——播放/暂停按钮、点击进度条跳转、`空格` 播放/暂停、`←/→` 快退快进 5 秒；不支持的格式回退“打开”按钮。
  - 文本/代码：只读文本框显示前 64 KB，附“约 N · 已显示前 M”提示。
  - PDF/Office 文档：Quick Look 渲染首页 + “打开”按钮。
  - 任意文件：文件名、类型、大小、修改/创建日期、位置，图片/视频/音频再附加尺寸、页数、时长、分辨率、编码等元数据（`mdls` / `ffprobe`）。
  - 加载全部在后台 goroutine 完成，结果以代际号（gen）原子替换，慢加载不会覆盖更新的选择。

## 代码结构

| 文件 | 职责 |
| --- | --- |
| `main.go` | 入口、`newApp`、`view` 组合各区域、键盘快捷键、选择辅助方法 |
| `state.go` | `App` / `tabState` 状态模型、常量与类型 |
| `tabs.go` | 导航、历史、排序/视图切换、标签增删与切换 |
| `fs.go` | 目录读取、过滤/排序、面包屑、快速访问与磁盘列表 |
| `theme.go` | Windows 11 亮/暗配色 |
| `icons.go` | 用 Painter 路径绘制的文件夹、文件、盘符与命令图标 |
| `chrome_tab.go` | 标签条与命令栏 |
| `chrome_nav.go` | 导航行：后退/前进/向上、地址栏、搜索框 |
| `sidebar.go` | 左侧导航窗格与宽度拖拽手柄 |
| `files.go` | 详情/列表/图标四种视图、选择、右键菜单、内联重命名 |
| `status.go` | 状态栏 |
| `ops.go` | 文件操作：打开、复制、剪切、粘贴、删除、重命名、新建文件夹 |
| `preview.go` | 预览窗格数据层：后台加载、代际号替换、图片/文本解码、`mdls`/`ffprobe` 元数据、Quick Look 缩略图 |
| `preview_pane.go` | 预览窗格 UI：标题、各类型预览体、属性列表、播放器控件、宽度拖拽手柄 |
| `media.go` | 媒体：MyGo `audio` 播放器封装（含句柄生命周期与播放重绘节拍器）、`ffmpeg` 视频海报帧 |
| `birthtime_darwin.go` / `birthtime_other.go` | 按平台读取文件创建时间 |

## 实现说明

- 所有图标与控件均由 MyGo 的 `ui` 包在 GPU 上自绘，未引入任何第三方图标资源。
- 删除采用“移动到 `~/.Trash`”而非 `os.RemoveAll`，更接近桌面系统的回收站语义，且不会因误操作直接丢失文件。
- 剪贴板是应用内实现（`clipPaths` / `clipCut`），不污染系统剪贴板；跨标签复制/粘贴同样可用。
- macOS 上打开文件调用系统 `open` 命令。

### Go 媒体方案调研（为什么预览窗格这样实现）

在 `CGO_ENABLED=0`、纯 Go 自绘 UI 的硬约束下，调研结论如下：

- **图片解码**：`ui.DecodeBitmap` 内置 PNG/JPEG/GIF/WebP/BMP（含 JPEG EXIF 方向）。纯 Go 扩展库有 `golang.org/x/image`（tiff 等）、`gen2brain/webp`、`gen2brain/avif`、`gen2brain/heic`（HEIC 无成熟纯 Go 解码器，多为 wasm/cgo 方案）。本项目取“能直接解的直接解，解不了的交给系统 Quick Look”——零依赖、格式覆盖最全。
- **视频播放**：纯 Go 没有可用的通用视频解码器；可用方案全是 cgo 绑定（`go-astiav`/`goav` 绑 ffmpeg、`go-mpv` 绑 libmpv）或捆绑二进制（mpv/ffmpeg 子进程）。与 Explorer 行为对齐的正确姿势就是：预览窗格显示海报帧 + 元数据，“打开”交给系统播放器。若未来要内嵌播放，最优路径是 `Bitmap.Update(*image.RGBA)`（MyGo 为视频帧预留的纹理更新接口）+ ffmpeg 子进程逐帧输出。
- **音频播放**：MyGo 自带 `audio` 包（`audio/mp3`、`audio/vorbis` 注册解码器，macOS 经 purego 走 Audio Queue，无 cgo），覆盖 WAV/MP3/Ogg，本项目的内置播放器即基于此。第三方纯 Go 方案还有 `faiface/beep`、`hajimehoshi/oto`，但会引入第二套输出栈，无增益。
- **元数据**：`mdls`（Spotlight 属性）与 `ffprobe`（时长/编码/分辨率）都是进程调用，无任何链接依赖；两者缺失时预览自动降级，不影响主流程。

## 已知限制

- 窗口使用系统标题栏，未实现资源管理器的自定义标题栏合并且标签页内嵌于标题栏的效果。
- ribbon、压缩/解压、属性对话框等资源管理器高级功能未实现。
- 视频预览依赖 `ffmpeg`/`ffprobe`（Homebrew 安装）；缺失时视频无海报帧，仅剩 Quick Look 兜底与“打开”按钮。
- 跨平台仅验证了 macOS；Linux/Windows 的磁盘枚举与路径分隔未专门适配（`qlmanage`/`mdls` 为 macOS 专属）。
