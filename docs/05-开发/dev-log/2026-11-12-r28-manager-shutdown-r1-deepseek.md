# 2026-11-12 R28 Manager 停止路径集中返修（R1）开发日志

起点 `c257c1c0e767ba60f1e016d60951bf3c88412f76`（首败对照 `2c16e37`）。分支 `codex/r28-manager-shutdown-r1`，独立树 `r28-manager-shutdown-r1`。范围：`internal/devx/process`、必要 Watch Manager 回归、本 dev-log。Preview 已另线（未触碰）。主库 `255c392` 只读未动。
提交：`c257c1c`（父）→ 本提交（stopJob 三缺陷修复 + 专有回归 + 三 overlay 变异）→ dev-log。

## 先立红（同材料，重定位 overlay）
用本树 overlay（`process/start_windows_test.go` → 协调者 `start_windows_r2_test.go`）重放两条精确选择器，冻结态 rc=1，精确命中：
- `TestReviewExpiredCleanupStillClosesOwnedJob`：`expired cleanup left the owned Job handle open: QueryInformationJobObject err=<nil>`。
- `TestReviewNoTaskkillSideEffectAfterJobProofReturns`：`external PID-based command acted after successful stop returned: /T /F /PID <pid>`。
（`raw/probe/red-frozen.txt`）

## 三个 P1 缺陷（c257c1c）与修复
| 缺陷 | c257c1c 根因 | 修复 |
|---|---|---|
| **P1 过期遗留 Job 句柄** | 过期提前返回发生在 `CloseJobOwnership` 之前 | **defer `CloseJobOwnership(job)` 覆盖所有返回路径**（含过期/错误）；过期仍返回明确错误 |
| **P1 Job 归零证明缺失** | 仅等初始快照已打开句柄，返回前不再查 `ActiveProcesses` | `waitJobEmptyAndHandles`：同一 StopTimeout 内 **双重证明** —— (a) Job `ActiveProcesses==0` 且 (b) 已捕获自有句柄全部 `WAIT_OBJECT_0`；Job 查询失败即报错阻止交接 |
| **P1 停止返回后仍可执行 PID 终止命令** | 成功分支启动 goroutine 后直接返回（fire-and-forget） | 外部 tree 命令**并行发起但返回前收口**；用**较早子截止**（deadline - 保留量）使其在外层 StopTimeout 前被取消并收口；为外层 `c.done` 收口保留预算；慢/无目标不挤掉证明预算（证明并行运行，不被串行阻塞） |

- 保持：单一 StopTimeout、先停旧后启新、失败不交接；未放大 StopTimeout、未放松退出守卫、未吞错、未依赖外层 recorder。
- 保留旧守卫：`TestCoordR1ManagerStopTimeoutIncludesTreeCommand`（taskkill 仍被调用，bounded）、过期错误 `TestExpiredJobCleanupDeadlineIsNotReportedAsProven`（fail-closed）、工具树取消 `TestReviewedRealBuildCancellationOwnsDescendants`/`TestReviewedCommandTimeoutIsHonored`、启动失败清理。
- 未移除正常路径 tree 命令、未改旧守卫覆盖策略（无需裁决提案）。

## 专有回归（Windows-only，`//go:build windows`，无窗口，自有根/后代）
`internal/devx/process/manager_shutdown_r1_windows_test.go`：
- `TestExpiredCleanupReleasesJobAndDescendantExits`：过期 → 明确错误 + 原句柄 `ERROR_INVALID_HANDLE`（已释放）+ 真实自有后代 `WaitForSingleObject` 无错且 `WAIT_OBJECT_0`（退出）。
- `TestNormalStopProvesJobEmptyAndHandles`：正常停止成功 + 后代退出（显式 `WAIT_OBJECT_0`）。
- `TestJobZeroProofRequiredBeyondHandles`：空句柄快照 + Job 内仍有活跃进程时，`waitJobEmptyAndHandles` 必须失败（句柄-only 不足，须 Job 记账归零）。
- 后代退出断言显式检查 `WaitForSingleObject` 错误并要求 `WAIT_OBJECT_0`。

## 三 overlay 变异（compile rc0 → 目标守卫 FAIL → 恢复绿）
- **MUT-expired-leak**（回退 defer-close）：`TestExpiredCleanupReleasesJobAndDescendantExits` FAIL（`did not release the owned Job handle`）。
- **MUT-late-command**（回退 fire-and-forget + 探针）：`TestReviewNoTaskkillSideEffectAfterJobProofReturns` FAIL（`acted after successful stop returned`）。
- **MUT-no-jobzero**（移除 ActiveProcesses==0 证明）：`TestJobZeroProofRequiredBeyondHandles` FAIL。
证据 `raw/mut/mut-*-compile.txt`(rc0)/`raw/mut/mut-*-red.txt`(rc1)；恢复 `raw/green-process.txt`、`raw/green-probe.txt`。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
process pkg PASS；full watch PASS；`-race` process+watch 全包 rc0 / **0 DATA RACE**（含 Manager 时间线与探针）；vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 process 73.5%（较 70.4% 基线上升，新增专有回归）。台账 `commands.jsonl` verify PASS。

## 已修 vs 未验证
- **已修**：过期/错误路径仍释放 Job 句柄；退出证明=ActiveProcesses==0 ∧ 句柄 `WAIT_OBJECT_0` 双重；外部 tree 命令返回前收口、有界、不饿死证明、慢/无目标不误报。
- **未验证**：原生窗口、跨平台运行、整应用性能采样；全树 `go test ./...` 组合门禁与合并/发布由协调者执行。process 覆盖率 73.5% 为实际记录。
