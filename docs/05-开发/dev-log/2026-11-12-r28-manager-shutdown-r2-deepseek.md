# 2026-11-12 R28 Manager R2：Windows 自有 Job 收口与 Watch Manager 停止状态 — 开发日志

起点 `38658b5a80ab8c6cde75f242c961e6af857bd6eb`。分支 `codex/r28-manager-shutdown-r2`，独立树 `r28-manager-shutdown-r2`。范围：Windows 自有 Job 收口、Watch Manager 停止状态、必要测试、本 dev-log。Preview `998d461` 由协调者组合，未触碰。主库 `255c392` 只读未动。
提交：`38658b5`（父）→ 本提交（Job-only Manager 收口 + 句柄单次消费 + 测试升级）+ dev-log。

## 三条先立红（本目录探针经本树 overlay 重放于 R1 冻结 38658b5，rc=1 精确命中）
用 detach worktree 于 `38658b5` 重放（`raw/red`）：
- `TestR1ReviewNeverTargetsPIDOutsideJob`：`invoked external command for PID never assigned to this Job: argv="/T /F /PID <pid>"`（外部命令取得 Job 外 PID）。
- `TestR1ReviewStopDoesNotLeaveTreeCommandDescendant`：`external command's descendant was alive at stop return and acted after release`（停止后替身后代仍可写标记）。
- `TestR1ReviewFailedShutdownDoesNotRetainConsumedJobHandle`：`Manager retained the closed Job handle after shutdown failure`（Watch 持有已关闭句柄值）。
修复后三者在 R2 树均 PASS（`raw/probe`）。

## 实现（最小、Job-only）
- **`internal/devx/process/proc_windows.go`**：`stopJob` 改为**仅用已持有 Job 句柄**（`TerminateJobObject` + `ActiveProcesses==0` ∧ 已捕获句柄 `WAIT_OBJECT_0` 双重退出证明）；**移除 PID 型外部命令** `KillProcessTree` 与 `runTaskkillBounded`；`StopJobTree(job, pid, …)` 保留签名但**忽略 pid**（Job-only），供探针/回归引用。过期仍报错并由 `defer CloseJobOwnership` 关闭所接管的句柄。
- **`internal/devx/watch/process.go`**：Manager 停止统一走 `terminateAndWaitJob`（Job 句柄），**无 PID 外部命令**。**Child 的 Job 句柄唯一消费点**：`stopMu` 下判定并取走 `job`、`stopDone=true`、消费后 `job=0`；记录 `proofErr`（本代真实证明结果）。再次 `stop`/`Shutdown`/`Swap` 不操作旧（已清零）句柄；**既有失败不得被空句柄哨兵转成成功**，也不启动新代（fail-closed）。
- **`internal/devx/watch/proc.go`** / **`process/proc_other.go`**：移除无用 `stopJobTree`/`killProcessTree`/`KillProcessTree` 帮手。
- **`TestCoordR1ManagerStopTimeoutIncludesTreeCommand` 改测**：Manager 停止在有界时间内收口 + PATH 替身**不得收到调用**（Job-only）+ 保留真实自有**根与后代**退出证明（`WAIT_OBJECT_0`）。旧「必须调用 taskkill」实现限制由协调者决定替代；慢替身红证仍存档。
- **过期测试 helper 升级（item 3）**：R1 `held` 仅写自身 PID → 升级为 **held 根 + held-leaf 真实后代**就绪握手；`TestExpiredCleanupReleasesJobAndDescendantExits` 停前持有**根与后代两个同步句柄**，停后明确断言两者 `WAIT_OBJECT_0`（错误路径释放后退出），并校验原 Job 句柄 `ERROR_INVALID_HANDLE`。

## 保留旧守卫（原绿）
`TestExpiredJobCleanupDeadlineIsNotReportedAsProven`（过期 fail-closed）、工具树取消 `TestReviewedRealBuildCancellationOwnsDescendants`/`TestReviewedCommandTimeoutIsHonored`、启动失败清理 `TestOwnedStartupFailuresNeverRunMainAndReapCreatedProcess`、`TestCoordProcessManagerCountsDescendants`、`TestManagerShutdownReaps*`、`TestGateOwnedTree*`、`TestNormalStopProvesJobEmptyAndHandles`、`TestJobZeroProofRequiredBeyondHandles`。所有新 Windows 专用测试 `//go:build windows`。

## 外部 overlay 变异（compile rc0 → 目标守卫真实翻红 → 恢复绿）
- **MUT-revert-pid**（StopJobTree 恢复 PID 外部命令）：`go test -c` rc=0 → taskkill boundary 两测 **rc=1 FAIL**（`invoked external …`/后代 post-release 动作）。
- **MUT-double-consume**（watch 消费后不清零 job）：`go test -c` rc=0 → `TestR1ReviewFailedShutdownDoesNotRetainConsumedJobHandle` **rc=1 FAIL**（保留已关闭句柄）。
证据 `raw/mut/mut-*-compile.txt`(rc0)/`mut-*-red.txt`(rc1)；恢复 `raw/green-pkg.txt`。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
process+watch **全包普通 PASS**；`-race` process+watch 全包 rc0 / **0 DATA RACE**；vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 process 74.9% / watch 87.8%。台账 `commands.jsonl` verify PASS。

## 已修 vs 未验证
- **已修**：Manager 自有 Windows Job 走 Job-only 收口（无 PID 外部命令、无 Job 外 PID 目标、无停止后迟到外部动作）；Child Job 句柄单次消费并清零、记录真实证明、失败不转成功/不启新代；过期仍报错并关闭所接管句柄。
- **未验证**：原生窗口、跨平台运行（POSIX unsupported）、整应用性能采样；全树 `go test ./...` 组合门禁与合并在协调者；Preview/DataGrid/Lifecycle 另线。
