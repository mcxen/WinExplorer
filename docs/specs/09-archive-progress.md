# 09 — 压缩 / 解压进度（ARCHIVE-PROG）

## 现状盘点

- `archive.go`：`doCompress`（选择 → ZIP，原同步）、`doExtract` → `extractArchives`（后台 goroutine，`aiFx` 呼吸灯）、`extractArchive`（zip/tar/tgz/tbz2 纯 Go；txz 走 bsdtar；7z/rar 走 7-Zip/unar）、`makeZip`/`addTree`/`addFile`。
- `archive_password.go`：无密码 → 密码本逐个重试，每次清空目标；`archive_nested.go`：归档套归档自动连解。
- 助手工具 `zip_files` / `unzip`（`assistant_mcp.go`），过程中只有「正在压缩 / 正在解压」活动行。
- 进度展示：OpenUI `ProgressBar`（表单内静态值，`forms_paint.go:paintFormProgress`）、Onevra `components.Progress`（支持不确定态）。无任务队列；状态栏只有「处理中 N 项」。

## 设计

- `archive_progress.go`：每次运行一个 `archiveTask`（kind、归档路径、来源 user/ai、已完成/总字节、当前成员、取消标志、结束时间、错误），按归档路径登记；结束后保留 10 分钟。
- 计数：`progressReader` 包住拷贝用的 reader，取消时返回 `errArchiveCanceled`。
  - 压缩：总量 = 选中项内常规文件字节（`entriesSize`），按读入源文件计数。
  - 解压 ZIP：总量 = 成员解压后大小之和，按写出字节计数；tar 家族：总量 = 归档文件大小，按读入的压缩字节计数。
  - 密码重试会重新计数；txz、7z、rar 走外部程序，显示不确定进度条。
- 速度 = 已完成 / 已用时；剩余 = (总 − 已完成) / 速度。
- 取消：状态栏或卡片上的 ×；压缩删半成品 ZIP，解压删目标文件夹；嵌套连解每层检查。
- `doCompress` 改为后台运行（无窗口的测试内联）。

## 界面

- 状态栏：最新进行中任务一行 —— 90 DIP 细进度条 + 「解压 a.zip  42% · 12 MB / 30 MB · 8.1 MB/s · 剩余 0:12」+ 多任务时「+N」+ ×；悬停显示当前成员。
- 助手：最后一轮对话底部按开始时间列出本轮 `ai` 来源任务卡（图标、动词 + 名称、数字行、×、4px 进度条、当前成员或错误），每 200ms 刷新直到完成 / 失败 / 取消，结束后保留终态。
- OpenUI `TaskProgress(title, value?, meta?, file?, status?)`（别名 `ArchiveProgress`，或对象 `{title, value, meta, file, status}`）：同一卡片的静态版，供模型总结。

截图：`docs/assets/shots/ARCHIVE-PROG/`。

## PROG-STYLE — 进度样式

- **细条**（`progress_style.go:paintProgressBar`）：全圆角，轨道为状态色 14% 透明度；填充按指数缓出逼近目标（时间常数 80ms，约 240ms 到 95%），回退直接跳；不确定（外部工具 txz/7z/rar）时 35% 宽色带以余弦缓动横扫，周期 1200ms。减弱动态效果时不动画。
- **状态色**：进行中强调色（仪表盘为绿色）、完成绿色、失败红色、取消灰色。
- **仪表盘**（`paintGauge`、`paintArchiveGaugeCard`）：132×76 半圆弧，10px 描边，轨道 16% 绿；值弧同样缓动；弧端内侧一根短指针；中心 20 号粗体百分比；右侧标题、已完成 / 总量、速度 · 剩余、当前文件或错误、×。不确定时 60° 弧段来回摆动（周期 2400ms），中心显示「—」。
- **选择**：
  - 设置 → 外观 → 主题 →「进度样式」条形 / 仪表盘（`appSettings.ProgressStyle`），用于助手实时任务卡；状态栏始终为细条。
  - OpenUI：`TaskProgress(…, {style: "gauge"})`、`ProgressBar(name, value, {style: "gauge"})`；同义词 dial、meter、仪表盘 / bar、line、条形。

截图：`docs/assets/shots/PROG-STYLE/01-bar.png`、`02-gauge.png`（进行中 / 不确定 / 完成 / 失败 / 取消）。
