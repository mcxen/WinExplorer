# Sunvale（晴谷）

一个用 [MyGo](https://github.com/egoist/mygo) 的**纯原生 UI** 实现的文件管理器，外观与排版完整对齐 Windows 11 文件资源管理器。界面组件与单色图标基于 [Onevra](https://github.com/oneo-me/onevra) 组件库（内嵌 Lucide 图标集），彩色文件/文件夹/盘符图标由 Go 在 GPU 上自绘。

没有 HTML、没有 WebView、没有 cgo——整个界面由 Go 绘制。

> 目录名仍为 `WinExplorer`（`/Users/mcx/Documents/OpenSpring/WinExplorer`），项目与可执行文件已更名为 **sunvale / 晴谷**。

## 性能与启动

- 首帧约 **150ms**（`WINEXPLORER_TIMING=1` 启动可在日志看到 `启动：…后第一帧`）。
- 打开方式注册表（287 个应用的 plutil 扫描）与画廊的 ~/Pictures 遍历都改为**惰性**：前者在第一次打开"打开方式"菜单时启动（`ensureOpenWithScan` 一次性护栏），后者在第一次访问画廊页时启动——启动时刻两者零开销、不再抢最紧张的启动 CPU。

## 运行

需要 Go 1.27 或更高版本（MyGo 的要求）。

```bash
cd /Users/mcx/Documents/OpenSpring/WinExplorer
go run .
```

若本机 Go 低于 1.27，但 `GOTOOLCHAIN` 能取到新版：

```bash
GOTOOLCHAIN=go1.27.2 go run .
```

构建单文件二进制：

```bash
go build -o sunvale .
```

## 界面布局

从上到下，对齐 Windows 11 资源管理器：

1. **标题栏（标签条内嵌）** —— 无边框窗口，标签条与最小化/最大化/关闭按钮共用一行；标题栏可拖动移动窗口、双击最大化。每个打开的目录一个标签，当前标签高亮，标签带关闭按钮，`+` 新建标签，右键标签弹出标签菜单。
2. **命令栏** —— `新建▾ | 剪切 复制 粘贴 重命名 分享 删除 | 排序▾ 查看▾ …`：中间一组为纯图标按钮（悬停出提示），`新建`/`排序`/`查看` 为带图标与下箭头的分割按钮，行尾 `…` 打开更多菜单（预览窗格开关、设置）。
3. **地址导航行** —— 后退、前进、向上，可点击的面包屑地址栏（点击空白处或编辑按钮切到路径输入），以及搜索框（占位符“搜索”）。
4. **主体** —— 左侧 Windows 11 导航树（主文件夹置顶，桌面/下载/文档/图片/音乐/视频，可展开的“此电脑”卷列表，以及回收站；可拖动调整宽度 180–420）；中间文件区；右侧预览窗格（`Alt+P` 切换，可拖动 220–460）。
5. **状态栏** —— 项目数量、选中数量与体积、搜索状态，以及当前视图模式。

## 功能

**标签页**
新建（`Ctrl+T`）、关闭（`Ctrl+W`）、切换（点选 / `Ctrl+Tab` / `Ctrl+Shift+Tab` / `Ctrl+1..9`）；每个标签独立的导航历史、列表、选择、搜索与重命名状态。`Ctrl+N` 打开新窗口：每个窗口一个独立 App（标签页、撤销栈、应用内剪贴板互不干扰，同资源管理器），但快速访问固定、最近文件与回收站还原日志共享同一份持久化状态；窗口关闭时停掉其预览的音频并注销。标签右键菜单：复制标签页、关闭标签页、关闭其他标签页、关闭右侧标签页。把文件拖到标签上即移动到该标签所在文件夹。

**导航**
双击进入文件夹、`Enter` 打开、面包屑任意层级跳转、地址栏直输路径（`此电脑` 可回到此电脑页）、前进/后退/向上、刷新（`F5`）；卷根或 `/` 向上回到“此电脑”。

**选择**
单击、`Cmd/Ctrl+单击` 多选、`Shift+单击` 范围选、`Cmd/Ctrl+A` 全选；`↑`/`↓` 按行移动选择（图标视图与画廊里 `←`/`→` 横向移动），`Home`/`End` 到首尾，`PageUp`/`PageDown` 按一屏，`Shift+` 与方向键组合从锚点扩展——移到的行会自动滚入可视区，到列表两端即停。选择按标签独立，剪贴板为应用级（跨标签复制/粘贴可用）。

**视图与排序**
详细信息、列表、小/中/大/超大图标六种视图；按名称/修改日期/类型/大小排序，升序降序；点击列头切换排序。`查看` 菜单还提供**紧凑视图**（行距 30→22）、**放大图标 / 缩小图标**（七档、最大 4×，故超大图标可达 384 像素；`Cmd/Ctrl+Shift+=` / `-`，在详细信息或列表中按一次即切到图标视图）、**显示 > 文件扩展名**（关闭时隐藏已知扩展名，同资源管理器默认）、**显示 > 隐藏的项目**。

**媒体缩略图与文件夹封面**
中等图标及以上的图标视图不再只画文件类型图标：图片与视频直接显示自身画面——图片走内置解码器，HEIC/TIFF/AVIF 交给 Quick Look，视频用 `ffmpeg` 抽帧并叠加播放角标；**含图片或视频的文件夹仍是一只文件夹**——标签页、金色描边、圆角都在，其中最多 4 个媒体填在文件夹的正面板里（1 张铺满、2 张左右分栏、3 张左一右二、4 张 2×2，与资源管理器一致），边框不薄于 2 DIP，因此中等图标（48 像素）下也不会退化成一张裸拼图。封面按正面板的宽高比拼图，贴进面板时不再二次裁切。解码在后台以 4 路并发进行、按 LRU 缓存（64 MB 预算），放大跨过一档时按更大尺寸重解，`F5` 刷新清空缓存重新读取。详情/列表视图的 16 像素图标仍用自绘图标，既省解码也更清楚。

**分组依据**
`查看 > 分组依据`（无/名称/修改日期/类型/大小），四种视图都插入组头（组名 + 数量）：名称按首字母、日期按 今天/昨天/本周/上周/更早、大小按 空/小于 100 KB/小于 1 MB/中/大/巨大、类型按文件类型。

**文件操作**
新建文件夹、新建文本文档、内联重命名（`F2`）、**批量重命名**（多选时 `F2`/右键“重命名”打开对话框：模式占位符 `{name}` `{stem}` `{ext}` `{n}` `{n:3}` 补零、起始编号，逐行实时预览、冲突与非法名标红且不应用该行、干净行可提交，整批记一次撤销）、复制/剪切/粘贴（Windows 风格 `名称 - 副本` 命名）、删除（移入 `~/.Trash`）、永久删除、复制为路径（系统剪贴板）、在终端中打开、属性（打开预览窗格并定位）。

**压缩 / 解压**
任意选择「压缩为 ZIP 文件」（单选以原名命名，多选为“压缩文件”）。解压支持 zip、tar、tar.gz/tgz、tar.bz2/tbz2（标准库纯 Go），tar.xz（系统 `bsdtar`），7z/rar（需安装 `7zz` 或 `unar`）。解压到以归档名命名的子文件夹；成员路径含 `..`、绝对路径或反斜杠（zip-slip）一律拒绝，符号链接成员跳过。

**回收站**
“此电脑”下的回收站页列出 `~/.Trash`。本应用删除的项目可**还原**到原位（删除时记录来源路径，原文件夹缺失会重建、重名自动避让）；**永久删除**与**清空回收站**弹出系统确认对话框（`mygo.Dialog`）。

**撤销**
`Ctrl+Z` 依次撤销最近的 复制/移动、重命名、删除（从废纸篓放回）、新建文件夹/文本文档、压缩、解压、还原；栈深 64，持久化在状态文件中。

**搜索**
在搜索框输入即在后台递归搜索当前文件夹及其全部子文件夹（上限 5000 条、8 秒超时，默认跳过隐藏目录）；详细信息视图在搜索时自动切换出「位置」列，状态栏显示「正在搜索…」「搜索结果」。

**主页（主文件夹）**
导航树顶部「主文件夹」是资源管理器的 Home 页：**快速访问**磁贴（单击进入；首次运行按 Windows 11 默认固定 桌面/下载/文档/图片/音乐/视频 中存在的项）+ **最近使用的文件**（本应用打开过的文件，最新在前，最多 20 条）。文件夹右键「固定到快速访问」，侧边栏或主页磁贴右键「从快速访问取消固定」。

**打开方式**
文件右键「打开方式」列出在 `Info.plist` 中声明支持该扩展名的应用（启动时后台扫描 `/Applications` 与 `/System/Applications`，经 `plutil` 解析），点选即用 `open -a` 打开；末尾提供「在 /Applications 中查看…」。

**扩展命令（Shell 扩展协议）**
对应资源管理器的注册表动词：在配置文件中登记的命令会出现在匹配文件的右键菜单里，按注册顺序排列，带占位符与可选终端执行（详见下方「配置文件」）。

**拖拽移动**
把文件拖到文件夹行、侧边栏位置、面包屑层级、标签页或主页磁贴即移动过去；按住 `Cmd/Ctrl` 拖为复制；行在选中集合内时携带整个选择；同名自动「名称 - 副本」避让，跨盘（EXDEV）移动回退为复制后删除，把文件夹拖进自身或子目录会跳过；可把文件夹拖到标签页移到该标签文件夹；Finder 等外部应用拖入的文件同样落到目标文件夹。拖过时目标高亮。

**空格快速预览（Quick Look）**
选中单个文件按 `空格` 打开模态快速预览（图片/文本/音频播放器/视频与 PDF 海报），底部一行文件摘要；`空格` 或 `Esc` 关闭，点击外部也关闭。它复用预览窗格已加载的解码结果，同一文件只解码一次。

**预览窗格**
选中单个文件时在右侧显示预览与属性，随选择实时切换：

- **图片**：PNG/JPEG/GIF/WebP/BMP 走 MyGo 的 `ui.DecodeBitmap`（含 JPEG EXIF 方向修正）；HEIC/AVIF/TIFF/SVG 走系统 Quick Look（`qlmanage -t`）出 PNG 后解码。
- **视频**：`ffmpeg` 抽帧生成海报 + `ffprobe` 读时长/分辨率/编码，Quick Look 兜底；面板给「打开」按钮交给系统播放器（与 Explorer 一致：静态帧 + 元数据，不内嵌播放）。
- **音频**：WAV/MP3/Ogg Vorbis 用 MyGo 的 `audio` 包（纯 Go、无 cgo）**内置播放**——播放/暂停按钮、点击进度条跳转、`空格` 播放/暂停、`←/→` 快退快进 5 秒；不支持的格式回退「打开」。
- **文本/代码**：只读文本框显示前 64 KB，附「约 N · 已显示前 M」提示。
- **PDF / Office 文档**：Quick Look 渲染首页 + 「打开」按钮。
- **任意文件**：文件名、类型、大小、修改/创建日期、位置；图片/视频/音频再附加尺寸、页数、时长、分辨率、编码等元数据（`mdls` / `ffprobe`）。

加载全部在后台 goroutine 完成，结果以代际号（gen）原子替换，慢加载不会覆盖更新的选择。

**此电脑**
以 `/`（命名为 Macintosh HD）加各 `/Volumes` 挂载点列出卷（含外接磁盘），按设备号去重；每个卷显示**容量使用条**（蓝色为已用比例）与「可用 X，共 Y」，详细信息视图有专门的宽列，图标视图在名称下方画条，悬停出百分比提示（`statfs`）。

**设置**
命令栏 `查看更多（…）> 设置` 或 `Ctrl/Cmd+,` 打开设置对话框，三组设置：**外观**（跟随系统 / 浅色 / 深色）、**查看**（文件扩展名、隐藏的项目、项目复选框、紧凑视图、预览窗格、默认视图）、**更新**（当前版本、自动检查更新、自动下载并安装、检查更新、上次检查）。设置持久化在 `sunvale/state.json` 的 `settings` 字段，更改即时应用到所有打开的窗口。

## 自动更新

应用内置 [MyGo](https://github.com/egoist/mygo) 的 `mygo.Updater` 与 updater 插件，从**签名发布**自更新：`mygo build` 用 `mygo keygen` 私钥签名每个平台的更新包，应用用编译期内置于可执行文件的公钥校验签名后替换自己。设置对话框更新区的选项（自动检查更新、自动下载并安装等）持久化在 `sunvale/state.json`。

**发布端配置**（本仓库已配好）：

- `mygo keygen` 生成密钥对到用户配置目录 `mygo/update-keys/`：`mygo-update.key`（私钥，不入库、只进 GitHub secret）与 `mygo-update.pub`（公钥）。
- `mygo.json` 的 `updates` 字段登记 `publicKey`（公钥内容）与 `github`（`mcxen/WinExplorer`）。
- GitHub 仓库 secret `UPDATE_PRIVATE_KEY` 存 `mygo-update.key` 的内容，`release.yml` 构建时经 `MYGO_UPDATER_PRIVATE_KEY` 传入：每个平台随安装包上传 `update-<platform>.json` 清单（版本、发行说明、日期与归档/增量的 URL、大小、签名）、签名的 `sunvale-<版本>-<平台>.tar.gz` 更新包与 `.delta` 增量包；未设 secret 时构建照常、只跳过这些文件（Linux 的 `.tar.gz` 仍会生成，供 `install.sh` 安装）。
- 发布（publish job 的 `--latest`）后，应用读 `https://github.com/mcxen/WinExplorer/releases/latest/download/update-<platform>.json` 发现新版本并下载安装。

**增量更新**：签名构建读取各平台已发布清单，下载最近 3 个版本（`updates.deltas` 可调）的更新包，与新版本做 bsdiff 二进制差分出 `.delta`；应用只下载变化的部分，按清单的大小与 SHA-256 校验重组出与发布完全一致的新版本，失败或不比整包小时回退整包下载。

`go run .` 的开发构建与包管理器安装（deb 在 `/opt`）不能写安装位置，更新区显示「此构建不支持自动更新」；macOS 安装到 `/Applications`（或 `~/Applications`）、Windows 用户级安装（`%LOCALAPPDATA%\Programs`）与 Linux 的 `install.sh` 安装（`~/.local`）均可自更新，deb 安装交由系统包管理器更新。仓库暂无 `CHANGELOG.md`，清单的发行说明为空（发布页的自动 notes 不受影响）。

## 远程访问（OpenList 兼容）

把主文件夹（虚拟根 `/` 即本机 home 目录）通过局域网 HTTP 暴露给手机或其他设备，协议兼容 [OpenList](https://github.com/OpenListTeam/OpenList)（AList v3 API）的读、写与分享接口集。

**开启与账号**

- 命令栏 `查看更多（…）> 远程访问` 开关服务；开启后状态栏显示局域网地址（默认端口 5244，被占用时自动顺延 5245–5254）。
- `查看更多 > 复制远程地址` 一次复制 地址 / 账号 / 密码。账号默认 `admin`；密码首次使用时随机生成，仅在本轮运行内可知——重启后点「复制远程地址」会重置一个新密码（旧密码随之失效）。
- 账号与端口持久化在 `sunvale/state.json` 的 `remote` 字段；应用退出时自动停服务，已签发的登录令牌全部失效。

**`/@manage` 管理界面**：浏览器打开 `http://局域网IP:5244/@manage` 即是内置管理控制台（单文件内嵌，无外部资源）——仪表盘（运行状态与开关、存储容量、分享概览、OpenList 客户端接入信息）、文件、分享（创建、复制链接、启停、删除）、设置（改用户名/密码、重置 API Token、改端口、远程访问开关）。

**文件页与桌面端同构**：命令栏 `新建▾ | 剪切 复制 粘贴 重命名 分享 删除 | 排序▾ 查看▾ | …`（没有选中时剪切/重命名/删除保持置灰，剪贴板为空时粘贴置灰，与客户端同一套开关规则）、地址行（后退/前进/向上/刷新 + 面包屑 + 搜索框）、左侧「位置 / 存储」导航树（主文件夹，桌面/文档/下载/图片/音乐/视频（存在才列出），以及本机存储与容量）、状态栏（项目数与选中体积，右侧当前视图）。六种视图（超大图标/大图标/中等图标/小图标/列表/详细信息）与名称·修改日期·类型·大小时升序/降序都可切换，选择记在 `localStorage`；详细信息视图可按列头排序，图标视图里图片直接以 `/d` 直链当缩略图。选择与键盘对齐客户端：单击、`Cmd/Ctrl+单击`、`Shift+单击` 范围选、`Ctrl+A` 全选，方向键移动（图标/列表视图按行列）、`Shift+方向键` 扩展、`Home`/`End` 到首尾、`Enter` 打开、`Backspace` 向上、`F2` 重命名、`F5` 刷新、`Delete` 删除、`空格` 快速预览、`Alt+P` 预览窗格。行右键菜单对齐 Windows 11：打开/预览、下载、剪切、复制、粘贴、复制为路径、重命名、压缩为 ZIP 文件、全部解压、解压到…、分享、删除、属性；空白处菜单提供新建文件夹/文本文档、上传、粘贴、排序方式、查看、显示隐藏的项目、显示项目复选框、刷新。

**拖拽**：行可拖拽，文件夹行、面包屑、导航树都是放置目标——直接拖就是移动，按住 `Cmd/Ctrl` 拖是复制（同桌面端）；从系统（Finder / 资源管理器）拖文件到页面即上传到当前文件夹、拖到某个文件夹行则上传进那个文件夹，带进度条与失败重试。

**压缩与解压（远程）**：`/api/fs/archive/unzip` 把每个归档解压到以归档名命名的子文件夹（重名按「- 副本」递增，与桌面端同名），`/api/fs/archive/zip` 把选中的项目打包成一个 ZIP（单选以原名命名、多选为「压缩文件」）；两者都接受 `names` 批量数组与可选 `dst_dir`，`/api/public/archive_extensions` 因此返回可解压的后缀表（`.zip` `.tar` `.tar.gz` `.tgz` `.tar.bz2` `.tbz2` `.tar.xz` `.txz` `.7z` `.rar`）。实现完全复用桌面端的 `extractArchive` / `makeZip`：zip-slip 拒绝、符号链接跳过、tar 家族纯 Go、tar.xz 走系统 `bsdtar`、7z/rar 走 `7zz`/`unar` 全一致；解压失败会删掉半成品文件夹，批量里个别失败在 `reason` 里说明、其余照常完成。同步完成，不排队。

**写操作与上传**：`/api/fs/mkdir`、`rename`、`move`、`copy`、`remove`、`dirs`（只列一层子文件夹）、`put`（`PUT` 上传：`File-Path` 头携带 URL 转义的目标路径，`Overwrite: false` 时冲突拒绝，`Last-Modified` 毫秒时间戳随行，落地走临时文件+原子改名）。删除与桌面端同语义：**移入 `~/.Trash` 并记录来源路径**，回收站页可还原；move/copy 整批预检——冲突既不跳过也不覆盖时整批拒绝且源不动，目录复制可 `merge` 合并，移入自身子树自动跳过。

**分享系统**：管理端 `/api/share/{create,list,get,update,delete,enable,disable}`（8 位随机 id、自定义 id 支持中文、密码/过期时间/访问次数上限/备注），访客链接形如 `http://IP:5244/@s/<id>`，**免登录**浏览（挂在 `fs/list`、`fs/get` 的 `@s` 分支）与下载（`/sd/<id>/<路径>?pwd=` 直链，支持 Range/HEAD）；同一 IP 半小时内多次访问只计一次，分享内的对象 `sign` 恒为空、`write` 恒为关；过期/停用/超次后访客得到协议的失效应答。分享持久化在 `sunvale/state.json`。主页面直接打开分享链接：带密码时弹框输入重试，单文件分享直接进预览。

**`/api/sunvale/*` 管理端点**：`status`（开关/端口/地址/容量/分享数）、`account`（改用户名/密码；改密后所有已签发登录立即失效）、`reset-token`（换发主令牌——它同时是登录签发与下载签名的密钥，旧令牌与旧签名直链全部失效）、`port`（范围校验、占用时整段顺延、失败回滚旧端口）、`enabled`（幂等开关）。供 `/@manage` 使用，OpenList 客户端不感知。

**内置网页端**：浏览器直接打开地址即可——登录、目录浏览（目录在前）、面包屑导航、当前目录递归搜索、图片/视频/音频/PDF/文本预览、下载、分享链接的访客模式（密码重试、免登录），右上角「管理」直达 `/@manage`。

**OpenList 客户端接入**：站点地址填 `http://局域网IP:5244`，账号 `admin` + 复制到的密码；列目录、取文件、写操作、上传、签名直链下载（含 Range 断点续传）均可用。

**协议范围**

- 已实现：`/api/public/settings`、`/api/public/archive_extensions`（可解压后缀表）、`/api/auth/login`（明文）与 `/api/auth/login/hash`（StaticHash）、`/api/me`、`/api/auth/logout`、`/api/fs/list`、`/api/fs/get`、`/api/fs/search`、`/api/fs/mkdir`、`/api/fs/rename`、`/api/fs/move`、`/api/fs/copy`、`/api/fs/remove`、`/api/fs/dirs`、`/api/fs/put`、`/api/fs/archive/unzip`、`/api/fs/archive/zip`、`/api/share/{create,list,get,update,delete,enable,disable}`、`/d/`（下载）、`/p/`（流式代理，支持 Range/HEAD）、`/sd/`（分享直链下载）、`/@s/<id>`（分享访客浏览）、`/@manage`（管理界面）、`/api/sunvale/{status,account,reset-token,port,enabled}`、`/`（内置网页）与 `/ping`。
- 未实现：`/api/admin/*` 与异步任务队列（写操作、上传与解压同步完成，即答即毕）——OpenList 客户端中的站点管理类按钮不可用属预期；`/api/fs/archive/list`（归档内预览）未实现，故 `settings` 里 `preview_archives_by_default` 保持 `false`。
- 所有 `/api/*` 响应恒为 HTTP 200，错误以 body `{"code":...}` 表达；`Authorization` 头携带裸令牌（无 `Bearer` 前缀）；下载签名 `sign = base64url(HMAC-SHA256(令牌, 路径 + ":0")) + ":0"`。

## 快捷键

对齐微软官方 File Explorer 快捷键表（`ui.Cmd` 为平台命令键）：

| 按键 | 操作 |
| --- | --- |
| `Enter` | 打开 |
| `空格` | 快速预览选中文件（音频预览打开时为播放/暂停） |
| `↑` / `↓` | 移动选择：列表与详细信息按行，图标视图与画廊按整行 |
| `←` / `→` | 图标视图与画廊中横向移动选择（音频预览打开时为快退 / 快进 5 秒） |
| `Home` / `End` | 选择第一个 / 最后一个项目 |
| `PageUp` / `PageDown` | 按一屏移动选择 |
| `Shift+方向键`、`Shift+Home`·`End`、`Shift+PageUp`·`PageDown` | 从锚点扩展选择 |
| `Backspace` / `Alt+↑` | 向上 |
| `Alt+←` / `Cmd+[` | 后退 |
| `Alt+→` / `Cmd+]` | 前进 |
| `Delete` / `Cmd+Backspace` / `Ctrl+D` | 删除（移入回收站） |
| `Shift+Delete` | 永久删除（确认后） |
| `F2` | 重命名（单选内联；多选打开批量重命名对话框） |
| `F5` | 刷新 |
| `Cmd/Ctrl+C` / `X` / `V` | 复制 / 剪切 / 粘贴 |
| `Cmd/Ctrl+A` | 全选 |
| `Cmd/Ctrl+Z` | 撤销 |
| `Cmd/Ctrl+Shift+C` | 复制为路径 |
| `Alt+Enter` | 属性对话框（文件与文件夹；仅单选） |
| `Ctrl+E` / `Ctrl+F` / `Cmd+F` / `F3` | 聚焦搜索框 |
| `Alt+D` / `Ctrl+L` / `F4` | 编辑地址栏 |
| `F11` | 最大化 / 还原 |
| `Cmd/Ctrl+Shift+N` | 新建文件夹 |
| `Cmd/Ctrl+T` / `Cmd/Ctrl+W` | 新建 / 关闭标签页 |
| `Cmd/Ctrl+N` | 新窗口（每窗口独立的标签页/撤销栈/应用内剪贴板；快速访问、最近文件、回收站日志跨窗口共享） |
| `Cmd+]` / `Ctrl+Tab`，`Cmd+[` / `Ctrl+Shift+Tab` | 下一个 / 上一个标签页 |
| `Cmd/Ctrl+1..8`，`Cmd/Ctrl+9` | 第 N 个 / 最后一个标签页 |
| `Cmd/Ctrl+Shift+1..6` | 视图：超大图标 / 大图标 / 中等图标 / 小图标 / 列表 / 详细信息 |
| `Cmd/Ctrl+Shift+=` / `Cmd/Ctrl+Shift+-` | 放大 / 缩小图标（图标视图，最大 4×；在详细信息或列表中按一次切到图标视图） |
| `Alt+P` | 预览窗格开关 |
| `Ctrl/Cmd+,` | 设置 |
| `Esc` | 关闭快速预览 / 取消重命名 / 取消路径编辑 / 清空搜索 |

> 内联重命名或路径编辑进行中时，窗口快捷键暂停，按键交给输入框：重命名 `Enter` 提交、`Esc` 取消；路径编辑 `Enter` 跳转、`Esc` 取消。

## 右键菜单

| 位置 | 条目 |
| --- | --- |
| 文件 / 文件夹 | 打开 ｜ 固定到快速访问（文件夹）｜ 打开方式 ▸ ｜ 剪切、复制、粘贴、复制为路径、重命名 ｜ 压缩为 ZIP 文件、全部解压 ｜ 在终端中打开（文件夹）、删除 ｜ 属性（对话框）｜ （登记的扩展命令） |
| 空白处 | 新建 ▸ 文件夹 / 文本文档 ｜ 粘贴 ｜ 在终端中打开 ｜ 刷新 ｜ 显示隐藏的项目 ｜ 属性 |
| 标签页 | 复制标签页、关闭标签页、关闭其他标签页、关闭右侧标签页 |
| 侧边栏位置 | 打开 ｜ 从快速访问取消固定 ｜ 属性 |
| 主页磁贴 | 打开 ｜ 从快速访问取消固定 ｜ 属性 |
| 回收站条目 | 还原 ｜ 永久删除 ｜ 属性 |
| 回收站空白处 | 清空回收站 ｜ 刷新 |
| 命令栏 | 新建 ▸ 文件夹；排序 ▸ 名称/日期/类型/大小 + 递增/递减；查看 ▸ 视图、紧凑视图、显示、分组依据；查看更多 ▸ 预览窗格、设置 |

## 配置文件

两类用户配置，均在 `os.UserConfigDir()`（macOS：`~/Library/Application Support`）下：

**`sunvale/state.json`** —— 快速访问固定项、最近使用的文件、回收站还原日志。由应用自动读写；旧版目录名 `winexplorer/` 会被自动迁移读取。

**`winexplorer/extensions.json`** —— 扩展命令注册表（Shell 扩展协议）。文件不存在则无扩展；格式错误会在启动时忽略并在状态栏提示。

```json
[
  { "name": "用 VSCode 打开", "match": ["*"],   "command": "code \"{path}\"" },
  { "name": "转为 MP3",      "match": ["wav", "flac"], "command": "ffmpeg -i \"{path}\" \"{stem}.mp3\"", "terminal": true },
  { "name": "在此打开终端",  "match": ["dir"],  "command": "cd \"{path}\" && clear", "terminal": true }
]
```

- `match`：`*` 匹配全部，`file` 仅文件，`dir` 仅文件夹，否则按扩展名（不区分大小写，可写 `wav` 或 `.WAV`）。
- `command` 占位符：`{path}` 完整路径、`{dir}` 所在目录、`{name}` 文件名、`{stem}` 去扩展名、`{ext}` 扩展名。
- `terminal: true` 在新 Terminal 窗口执行（经 `osascript`），否则以 `sh -c` 分离执行，慢命令不阻塞界面。

## 外部命令依赖

| 命令 | 用途 | 是否必需 |
| --- | --- | --- |
| `open` | 用系统默认应用打开文件 / `open -a` 指定应用 | 必需（打开功能） |
| `open -a Terminal` | 在终端中打开 | 菜单项依赖 |
| `qlmanage` | Quick Look 缩略图（HEIC/AVIF/TIFF/SVG、PDF 首页、视频兜底） | 相关格式必需，失败降级为仅元数据 |
| `mdls` | 图片尺寸、页数等 Spotlight 元数据 | 可选，缺失则无附加元数据 |
| `ffprobe` / `ffmpeg` | 视频时长/编码/分辨率元数据、视频海报帧 | 可选，缺失则无海报 |
| `plutil` | 解析应用 `Info.plist` 以构建「打开方式」 | 可选（macOS 自带） |
| `tar` / `7zz` / `unar` / `unrar` | 解压 tar.xz / 7z / rar | 按归档格式需要 |

音频/视频播放本身用 MyGo 的纯 Go `audio` 包，不依赖外部播放器。

## 代码结构

| 文件 | 职责 |
| --- | --- |
| `main.go` | 入口、`newApp`、`view` 组合各区域、快捷键表、选择辅助 |
| `state.go` | `App` / `tabState` 状态模型、视图/排序/分组枚举与常量 |
| `tabs.go` | 导航、历史、排序/视图/分组切换、标签增删与切换 |
| `fs.go` | 目录读取、过滤/排序、面包屑、快速访问与卷列表、特殊路径 |
| `theme.go` | Windows 11 亮/暗配色（`// COLOR TYPE: ui.Color`） |
| `icons.go` | 彩色文件/文件夹/盘符/位置图标（Painter 自绘）+ Onevra Lucide 单色图标封装 |
| `chrome_tab.go` | 标题栏（标签条 + 窗口控制按钮）与命令栏 |
| `chrome_nav.go` | 导航行：后退/前进/向上、地址栏、搜索框 |
| `sidebar.go` | 左侧导航树与宽度拖拽手柄 |
| `files.go` | 详情/列表/图标视图、分组、选择、行与空白右键菜单、内联重命名 |
| `keynav.go` | 键盘导航：`↑`/`↓`/`←`/`→`、`Home`/`End`、`PageUp`/`PageDown` 移动选择，`Shift` 从锚点扩展，并把移到的行滚入可视区 |
| `thumb.go` | 图标视图的媒体缩略图与文件夹封面：四档解码尺寸、后台并发解码、面向像素预算的 LRU 缓存、封面拼图 |
| `status.go` | 状态栏 |
| `ops.go` | 文件操作：打开、复制、剪切、粘贴、删除、重命名、新建、终端、属性、复制为路径 |
| `undo.go` | `Ctrl+Z` 撤销栈（created / moved 两类记录） |
| `home.go` | 主页（主文件夹）：快速访问磁贴、最近使用的文件 |
| `trash.go` | 回收站页：还原、永久删除、清空 |
| `search.go` | 递归搜索：后台遍历子树（5000 条 / 8 秒）、位置列数据 |
| `archive.go` | 压缩为 ZIP 与解压（zip/tar 纯 Go，xz 走 bsdtar，7z/rar 走 7zz/unar），zip-slip 拒绝与唯一命名 |
| `dropops.go` | 拖拽：窗口内载荷、移动/复制落地、跨盘回退、外部文件拖入 |
| `openwith.go` | 打开方式：扫描应用 Info.plist 的扩展名声明并以 `open -a` 启动 |
| `extensions.go` | 扩展命令协议：读取 `extensions.json`、匹配、占位符展开与执行 |
| `quicklook.go` | 空格快速预览模态（复用预览窗格解码结果） |
| `preview.go` | 预览数据层：后台加载、代际号替换、图片/文本解码、`mdls`/`ffprobe`、Quick Look 缩略图 |
| `preview_pane.go` | 预览窗格 UI：各类型预览体、属性列表、播放器控件、宽度拖拽 |
| `settingsdialog.go` | 设置对话框（外观/查看/更新与自动更新集成） |
| `media.go` | 媒体：MyGo `audio` 播放器封装（句柄生命周期、播放节拍器）、`ffmpeg` 视频海报帧 |
| `statestore.go` | 持久化状态：固定项、最近文件、回收站还原日志（JSON），旧目录名迁移 |
| `statfs_unix.go` / `statfs_other.go` | 按平台读取卷容量 |
| `birthtime_darwin.go` / `birthtime_other.go` | 按平台读取文件创建时间 |
| `remote*.go` | 远程访问（OpenList 兼容）：服务生命周期/账号与 JWT（`remote.go`）、auth/public API（`remote_api.go`）、fs/list·get·search 与虚拟路径映射、下载签名（`remote_fs.go`）、fs 写操作与上传（`remote_manage.go`）、压缩与解压端点（`remote_archive.go`）、分享协议、`/@s` 访客流与 `/sd` 直链（`remote_share.go`）、`/api/sunvale/*` 管理端点（`remote_admin.go`）、`/d`/`/p` 流式下载（`remote_dl.go`）、内置网页（`remote_web.go` + `remote_web.html`）、管理界面（`remote_manage_web.go` + `remote_manage_web.html`） |

## 测试

```bash
go test ./...
```

无界面回归测试（不启动 `mygo.App`；`keynav_ui_test.go` 用 MyGo 的 `ui.NewTester` 跑真实窗口视图）：

- `archive_drop_test.go` —— 压缩→解压 ZIP 回环、tar.gz（系统 tar）、zip-slip 拒绝、唯一命名、拖拽移动/复制/边界情形。
- `features_test.go` —— 特殊路径与中文别名、固定/最近状态、主页条目、回收站还原 + 撤销、撤销移动/新建、递归搜索、分组行。
- `keynav_test.go` —— 方向键步进与两端即停、`Shift` 从锚点扩展、图标视图按行/按屏、分组头下的行号映射。
- `keynav_ui_test.go` —— 真实视图上的同一套按键：快捷键分发、移到的行滚入可视区（详情/列表/图标/分组图标）、搜索框持有焦点时独占按键。
- `statestore_test.go` —— 旧目录名（`winexplorer`）状态迁移。
- `ext_openwith_test.go` —— 扩展命令配置解析与匹配、占位符展开、`Info.plist` 扩展名解析、快速预览门控。
- `remote_test.go` —— 远程访问协议回归：登录三态（错误/明文/StaticHash）、`/api/me` 三态、fs 列表/获取/搜索、签名下载（全量/Range/HEAD/过期签名/目录拒绝）、路径防逃逸，及真实端口 + curl 端到端冒烟。
- `remote_manage_test.go` —— 管理界面一轮回归：fs 写操作（mkdir/rename/move/copy/remove 进回收站/dirs/put 上传，含中文与转义路径）、分享全链路（管理端 CRUD 与改名、`/@s` 免登录访客流、密码/过期/停用/超次、按 IP 去重计数、`/sd` 下载与 Range/HEAD）、`/api/sunvale/*`（状态、改密后旧登录失效、重置令牌、端口校验、开关幂等与真实启停）、`/@manage` 页面，及真实端口 + curl 全故事线冒烟（登录→上传→分享→访客下载→管理页→状态）。

- `remote_archive_test.go` —— 远程压缩/解压回归：解压到以归档名命名的子文件夹、重名「- 副本」、`dst_dir` 与批量（个别失败在 `reason` 里说明）、压缩单个文件夹/单个文件/批量的三种命名、zip-slip 拒绝且不留半成品文件夹、来宾 401 与参数校验。
> 删除/还原相关测试会真实地在 `~/.Trash` 中创建并恢复一个临时文件。

- **属性对话框**：右键“属性”与 `Alt+Enter` 打开模态对话框（对齐资源管理器的属性页）：图标与名称抬头、图片先显示缩略图、完整属性表（名称/类型/位置/大小/修改与创建日期 + 格式元数据），底部 在 Finder 中显示（`open -R`）/ 打开 / 确定；文件夹属性由专属后台加载（预览管线不覆盖目录）。

## 实现说明

- **图标分层**：单色命令/导航图标（后退、前进、向上、刷新、剪切、复制、粘贴、重命名、分享、删除、新建、排序、查看、更多、搜索、播放/暂停等）统一使用 Onevra 内置 Lucide 图标，经 `components.Icon` 渲染、继承文本色；命令栏与导航行的图标按钮用 `components.IconButton`。**彩色**文件、文件夹、盘符、位置图标仍由 MyGo `ui` 包在 GPU 上以 Painter 路径自绘——它们承载资源管理器的视觉识别度，单色图标集无法替代。第三层是**真实画面**：图标视图里的图片/视频用解码出的缩略图，含媒体的文件夹用拼合封面（`thumb.go`），经 `ui.Image(...).Fit(ui.Cover)` 铺满方块；三层各有各的用意，媒体缩略图不是替代自绘图标，而是叠在它上面——解码未落地的那几帧显示的正是自绘图标。
- **Onevra 本地依赖**：`go.mod` 以 `replace` 指向本地克隆 `/Users/mcx/onevra/source`（含未发布的提交）；下一个 Onevra 版本发布后可移除该行。
- **删除语义**：删除是「移动到 `~/.Trash`」而非直接抹除，并在状态文件里记录来源路径，供还原与撤销使用。
- **剪贴板**：应用内实现（`clipPaths` / `clipCut`），不污染系统剪贴板；跨标签复制/粘贴可用。仅「复制为路径」写入系统剪贴板。
- **窗口**：无边框（`Frameless`），标题栏由应用自绘；窗口尺寸/位置不持久化（未用 MyGo 的 `StateKey`）。

### Go 媒体方案调研（为什么预览窗格这样实现）

在 `CGO_ENABLED=0`、纯 Go 自绘 UI 的硬约束下：

- **图片解码**：`ui.DecodeBitmap` 内置 PNG/JPEG/GIF/WebP/BMP（含 JPEG EXIF 方向）。纯 Go 扩展库有 `golang.org/x/image`（tiff 等）、`gen2brain/webp`、`gen2brain/avif`、`gen2brain/heic`（HEIC 无成熟纯 Go 解码器）。本项目取「能直接解的直接解，解不了的交给系统 Quick Look」——零依赖、格式覆盖最全。
- **视频播放**：纯 Go 没有通用视频解码器；可选方案全是 cgo 绑定或捆绑二进制。与 Explorer 行为对齐的正确姿势是：预览窗格显示海报帧 + 元数据，「打开」交给系统播放器。若未来内嵌播放，最优路径是 `Bitmap.Update(*image.RGBA)` + `ffmpeg` 子进程逐帧输出。
- **音频播放**：MyGo 自带 `audio` 包（注册 `audio/mp3`、`audio/vorbis` 解码器，macOS 经 purego 走 Audio Queue，无 cgo），覆盖 WAV/MP3/Ogg，内置播放器即基于此。
- **元数据**：`mdls` 与 `ffprobe` 都是进程调用，无链接依赖；缺失时预览自动降级，不影响主流程。

## 已知限制

- Windows 的 **Snap Layouts**（悬停最大化按钮弹出布局选择）未实现；ribbon、OneDrive、网络发现等高级功能未实现。
- `Ctrl+滚轮` 缩放图标不可用：MyGo 不向应用暴露原始滚轮事件（滚轮仅驱动滚动）；图标大小改用 `查看 > 放大图标 / 缩小图标`、`Cmd/Ctrl+Shift+=` / `-` 或 `Cmd/Ctrl+Shift+1..6` 切换。封面与缩略图依赖目录读取：网络卷或极深的目录里，首帧先显示自绘图标，解码落地后再换成画面。
- 视频海报帧优先 `ffmpeg`；macOS 缺失时回退 Quick Look，其他平台缺失时显示类型图标。
- 7z / rar 解压依赖 `7zz` 或 `unar`；tar.xz 依赖系统 `bsdtar`。
- `qlmanage` 缩略图、`mdls` 元数据、`afplay` 音频兜底、`~/.Trash`、`/Volumes` 现全部经平台层（`platform.go` → `sys_darwin.go` / `sys_windows.go` / `sys_linux.go` / `sys_other.go`）分发，共享代码零平台分支。

## 平台支持

| 能力 | macOS | Windows | Linux |
| --- | --- | --- | --- |
| 打开 / 打开方式 | `open` / `/Applications` 注册表（plutil + Info.plist + UTI） | `rundll32` 协议 / 系统「选择应用打开」对话框 | `xdg-open` / `.desktop` 注册表（MimeType） |
| 在文件管理器中显示 | `open -R` | `explorer /select,` | 打开父目录 |
| 在终端中打开 | Terminal.app | Windows Terminal，回退 cmd | gnome-terminal / konsole / xfce4-terminal / kitty / foot |
| 回收站 | `~/.Trash`（重命名，首次自建） | **原生回收站**：`SHFileOperationW` 删除入箱，`$I/$R` 元数据解析实现列出 / 还原 / 永久删除 / 清空 | **freedesktop 规范**：`~/.local/share/Trash/{files,info}` + `.trashinfo` |
| 磁盘与卷 | `statfs` + `/Volumes` | `GetDiskFreeSpaceExW` + `GetLogicalDrives` + 卷标（kernel32） | `statfs` + `/media`、`/run/media` |
| 系统缩略图（HEIC/TIFF/影片帧） | Quick Look（qlmanage） | 不可用（标准库格式不受影响） | 不可用 |
| 文件元数据 | mdls | ffprobe（媒体） | ffprobe（媒体） |
| 音频兜底（m4a/aiff/flac…） | afplay + afinfo | ffplay + ffprobe（需 ffmpeg） | ffplay + ffprobe（需 ffmpeg） |
| 安装包 | dmg | NSIS `Setup.exe`（GitHub Actions tag 构建） | deb + tar.gz |

- 回收站的还原日志（`state.json`）跨平台通用：删除时记录来源路径与箱内键名，Windows 的键取自 `$I/$R` 共享后缀。
- 交叉编译矩阵：`darwin/arm64`、`linux/amd64`、`linux/arm64`、`windows/amd64`、`windows/arm64`、`js/wasm` 全部零错误（`GOOS=js` 走 `sys_other.go` 降级，保证可编译）。
- 发布：`git tag v0.1.x` 触发 `release.yml` —— 测试 → 草稿 → 三平台并行构建安装包并上传 → 发布。Windows 安装包由 NSIS 制作（CI 中 `apt install nsis`；本机 `brew install makensis`），用户级安装到 `%LOCALAPPDATA%\Programs\Sunvale`，无需管理员，带开始菜单快捷方式、控制面板卸载项、**文件关联**（压缩格式双击即在 Sunvale 中打开并选中，`OnOpenFile` 接收路径）与 URL scheme 注册，末页可选运行与桌面快捷方式，支持 `/S` 静默安装。
- 应用图标 `resources/icon.png`（1024²，程序生成的 Windows 11 双色黄文件夹），构建时由 CLI 转成 .icns（macOS）/ 可执行文件图标资源（Windows）/ desktop entry 图标（Linux）。
- `mygo.json` 的 `fileAssociations` 注册 zip/tar/gz/tgz/bz2/tbz2/xz/7z/rar 九种压缩格式；本机构建 Windows 安装包：`go run github.com/egoist/mygo/cmd/mygo@v0.5.5 build -platform windows/amd64`。
