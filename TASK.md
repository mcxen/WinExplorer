# TASK.md — 编码代理任务清单

本文件是**任务状态的唯一来源**。`docs/specs/README.md` 只做规格索引，不再维护状态。状态取值：待开发 / 进行中 / 已完成。开始任务时把状态改为“进行中”，合并时改为“已完成”，与代码同一提交或同一 PR。

## 项目目标

晴谷（Sunvale，模块 `sunvale`）是 Go + mygo v0.5.5（immediate-mode）自绘的 Windows 11 资源管理器风格文件管理器，跨 Windows / macOS / Linux / Web（wasm）。目标是在外观和交互上贴近 Windows 资源管理器，同时提供 Everything 式的全局搜索和右侧文件助手。

## 工作方式

1. 开工前阅读：`AGENTS.md`、`docs/UI.md`（界面与动效规范）、对应的 `docs/specs/*.md`。
2. **一个任务一个分支**，从最新 `origin/main` 拉出，分支名见任务表。不在一个分支里混做多个任务。
3. **单次提交少于 1000 行**（新增 + 删除，`git show --shortstat`）。需要更多时拆提交。
4. **永远不要 force-push**，不改写已推送历史。
5. **不要安装 post-commit hook**（不运行 `scripts/install-git-hooks.sh`），避免自动推送/发版。
6. 每次提交前必须全部通过：
   ```sh
   gofmt -l $(git diff --name-only --diff-filter=AM HEAD -- '*.go')   # 无输出
   go build ./... && go vet ./...
   GOOS=windows go build -o /tmp/sv.exe .
   GOOS=js GOARCH=wasm go build -o /tmp/sv.wasm .
   GOOS=darwin go build -o /tmp/sv-mac .        # 涉及平台代码时
   go test -count=1 ./...
   ```
   **已知的既有失败**：无。
7. **离屏截图**：UI 改动必须附截图。在包内临时建 `zz_shot_test.go`：
   ```go
   a := newStoreApp(t, dir)
   a.sortAsc, a.grouping, a.iconZoom = true, groupNone, 1
   tt := ui.NewTester(a.view, 1200, 800)
   tt.Frame(); tt.Frame()
   f, _ := os.Create("/tmp/shots/<任务ID>/01-xxx.png"); png.Encode(f, tt.Image()); f.Close()
   ```
   参考 `/workspace/winexplorer-demo/search/current/searchshot_test.go.txt`。**截图测试不要提交**，看完删除；截图放 `docs/assets/shots/<任务ID>/`（每任务 ≤ 6 张，PNG）随 PR 提交。
8. 界面文字简体中文、不用 emoji，颜色只用 `theme.go` 令牌（见 `docs/UI.md` §1、§7）。
9. 提交信息格式：`<模块>：<做了什么>`（中文，与历史一致，如 `搜索：结果显示完整文件名与相对路径`）；文档提交 `docs: …`。PR 标题同首个提交，正文列出：任务 ID、改动文件、测试、截图路径、已知问题。
10. 用户可见行为变化需同步更新 `README.md` 对应段落；界面/动效数值变化同步更新 `docs/UI.md`。

## 任务表

