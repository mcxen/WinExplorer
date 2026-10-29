# 21 搜索缓存内存占用（SEARCH-MEM）

- **问题**：
  1. 标签页搜索和搜索面板每次按键启动新的全盘 / 文件夹遍历，旧遍历只靠 `gen` 丢弃结果，本身一直跑到截止时间（全局 30 秒），goroutine、通道和结果批次都留在内存里。
  2. `search.Index.Note` 每浏览一个文件夹就把它的条目记进 `dirs`，没有上限，也不受 `MaxEntries` 约束；重建时 `dirs` 还会整体并回快照。
  3. `Stream` 发出批次后复用同一个切片（`batch[:0]`），接收方可能读到被覆盖的数据。
- **修复**：
  - `search.Options.Ctx`：新查询、清空、关闭面板或关闭标签页时 `cancel`，旧遍历立即退出（`searchState.stop`、`searchBoxState.stopWalk`）。
  - `Note` 改为 LRU：最多 256 个文件夹、`MaxEntries/4` 行；淘汰的文件夹在快照未超上限时保留为普通索引行，超了就删除。
  - `indexEntry` 不再存扩展名，查询时从路径计算。
  - 每个批次用新切片。
- **测试**：`internal/search/mem_test.go`（10 万文件，40 次按键式搜索，1000 个文件夹 Note，上限 20000）。
  - 修复前：残留 105 个 goroutine，堆 58 MB；索引 100000 条（超出上限）、24 MB。
  - 修复后：goroutine 回到起始值，堆 0.3 MB；索引 20000 条、3.7 MB。
