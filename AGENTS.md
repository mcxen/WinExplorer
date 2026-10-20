# Sunvale 开发与发布约定

Go + 自绘 UI 的 Windows 资源管理器风格文件管理器，模块名 `sunvale`，单包 `package main` 为主。
界面基于 `github.com/egoist/mygo/ui`（immediate-mode）与 Onevra 组件。

## 发布规则（自动执行）

仓库根目录的 `scripts/git-hooks/post-commit` 固化了三条规则，按**单次提交的变更行数**
（新增 + 删除，即 `git show --shortstat` 的口径）判定：

| 单次提交行数 | 自动动作 |
| --- | --- |
| > 1000 行 | 自动 `git push origin main` |
| > 3000 行 | 推送 + 自动发版：bump `mygo.json` 的 `version` → 提交 `[auto-release] vX.Y.Z` → 打注释 tag → 推送 tag |
| ≤ 1000 行 | 不做任何自动动作，按需手动推送 |

推送 tag 会触发 `.github/workflows/release.yml`，构建三平台安装包并发布 Release。

**手动发版**（需要精确控制版本号时）：

```sh
# 1. 改 mygo.json 的 version，提交
# 2. 打 tag 并推送（release.yml 会校验 tag 与 mygo.json 一致）
git tag -a v0.1.6 -m "v0.1.6"
git push origin main
git push origin v0.1.6
```

**注意**：tag 与 `mygo.json` 的 `version` 必须完全一致，`release.yml` 的
`Check the tag against the version` 步骤会直接失败。版本号格式 `vMAJOR.MINOR.PATCH`，
当前为 v0.1.x 递增。

自动规则会跳过的情形：merge 提交、`[auto-release]` 提交、非 `main` 分支、无
`origin` 远程的仓库。推送或打 tag 失败只在 stderr 提示，不会让已存在的提交失败。

### 换机器后必做

```sh
scripts/install-git-hooks.sh
```

git hook 不会随 clone 一起走，每台机器（每个 clone）都要装一次。未安装时自动规则不生效，
但手动发版流程不受影响。

## 代码约定

- **构建/测试**：`go build .`、`go vet .`、`go test -count=1 .`。提交前这三条必须通过，
  且 `gofmt -l` 对改动的文件无输出。
- **跨平台**：改动涉及平台相关代码时，至少验证 `GOOS=windows`、`GOOS=linux`、`GOOS=js`
  能编译（`go build -o /tmp/x .`）。
- **单包结构**：主逻辑都在 `package main`；`diskhealth/`、`brandicons/`、`tools/` 是独立子包。
  主包不依赖子包的内部实现，子包也不反向引用主包。
- **注释风格**：英文，解释"为什么"而不是"是什么"，保留既有的比喻与背景说明。
  新增文件顶部用一段话说明该文件的分工。
- **测试**：无界面回归测试为主，UI 用 `ui.NewTester` 跑真实视图。
  测试不得依赖特定硬件能力（如 SMART、真实网卡）——CI 跑在虚拟机上。
  同一批产物不要拆成多个用例重复断言；不要写只断言常量取值的测试。
- **并发**：有界后台工作统一用 `parallelFor`（`parallel.go`），不要各自再写一套
  channel + WaitGroup。
- **可替换接口**：目录读取走 `lister` 接口（`listing.go`），新数据源实现该接口即可，
  不要往 `reload` 里加分支。
- **包结构**：应用本体在根目录单包 `package main`，按职责分文件；可独立复用的
  模块放 `internal/`（`netfs`、`searchquery`、`rename`），自带测试，不得反向引用主包。
  新增一个 `internal/` 包前先确认它真的不依赖 `App`/`Entry`/`ui`。

## 文档

- `README.md` 是面向用户的功能说明，中文。
- 深层技术说明放在 `docs/`，例如 `docs/marquee-selection.md`（鼠标圈选）。
- 新界面先查 `docs/ui-components.md`，能用 mygo / Onevra 现成控件就不要再手写一套。
- 改动了用户可见行为时同步更新 README；纯内部重构不需要。

## 不要做的事

- 不要在未确认的情况下 `git push --force`（移动已发布的 tag 需先确认）。
- 不要提交 `dist/`、`sunvale`、`sunvale.exe` 等构建产物。
- 不要把本地绝对路径写进 `go.mod`（例如 `replace` 指向本机的 Onevra 克隆）。
  `release.yml` 虽然会 `go mod edit -dropreplace` 兜底，但 CI 的构建仍可能因路径不存在而失败。