| ID | 标题 | 优先级 | 状态 | 规格 | 依赖 | 分支 |
| --- | --- | --- | --- | --- | --- | --- |
| F6 | gofmt gifplay.go | 高 | 已完成 | 本文 | — | `chore/gofmt-gifplay` |
| F7 | 修复 CI 圈选测试偶发失败（全局 AI 动效隔离） | 高 | 已完成 | 本文 | — | `fix/marquee-flake` |
| F1 | 修复 TestDiskPageRenders | 高 | 已完成 | 本文 | — | `fix/test-diskpage` |
| F2 | 修复 TestAppDeclaredExtsBundle | 高 | 已完成 | 本文 | — | `fix/test-extsbundle` |
| F3 | 修复 TestBatchRenameDialogRowsAndDrag | 高 | 已完成 | 本文 | — | `fix/test-batchrename` |
| F4 | 修复 diskhealth smartKeys（Linux） | 高 | 已完成 | 本文 | — | `fix/test-smartkeys` |
| F5 | 消除 TestRemoteExtRun 偶发失败 | 中 | 已完成 | 本文 | — | `fix/test-remoteext` |
| R1 | 拆分 searchbox.go | 高 | 已完成 | [03 §3](docs/specs/03-refactor-next.md) | — | `refactor/searchbox-split` |
| S1 | 搜索：完整文件名 + 相对路径 + 两行行 | 高 | 已完成 | [01 阶段 1](docs/specs/01-global-search.md) | R1 | `feat/search-p1` |
| S2 | 搜索：路径分段可点击 | 中 | 已完成 | 01 阶段 2 | S1 | `feat/search-p2` |
| S3 | 搜索：相关度排序 | 高 | 已完成 | 01 阶段 3 | S1 | `feat/search-p3` |
| S4 | 搜索：匹配原因标签 | 中 | 已完成 | 01 阶段 4 | S3 | `feat/search-p4` |
| S5 | 搜索：筛选条 | 中 | 已完成 | 01 阶段 5 | S3 | `feat/search-p5` |
| S6 | 搜索：分组切换 | 低 | 已完成 | 01 阶段 6 | S3 | `feat/search-p6` |
| S7 | 搜索：右侧预览 | 中 | 已完成 | 01 阶段 7 | S1 | `feat/search-p7` |
| S8 | 搜索：页脚索引进度 | 低 | 已完成 | 01 阶段 8 | S1 | `feat/search-p8` |
| W1 | Windows 隐藏属性用于地址栏补全 | 中 | 已完成 | 本文 | — | `fix/win-hidden` |
| T1 | 侧栏目录树润色 | 中 | 已完成 | [02](docs/specs/02-tree-polish.md) | — | `feat/tree-polish` |
| TB1 | 拖动文件悬停标签自动切换 | 中 | 已完成 | [04 A](docs/specs/04-tabs-followups.md) | — | `feat/tab-drag-hover` |
| TB2 | 会话恢复后退/前进历史 | 中 | 已完成 | 04 B | — | `feat/session-history` |
| TB3 | 切换标签时保存进行中的重命名 | 高 | 已完成 | 04 C | — | `fix/rename-on-tab-switch` |
| A1 | 提示词与工具同步检查 + 真实模型回归 | 中 | 已完成 | [05 A、C6](docs/specs/05-assistant-followups.md) | — | `feat/assistant-eval` |
| A2 | OpenUI/Markdown 遗留修复 | 中 | 已完成 | 05 C1–C5 | — | `fix/assistant-render` |
| A3 | 回复中的目录树组件 | 低 | 已完成 | 05 B | A2 | `feat/assistant-tree` |
| A4 | 助手配置迁入系统设置 | 高 | 已完成 | [06](docs/specs/06-assistant-settings.md) | A1–A3、R6 | `feat/assistant-settings` |
| FX1 | AI 处理动效可见（助手旁呼吸光/滑出） | 高 | 已完成 | 本文 / `docs/UI.md` §6 | — | `feat/ai-fx-visible` |
| A5 | 助手斜杠命令菜单 + Enter/Shift+Enter 文档与测试 | 高 | 已完成 | 本文 / `docs/UI.md` | A4 | `feat/assistant-slash` |
| R2 | Entry 移出 state.go | 中 | 已完成 | 03 §1 | — | `refactor/entry` |
| R3 | fsops 剩余：freeName/placeItem/absPath/undo 类型 | 中 | 已完成 | 03 §2 | — | `refactor/fsops-rest` |
| R4 | 拆分 diskhealth.go | 低 | 已完成 | 03 §5 | F1 | `refactor/diskhealth-split` |
| R5 | internal/search | 低 | 已完成 | 03 §4 | R2、S1–S8 全部 | `refactor/internal-search` |
| R6 | internal/assistant（6a–6d） | 低 | 已完成 | 03 §6 | R3、A1–A3 | `refactor/internal-assistant` |
| SCH1 | 定时任务：每日钟点 + 文件规则/脚本/助手 + 设置页 + MCP | 高 | 已完成 | [07](docs/specs/07-scheduled-tasks.md) | A4 | `feat/scheduled-tasks` |
| FORM1 | 规范表单组件：共享 schema + OpenUI 扩展 + present_form + 定时任务表单 v2 | 高 | 已完成 | [08](docs/specs/08-forms.md) | SCH1、A5 | `feat/form-components` |
| OPENUI2 | OpenUI 扩展控件 + 六场景演示 + 演示路径沙箱 | 高 | 已完成 | [08](docs/specs/08-forms.md) OPENUI2 | FORM1 | `demo/openui-scenes` → `main` |
| OPENUI3 | OpenUI 列表/图片/视频控件 + 媒体演示场景 | 高 | 已完成 | [08](docs/specs/08-forms.md) OPENUI3 | OPENUI2 | `feat/openui-media` → `main` |
| OPENUI-SKEL | OpenUI 加载骨架：虚线占位 + 呼吸条 + 平滑替换 | 高 | 已完成 | 本文 / `docs/UI.md` §6 | OPENUI2 | `feat/openui-loading-skel` → `main` |
| OPENUI-MEDIA | OpenUI 图文列表 MediaList：左缩略图卡 + 右标题正文 | 高 | 已完成 | [08](docs/specs/08-forms.md) OPENUI-MEDIA | OPENUI3 | `main` |
| OPENUI-MORE | OpenUI 文件行 Files、步骤 Steps、指标 StatCards、图库 Gallery | 高 | 已完成 | [08](docs/specs/08-forms.md) OPENUI-MORE | OPENUI-MEDIA | `main` |
| TASKSYS | 统一任务系统：internal/tasks、中间区翻转任务视图、助手任务工具（T1–T7） | 高 | 进行中（T1–T4） | [10](docs/specs/10-task-system.md) | ARCHIVE-PROG、PROG-STYLE | — |
| TREE-NOEXPAND | 设置「展开到打开的文件夹」：关闭后导航不展开侧栏树，保留手动展开，当前文件夹不可见时淡标最近可见祖先 | 低 | 完成 | — | — | — |
| FLIP-PERF | 任务视图翻转降卡顿：翻转期间冻结两面数据、预热任务视图、背面只建 8 张卡、边缘模糊、超 20ms 回退淡入淡出、落定 80ms 淡入；`TestFlipFrameTimes` 2000 文件 + 500 历史：p50 67.8→17.5ms、p95 149→27ms | 中 | 完成 | — | TASKSYS | — |
| VISUAL2 | 视觉语言 v2：小圆角令牌、默认 v2、经典可切、纵向对齐 | 高 | 已完成 | [11](docs/specs/11-visual-language.md) | — | — |
| ADDR-RECENT | 地址栏下拉：最近访问文件夹（标题·路径），点选导航 | 中 | 已完成 | — | — | — |
| EXTRACT-ASK | 右键解压：密码本失败后任务卡询问密码（可写入密码本）；嵌套内层同样询问 | 高 | 已完成 | 本文 | ARCHIVE-PROG、TASKSYS T3 | `main` |
| PROG-STYLE | 进度条样式优化与可选绿色仪表盘 | 中 | 已完成 | [09](docs/specs/09-archive-progress.md) PROG-STYLE | ARCHIVE-PROG | `main` |
| ARCHIVE-PROG | 压缩 / 解压按字节进度：百分比、速度、当前文件、剩余时间、取消；助手实时进度卡 | 高 | 已完成 | [09](docs/specs/09-archive-progress.md) | OPENUI-MORE | `main` |
| OPENUI-MORE2 | Gallery 列数与双击打开、Files 文件夹图标进入与 stat 缓存 | 中 | 已完成 | [08](docs/specs/08-forms.md) OPENUI-MORE | OPENUI-MORE | `main` |
| SCH2 | AI 任务立刻跑：助手预设 + assistant_task_run + 斜杠 /分类 /立即执行 | 高 | 已完成 | [07](docs/specs/07-scheduled-tasks.md) | SCH1、FORM1、A5 | `feat/ai-task-run` |
| A6 | 助手全量文件读取与看图 | 高 | 已完成 | 本文 | A5 | `feat/assistant-fileview` |
| CHAT1 | 助手对话窗口改版：文件操作卡片、确认/撤销、执行计划、离线演示 | 高 | 已完成 | 本文 / `docs/UI.md` 助手对话窗口 | A5、FX1 | `demo/assistant-chat` → `main` |
| IMAGE1 | 图片快速查看：侧栏大图 + 空格缩放/平移/同目录翻页 | 高 | 已完成 | 本文 / `docs/UI.md` IMAGE1 | — | `feat/image-quickview` → `main` |
| VIDEO1 | 视频关键帧预览：空格 scrub 抽帧 + 系统播放器打开 | 高 | 已完成 | 本文 / `docs/UI.md` VIDEO1 | IMAGE1 | `feat/video-quickview` → `main` |
| DOCPREV1 | 文档快速预览：空格叠层 PDF/Office 栅格翻页 + 文本等宽 + 侧栏首页缩略图 | 高 | 已完成 | 本文 / `docs/UI.md` DOCPREV1 | VIDEO1、DOC1 | `feat/doc-preview` → `main` |
| DOC1 | 文档图标：PDF/HTML 配色字形 + 大图标 PDF 首页 / HTML favicon | 中 | 已完成 | 本文 | — | `feat/doc-icons` |
| R7 | 远程处理函数移入 internal/remote | 低 | 待开发（以后） | 03 §7 | R5、R6 | — |
| — | 地址栏修复 | — | 已完成 | — | — | `fix/addressbar` |


建议顺序：F6 → F1–F4 → R1 → S1 → S3 → TB3 → 其余并行；R5 必须在 S1–S8 全部完成后。

---

## 任务详情

通用交付要求（每个任务都适用，下文不再重复）：代码 + 测试、通过工作方式第 6 条、UI 任务附截图到 `docs/assets/shots/<ID>/`、更新本表状态、必要时更新 `README.md` 与 `docs/UI.md`。

### IMAGE1 — 图片快速查看

- **目标**：选中图片时快速预览——侧栏/详情区大图（Contain）+ 空格轻量预览层（缩放、平移、同目录上一张/下一张）；不做视频内嵌播放。
- **范围**：`imageview.go`、`quicklook.go`、`preview_pane.go`、`state.go`；复用 `decodeBytesScaled` / OpenUI `formDecodeBitmap` 解码路径；`docs/UI.md`、本文；截图 `docs/assets/shots/IMAGE1/`。
- **验收**：JPEG/PNG/GIF/WebP/BMP 可预览；坏图占位；大图异步解码不卡列表；滚轮缩放与拖拽平移；`←`/`→` 仅在同目录图片间步进；`gofmt`/`go build`/`vet`/`test` 与 Windows 交叉编译通过；仅推 `feat/image-quickview`。
- **已知限制**：HEIC/AVIF/TIFF 依赖系统缩略图；缩放基于已解码位图（最长边约 2048），不是原图像素级；视频见 VIDEO1。
- **状态**：已合入 `main`（`74106f8`）。

