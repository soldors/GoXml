# 2026-11-04 R21-A GO-OVERLAY-CONSISTENCY（DeepSeek）dev-log

基线 `4e413f0fba9c78a96549eb683559a77764cecc4b`（开工核验 clean；主仓 porcelain 实测 40，common-task 记为 68，**差异登记**：开工时点主仓本地未提交项为 40 条，非 68）。
树 `r21-deepseek-overlay`，分支 `codex/r21-deepseek-overlay`。只拥有 `internal/cli` 与直接回归、dev-log。

## A1 先用真实 Go 与 CLI 定案（raw/A1-overlay-matrix.json、raw/A1b-stages.json）

### 阶段与优先级（实测）
- `go list`/`go build` 的 **argv `-overlay` 覆盖 GOFLAGS/GOENV 里的 `-overlay`**（最小实验：env overlay→`q`，argv overlay→`r`，argv 胜）。
- `target_package.go`/`target_batch.go` 在**定名阶段**为确保只见真实用户源，会传 argv `-overlay <工具自有旧产物映射>`。此前该 argv overlay **不含用户映射**，于是：
  - `go list`（带工具 argv overlay）丢掉用户 overlay，看到**磁盘真相**；
  - 而逐文件校验又**读磁盘**；
  - 而 `--check`（precommit_check 的 `mergePrecommitUserOverlay`）**合并了**用户 overlay。
  ⇒ 定名/批查 与 go list/build/--check **口径不一致**。

### 反例矩阵（修复前 → 修复后，均以同环境 `go list -json`/`go build` 为基准）
| 场景 | go 基准 | 修复前 CLI | 修复后 CLI |
|---|---|---|---|
| 用户 overlay 替换用户源、改包名（p/a.go→package q） | `q` | ❌ 假报「多个非测试 package（q / p）」 | ✅ 生成 package `q` |
| 仅存在于 overlay 的文件（磁盘无 extra.go） | 可见 | ❌ 「读取包源码 p/extra.go: 不存在」 | ✅ rc0 |
| 用户 overlay 与自有候选同路径（磁盘有 _gen.go） | `p` | ✅（未触发） | ✅ 生成成功 |
| 空格 + Unicode 目录 | `q` | ❌ 同 1 | ✅ package `q` |
| 删除语义（`p/a.go`→`""`） | `p [keep.go]` | ❌ `expected 'package', found 'EOF'` | ✅ rc0 |
| 批查 vs 单查（p1→q1,p2→p2） | `q1 p2` | — | ✅ `q1 p2` 一致 |
| `--check`（用户 overlay 改名） | 通过 | ✅ | ✅ 通过 |
| 坏 overlay（JSON 损坏） | go 报错 | — | ✅ 可消费失败（`解析用户 overlay`） |
| 旧生成物存在 + 用户 overlay 改名（未登记为产物） | go 自身 `found packages q and p` 报错 | — | ✅ 与 go 一致拒绝（自有过滤需所有权依据，未登记不排除） |

## A2 只修已证分叉（commit `5c68998`）

复用现有 Go flags/所有权/overlay 逻辑，最小修复三处：
1. `target_package.go` 新增 `effectiveTargetOverlay(moduleRoot, man, dir, flags)`：在 `targetNameOverlay`（工具自有旧产物，含所有权证明）**之上**并入 `userOverlayBytes(flags)`（GOFLAGS/GOENV 的 `-overlay` JSON，读不到/坏 JSON → 可消费失败）；**同路径工具自有优先**（所有权依据）。
2. `pipeline.go`：`overlayForDir` 改用 `effectiveTargetOverlay(..., ctxValues.GOFLAGS)`——把用户 overlay 一并传给**定名 go list** 与**逐文件校验**，与 go build/--check 口径一致。
3. `target_batch.go` / `target_package.go`：
   - 校验时 **overlay 优先于读盘**（支持 overlay 新增文件；nil=删除 → 跳过该文件包子句校验）；
   - 计数「真实用户源」只排除**工具自有旧产物**（`isOwnedOldArtifact` = `//go:build ignore` 标记），**不排除用户 overlay 的替换/新增**；
   - overlay JSON 序列化时 **nil → `""`**（Go 的删除语义）。

**未扩大 Analyzer 输入契约**；用户 overlay 对普通用户输入保持有效；自有旧生成物过滤仍以 Manifest+保护头为所有权依据；未初始化包、多包、GOENV/-modfile 的既有失败语义保留。

### K10/K11/K12 与缓存版本
- **K10**（build context / GOFLAGS 字符串）：不变。
- **K11**（BuildInputs，GOFLAGS `-overlay=/-modfile=` 内容指纹，R15-A 起）：**逻辑不变**；用户 overlay **内容**变化早已触发安全 miss（本线 A3 #2 实证：`缓存未命中`）。
- **K12**（Image 资产内容）：不变。
- **cacheFormatVersion**：**仍为 8，不变**（未改键结构/记录结构，仅改阶段归属与 overlay 视图一致性）。

## A3 验收交付（raw/A3-acceptance.json、A3b-acceptance.json、A3c-querycount.json、A1b-stages.json）

真实 CLI：
- 冷生成 rc0 → **真命中**（bytes/mtime/Manifest 不变）→ 同路径 overlay **内容变化安全 miss**（`缓存未命中`）。
- 坏 overlay **拒绝并保旧**（gen 字节/mtime/Manifest 全保留）→ 恢复 rc0。
- **混合命中批次**：p 命中 + p2 新生成，rc0。
- **含 Image 项目对照**：冷+命中，`//go:embed` 存在。
- **坏包不写盘**：语法错目标 → 拒绝，`page_gen.go`/Manifest 均未创建。
- **批量 Go 查询次数无退化**：shim 计数 25 目标 → 恰 **2** 次 `go list`（=ceil(25/20)，chunk=20），未新增 exec 站点。

### 判别回归 + 变异红
`internal/cli/overlay_consistency_r21_test.go`：
- `TestEffectiveTargetOverlayMergesUserAndOwned`（单元）；
- `TestUserOverlayRenamesPackageConsistent`（真实 go list 基准：CLI 定名 == 用户 overlay 视角 `q`）。
**变异红**：禁用 `effectiveTargetOverlay` 的用户合并后，两测试立即 FAIL（`定名 "p" 与基准 "q" 不一致`）；恢复全绿。

## 门禁
`go build ./...` ok；`go vet ./internal/cli` rc0；`gofmt -l internal/cli` 空；`go test ./internal/cli -cover` = **86.0%**；台账 verify PASS。

## 五态
实现 done(`5c68998`) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
