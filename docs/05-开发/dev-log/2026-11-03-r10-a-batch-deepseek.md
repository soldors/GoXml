# 2026-11-03 R10-A BATCH-TARGET-RESOLUTION（DeepSeek）dev-log

基线 `29a0e3b3ce61ece7ba0807ee97a73d595ce8a0db`；树 `product-r10-deepseek`，分支 `codex/product-r10-deepseek-20261103`。
只改 internal/cli 的 target_package.go/pipeline.go/新增 batch 解析+测试/dev-log；不改 commit.go/clean/cache 键/Compiler/scripts/examples/公开词表。

## A1 真实基线（raw/A1-baseline.json）—— 子 Go 查询次数来自真实命令拦截

方法：PATH 前置 go.bat wrapper（记录每条 go 调用 argv 到 log，再转发真 go），真实 CLI `generate <dir>`（目录输入，批内多目标）。计数纯来自拦截，不由耗时猜测；`go env` 单列，不计入 list。

| 目标数 | go list -json 次数（3 轮） | 墙钟中位/范围(s) |
|---|---|---|
| 1  | 1/1/1 | 中位 0.402（0.377–0.555） |
| 10 | 10/10/10 | 中位 1.672（1.460–3.008） |
| 30 | 30/30/30 | 中位 3.978（3.894–4.034） |

结论（基线）：`go list -json` 次数 = 目标目录数（线性，每目录一次）。`go env` 恒 1（批内已共享）。A2 目标：把「每目录一次」降为**有界批次**（一次或明确上限分组）。

## A2 批量实现 + 效果（raw/A2-compare.json）

实现：新增 `internal/cli/target_batch.go`
- `resolveTargetPackagesBatch`：阶段零收集全部 targetDir，按 `batchListChunk=20` 有界分组，每组一次 `go list -e -json dir...`（合并 overlay）；按返回对象 `.Dir` 归因。
- 每目录做与单目录版本**同等严格校验**（`validateBatchObject`：Name/InvalidGoFiles/选中文件逐文件包声明解析一致）；不改所有权/overlay 证明（仍由 `targetNameOverlay` 逐文件 Manifest+保护头+Source）。
- **正确性/失败边界**：无法归因对象（如跨 module 目录 Dir=""）、缺对象、多对象 → **该目录不填 batchRes，交 resolver 单查兜底**（单查给出与既有完全一致的诊断）；权威查询失败不回退宿主标签；多包/读失败 → fail-closed 进 batchRes.err。
- pipeline：`collectTargetDirs` 枚举 + 批量 + resolver（批量命中查表，批量外目录单查兜底）。**不引入包级可变 resolver、跨 Run 全局缓存、并发提交或新持久缓存格式**。

效果（真实命令拦截计数，3 轮，同规模独立目录）：
| 目标数 | 基线 list -json | 批量 list -json | 基线墙钟中位 | 批量墙钟中位 |
|---|---|---|---|---|
| 1 | 1 | 1 | 0.26s | 0.23s |
| 10 | 10 | **1** | 1.53s | **0.31s** |
| 30 | 30 | **2**(30/20) | 4.05s | **0.54s** |

回归：`target_batch_r10_test.go`（全解析/分块30→全解析/不存在目录回退/多包 fail-closed；可编译变异红——错映射或漏判多包会红）。全包 CLI 测试通过，cover 86.2%、vet rc0、gofmt 空。

## A3 全链闭环（raw/A3-e2e.json，全 PASS）

同规模对照（30 目标，真实拦截计数）：
- 基线：`go list -json`=30（线性），生成 30/30；命中再 30。
- 批量：`go list -json`=2（有界 30/20 分组），生成 30/30；命中 2；命中字节一致。
- 结论：权威查询次数由「随目录数线性增长」降为「ceil(N/20) 有界批次」，正确性不变（生成/命中字节一致）。

定向回归（批量路径）：
- 改坏一个目标（非法 gxml）→ 整批拒绝 rc=2，**5 个产物字节全部保留**（批量失败保旧，不部分写盘）。
- GOENV windows/386（进程 GOARCH 未设）→ CLI 目标包名 `expdisabled` == 独立 go list（批量路径权威）。
- 冗余 `GOEXPERIMENT=greenteagc` → 不误拒，包名 `ui`（合法类型/依赖错误阶段归属保持 R9 边界）。
- GOAMD64 v1→v3 → 未命中重分析；恢复 v1 同字节（K10 含 CPU 特性）。

复用既有 R9 测试：target_package_s1_test / dependency_fingerprint；未重造全矩阵。批量判别红见 target_batch_r10_test.go（错映射/漏判多包会红）。

## 门禁
`go test ./internal/cli -count=1 -cover` = 86.2%（≥85%）；vet rc0；gofmt 空；真实 CLI 对照 PASS。

## 五态
源实现 done(A1 706163b / A2 a4965e3 / A3 dev-log 本轮) / 自测 pass / 外审 PENDING（ZCode 审 A）/ 主仓未合 / 远端未推送。