### VIDEO1 — 视频关键帧预览

- **目标**：选中视频时快速预览——侧栏关键帧海报 +「用系统播放器打开」；空格轻量层关键帧 + 细进度 scrub 抽帧；真正播放走系统播放器（无内嵌完整播放器）。文档栅格预览另开任务。
- **范围**：`videoview.go`、`media.go`（`videoFrameAt`）、`quicklook.go`、`preview.go`（保持 `previewVideo`）、`preview_pane.go`、`state.go`；`docs/UI.md`、本文；截图 `docs/assets/shots/VIDEO1/`；参考 `PREVIEW-DEMO/preview-video.png` 与 mockup NOTE。
- **验收**：空格对视频可 scrub 看帧；按钮文案「用系统播放器打开」；无 ffmpeg 时优雅降级；`gofmt`/`go build`/`vet`/`test` 与 Windows 交叉编译通过；合入 `main` 并 push（禁止 force-push）。
- **已知限制**：抽帧依赖本机 `ffmpeg`/`ffprobe`；scrub 防抖约 120ms，拖动极快时帧会略滞后。
- **状态**：本提交合入 `main`。

### DOCPREV1 — 文档快速预览

- **目标**：空格打开应用内叠层：PDF 与 Office 栅格化前几页（每屏 3 页，最多 30 页），可翻页；文本/代码等宽字体预览，大文件截断（64 KB）并提示；html 按文本显示。侧栏显示首页缩略图。回车与双击仍用系统默认应用打开（叠层内回车同样交给系统并关闭叠层）。
- **范围**：新增 `docview.go`（`docPDFSource` LibreOffice 转 PDF 并按路径+大小+mtime 缓存、`pdfPageCount`/`pdfPageImage`、`docQuickView` 翻页状态、`docPoster`、`monoFont`/`expandTabs`）、`docview_test.go`；`quicklook.go`（`previewDoc` 走 `docQuickContent`、文本等宽与截断提示、叠层内回车）、`preview.go`（`kindByExt` 增 odt/ods/odp/rtf/htm 等，`previewDoc` 用 `docPoster`）、`preview_pane.go`（首页缩略图说明、降级原因、等宽）、`state.go`；复用 `pdfthumb.go`（pdftoppm / Quick Look）。参考 `PREVIEW-DEMO/preview-doc.png`。
- **降级**：无 LibreOffice 时 Office 显示占位文案「未安装 LibreOffice，无法在应用内预览 Office 文档」+「打开」；无 pdftoppm 时仅首页（macOS Quick Look），翻页页卡显示「此页无法渲染」。
- **验收**：`go build`/`vet`/`test ./...` 与 `GOOS=windows`/`darwin` 交叉编译通过；`TestDocPageWindow`、`TestKindByExtDocs`、`TestDocPDFSourceNoOffice`、`TestDocOfficeConvertAndPages`（有 soffice 时）；截图 `docs/assets/shots/DOCPREV1/`。
- **已知限制**：首次预览 Office 需等待 LibreOffice 转换（数秒，超时 40 秒）；html 不渲染；页卡比例按 3:4 估计。Linux 下截图环境中 `mygo.Clipboard.Read` 可能阻塞主线程（与本任务无关，截图时临时绕过，未提交）。
- **状态**：本提交合入 `main`。

### OPENUI-SKEL — OpenUI 加载骨架占位
- **目标**：OpenUI/`form` 流式未闭合时，助手对话里先画出虚线圆角骨架（GPT 式呼吸条），内容到达后 180ms 交叉淡入替换，无硬切闪白。
- **范围**：`assistant_markdown.go`（`assistOpenPlaceholder` / `assistOpenSkelBar` / `assistOpenReveal`）；`docs/UI.md` §6；截图 `docs/assets/shots/OPENUI-SKEL/`；本文。
- **参考**：mygo `ui.BorderDashed`（`examples/gallery` 虚线放置区）、`Element.Loop("pulse", …, Bounce(EaseInOut))`（`ui/widgets.go` 占位说明）；本仓 `marquee.go:StrokeDashed`、`aifx.go` 呼吸节拍 `aiFxPulse`。
- **验收**：加载态可见虚线框+呼吸条；闭合后变为真实 OpenUI；高度跳变 ≤2 DIP；`gofmt`/`go build`/`vet`/`test` 与 Windows 交叉编译通过；合入 `main` 并 push（禁止 force-push）。

### OPENUI-MORE — 文件行 / 步骤 / 指标卡 / 图库
- **目标**：补齐 ChatGPT 式高信息密度展示组件，适配文件管理助手：文件结果、执行进度、关键指标、图片网格。
- **范围**：`internal/assistant/openui_more.go`（解析、别名、状态归一）、`openui.go` 分派、`prompt.go`；`assistant_openui_more.go`（绘制，复用 `mediaThumb`/`fileIcon`/`videoBadge`/`openQuickLookPath`/`openWithOS`）；离线演示 `assistant_openui_scenes.go`（「更多组件」/`openui-more`）；`docs/specs/08-forms.md`、`docs/UI.md`。
- **交付**：`Files`/`FileCard`、`Steps`/`Step`、`StatCards`/`StatCard`、`Gallery`，均支持位置参数与对象形式；系统提示写明何时用；截图 `docs/assets/shots/OPENUI-MORE/01-stats-steps.png`、`02-files-gallery.png`、`03-narrow-pane.png`、`04-gallery-quicklook.png`。
- **验收**：解析与绘制单测通过（含点图标打开快速预览）；`go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过。
- **已知限制**：~~Gallery 无列数参数、无双击系统打开；Files 每帧 stat；文件夹图标单击无反应~~（OPENUI-MORE2 已解决）；远程路径不读大小/时间。
- **状态**：已完成，合入 `main`。

### OPENUI-MORE2 — 图库列数 / 双击打开 / 文件夹进入 / stat 缓存
- **目标**：消除 OPENUI-MORE 的已知限制，交互与 Files、MediaList 一致。
- **范围**：`internal/assistant/openui_more.go`（`openGalleryCols`，Extra 存列数）、`prompt.go`；`assistant_openui_more.go`（`openGalleryLayout`、双击判定、`openStat` 缓存、`openUISysOpen`/`openUINow` 钩子）；演示 `Gallery(…, 3)`；`docs/specs/08-forms.md`、`docs/UI.md`。
- **交付**：`Gallery(items, title?, cols?)` / `{cols}`（1–8，窄于 72 DIP 退回自动排，缩略图 256/512 随瓦片宽）；Gallery 单击延迟 300ms 快速预览、双击系统打开；Files 文件夹图标单击进入；本地 stat 按路径缓存 2s；瓦片悬停/聚焦描边、提示含说明；截图 `docs/assets/shots/OPENUI-MORE/05-gallery-cols.png`、`06-gallery-narrow.png`。
- **验收**：单测覆盖列数解析、布局回退、单击预览/双击系统打开（注入打开钩子）、文件夹进入、stat 缓存 TTL；`go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过。
- **已知限制**：单击预览有 300ms 延迟；stat 缓存不监听文件系统，靠 TTL 过期；首帧按默认宽度排版，第二帧起按实测宽度。
- **状态**：已完成，合入 `main`。

