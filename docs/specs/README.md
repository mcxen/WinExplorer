# 技术方案索引（docs/specs）

本目录是交给编码代理（Grok CLI）执行的实现规格。规格由方案维护者更新；编码代理只按规格开发，发现规格与代码不符时在 PR 描述里指出，不要自行改规格的设计决策。

所有 `文件:函数` 引用基于 `origin/main` 的 `bf9ae80`（助手：去掉访问路径限制）。

## 规格列表

| 编号 | 方案 |
| --- | --- |
| 01 | [全局搜索显示增强](01-global-search.md)（8 个阶段） |
| 02 | [侧栏目录树打磨](02-tree-polish.md) |
| 03 | [下一步重构](03-refactor-next.md) |
| 04 | [标签页后续](04-tabs-followups.md) |
| 05 | [助手后续](05-assistant-followups.md) |

**任务状态、分支、依赖与验收统一维护在仓库根目录 [`TASK.md`](../../TASK.md)**，本目录不再记录状态，避免两处不一致。界面与动效规范见 [`docs/UI.md`](../UI.md)。

## 编码代理约定

1. **先读 `AGENTS.md`**（仓库根目录），其中的构建、注释、测试、并发、跨平台约定全部适用。
2. **一个规格（或一个阶段）一个分支**，从最新 `origin/main` 拉出；不要在一个分支里混做多个规格。
3. **单次提交少于 1000 行**（新增 + 删除，`git show --shortstat` 口径）。超过 1000 行在 `main` 上会触发自动推送，超过 3000 行会触发自动发版；拆成多个提交。
4. **不要安装 post-commit hook**（不要运行 `scripts/install-git-hooks.sh`），避免意外推送或发版。
5. **永远不要 force-push**，不要改写已推送的历史。
6. 每次提交前必须通过：
   ```sh
   gofmt -l $(git diff --name-only HEAD -- '*.go')   # 应无输出
   go build . && go vet .
   GOOS=windows go build -o /tmp/x.exe . && GOOS=linux go build -o /tmp/x . && GOOS=darwin go build -o /tmp/x . && GOOS=js GOARCH=wasm go build -o /tmp/x.wasm .
   go test -count=1 ./...
   ```
7. **已知的既有失败**（与本次改动无关，不要尝试顺手修，也不要因此停工）：
   
   除此之外的任何失败都必须修好再提交。
8. **截图验证（离屏渲染）**：UI 改动要附截图。做法与现有 `*_ui_test.go` 一致：用 `ui.NewTester(a.view, 1200, 800)` 建窗口，跑几帧 `tt.Frame()`，再 `png.Encode(f, tt.Image())` 写到 `/tmp` 或 PR 附件。截图用的测试放在 `zz_*_test.go` 里，**不要提交**（看完删除）。参考 `/workspace/winexplorer-demo/search/current/searchshot_test.go.txt`。
9. **界面文字一律简体中文**，不使用 emoji；颜色只用 `theme.go` 的 `colorXxx()` 函数，不写死色值。
10. 注释用英文，解释“为什么”；新文件顶部写一段分工说明。
11. 不新增第三方依赖，除非规格明确要求。
