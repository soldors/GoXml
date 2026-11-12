# 2026-11-12 R28 组合门禁：Watch 已拥有工具树的取消（gate）开发日志

起点 `1e83f8a317b16cbab5e7fc7a03b9b8bdf3843e13`（候选 `codex/r28-release-candidate-review`）。分支 `codex/r28-watch-command-cancel-gate`，独立树 `r28-watch-command-cancel-gate`。所有权：`internal/devx/process/` + 必需 Watch 受控回归 + 本 dev-log；Watch 服务进程策略/Preview 5s 修复/CLI/API/跨平台契约未改。主库 `255c392` 只读未动。
提交：`1e83f8a`（父）→ 本提交（窄修复 + 门禁回归 + 变异）→ dev-log。

## 先立事实（同材料红）
- 最终组合 MinGW race 与基线同 argv 对照均红：`TestReviewedRealBuildCancellationOwnsDescendants` `bounded=false … lateWrite=true`；`TestReviewedCommandTimeoutIsHonored` 40ms 超时后要等外部取消才结束。
- 本地复现：`-race` 下两测同型失败（`raw/red/watch-cancel-race-red.txt`）；非 race 单跑 build-cancel 有时侥幸 PASS（时序相关），timeout 稳定 FAIL。
- 根因定位：`process.RunCommandTree` 取消分支调用 `StopJobTree`，后者**先等外部 `taskkill`**，之后才 `TerminateAndWaitJob`（已持有的 Job 句柄）。taskkill 等待期给后代继续写盘（lateWrite）、并拖延有界返回（timeout 800ms 不满足）。

## 窄修复（只改 `internal/devx/process/run.go` 取消分支）
- 取消分支**优先用已持有的 Windows Job 句柄**终止整棵自有工具树：`TerminateAndWaitJob(job, deadline)`（TerminateJobObject 同步 kill 全部 Job 成员含后代，轮询 ActiveProcesses 归零），有界于同一 cleanup deadline；错误可观测（不吞）。
- **不再**在 RunCommandTree 取消路径调用 taskkill 优先的 `StopJobTree`（其等待期即 lateWrite/超时根因）。`process.StopJobTree` 语义**保持不变**（Manager 路径仍需 taskkill，见下）。
- 保持：有界（deadline）、错误可区分（ctx.Err / 退出证明错误 join）、所有权范围（仅自有 Job/进程句柄）、实际退出证明（ActiveProcesses 归零 + 进程句柄 wait）。不改 Watch 服务进程 stop-old→start-new、Preview 5s、CLI/API、跨平台契约。

## 为何不改 StopJobTree（避免破坏既有契约）
`ProcessManager.stop`（watch/process.go → `process.StopJobTree`）的既有回归 `TestCoordR1ManagerStopTimeoutIncludesTreeCommand` 要求**确实调用 taskkill**（受 StopTimeout 有界）。若把 StopJobTree 改成 Job-first 会使其失败。故修复只落在 RunCommandTree 取消分支，StopJobTree 保持。`TerminateAndWaitJob` 的过期 deadline 报错语义（`TestExpiredJobCleanupDeadlineIsNotReportedAsProven`）亦保持不变。

## 守卫与变异
- 门禁回归（本树，门闩控制自有根/子进程，无窗口）：
  - `TestGateOwnedTreeCancelStopsDescendantsNoLateWrite`：显式 cancel → RunCommandTree 有界返回 Cancelled → **取消后再放行 release 也不写 late 文件**、后代进程句柄证明已退出。
  - `TestGateOwnedTreeNormalExitPositiveControl`：正常退出 rc=0、未取消、不误杀。
- 既有两测（`TestReviewedRealBuildCancellationOwnsDescendants`、`TestReviewedCommandTimeoutIsHonored`）修复后 `-race` PASS，断言未放宽（仍 1s/800ms 判定）。
- 变异（独立源码 overlay：用父 `1e83f8a` 的 `run.go` 回退取消路径）：`go test -c` **rc=0** → `TestReviewedCommandTimeoutIsHonored` **rc=1 FAIL**（`raw/mut/mut-revert-run.overlay.json`、`mut-compile.txt` rc0、`mut-red.txt` rc1）。恢复 PASS。
- 无后代遗留：门禁测试用 `Windows WaitForSingleObject(SYNCHRONIZE)` 证明 pid 已退出。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
process pkg PASS；full watch PASS；`-race` process+watch+hydration+hydrationchannel rc0 / 0 DATA RACE（`raw/race/race.json`，含两门禁测）；vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 process 78.1% / watch 88.4%；preview 未改，单独复跑 PASS。台账 `commands.jsonl` verify PASS。

## 修复 vs 未验证
- **已修**：RunCommandTree 取消路径优先用已持有 Job 句柄终止自有工具树 → 有界返回、无迟到写、退出证明。
- **未改/保留**：Manager 的 StopJobTree/taskkill 语义；Preview 5s 修复；Watch stop-old→start-new；CLI/API/跨平台。
- **未验证**：原生窗口、跨平台运行、整应用性能采样；taskkill 可用性环境差异（基线 preview 偶发红属环境，本树 preview 复跑绿）。全树门禁由协调者在最终组合统一执行。