### OPENUI-MEDIA — 图文列表 MediaList
- **目标**：助手 OpenUI 可画 ChatGPT 式图文步骤列表：每行左侧圆角缩略图卡（图片 / 视频关键帧+播放角标 / 文档缩略，失败退回文件图标），右侧粗体标题（可编号）+ 正文；点缩略图打开快速预览。
- **范围**：`internal/assistant/openui_media.go`（`MediaList`/`MediaItem` 解析、kind 推断）、`openui.go` 分派、`prompt.go`；`assistant_openui_media.go`（绘制、http(s) 缓存、`openQuickLookPath`）、`quicklook.go`/`preview.go`/`state.go`（`quickLookFile` 覆盖）；`docs/specs/08-forms.md`、`docs/UI.md`；截图 `docs/assets/shots/OPENUI-MEDIA/04-medialist.png`、`05-medialist-quicklook.png`；离线演示：`SUNVALE_OPENUI_DEMO=1` 下发送含「图文」或 `medialist` 的消息（`SUNVALE_DEMO_MEDIA_DIR` 可指向真实样例）。
- **交付**：语法 `MediaList(items, title?, "numbered"?)`，`MediaItem(media, title, text, kind?, n?)` 或对象 `{media, kind, title, text, n}`；系统提示已写明用法；流式时沿用 OPENUI-SKEL 骨架。
- **验收**：解析（位置参数/对象/kind 推断/编号）与绘制（标题、正文可见，点缩略图打开快速预览）单测通过；`go build` 与 `go test ./... -short` 通过；截图可见图片、视频角标、文档、图标回退四种卡片。
- **已知限制**：http(s) 仅首次后台下载（≤16 MB），下载期间显示图标；远程视频/文档不抽帧（显示图标，下载完成后按本地文件处理）；Office 文档缩略依赖系统缩略图，多数退回文件图标。
- **状态**：已完成，合入 `main`。

### OPENUI3 — 列表 / 图片 / 视频

- **目标**：助手表单可用可勾选/单选文件清单、内嵌图片预览、视频关键帧+系统打开播放；附演示场景与截图。
- **范围**：`internal/forms`（`list`/`image`/`video` kind、`ListItem`、scenes `filelist|image|video`、sandbox PNG/clip）、`forms_paint.go` / `forms_media.go`、`internal/assistant/openui*.go`、`assistant_openui_paint.go`、`prompt.go`；截图 `docs/assets/shots/OPENUI-MEDIA/`；`docs/UI.md`、`docs/specs/08-forms.md`。
- **验收**：`ListField`/`ImageField`/`VideoField` 解析与绘制单测通过；空列表占位、坏图占位、关键帧+「打开播放」可见；`gofmt`/`go build`/`vet`/`test` 与 Windows 交叉编译通过；仅推 `feat/openui-media`（不 force-push main）。
- **已知限制**：mygo/Onevra 无原生 video；关键帧依赖本机 `ffmpeg`；列表「选择方式」分段仅为表单字段，不动态改写同一 List 的 multi/single。

### F6 gofmt gifplay.go
- **目标**：`gofmt -l .` 无输出。
- **范围**：仅 `gofmt -w gifplay.go`，不改逻辑。
- **验收**：`gofmt -l .` 为空；`git diff --stat` 只有 `gifplay.go`。

### F7 修复 CI TestMarqueeBand 偶发失败
- **目标**：`go test ./...` 在 CI（全量、macOS runner）稳定通过。
- **根因**：`aiFx` 是进程级全局，文件工具给行点灯、靠一轮结束的 `endRun` 熄灯；工具用例直接调 `copy_items` 只 `markBusy` 不清，状态栏的强调色呼吸点“AI 处理中”被 `TestMarqueeBand*` 的整块像素扫描区（含状态栏）收进包围盒，右边界从 685 漂到 1131。单跑该用例永远过，全量才挂。
- **修法**：`newTestApp` 给每个 UI 用例一份自己的 fx 店并在结束时还回去；用例自带 fx 店的（`swapTestFx`）不动。只改测试，不改产品代码。
- **验收**：`go test -run 'TestSunvaleMCPReachesAnyPath|TestMarqueeBand' .` 10/10；`go test ./...` 三轮全绿。

### F1 修复 TestDiskPageRenders
- **目标**：该用例在 Linux CI（无 SMART、虚拟机）下稳定通过。
- **范围**：先定位原因（多半依赖真实磁盘探测或 smartctl）。修法优先“测试注入假的 `diskhealth.Report`”，不得让测试依赖硬件（`AGENTS.md`）。只有当失败暴露真实缺陷时才改 `diskhealth.go`。
- **交付**：提交信息写明根因。
- **验收**：`go test -count=5 -run TestDiskPageRenders .` 连续通过；`TestDiskPageRenders` 从已知失败列表删除（更新本文件与 `docs/specs/README.md`）。

### F2 修复 TestAppDeclaredExtsBundle
- **目标**：用例通过。
- **范围**：定位为测试数据过期（扩展声明与打包清单不一致）还是 `extensions.go` 缺陷；两者择一修，说明理由。
- **验收**：`go test -count=3 -run TestAppDeclaredExtsBundle .` 通过；已知失败列表删除该项。

### F3 修复 TestBatchRenameDialogRowsAndDrag
- **目标**：用例通过。
- **范围**：`batchrename.go` 与其测试；若因对话框宽度/行高在近期界面打磨（ac058d7）后变化，更新断言并在测试注释说明新布局依据。
- **验收**：`go test -count=3 -run TestBatchRenameDialogRowsAndDrag .` 通过；附一张批量重命名对话框截图。

### F4 修复 diskhealth smartKeys（Linux）
- **目标**：`go test ./diskhealth/` 在 Linux 通过。
- **范围**：`diskhealth/linux.go`、`smartctl.go` 与测试；不得要求真实 smartctl，用固定 JSON 夹具。
- **验收**：Linux 下 `go test -count=3 ./diskhealth/` 通过；Windows/macOS 交叉编译通过。

### F5 消除 TestRemoteExtRun 偶发失败
- **目标**：不再偶发失败。
- **范围**：`remote_ext_test.go:TestRemoteExtRun` 与相关 `remote_ext.go`。查找固定端口、sleep 计时、共享全局状态；用可等待的信号替代 sleep。
- **验收**：`go test -count=50 -run TestRemoteExtRun .` 全部通过；`-race` 下无数据竞争报告。

### R1 拆分 searchbox.go
- **目标**：S1 开工前把 1140 行的 `searchbox.go` 拆成面板/快捷键/主体三个文件，减少后续冲突。
- **范围**：纯移动，按 `docs/specs/03-refactor-next.md` §3 的函数清单；不改任何语句。
- **交付**：提交信息 `重构：searchbox.go 拆分为面板、快捷键与主体，代码原样移动`。
- **验收**：`git diff -M --stat` 显示仅移动；`git show --color-moved` 无非移动改动；全部测试与改前结果相同。

