# 2026-11-04 R13-A BATCH-PRECOMMIT-CHECK（DeepSeek）dev-log

基线 `ed76cbbb353511cc3de94c20b5f6726104ac9842`；树 `product-r13-deepseek`，分支 `codex/product-r13-deepseek-20261104`。
本线只改 `internal/cli` 的提交前检查实现与直接回归 + 本线 dev-log；未改 CLI 选项/退出码/原生检查标记语义。

## 0. 现状（R11/R12 遗留）

`internal/cli/precommit_check.go` 的 `checkPrecommitCandidates` 在末尾**逐目标包各跑一次 `go build`**：

```go
for _, key := range order {
    runGoBuildCheck(goExe, moduleRoot, overlayPath, []string{groups[key].dir}, workDir)  // 每包一次
}
```

## 1. A1 真实基线（raw/A1-baseline.json；argv 拦截 wrapper，3 轮）

| 目标包数 | go build 次数 | go list 次数 | 墙钟中位 |
|---|---|---|---|
| 1  | 1  | 1 | 0.68s |
| 10 | **10** | 1 | 5.54s |
| 30 | **30** | 2 | 13.52s |

⇒ 构建次数与目标包数**线性**，是多目标 `--check` 的主要成本。

## 2. 可行性与安全边界（先实测再改）

用真实 `go build` 验证批量语义：
- 一次传**多个包**（含两个同名 `x` 的 main）→ rc0 且**不写任何文件**（多包只做编译检查）；
- 组内含坏包 → rc1，stderr 精确定位坏包（诊断保留）；
- 组内**单个 main** → 会写 `x.exe` 到 cwd ⇒ cwd 必须是**自有临时目录**。

## 3. A2 实现（`precommitBatchSize = 12` 有界分组）

- 末尾循环改为按 `precommitBatchSize` 分组，每组**一次** `go build <dir...>`。
- **cwd 必须留在 `moduleRoot`**：相对 GOFLAGS（`-modfile=alternate.mod`、`./x` 包路径）按
  module 根解析；改 cwd 会丢上下文（真实回归 `TestReviewCheckGOENVOverlayAndRelativeFlags`）。
  为不把单 main 的可执行文件写进用户 module 根：
  - 组内**多于一个**包 → **不加 `-o`**（go 多包不写产物）；
  - 组内**恰好一个**包 → 加 `-o <自有 workDir>/out-N`（可执行输出落临时目录）。
- 用户 `-overlay=` 已由 `mergePrecommitUserOverlay` 合并进本次**绝对** overlay（候选优先），
  故从子进程 GOFLAGS 中 `stripGoflagsOverlay` 去掉，避免重复/相对路径冲突。其余 `-mod/-tags` 保留。
- 新增接缝 `var precommitBuildFunc = runGoBuildCheck`（同仓库既有 `pipelineFunc` 风格），
  让测试**用执行计数**证明分组生效，不靠耗时推断。

## 4. 效果（raw/A2-batch.json；同一 argv 拦截法，3 轮）

| 目标包数 | 基线 build | 批量 build | 基线墙钟 | 批量墙钟 |
|---|---|---|---|---|
| 1  | 1 | 1 | 0.68s | 0.74s |
| 10 | **10** | **1** | 5.54s | **1.30s** |
| 30 | **30** | **3**（30/12） | 13.52s | **3.27s** |

## 5. 回归（`internal/cli/precommit_batch_r13_test.go`，全 PASS）

覆盖任务要求的全部形态：
- 单包 / 10 包（1 次构建）/ 30 包（⌈30/12⌉=3 次且每组 ≤12）；
- **两个同名 main** 同批不冲突且通过，用户目录无 `.exe` 残留；
- **同批跨包候选互引**（v2 引用 v1 生成类型，overlay 可见）通过；
- **全命中**仍执行检查且只 1 次构建；**混合命中**（命中/未命中同现）检查通过；
- **一个坏目标 → 整批拒绝**：不写产物/Manifest/缓存、无"已生成"；
- **`--clean` 在检查失败前不删除**（既有产物字节不变）；
- **用户 GOFLAGS overlay 合并优先级**：用户 overlay 提供上下文，但同路径的**坏候选优先**（不被掩盖）。

**变异红**：把 `precommitBatchSize` 改为 `1` 后
`TestCheckBatchReducesBuildCalls`（`10 包应只调用 1 次 go build，实际 10 次`）与
`TestCheckBatchAllHitsStillRuns`（`全命中 3 包应 1 次构建，实际 3`）立即失败；恢复后全绿。

## 6. 边界（如实）

- 检查仍**只编译**（`go build`），不执行 `go test`/`go run`/用户程序；编译产物只落自有临时目录
  （分组 >1 包不写；=1 包写 `-o workDir/out-N`；workDir 由 `defer` 清理）。
- 失败语义与 R11/R12 一致：检查在阶段一后、阶段二写盘前，失败则产物/Manifest/成功缓存不落盘、`--clean` 未执行。
- 未改 `commit.go`/clean 事务、缓存键/格式、Compiler；未新增 CLI 选项/退出码。

## 7. 门禁

`go test ./internal/cli -count=1 -cover` = **86.4%**（≥85%）；`go build ./...` ok；`go vet` rc0；`gofmt -l` 空。

## 8. 五态

实现 done / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。
