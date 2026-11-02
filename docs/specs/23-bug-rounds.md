# 23 缺陷扫描轮次

## BUGSCAN-1 — 文件列表视图与排序分组

- **范围**：详细信息、列表、小至超大图标、紧凑视图、排序与分组。
- **发现**：图标视图每帧调用 `netfs.IsNetworkPath`。其 5 秒缓存过期后，Linux 会读取挂载表并调用 `statfs`，macOS 调用 `statfs`，Windows 调用 `GetDriveTypeW`；网络盘响应慢时会阻塞 UI 线程。
- **修复**：目录重载和装饰条目时探测一次网络挂载，并把结果及网络缩略图预热设置缓存到标签状态；图标视图只读取缓存。
- **回归**：无界面测试在首次绘制后清除网络挂载缓存，再绘制图标视图，确认视图构建不会触发阻塞探测。排序、分组及分组图标键盘滚动测试保持通过。
- **界面巡检**：在 `DISPLAY=:10`、独立 HOME 下检查详细信息、查看菜单和排序菜单；截图位于 `/workspace/sunvale-pi-shots/b1/`。

## BUGSCAN-2 — 侧栏与目录树

- **范围**：导航窗格、此电脑/目录树、快速访问与最近位置、栏宽分隔条。
- **发现**：
  - 侧栏每帧经 `thisPCPlaces` 调用 `sysVolumes`：Linux 每帧读 `/media`、`/run/media`，macOS 每帧 `statfs` 各挂载点，Windows 每帧查询全部盘符与卷标。
  - 目录树首次展开某层时在视图里 `os.ReadDir` 并逐项 `stat` 符号链接；最近位置的 `dirExists` 会在视图里 `os.Stat`，失效目录还会在视图里同步写 `state.json`。
  - `显示 > 隐藏的项目`（菜单与设置页）切换后没有让树已读的子项重新过滤，隐藏文件夹要等刷新才出现或消失。
- **修复**：
  - 新增 `volumes.go`：卷列表缓存 5 秒，启动时在视图外预热；过期时视图只读旧列表并在后台重读，读完重绘并让停留在「此电脑」的窗口重载行。`volumeRootOf` 与全局搜索根也改用该缓存。
  - 目录树子项复用 `pathinput.go` 的目录名后台缓存：视图内的首读在 goroutine 完成，下一帧出行；视图外仍同步读。子项缓存保留，`F5`/当前目录重载会使对应缓存失效。
  - `dirExists` 视图内未命中时先乐观显示，再后台 stat；失败后下一帧移除，同步写状态文件也移到 goroutine。`dirExistsStat` 留作测试阻塞点。
  - 隐藏项切换清空树的子项缓存，用已缓存的全部名字按新设置重新过滤；设置页同步该字段时同样清空。
- **测试**：`TestSidebarVolumesAreCached` 统计视图内卷列表读取次数为 0；`TestDirExistsDoesNotWaitInView` 用阻塞的 stat 证明视图不等待；`TestNavTreeHiddenToggleRefilters` 验证切换隐藏项后树子项立即变化。既有树展开/键盘/最近位置测试改为等待后台目录读取后通过。
- **门禁回归**：`TestSearchBoxRebuildButtonWalksAgain` 在索引构建完成后补一帧再读页脚，避免构建落在第一帧之后时误报“未完整”缺失。
- **界面巡检**：在 `DISPLAY=:10`、独立 HOME 下检查侧栏展开/收起、悬停引导线、栏宽拖动、原生右键菜单，并回归搜索、设置与任务页；截图位于 `/workspace/sunvale-pi-shots/b2/`。

## BUGSCAN-8 — 助手面板（发送/停止/历史/错误显示/斜杠菜单）

- **范围**：`assistant.go`（输入区与拖放提示）、`assistant_run.go`/`assistant_run_stream.go`（发送/停止/流式）、`assistant_hist.go`（历史列）、`assistant_error.go`（出错行）、`assistant_slash.go`（斜杠菜单）。
- **发现**：
  - 出错行文字与图标写死 `ui.Hex("#D14343")`，不随主题变化：深色模式下红字对比度不足，且违反「颜色只用 theme.go 令牌」。
  - 拖放提示胶囊底色是强调色，图标与文字却写死白色；深色模式下强调色为浅色（`#60CDFF`），白字发灰、几乎看不清。
  - `go test -race` 下 `TestAssistantContinueKeepsHistoryAndComposerFocus` 报数据竞争：`postAssist` 在应用未就绪（无界面/测试）时直接在模型 goroutine 上改 `a.assist`，与测试线程逐帧构建视图并发读。生产环境走 `RunOnMain`，仅在无界面测试路径出现；已记录，未改（改动面与回归风险大于收益）。
- **修复**：
  - `assistant_error.go`：出错行图标与文字改用 `tokDanger`，删除写死的 `assistErrorColor`。
  - `assistant.go`：把拖放提示抽成 `assistantDropHint`，图标与文字改用 `colorBtnText()`（强调底上的文字令牌：浅色为白、深色为近黑）。两者都随外观主题/对比度偏好重算。