### S1 搜索：完整文件名 + 相对路径 + 两行行
- **目标/范围**：见 01 阶段 1（`searchpath.go:searchPathSegments`、`searchPathText`、`searchNameSpans`，`searchHitRow` 两行 44px，`files_details.go:searchLocation` 相对路径）。
- **交付**：`searchpath_test.go`；更新 `searchbox_test.go` 行距断言；截图：快速搜索结果 + 窗格搜索详细信息视图（使用 01 规格中的嵌套样例数据）。
- **验收**：
  - 样例中 `report.docx/.md/.txt/.pdf` 显示完整扩展名；
  - `~/Library/Caches/com.app/very/deep/nested/folder/structure` 显示为 `主文件夹 › Library › … › folder › structure`；
  - 悬停行显示完整路径；
  - 窗格搜索“位置”列显示相对搜索根的路径而非单个文件夹名；
  - 截图与 `docs/specs/assets/search-01-current-vs-improved.png` 右侧的行结构一致。

### S2 搜索：路径分段可点击
- **目标/范围**：01 阶段 2。
- **验收**：Tester 点击分段后 `a.cur().path` 为该段路径且面板关闭；点击分段不改变光标；省略段不可点击但有提示。

### S3 搜索：相关度排序
- **目标/范围**：01 阶段 3（`searchquery.Query.Score`，`sortHitsByScore`，相关度/名称切换）。
- **验收**：样例数据顺序前三为 `report-final.docx`、`report.md`、`report.txt`（或与规格权重一致的确定顺序，写进测试）；`Library/Caches` 下结果排在主文件夹普通结果之后；流式批次到达时已选中的结果不移位；切换“名称”恢复旧顺序。

### S4 搜索：匹配原因标签
- **验收**：名称命中显示“文件名”，只有路径命中显示“路径”且高亮落在第二行路径中；不出现“内容”标签。

### S5 搜索：筛选条
- **验收**：`dm:`、`size:`、`kind:` 修饰符有解析与匹配单测；点击“类型：文档”后计数显示 `x / y 个结果` 且 x 正确；切换筛选不重新遍历磁盘（测试中计数遍历次数）；“位置：当前文件夹”与本文件夹范围一致。

### S6 搜索：分组切换
- **验收**：按文件夹/按类型分组标题数与组内数量正确；↑↓ 跳过标题行；1 万条结果下切换分组一帧内完成（基准测试 < 16ms）。

### S7 搜索：右侧预览
- **验收**：面板 960 宽；窗口宽 < 900 时隐藏预览；选中第二条后预览显示其完整路径；“复制路径”与 Ctrl+C（查询框无选区时）写入剪贴板 `e.Path`；“打开所在位置”等同 Ctrl+Enter；排序依据列出 `Score` 返回的原因。截图对照 `docs/specs/assets/search-02-preview-filters.png`。

### S8 搜索：页脚索引进度
- **验收**：构建中页脚显示 `索引 n 项` 与进度条，`estimate` 为上次 `total`；首次构建为不定进度；数字有千分位；`ReduceMotion` 下不定进度条静止。

### W1 Windows 隐藏属性用于地址栏补全
- **目标**：地址栏补全（`pathinput.go:readFolderNames`）在 Windows 上把带 `FILE_ATTRIBUTE_HIDDEN` 的文件夹视为隐藏（目前只认点开头）。
- **范围**：新增平台函数 `sysHiddenName(dir string, e os.DirEntry) bool`：`sys_windows.go` 读属性（`syscall.Win32FileAttributeData` 或 `e.Info().Sys()`），其他平台返回点开头判断；`readFolderNames` 调用。评估文件列表（`fs.go:visibleEntries`）是否沿用同一函数——若沿用，单独提交。
- **交付**：Linux 下单测点开头规则；Windows 专用测试加 `//go:build windows`（用 `attrib +h` 或 `SetFileAttributes` 建隐藏目录）。
- **验收**：`GOOS=windows` 与 `GOOS=js` 编译通过；Windows 上隐藏的 `AppData` 在“不显示隐藏项目”时不出现在补全中，开启后出现。

### T1 侧栏目录树打磨
- **目标/范围**：02 规格 A（箭头 150ms 旋转）、B（悬停缩进引导线）、C（方向键导航）、D（平滑滚动，可选；mygo 不支持则在 PR 说明并跳过）。可拆为 4 个提交。
- **交付**：`navtree_keys_test.go`；截图：箭头旋转中间帧、引导线、焦点环；更新 `docs/UI.md` §6.2 → §6.1。
- **验收**：展开后 ≥200ms 箭头为 90°；`ReduceMotion` 下直接到终值；方向键行为符合规格表且不触发导航（Enter 才导航）；现有 `keynav_ui_test.go`、`navtree_test.go`、`navtree_jump_test.go` 全部通过。

### TB1 拖动文件悬停标签自动切换
- **验收**：纯函数 `tabHoverSwitch` 单测覆盖 <600ms 不切换、≥600ms 切换、当前标签不切换、离开清零；放下仍为移动到该标签文件夹。

### TB2 会话恢复后退/前进历史
- **验收**：`session_test.go` 往返测试恢复 `history/histIdx`；每标签最多 50 条；不存在的目录被剔除且 `histIdx` 仍指向当前路径；无新字段的旧会话文件正常加载。

### TB3 切换标签时保存进行中的重命名
- **验收**：`rename_ui_test.go`：开始重命名 → 输入 → 切换标签 → 文件已改名且可撤销；重名时给出与 Enter 相同的提示且切换不受阻；地址栏草稿仍被丢弃。

### A1 提示词与工具同步检查 + 真实模型回归
- **范围**：05 C6 `TestPromptListsAllTools`（默认运行）；05 A `assistant_eval_test.go`（`//go:build assistanteval`，只从环境变量读密钥）。
- **交付**：README 增加“助手回归”小节说明运行方法；PR 附一次运行汇总表（不含密钥）。
- **验收**：默认 `go test` 不访问网络；`TestPromptListsAllTools` 对 `assistant_mcp.go` 注册的每个工具名都通过；8 条评测用例在至少一个模型上通过，失败项附提示词修改。

### A2 OpenUI/Markdown 遗留修复
- **范围**：05 C1–C5。
- **验收**：流式占位到卡片无高度跳变（逐帧截图高度差 ≤ 2px）；表格长路径单元格单行中间省略并有提示；反引号中含空格的 Windows 路径整体可点击；深色主题截图一张。

### A3 回复中的目录树组件
- **验收**：`Tree/TreeItem` 解析与绘制测试；深度 > 6 或节点 > 200 截断并显示“…另有 n 项”；点击带路径节点在窗口打开；提示词已包含组件说明。

### R2 Entry 移出 state.go
- **验收**：新包仅含 `Entry`；主包 `type Entry = …` 别名；除 `state.go` 与新包外无文件改动；测试结果与改前相同。

### R3 fsops 剩余
- **验收**：`fsops.FreeName`、`fsops.PlaceItem`、`fsops.AbsFrom` 与 undo 数据类型有子包单测；主包行为不变（`assistant_tools_test.go`、撤销相关测试通过）。

### R4 拆分 diskhealth.go
- **验收**：纯移动（同 R1 的检查方法）；`TestDiskPageRenders`（F1 后）仍通过。

### R5 internal/search
- **前置**：S1–S8 全部“已完成”。
- **验收**：子包不 import 主包；`globalSearchRoots` 以参数传根；搜索相关测试全部通过，非 UI 用例移入子包；无行为变化（截图对比 S8 后的快速搜索一致）。

