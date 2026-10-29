# 20 压缩包按文件夹浏览（ARCH-FOLDER）

- 双击 / Enter 打开 zip、tar、tar.gz/tgz、tar.bz2、tar.xz、7z、rar（`github.com/mholt/archives`，纯 Go）进入只读文件夹；路径形如 `/x/示例.zip/子目录`，面包屑、地址栏输入、后退/前进/向上均可用；目录（含 macOS `.app`）永不当作压缩包。
- 实现：`archive_folder.go` 的 `archiveSplit`（逐级找第一个存在的压缩包文件）、按 路径+大小+修改时间 缓存的只读头索引、`archiveLister`（`lister` 接口，由 `entryLister` 选择）。`reload` 时填充 `inArchive` 缓存，视图只读缓存。
- 只读：剪切、粘贴、重命名、删除、新建、压缩禁用或隐藏，调用时提示「压缩包为只读」。
- 打开条目：后台解压到缓存目录后交系统打开；预览窗格同样后台解压单个条目（≤64MB），保留成员的修改时间。
- 复制/拖出到真实文件夹 = 解压到目标；「解压到…」出现在命令栏「更多」与原生右键菜单。
- 测试：`archive_folder_test.go`（zip 与 tar.gz 的拆分、列表、隐式目录、单文件与目录解压、App 内进出与只读）。