- **测试**：新增 `assistant_theme_test.go`：`TestAssistErrorLineFollowsTheme` 分别渲染浅/深色出错行，断言深色红色的主导像素更亮（写死红色则两态相同而失败）；`TestAssistDropHintKeepsContrast` 渲染提示胶囊，断言深色下文字最暗像素足够暗、浅色下足够亮（写死白字在深色下最暗像素只到强调底亮度而失败）。两用例在修复前均失败，修复后通过。
- **门禁**：`gofmt -l` 无输出；`go build ./...`、`go vet ./...`、`GOOS=windows`/`GOOS=darwin`/`GOOS=js GOARCH=wasm` 交叉编译通过；助手相关测试 `go test -count=1 -run 'TestAssistant|TestAssist|TestStop|TestLate|TestQueued|TestQueue|TestStarter|TestReadAny|TestHist|TestError|TestBudget' .` 通过。
- **界面巡检**：在 `DISPLAY=:10`、独立 HOME（`/workspace/pi-sched/home-b8`）、独立 XDG 下用新构建启动，点击命令栏「助手」开关面板、顶栏「会话」切换历史列、「全屏」切换全宽，检查对齐与分隔线；截图位于 `/workspace/sunvale-pi-shots/b8/`（`01`–`08`）。该显示环境下 XTEST 合成键盘事件不被 mygo 接收（指针点击正常），斜杠菜单与出错行改由 `ui.NewTester` 离屏渲染验证。

## BUGSCAN-10 — 深浅色×经典/v2 全局巡检、搜索面板（Ctrl+K）、远程/MCP

- **范围**：`theme.go`/`visual.go` 令牌与两套外观、`searchbox*.go`/`searchpreview.go`、`remote*.go`/`assistant_mcp*.go`/`settings_assistant_more.go` 以及会读这些令牌的对话框（`sharedialog.go`、`settings_ext.go`、`batchrename.go`、`videoview.go`、`assistant_openui_more.go`）。
- **发现**（深浅色 × 经典/v2 全局巡检）：
  - 分享对话框、扩展编辑器、批量重命名对话框出错行与冲突行写死浅色红 `#C4361D`，深色模式下与浮层底 `tokSurface` 对比约 2.6:1，且违反「颜色只用令牌」。
  - OpenUI `Steps` 步骤圆点用 `tokSuccess`/`colorAccent()`/`tokDanger` 填充后，勾/叉/编号仍写死白色；深色下这些填充是浅色（如强调 `#60CDFF`），白字对比约 1.8:1。
  - 视频快速预览的强调底按钮「用系统播放器打开」文字写死白色，深色强调下同样发灰。
- **观察但未改**：
  - 磁盘健康页 `diskBadge`/`diskGlyph` 在强调/警告/危险底上画白色盘符，深色下偏灰；修复需连带调整盘形内部细节的对比（盘身与刻线同色系），改动面大于本轮收益，暂记于此。
  - `remoteStart` 的「先查 nil 再 Listen」之间未持锁，UI 与 `/api/sunvale/enabled`、`/api/sunvale/port` 的 HTTP 线程并发调用时，理论上可能多起一个未回收的 listener（当前调用点都是 UI 线程 + 单个管理页，未观测到），未修。
  - `probeRemoteMCP` 的错误原样显示在 MCP 行（`settings_assistant_more.go`）；属诊断信息，保留。
- **修复**：`sharedialog.go`、`settings_ext.go`、`batchrename.go` 的错误行/冲突行改用 `tokDanger`；`assistant_openui_more.go` 步骤圆点前景与 `videoview.go` 按钮文字改用 `colorBtnText()`。搜索面板、远程管理页与 MCP 设置页本身已全量走令牌，无改动。
- **测试**：新增 `theme_contrast_test.go`：`TestShareDialogErrorRedFollowsTheme`、`TestExtEditorErrorRedFollowsTheme`、`TestBatchRenameErrorRedFollowsTheme` 在浅/深两态渲染真实对话框，断言最红像素的红色通道在深色下更亮（写死红则两态相同而失败）；`TestOpenUIStepDotFollowsTheme`、`TestVideoOpenButtonFollowsTheme` 渲染圆点/按钮，断言深色下前景足够暗、浅色下足够亮。五个用例在修复前均失败、修复后通过。
- **门禁**：`gofmt -l` 无输出；`go build ./...`、`go vet ./...`、`GOOS=windows`/`GOOS=darwin`/`GOOS=js GOARCH=wasm` 交叉编译通过；`go test -count=1 -timeout 900s ./...` 通过。
- **界面巡检**：`DISPLAY=:10`、独立 HOME（`/workspace/pi-sched/home-b10`）下启动新构建，xdotool 合成 Ctrl+K 打开快速搜索面板并输入查询（浅色 v2、深色 v2 各一张）；浅/深 × 经典/v2 的文件列表与设置页（插件=远程访问、助手=MCP）用 `ui.NewTester` 离屏渲染。截图位于 `/workspace/sunvale-pi-shots/b10/`（`02-search-hits`、`04-search-dark-v2`、`files-*` 四张、`settings-plugins-dark-v2`、`settings-assistant-dark-v2`，共 8 张）。该显示器上 Ctrl+K 的进程级全局绑定常被其他实例占用（窗口内快捷键与控制器点击仍可用）。