### R6 internal/assistant
- **验收**：按 6a–6d 四个分支或四个提交；每步测试通过；绘制与 `sunvaleTools` 留在主包。

### R7 远程处理函数
- 暂不执行；R5、R6 完成后由方案维护者补规格。

---


### DOC1 — 文档图标（PDF / HTML）
- **目标**：列表里 PDF/HTML 更好认；大图标下 PDF 尽量有首屏、HTML 尽量有 favicon；失败有手绘回退。
- **范围**：`icons.go`、`thumb.go`、`pdfthumb.go`、`htmlfavicon.go`、`preview.go`、`files_icon.go`；`docs/UI.md`、本文。
- **验收**：`go test -count=1 -run 'TestFileTintDocTypes|TestHtmlFavicon|TestPdfPoster|TestMediaThumbStarts' .`；详情 16px 自绘；中等及以上可出 PDF/favicon 缩略图。

## 已完成（main）

| 内容 | 提交 |
| --- | --- |
| 助手处理文件的行内动效：呼吸光、改名高亮、滑出收起 | `914a6a3` |
| 界面打磨：标签中文名、平台根卷名、详细信息列自适应、空文件夹大小留空等 | `ac058d7` |
| 动效改为绘制时取状态、按帧重建，补窗口级测试 | `9fff3ce` |
| 导航树点击快速访问/最近位置不再跳动 | `f69b01d` |
| 重构第一轮：`internal/fsops`、`internal/remote`、`files.go` 按视图拆分 | `8c9cd2d`、`d724845`、`5b6e67a`（合并 `76aae4e`） |
| 助手 Markdown 流式渲染、OpenUI 卡片原位与一次作答锁定、系统提示更新 | `17482c0`、`af4d029`、`aa659f1`（合并 `dfa1ffb`） |
| 搜索后台批次按本次范围与查询比对 | `26fbad4` |
| 标签页：切换重读、关闭选邻近、重新打开、拖动排序、中键关闭、溢出滚动等 | `5dd344e`、`e01aa73` |
| 助手去掉访问路径限制 | `bf9ae80` |
| 地址栏：输入解析与补全、修复无法输入、面包屑菜单、行内错误 | `affef9a`、`d4bd849`、`e53b077` |
| 技术方案文档 `docs/specs/` | `6c23c7c` |

### FX1 AI 处理动效可见（助手旁呼吸光/滑出）
- **目标**：助手打开时，文件行右侧呼吸光与移出滑出仍清晰可见，不被右侧助手窗格遮挡；快操作也至少亮满 `aiFxMinGlow`。
- **范围**：`aifx.go`（右缘内缩渐变带、`endRun` 冷却）、`files.go`（文件窗格 `Clip`）、`aifx_*_test.go`；截图 `docs/assets/shots/FX1/`；更新 `docs/UI.md` §6。
- **验收**：助手打开时 UI 测试在行右缘采到强调色、助手区内无溢色；滑出后行从列表消失；`go test -count=1 -run TestAIFx ./...` 通过。

### A5 助手斜杠命令菜单 + Enter/Shift+Enter
- **目标**：输入框草稿以 `/` 开头时弹出命令菜单；确认 Enter 发送、Shift+Enter 换行已由 Onevra `GlassComposer` 提供，不重绑按键，补 UI 测试与 README。
- **范围**：`assistant_slash.go`（目录/过滤/选用/Esc）、`assistant.go:assistantComposer`（菜单叠在输入框上方、`SendDisabled`、Trailing 停止）、`assistant_slash_test.go`；截图 `docs/assets/shots/A5/`；更新 `README.md`、`docs/UI.md`、本表。
- **命令**：`/记忆` 打开会话并填「记住：」；`/整理` `/重命名` 填入提示不发送；`/清空会话` `/新会话` 在空闲时新开会话；`/设置` 打开助手设置。已启用 Skills（files/memory/archive）各一条填能力提示。
- **验收**：`go test -count=1 -run 'TestAssistSlash|TestAssistantComposerEnter|TestAssistantSlash' .` 通过；菜单 ↑↓ Enter Esc 行为符合规格。

### SCH1 定时任务
- **目标**：设置 → 定时任务；进程内分钟调度；文件规则/脚本/助手三种动作；助手可通过 MCP 创建任务。
- **范围**：见 `docs/specs/07-scheduled-tasks.md`。持久化 `sunvale/schedules.json`。
- **验收**：`go test ./internal/schedule ./...`；`TestPromptListsAllTools` 含 schedule_*；手动或 MCP 可建「每晚 19:00 移动下载文档」。

### A6 助手全量文件读取与看图
- **目标**：`read_file` / `read_files` / `read_outside` 读**所有**类型：文本/代码、Word/Excel/PPT/PDF/epub/ODF/RTF 正文、图片、视频帧、音频、压缩包清单、二进制十六进制与格式判断；每条结果写明读取方式。视频与文档缺库时走脚本路线（内置 Python 脚本，可选 Pillow/OpenCV/pypdf/python-docx/mutagen/matplotlib），命令行兜底 `ffprobe`/`ffmpeg`/`pdftotext`，最后系统渲染（Quick Look）。
- **看图**：图片与视频帧作为真实图片分片进模型请求（工具结果经 `[[sunvale-media:…]]` 标记 + 每轮图片袋，`visionModel` 在模型边界转成分片；用户附件同理）。供应商可选「自动判断 / 总是看图 / 不看图」，自动按模型名判断；请求因图片被拒时自动退回纯文字重试。不能看图时结果里写明，不让模型假装看过。
- **范围**：新增 `internal/fileview`（内核 + 内置 `extract.py`）、`assistant_files.go`（读取与图片袋）、`assistant_vision.go`（模型包装）、设置 → 助手新增「文件读取」后端清单与供应商「看图」选项；提示词同步。不改远程预览的抽取实现。
- **验收**：`internal/fileview` 单测（文本/编码/office/图片帧/压缩包/二进制分类）；`assistant_files_test.go`（标记、图片袋、看图重写与降级重试）；`internal/assistant` 看图判定单测；`go build/vet/test` 与 `GOOS=windows/linux/js` 编译通过；截图 `docs/assets/shots/A6/`。

### SCH2 AI 任务立刻跑
- **目标**：助手预设（分类下载 / 收件箱整理 / 按内容重命名）；`assistant_task_run` 一次性全托管；`schedule_run` 与提示词引导「现在跑」；斜杠 `/分类` `/立即执行`。
- **范围**：见 `docs/specs/07-scheduled-tasks.md` SCH2。按内容分类仅走助手预设，不扩展 `file_rule`。
- **验收**：预设单测；`TestPromptListsAllTools` 含 `assistant_task_run`；斜杠测试；截图 `docs/assets/shots/SCH2/`。


