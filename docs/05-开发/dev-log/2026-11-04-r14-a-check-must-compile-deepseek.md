# 2026-11-04 R14-A CHECK-MUST-COMPILE（DeepSeek）dev-log

基线 `03c1f8aa1986f59c1183c41c710c99920253d680`；树 `product-r14-deepseek`，分支 `codex/product-r14-deepseek-20261104`。
本线只改 `internal/cli` 提交前检查实现与直接回归 + 本线 dev-log；不改 CLI 选项/退出码/信任模型，不全量重写 GOFLAGS。

## 1. 风险与真实缺口（先立红，raw/R1-gap.json）

场景：目标目录内用户文件 `use.go: var _ = NewGoxmlHomeView(nil)`（无 ViewModel 页生成的
`NewGoxmlHomeView()` 零参数 ⇒ 组合编译失败）。四组真实 CLI 对照：

| 场景 | rc | 结果 |
|---|---|---|
| 真实 `go build ./ui`（无 -n） | 1 | 真失败 `undefined: NewGoxmlHomeView`（包确实编译不过） |
| 无 `--check` 生成 | 0 | 生成并提交（说明失败本应发生在**提交前检查**） |
| `--check` + 进程 `GOFLAGS=-mod=readonly -n` | **0** | **假通过**：stdout `提交前检查通过：1 个目标包` + `已生成`，产物/Manifest 均写入 ⇒ **缺口** |
| `--check` + GOENV 持久化 `-n` | 2 | 该路径恰好失败（但仍需一并加固） |

根因：`runGoBuildCheck` 的子进程 `cmd.Env` 继承 `GOFLAGS`/`GOENV`；`-n`（dry-run）让
`go build` **只打印命令并以 0 退出**，于是检查把"没编译"当成"编译通过"。

## 2. 最小修复（argv 追加 `-n=false`）

`internal/cli/precommit_check.go` 的 `runGoBuildCheck` 在 `go build` 参数里追加：
```go
args = append(args, "-n=false")
```
命令行 flag 优先于 GOFLAGS/GOENV ⇒ 即使进程或 GOENV 持久化了 `-n`，检查也**真正编译**。
- **不改写**用户 GOFLAGS（不删除/重排其标志）；不影响正常 `-x`（仍打印命令、仍编译）；
  用户显式 `-n=false` 时 `-n=false` 冗余但语义一致。
- 不改变"用户自定义工具/受信任可执行"的信任模型；`goExe` 仍来自既有解析。
- 保留 R13 的有界分组（`precommitBatchSize`）与输出隔离（编译产物仅落自有临时目录）。

实测（`go build` 语义）：`GOFLAGS=-n` 时仅打印；同 argv 加 `-n=false` 时真编译并以 1 失败
（`undefined: undefinedSym`）。修复后 CLI 全部验收见下。

## 3. 修复后真实 CLI 验收（raw/A2A3-e2e.json，全 PASS）

| 场景 | 期望 | 实测 |
|---|---|---|
| 进程 `GOFLAGS=-n` + 坏目标 | 拒绝、不落产物/Manifest | rc2 `提交前检查失败`，gen/man 均未写 |
| GOENV 持久化 `-n` + 坏目标 | 拒绝、不落产物 | rc2，gen 未写 |
| 合法目标 + `-n` | 通过并写盘（`-n` 不误伤正常路径） | rc0，`提交前检查通过`，产物写出 |
| 显式 `-n=false` + 坏目标 | 拒绝 | rc2 |
| 正常 `-x` + 坏目标 | 拒绝（-x 打印命令但仍编译） | rc2 `提交前检查失败` |
| 多包、一个坏 + `-n` | 整批拒绝、无任何产物/Manifest | rc2，三包产物均无、Manifest 无 |
| 全缓存命中 + `-n` | 通过，且检查确实执行 | rc0，`缓存命中` + `提交前检查通过` |
| 含空格 `-modfile` + 用户 overlay + 坏目标 | 拒绝（候选优先，不被 overlay 掩盖） | rc2 `提交前检查失败` |

## 4. 判别回归（internal/cli/precommit_mustcompile_r14_test.go，8 项全 PASS）

`TestCheckMustCompileDespiteDashN`（进程 -n 坏目标拒绝且不落产物/Manifest/缓存）/
`TestCheckMustCompileDespiteGOENVDashN` / `TestCheckPassesWithDashNForValidTarget` /
`TestCheckExplicitNFalseStillCompiles` / `TestCheckNormalXStillCompiles` /
`TestCheckMultiBadRejectedUnderDashN`（多包整批拒绝）/ `TestCheckAllHitUnderDashN` /
`TestCheckSpaceModfileAndUserOverlay`。

**变异红**：删除 argv 的 `-n=false` 后
`TestCheckMustCompileDespiteDashN`（`-n 下坏目标必须被拒绝（否则假通过），实际 rc=0`）与
`TestCheckMustCompileDespiteGOENVDashN` 立即失败；恢复后全绿。

## 5. 失败语义保持（未回退）

检查仍在阶段一后、阶段二写盘前；失败 ⇒ 产物/Manifest/成功缓存都不落盘，`--clean` 尚未执行
（既有 `TestCheckBatchCleanNotRunOnFailure` 等 R13 回归继续通过）。

## 6. 门禁

`go test ./internal/cli -count=1 -cover` = **86.6%**（≥85%）；`go build ./...` ok；`go vet` rc0；`gofmt -l` 空。

## 7. 五态

实现 done / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。
