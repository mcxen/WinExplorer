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
- **界面巡检**：在 `DISPLAY=:10`、独立 HOME 下检查侧栏展开/收起、悬停引导线、栏宽拖动、原生右键菜单，并回归搜索、设置与任务页；截图位于 `/workspace/sunvale-pi-shots/b2/`。