### CHAT1 助手对话窗口改版 + 离线演示
- **状态**：演示范围已合入 `main`（`demo/assistant-chat`）。
- **目标**：分清用户与助手消息；助手的文件操作做成卡片（类型、路径、diff 或新文件预览、待确认/执行中/完成/失败/已撤销、撤销按钮）；危险操作确认卡；批量进度；多步计划列表；空状态示例；输入框上方当前目录；时间戳与复制。与文件列表 AI 动效（呼吸光、改名高亮、滑出）对应。
- **范围**：`internal/assistant/fileop.go`（`FileOp`、`PlanStep`、行级 diff）、`types.go`（`Turn.At/Op/Steps`）、`assistant_opcard.go`、`assistant_demo.go`（`SUNVALE_ASSIST_DEMO=1` 离线脚本，在临时文件夹 `sunvale-demo-*` 中真实执行）、`assistant.go`（交换渲染、空状态、目录条、对话列 `FillHeight` 修复）、`assistant_run.go`（演示入口、时间戳）；截图 `docs/assets/shots/CHAT-DEMO/`；更新 `README.md`、`docs/UI.md`。
- **验收**：`go test -count=1 -run 'TestAssistDemo|TestDiffHunks' ./...`（六个场景改动磁盘与卡片一致、删除先确认、各卡撤销复原、取消不改动）；全量测试与 `GOOS=windows/js/darwin` 编译通过。
- **未完成**：真实模型的工具调用尚未产出 `Turn.Op`（卡片目前只由演示脚本生成）；撤销记录只在本窗口内存中，重开会话后卡片不再显示撤销。

## 快速预览文案精简

- **状态**：已合入 `main`，随 v0.2.0 发布。
- **范围**：`quicklook.go`、`preview_pane.go`、`videoview.go`、`docview.go`：删除快捷键提示与解释性文案，错误/降级原因缩为一句，截断提示缩短；`mygo.json` 版本升至 0.2.0。
- **验收**：`go build`、`go vet`、`go test ./...` 通过；截图未重拍。

## UPD1 自动更新镜像换线

- **状态**：已合入 `main`，未发版。
- **根因**：更新检测请求的是 `https://github.com/mcxen/WinExplorer/releases/latest/download/update-<平台>.json`（不走 api.github.com，与限流无关）；v0.2.1 的 release 没有 `update-*.json`（仓库未配 `UPDATE_PRIVATE_KEY`，CI 跳过签名产物），所以检测必然 404。
- **范围**：`updatemirror.go`（`http.DefaultClient` 的传输层：github.com release 下载按镜像顺序换线，每线 20 秒响应头超时；代理不跟随 `latest/download` 跳转，先 HEAD `releases/latest` 解析 tag 再走代理）、`statestore.go`（`updateMirrors`，默认 `https://tvv.tw/`→直连）、设置页「更新」显示下载线路、`main.go` 安装。签名仍由 mygo 用 `mygo.json` 的 `updates.publicKey` 校验。
- **待用户**：本地 `mygo keygen` 生成密钥，私钥内容写入仓库 secret `UPDATE_PRIVATE_KEY`，`mygo-update.pub` 与 `mygo.json` 的 publicKey 必须一致（不一致则换公钥，且旧版无法自动更新到新签名），然后发新版本。

## AST1 助手报错后输入框可用

- **状态**：已合入 `main`，未发版。Linux 上未复现 Windows 的遮挡，按代码推断修复。
- **根因（推断）**：运行失败时错误全文（代理/供应商常返回整页 HTML，Windows 上带 `\r\n`）作为一条助手回复进入 Markdown 渲染，长文本撑高对话流并压在浮动输入框（`FloatingConversation` 的绝对定位层）一带；忙碌状态只在运行收尾的 defer 中复位，失败与复位分两次投递到主线程。
- **范围**：`internal/assistant/types.go`（`Turn.Error`）、`assistant_error.go`（错误文本规范化：`\r\n`→`\n`、截断 2000 字；对话流内单行可折叠、展开限高 160 滚动）、`assistant.go`（错误行渲染在回合页脚）、`assistant_run.go`（`failAssist` 立即复位 running/stop/pending/status）。
- **验收**：`TestAssistantComposerUsableAfterError`（长错误后可点击输入框并再次发送）、`TestAssistErrorText`。

## WIN1 Windows 只开一个窗口

- **状态**：已合入 `main`，未发版。Linux 上无法复现 Windows 双窗口，按代码推断修复。
- **根因（推断）**：`main.go` 从未调用单实例锁。关闭窗口只是藏进托盘，进程常驻；再点快捷方式、NSIS 完成页「运行」叠加开机自启、文件关联打开压缩包，都会再起一个进程并新建窗口（再加上会话恢复，每个进程各开一扇）。mygo 本身不会建两个窗口。
- **范围**：`singleinstance.go`（先走 mygo 的单实例 socket，第二个实例把命令行交给第一个后退出；第一个在 `OnSecondInstance` 里从托盘唤起并聚焦窗口）、`singleinstance_windows.go`（`Local\com.mcxen.sunvale.single-instance` 命名 mutex 兜底：socket 不可用时仍判定为第二实例，按标题找窗口 `SW_RESTORE`+`SetForegroundWindow`；更新重启时旧进程尚未退出，mutex 最多重试 3 秒）、`singleinstance_other.go`、`main.go`。
- **验收**：`TestAcquireSingleInstance`（首个实例、socket 交接、更新重启竞态、socket 失效四种情况）。

### ARCHIVE-PROG — 压缩 / 解压进度
- **目标**：压缩、解压（含助手 zip_files / unzip）可见按字节的进度、速度、当前文件、剩余时间，并可取消。
- **范围**：`archive_progress.go`（任务登记、计数 reader、快照）、`archive_progress_paint.go`（状态栏条、任务卡）、`archive.go`（zip/tar 计数与取消、`doCompress` 后台化）、`archive_nested.go`、`assistant_mcp.go`、`status.go`、`assistant.go` Footer；OpenUI `TaskProgress`（`internal/assistant/openui_more.go`、`prompt.go`）；`docs/specs/09-archive-progress.md`、`docs/UI.md`。
- **交付**：状态栏进度条 + 数字行 + ×；助手本轮任务卡实时刷新至完成 / 失败 / 取消；`TaskProgress` 组件；截图 `docs/assets/shots/ARCHIVE-PROG/01-assistant-live.png`、`02-assistant-done-error.png`、`03-status-bar.png`。
- **验收**：单测覆盖 zip 压缩 / 解压与 tar.gz 字节计数、取消、速度与剩余时间、状态栏 × 取消、助手卡只显示 ai 任务且显示错误、`TaskProgress` 解析与绘制；`go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过。
- **已知限制**：txz / 7z / rar 及加密 ZIP 走外部程序，只有不确定进度条，且不能中途取消；内层归档连解不单独计数；助手任务卡只在最后一轮显示；tar 家族按压缩字节计数，当前成员按 tar 顺序。
- **状态**：已完成，合入 `main`。

### PROG-STYLE — 进度样式与绿色仪表盘
- **目标**：进度条更精致（圆角、淡轨道、缓动填充、不确定横扫、状态着色），并可选绿色半圆仪表盘。
- **范围**：`progress_style.go`（`paintProgressBar`、`paintGauge`、`paintArchiveGaugeCard`、`progressEased`）、`archive_progress_paint.go`、`forms_paint.go:paintFormProgress`、`settingsdialog.go`（进度样式）、`statestore.go`（`ProgressStyle`）；OpenUI `{style}`（`internal/assistant/openui_more.go`、`openui_fields.go`、`prompt.go`）；`docs/specs/09-archive-progress.md`、`docs/specs/README.md`、`docs/UI.md`。
- **交付**：细条与仪表盘两种样式，覆盖进行中 / 不确定 / 完成 / 失败 / 取消；设置一行切换（助手任务卡），状态栏固定细条；`TaskProgress`、`ProgressBar` 支持 `{style: "gauge"}`；截图 `docs/assets/shots/PROG-STYLE/01-bar.png`、`02-gauge.png`。
- **验收**：缓动 / 回退 / 减弱动态单测，设置切到仪表盘后助手卡显示百分比与取消，OpenUI style 解析与绘制；`go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过。
- **已知限制**：缓动状态按字符串键全局保存（上限 256 个）；仪表盘固定 132×76，窄栏下右侧信息被压缩；无头截图不体现动画。
- **状态**：已完成，合入 `main`。

