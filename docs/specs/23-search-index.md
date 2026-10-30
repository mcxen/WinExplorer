# 23 全局文件搜索的磁盘索引（SEARCH-IDX）

目标：存储换时间。全局（全部磁盘）搜索先查磁盘上的索引文件，按键即答，不再每次遍历磁盘；常驻内存保持很小。

## 1. 开源方案调研

- **plocate**：对文件名建三元组（3 字节）倒排表，posting list 用 delta + 压缩编码；查询求各三元组列表的交集，再逐个核对候选。2700 万文件几毫秒出结果，库比 mlocate 小一半以上。整库由 `updatedb` 定期重建。
- **mlocate / updatedb**：库按目录存放；重建时目录 mtime 与旧库一致就直接复用旧库里的条目，只重读 mtime 变化的目录。代价是“每个目录一次 stat”，而不是“每个文件一次 stat”。文件改大小 / 时间不改目录 mtime，会滞后到下次（可接受）。
- **FSearch**：内存里存路径分量（文件夹表 + 文件表，按名排序），数据库文件 `fsearch.db` 只用来快速加载；查询是对全部条目线性匹配（多线程）。快，但条目全在堆上，百万文件要上百 MB 内存。
- **Everything**：直接读 NTFS 的 MFT 建库，用 USN 日志增量更新，速度最好；只适用于 NTFS 卷，并需要管理员权限（或服务）。
- **Spotlight / mdfind**：系统守护进程 + FSEvents 卷级变更流，持续更新内容与元数据索引；第三方无法复用其文件名库的内部格式（只能调 `mdfind`，结果受 Spotlight 排除规则影响）。
- **Linux inotify / fanotify**：inotify 按目录加 watch，默认上限（`max_user_watches`）在许多发行版只有 8192 左右，全盘监视不现实；fanotify 能看整个挂载点但需要 `CAP_SYS_ADMIN`。
- **Go 库**：`fsnotify` 封装 inotify / kqueue / ReadDirectoryChangesW，适合监视少量目录；`bleve` 是通用全文检索（分词、倒排、评分、BoltDB 存储），对“文件名子串”这种需求太重：依赖多、写入慢、索引体积大，子串查询还要靠 n-gram 分析器另配。

## 2. 选定方案

- **文件**：`os.UserCacheDir()/sunvale/index/files.idx`（派生数据，删掉可重建）。旧版 `~/.config/sunvale/index.json` 启动时删除。
- **格式**（`internal/search/idxfile.go`）：
  - 48 字节头：魔数 `SVIX0003`、建立时间、是否完整、条目数、根数、各段长度。
  - 条目表：每条 36 字节定长（父 id、名字偏移/长度、标志 dir/hidden、首个子项 id、子项数、大小、修改时间）。广度优先排列，同一文件夹的子项连续且按名排序，可二分查找；根条目存完整路径，其余只存名字，路径由父链拼出。
  - 名字区：所有名字首尾相接。
  - 三元组表：小写名字的 3 字节窗口（UTF-8 字节，中文同样适用），每项 12 字节（三元组、偏移、个数），排序后二分。
  - posting 区：每个三元组的条目 id 列表，delta + uvarint。
