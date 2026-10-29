# 18 文件类型图标、应用图标与助手真机测试（ICONS2、AI-E2E）

分支 `feat/ai-test-icons` → `main`。截图 `/workspace/sunvale-icons-shots/`（前后对比、`after-contact.png`、`ai/`）。

## 1. 扩展名 → 图标

`fileicons.go:kindOf` 把条目归类，`drawKind` 绘制。匹配不分大小写；`.tar.gz/.tar.bz2/.tar.xz/.tar.zst` 等复合扩展名整体识别；`Makefile`、`Dockerfile`、`README` 等无扩展名按文件名识别。

| 类别 | 扩展名（节选） | 图形 |
| --- | --- | --- |
| 应用 | `.app` 包目录、exe、com、appimage、desktop、ipa | 蓝色圆角方块 + 窗口；有自身图标时显示自身图标 |
| 安装包 | msi、msix、appx、pkg、deb、rpm、apk、snap、flatpak、cab | 纸箱 + 下载箭头 |
| 磁盘映像 | dmg、iso、img、vhd/vhdx、vmdk、qcow2、wim | 光盘（dmg 不是应用） |
| 压缩包 | zip、7z、rar、tar、gz、bz2、xz、zst、lz4、jar | 纸 + 拉链 |
| 图片 / 视频 / 音频 | png…psd / mp4…rmvb / mp3…midi | 纸 + 山景 / 播放 / 音符 |
| 文档 | pdf、doc(x)、rtf、odt、xls(x)、csv、ppt(x)、md、txt、epub | PDF 红标；表格网格；幻灯片；文字行 |
| 代码 / 脚本 | go、ts、js、py、rs、c/cpp/h、java、json、yaml、toml、xml、css / sh、bat、ps1 | 纸 + `<>`；脚本加斜杠 |
| 字体 / 种子 / 快捷方式 | ttf、otf、woff(2) / torrent / lnk、alias | 纸 + A / 下载箭头 / 角标箭头 |

所有图形画在同一个 `px × px` 方框内：纸张占 0.16–0.84 宽、0.06–0.94 高，应用与安装包占 0.08–0.92，详情、列表、图标、侧栏、预览共用 `fileIcon`，左边距与垂直居中由各视图的图标框统一保证。

## 2. 应用自身图标

`appicon.go`：`fileIcon` 遇到应用类条目时调 `appIconThumb`，经 `thumb.go:cachedThumb` 在后台工作池解码，键为 `a:` + 路径 + 修改时间，与缩略图共享 64 MB / 2000 条预算；视图只读缓存，未就绪时画通用图形。

| 平台 | 来源 | 说明 |
| --- | --- | --- |
| 任意（含 Linux） | macOS `.app`：`Contents/Info.plist` 的 `CFBundleIconFile`（XML 或二进制 plist），取 `.icns` 中不小于目标尺寸的最小 PNG 条目 | JPEG 2000 与旧式 RLE 条目不解，回退通用图形；无键时用 `AppIcon.icns` 或唯一的 `.icns` |
| 任意 | `.desktop`：`Icon=` 绝对 PNG 路径，或 hicolor（256…48）/ pixmaps 中的 PNG | SVG 与其他主题不解；`.AppImage` 用通用图形 |
| Windows | `.exe`、`.lnk`：`IShellItemImageFactory::GetImage`（ICONONLY），COM 单线程套间，HBITMAP → RGBA | 仅交叉编译通过，未在 Windows 真机验证 |

## 3. 助手真机测试（OpenAI 兼容端点）

在 DISPLAY=:10 上用独立 HOME 运行，xdotool 驱动：流式回复、工具调用（list_dir、search_names）、停止后再发、Enter / Shift+Enter / Esc、会话列表、复制、错误行展开与复制、`sunvale.log` 的请求 / 工具 / 错误 / 耗时记录。修复：

- 完成的回合在折叠行下又出现一行相同的「调用了…」，需点两次才见卡片：内层改为「N 次调用」并默认展开。
- 发送新消息时对话停在原位置（展开过折叠或上滚后）：发送即跟到底部。
- 窄面板（< 520）点「会话」只亮按钮、不显示列表：改为列表替换对话显示，选会话或再点按钮恢复。