### TASKSYS — 统一任务系统（设计）
- **目标**：用户、助手、定时任务发起的长操作（压缩解压、复制移动、删除、批量重命名、脚本、远程）统一为 Task：进度、暂停 / 继续 / 取消 / 重试、冲突策略、历史、撤销，以及统一查看位置。
- **范围**：新包 `internal/tasks`（模型、Manager、Reporter、冲突、历史、事件）；主包 `tasks_ui.go`、`tasks_pane.go`；迁移 `archive_progress.go`、`ops.go`、`dropops.go`、`trash.go`、`batchrename.go`、`assistant_mcp.go`、`schedule_run.go`、`plugintask.go`。
- **交付（本阶段）**：设计稿 `docs/specs/10-task-system.md`；位置示意 `docs/assets/shots/TASKSYS/01-option-a-bottom-dock.png`、`02-option-b-side-tab.png`、`03-failed-detail-and-status.png`。
- **实现阶段验收**：
  - T1 内核：状态机、并发槽、优先级、路径锁、暂停 / 取消、节流单测，`-race` 通过。
  - T2 归档迁移：现有 ARCHIVE-PROG / PROG-STYLE 测试不变通过，可暂停继续。
  - T3 复制 / 移动 / 删除后台化：1 万小文件复制时 UI 不卡（帧 < 16ms），冲突内联选择，失败项与撤销。
  - T4 任务视图：中间文件区 3D 翻转（360ms EaseInOut，减弱动态淡入淡出），状态栏指示 / 工具栏按钮 / Ctrl+Shift+J / Esc，任务卡含子项进度、最近项、失败项与操作，历史持久化，无浮层。
  - T5 重命名与助手：`task_list` / `task_get` / `task_control`、`TaskProgress(task:)` 实时绑定，助手仅能控制自己的任务。
  - T6 定时与插件：定时运行入任务历史，`taskRunner` 改为薄封装，缩略图 / 大小借用 IO 槽。
  - T7 远程：远程发起的复制 / 归档可见。
  - 每阶段 `go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过，单次提交 < 1000 行。
- **已定**：位置改为中间文件区翻转；冲突、历史、托盘、并发、通知按设计稿默认。
- **状态**：T1–T4 已完成；T5–T7 待做。T3：粘贴 / 拖放 / 删除到回收站为后台任务（逐文件子进度、复制按字节、暂停取消、冲突内联、失败项、结束时入撤销栈）；压缩解压经 `Submit` 真正排队（每盘 2 个）；压缩可重试（保存原选择）；翻转中忽略指针。截图 `07-copy-conflict.png`、`08-tasks-queued.png`。
- **T3 限制**：回收站内还原 / 永久删除 / 清空、助手 `trash_items` 等工具仍同步；上次运行的任务（历史）仅解压可重试；7z / rar 外部工具不可暂停。


### VISUAL2 — 视觉语言 v2（默认）
- **目标**：采用 v2 视觉（mist 底、浮起内容卡、侧栏 chip、蓝强调），圆角更小（卡 8 / 行·按钮·输入 4 / 对话框 8）；v2 为默认，经典可切换；修工具栏/侧栏/列表/状态栏/任务卡纵向对齐。
- **范围**：`visual.go`、`theme.go`、`statestore.go`、`settingsdialog.go`、`main.go`、`sidebar.go`、`tasks_view.go`、`chrome_nav.go`、`addressbar.go`、`archive_progress_paint.go`、`files.go`；`docs/specs/11-visual-language.md`、`docs/UI.md`；截图 `docs/assets/shots/VISUAL2/`。
- **验收**：`go build`、`go vet`、`GOOS=windows go build`、`go test ./... -short`；设置可切经典；截图含 files/tasks × classic/v2 × light/dark。

### ADDR-RECENT — 地址栏最近访问
- **目标**：地址栏面包屑右侧下拉列出最近访问文件夹（标题 · 路径），点选即导航；沿用已持久化的 `Visited`/`visitList`；高密度、无多余文案。编辑地址仍用空白处单击 / Ctrl+L / F4 / Alt+D / 右键菜单。
- **范围**：`addressbar.go`（`addressRecentButton`/`addressRecentItems`）、`addressbar_ui_test.go`；`TASK.md`。
- **验收**：`go test -run TestAddressRecentDropdown`；`go build`/`go vet`/`GOOS=windows go build`/`go test ./... -short`（跳过已知死锁 `TestCopyConflictCardAndQueueShots`）。

#
### EXTRACT-ASK — 解压密码询问
- **目标**：右键 / `doExtract` / 任务解压时，加密归档在密码本全部失败（或为空）后，在任务卡内联询问密码；提交后重试；可选「写入密码本」；取消或留空则该归档失败。嵌套内层归档在书失败后同样询问。
- **范围**：`internal/tasks/ask.go`（`AskKindPassword`、`Answer.Text/Save`）、`archive_password.go`（`extractWithPasswordsAsk`）、`archive_nested.go`、`archive.go`（`extractPasswordAsk`）、`tasks_view.go`（密码卡）、`archive_extract_ask_test.go`；`README.md`、本文。
- **验收**：短测覆盖取消失败、内层询问、Ask 携带 Text/Save；`go build`/`go vet`/`GOOS=windows go build`/`go test ./... -short`。
- **已知限制**：助手 `unzip` / 远程解压仍只走密码本、不弹 UI；无头测试里无 Reporter 时不询问（与冲突 Ask 一致）。
- **状态**：已完成，合入 `main`。

## LOG1 — 诊断日志（尤其助手）
- **目标**：Windows GUI 程序无控制台，也能通过日志文件调试助手与任务。
- **范围**：`internal/applog`（slog、轮转、脱敏、panic）、`applog_glue.go`、`main.go`、`assistant_run.go`、`assistant_mcp.go`、`assistant_openui_adapter.go`、`internal/assistant/openui.go`、`task_run.go`、`updatemirror.go`、`singleinstance.go`、`schedule_run.go`、`assistant_http.go`、`settingsdialog.go`、`assistant_error.go`、`statestore.go`；`docs/specs/12-logging.md`、`docs/UI.md`。
- **验收**：日志写到 `%LOCALAPPDATA%\Sunvale\logs\sunvale.log`（macOS `~/Library/Logs/Sunvale`，Linux `~/.local/state/sunvale/logs`），5 MB × 5 轮转；main 第一行初始化，顶层与助手运行 panic 带栈；设置可调级别、打开文件夹、导出 zip；助手错误行“查看日志”；助手 run id、模型、Base URL、HTTP 状态 / 耗时 / 错误体片段、token 用量、工具调用与结果耗时、MCP、OpenUI 解析失败、定时触发均有记录且密钥脱敏；单测覆盖脱敏、轮转、级别过滤；`go build`、`go vet`、Windows 交叉编译、`go test ./... -short` 通过。
- **已知限制**：流式响应仅记首包耗时；脱敏为规则匹配；非 Go 运行时的原生崩溃不记录。
- **状态**：已完成，合入 `main`。
