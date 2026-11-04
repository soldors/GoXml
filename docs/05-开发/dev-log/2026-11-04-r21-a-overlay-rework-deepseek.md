# 2026-11-04 R21-A GO-OVERLAY-CONSISTENCY 集中返修（DeepSeek）dev-log

父提交 `249ee942863d8435ba7426d76351d5b15a386105`（协调者 BLOCK 冻结版）。新隔离分支 `codex/r21-deepseek-overlay-rework`，树 `r21a-deepseek-rework`。旧提交与证据保留。
只拥有 `internal/cli` 与直接回归、dev-log。

## 协调者阻断（review-a.md + adversarial-final/result.json）
| 反例 | 同环境 Go | 冻结 CLI `249ee94` | 位置 |
|---|---|---|---|
| ① 目标目录仅存在于 overlay，虚拟 doc.go=package q | `q [doc.go]` | 生成 `package src`（回退源包名） | 单查/批查共同依赖磁盘目录探测 |
| ② 用户 overlay 把唯一磁盘 Go 文件换成带 ignore 构建标签 | rc1 构建约束排除全部 | rc0 回退源包名**写盘** | 物理探测未看 overlay 替换内容 |
| ③ 合法 package q 原始串含两个旧产物标记 | `q [doc.go]` | 生成 `package src` | `isOwnedOldArtifact` 字节启发式误判所有权 |

## 返修（commit `42fbb1c`）

**根因**：源探测/计数用**物理 ReadDir** + **字节启发式所有权**。

**最小修复**：
1. `effectiveTargetResolutionWithUser(moduleRoot, man, dir, user)`：`owned` **只**取自 `targetNameOverlay`（Manifest + `HasProtectedHeader` + `headerSourceMatches` 证明）；用户 overlay 条目并入 `effective` 但**永不**进 `owned`；同路径工具自有优先。**彻底删除 `isOwnedOldArtifact` 字节启发式**。
2. **统一有效源探测** `effectiveUserGoFiles(dir, overlay, owned) (bool, error)`：物理目录存在则取非测试 `.go`（排除 `owned`、排除 overlay 删除 `nil`）；**另加 overlay 新增（磁盘不存在、非 owned、非测试、非删除）**。物理目录不存在时仍可由 overlay 提供源。单查 `newTargetPkgResolver` 与批查 `resolveOneChunk` **共用**此函数。
3. overlay JSON 序列化 `nil → ""`（Go 删除语义）；校验 overlay 优先于读盘（已在 `249ee94` 具备，保留）。
4. **用户 overlay 只读一次**：`pipeline.go` 在每次运行预先 `userOverlayBytes(ctxValues.GOFLAGS)` 一次并传入，逐目录复用（`effectiveTargetResolutionWithUser`），读取次数有界（每运行 1 次）。

**未扩大** Analyzer 输入契约；普通用户 overlay 保持有效；坏 overlay/坏包/多包/未初始化/GOENV/-modfile 失败语义保留。

## 先红后绿（同一组真实 Go + CLI；raw/rework-raw/A-rework-blockers.json、A-rework-prefix-red.json）

| 反例 | 父 249ee94（红） | rework 42fbb1c（绿） |
|---|---|---|
| ① virtual | 生成 `package src`（含干净 target 目录缺失态复现） | 生成 `package q`，go list `q [doc.go]` |
| ② ignored | rc0，**写盘** `package src` | **rc2 权威失败，不写盘**（go list rc1 排除全部） |
| ③ ownership | 生成 `package src` | 生成 `package q` |

> 注：① 的父红需清理物理 `target/` 目录（先前运行已创建），在**目录不存在**的干净态下父确为 `src`、rework 为 `q`（`rwp-virtual2.*`、`rw-virtual2fix.*`）。

## A 回放（raw/rework-raw/A-rework-replay.json，PASS）
冷生成→**真命中**（bytes/mtime/Manifest 不变）→ 坏 overlay **拒绝保旧**→恢复；`--check` 通过；**含 Image** 项目命中且 `//go:embed` 存在。

## 判别回归 + 变异红（`internal/cli/overlay_blockers_r21_test.go`、`overlay_consistency_r21_test.go`）
- `TestR21VirtualOverlayOnlyDirNamesQ` ①；
- `TestR21OverlayExcludedFileIsAuthoritativeFailure` ②（单查 resolver 必须返回权威错误而非回退）；
- `TestR21OwnershipNotFromByteHeuristic` ③；
- `TestEffectiveTargetOverlayMergesUserAndOwned`、`TestUserOverlayRenamesPackageConsistent`（既有）。
**变异红**：
- 令 `effectiveUserGoFiles` 忽略 overlay 新增 → `TestR21VirtualOverlayOnlyDirNamesQ` FAIL（`应探测到有效用户源`）；
- 在 `effectiveTargetResolutionWithUser` 重新引入字节启发式 → `TestR21OwnershipNotFromByteHeuristic` FAIL。
恢复后全绿。

## 批量查询次数（raw/rework-raw/A-rework-querycount.json）
go shim 计数：25 目标 → **2 次** `go list`（=ceil(25/20)），**无退化**；用户 overlay 读取 **1 次/运行**（有界）。

## 门禁
`go build ./...` ok；`go vet ./internal/cli` rc0；`gofmt -l internal/cli` 空；`go test ./internal/cli -cover` = **86.0%**；台账 verify PASS；每命令锁 token 释放（lockRelease=true）。

## 五态
实现 done(`42fbb1c`) / 自测 pass / 独立复核 PENDING（协调者重放三反例）/ 主仓未合 / 远端未推送。
