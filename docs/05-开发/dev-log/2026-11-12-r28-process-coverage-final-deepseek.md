# 2026-11-12 R28 发布门禁 A：Windows process 覆盖率 — 开发日志

共同基线 `ae4b07f7fa2dc7f39c6d715d8dd6498ca34d5b0c`（`codex/r28-final-combined-review`）。分支 `codex/r28-process-coverage-final`，工作树 `r28-process-coverage-final`（协调者已建，开工核对 HEAD 精确相等、status/index 空）。只提交 `internal/devx/process/` 下测试 + 本 dev-log。主库/组合树/历史证据只读。
提交：`ae4b07f`（父）→ 本提交（process 真实边界覆盖测试）+ dev-log。

## 前后覆盖率（Windows CGO=0，树外 coverprofile）
- 基线：process **74.9%**、watch **89.4%**（`raw/baseline-test.txt`，`go test -count=1 -coverprofile=<树外> ./internal/devx/process ./internal/devx/watch`）。
- 后：process **86.2%**、watch **89.4%**（`raw/final-both-test.txt`）；两包合并 `total 88.5%`。
- **达标 process 独立包 ≥85%（86.2%）**；watch 无退化。未改统计分母、未过滤源码、未改生产代码（仅新增测试）。

## 缺口分析（`go tool cover -func`）
基线低覆盖：`terminateJob` 0.0%（无调用者的既有函数）、`RunCommandTree` 65.2%、`jobProcessHandles` 78.6%、`waitJobEmptyAndHandles` 84.2%、`waitProcessHandles` 87.5%、`NewJobOwnership` 66.7%、`resumeOwnedCommand` 77.8%、`exitCodeOf` 75.0%。

## 新增真实边界测试（`internal/devx/process/manager_coverage_r28_test.go`，`//go:build windows`）
仅用本测试创建的自有 Job/进程/句柄，复用已有受控 seam（TestMain 的 held/held-leaf helper）；每个用例断言真实行为/错误，非行数触达：
- `TestTerminateJobNormalTerminatesAndCloses`：真实 Job+运行中 helper → `terminateJob` 终止成员、ActiveProcesses 归零、关闭句柄（原句柄 `ERROR_INVALID_HANDLE`）。
- `TestTerminateJobExpiredDeadlineErrorsAndCloses`：过期 deadline → 明确错误且仍关闭句柄（不泄漏）。
- `TestWaitProcessHandlesEmptyAndExpired`：空句柄 nil；存活句柄 + 过期 deadline → 过期错误（**不得把不完整退出证明当成功**）。
- `TestWaitJobEmptyAndHandlesInvalidHandleErrors`：无效 Job 句柄 → Job 查询失败必须报错。
- `TestJobProcessHandlesInvalidHandleErrors`：无效 Job 句柄 → 捕获进程失败报错。
- `TestAssignJobOwnershipInvalidPIDErrors`：无效 PID → `AssignJobOwnership` 报错（不静默）。
- `TestRunCommandTreeNilContextAndPreCancelled`：nil ctx → error；预取消 ctx → Cancelled（有界、不启动命令）。
- `TestExitCodeOfBranches`：nil-ProcessState + nil werr → 0；非 ExitError → -1；真实 ExitError → 其码。
- `TestRunCommandTreeTimeoutCancelsOwnedTree`：自有 helper 阻塞 + 短 timeout → `RunCommandTree` ctx.Done 取消分支：Cancelled + `DeadlineExceeded` + 有界返回 + Job 终止无幸存根。
（说明：`NewJobOwnership` 的 SetInformationJobObject 失败分支、`resumeOwnedCommand` 无线程分支、`jobProcessHandles` 的 OpenProcess/快照不完整分支在受控环境不可自然触发，未造合成用例；记入真实未证分支。）

## 关键新增守卫 overlay 翻红（树外 overlay，compile rc0 → 目标断言 FAIL）
- `raw/mut/mut-timeout-noop.overlay.json`：`waitProcessHandles` 过期 deadline 改为返回 nil（假装已证明）→ `go test -c` rc=0；`TestWaitProcessHandlesEmptyAndExpired` **FAIL**（"expired deadline with live handle must error"）。证明该守卫判别力。

## 门禁（Windows/amd64）
- process+watch 全量普通测试 PASS（`raw/final-both-test.txt`）。
- MinGW-W64 16.1.0 64bit `-race`（CGO=1）：process+watch **rc0 / 0 DATA RACE**（`raw/race/race.json`）。
- `go build ./...` CGO=0 rc0；`go vet` 两包 rc0；`gofmt -l` 空；docgate/diagdoc ok。
- 保持原 StopTimeout、Job-only、先停旧再启新、tool-tree/Manager 守卫（未改生产代码）。

## 真实未证边界
- 覆盖率仅 Windows 实测；非 Windows `proc_other.go` unsupported 未运行。
- `NewJobOwnership`/`resumeOwnedCommand`/`jobProcessHandles` 的 OS 失败分支在受控环境不可自然触发（未合成）；process 剩余 ~13.8% 主要为这些不可达/难触发分支与少量未走路径。
- 原生窗口/跨平台运行/整应用性能采样未做；全树最终门禁与合并在协调者。