- **加载**：Unix 用 `mmap` 只读映射，数据在页缓存里，不占 Go 堆，内存紧张时内核可回收；Windows / wasm 用一次 `ReadFile`，仍是定长视图，没有逐条对象。
- **查询**（`index.go:Query`）：取查询里所有名字词（`searchquery.Query.NameNeedles`），≥3 字节的词拆三元组，按列表长度从短到长求交集；没有 ≥3 字节的词（1–2 个字符、只有 `ext:` / `path:` 等）时线性扫名字区（先做 ASCII 折叠的字节匹配，不分配）。候选再走原有 `Query.Matches`（file/folder/ext/dm/size/kind/path:/case:）和范围过滤，排序沿用 `sortHitsByQuery`。隐藏标志包含“上层文件夹以点开头”，与实时遍历一致。
- **增量重建**（`idxbuild.go`，updatedb 式）：每个文件夹 stat 一次，mtime 与旧快照一致就复用旧行，只 `ReadDir` 变化的文件夹；新文件写临时文件后 rename，再映射新文件并替换。可取消（`context`）、有总时限；有旧快照时每 256 个文件夹暂停 2 ms 让出磁盘。中途取消的刷新丢弃，保留旧的完整快照；首建被取消则写入“未完整”快照。
- **变动目录**：刷新过程中发现 mtime 变化的文件夹（最多 64 个）记为 dirty；全局搜索先给索引结果，若快照完整但范围内有 dirty 文件夹，只对这些文件夹做实时遍历补全；快照不完整时照旧遍历全部范围。索引答完后若快照超过 30 分钟，后台触发一次刷新。
- **实时更新**：最近列出的 64 个文件夹用 `fsnotify` 监视（不监视整盘），事件去抖 300 ms 后重新列目录并写入覆盖层（原 `Note` LRU：256 个文件夹 / `MaxEntries/4` 行）；覆盖层里的文件夹在查询时取代快照里的同目录行。
- **排除**：平台虚拟目录（`/proc` `/sys` `/dev` `/run` `/media` `/mnt` 等）、Linux 网络挂载（nfs/cifs/smb3/sshfs/9p/davfs/rclone/gvfs）、名为 `node_modules` `.git` `.hg` `.svn` `__pycache__` `.cache` `.gradle` `.m2` `.npm` `.Trash` 的文件夹。
- **上限**：100 万条（约 80 MB 磁盘）；达到上限标记未完整。每次建立写一条 `search index built` 日志（条目、文件夹、复用 / 重读数、字节数、耗时）。
- **设置**：设置 › 快速搜索 增加「文件索引」开关（描述行是“大小 · 条目数 · 更新时间 · 耗时”）与「重建索引」按钮（增量）。关闭后卸载映射、停止刷新与监视，全局搜索回到逐次遍历；文件保留。
- **Windows**：沿用目录遍历 + 增量刷新。USN 日志需要管理员权限和卷句柄，未做，列为后续。macOS 未用 FSEvents（同样列为后续）。

## 3. 测量

20 万文件（2000 个文件夹 × 100，随机英文名），测试临时目录（Linux 容器，页缓存已热），`SUNVALE_IDX_BENCH=1 go test -run TestIndexBench ./internal/search`。旧版为 `a54b0ea`（walk + map + JSON）。查询为 20 次平均；“实时遍历”为 `search.Stream`。

| 项目 | 旧（map + JSON） | 新（mmap + 三元组） |
| --- | --- | --- |
| 冷建立 | 908 ms | 802 ms |
| 磁盘占用 | 19.5 MB | 16.1 MB |
| 加载 | 116 ms | 0.17 ms |
| 加载后常驻堆 | 31.6 MB | 0.0 MB（页缓存） |
| 3 字符 `pho`（≥5000 命中，截断） | 32 ms | 21 ms（实时遍历 142 ms） |
| 5 字符 `invoi`（≥5000 命中） | 35 ms | 20 ms |
| 5 字符 `00042`（3 命中） | 44 ms | 0.02 ms（实时遍历 437 ms） |
| 10 字符 `budget_fin`（484 命中） | 50 ms | 2.1 ms（实时遍历 549 ms） |
| 10 字符 `_final_000`（10 命中） | 44 ms | 0.33 ms（实时遍历 423 ms） |
| 1 字符 `e`（扫描回退） | 25 ms | 15 ms |
| 1% 文件夹变动后刷新 | 847 ms（全量重建） | 326 ms（复用 2021、重读 20） |

- 大量命中的查询耗时主要在 5000 条结果的打分排序，不在索引。
- 建立过程中的临时树与计数表是瞬时内存，写完即释放；查询期间只分配命中的条目。

## 4. 未覆盖

- 真实 Windows（`ReadFile` 加载、盘符根、网络盘）与 macOS（`mmap`、`/Volumes`）只做了交叉编译，没有在真机运行。
- USN 日志 / FSEvents 卷级增量。
- 文件内容变化但目录 mtime 不变（大小、修改时间）要等下次刷新才更新，与 updatedb 相同。
