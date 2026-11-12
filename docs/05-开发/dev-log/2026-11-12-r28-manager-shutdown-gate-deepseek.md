# 2026-11-12 R28 全树阻断 1：Manager 服务进程树收口（gate）开发日志

起点完整 `2c16e37109c017fb025d5ded5b8f88530f7da0c9`。分支 `codex/r28-manager-shutdown-gate`，独立树 `r28-manager-shutdown-gate`。所有权：`internal/devx/watch/` Manager 与受控测试、`internal/devx/process/` 必要 Manager 帮手、本 dev-log；Watch 服务进程策略/Preview 5s/CLI/API/跨平台未改；与 HY4 Preview 不交叉。主库 `255c392` 只读未动。
提交：`2c16e37`（父）→ 本提交（Manager 收口修复 + 时间线复现 + 判别回归 + 变异）→ dev-log。

## 先立事实（精确复现，非仅嫌疑）
首败：`TestCoordProcessManagerCountsDescendants` → `gen=1 job 终止失败: job 未在 StopTimeout 内清空（ActiveProcesses 未归零）` + `process exit proof deadline expired`（`raw/red` / gate `raw/watch-final-full-tree`）。
- 该红只在**全树 `go test ./...` 重负载竞争**下出现；本机单测/`-race` 单跑 5x 均 PASS，故用**确定性垫片**复现：PATH 覆盖 `taskkill.exe` 垫片 `sleep(1200ms) > StopTimeout(1s)`。
- 确定性时间线复现（`raw/red` 记录于测试日志）：关闭 stdin 后进入 `stopJobTree`；`stopJob` **先等外部 taskkill**（1200ms，ctx=1s deadline 到点被取消）→ 单 StopTimeout 被吃光 → `terminateJob`/`waitProcessHandles` 无预算 → `ActiveProcesses 未归零` + `process exit proof deadline expired`；leaf 仍存活。**根因**：taskkill 与 Job 终止/退出证明**共用同一个 StopTimeout deadline**，taskkill 耗时把 Job 收口饿死（线索已证实，非预判）。

## 最小修复（`internal/devx/process/proc_windows.go` stopJob；`internal/devx/watch` 无生产改动）
- **先用已持有的 Windows Job 句柄终止整棵自有树**：`TerminateJobObject(job)` 立即同步 kill 全部 Job 成员（含后代），**不被外部 taskkill 挤占**。
- **退出证明在 taskkill 之前**：`waitProcessHandles(handles, deadline)` 等已捕获的自有句柄在截止前 signal（权威收口依据）。
- taskkill 仅作**有界补充**：**异步**发起（bounded 同一 deadline），仅当 Job 终止/退出证明**失败**时才在有界余量内等待并保留其错误（fail-closed，不吞错）；Job 已证明退出时其耗时/无目标不误报失败。
- **fail-closed 保留**：调用时 deadline 已过 → 立即 `process exit proof deadline expired`（`TestExpiredJobCleanupDeadlineIsNotReportedAsProven` 保持）。
- 不改：单一 StopTimeout、先停旧再启动新代、失败不交接；未放大 StopTimeout、未放松退出守卫、未吞错、未依赖外层 recorder。

## 守卫与变异
- 判别回归（Windows-only、无窗口、自有根/后代、门闩/时间线）：
  - `TestManagerShutdownReapsDespiteSlowTaskkillExceedingStopTimeout`：taskkill 垫片 sleep(1.2s)>StopTimeout(1s)，修复后 Shutdown 仍**有界成功收口、leaf 退出**。
  - `TestManagerShutdownReapsAfterTaskkillDelay`：taskkill 0.4s 慢但 <StopTimeout(3s)，Shutdown 成功收口。
- 保留原绿：`TestCoordProcessManagerCountsDescendants`、`TestCoordR1ManagerStopTimeoutIncludesTreeCommand`（taskkill 仍被调用且有界）、`TestExpiredJobCleanupDeadlineIsNotReportedAsProven`（过期 fail-closed）、`TestReviewedRealBuildCancellationOwnsDescendants`/`TestReviewedCommandTimeoutIsHonored`（工具树取消原绿）、`TestGateOwnedTree...` 门禁原绿。
- 变异（overlay 回退 `proc_windows.go` 至父 `2c16e37` taskkill-first）：`go test -c` **rc=0** → `TestManagerShutdownReapsDespiteSlowTaskkillExceedingStopTimeout` **rc=1 FAIL**（复现 `未归零`+`exit proof expired`，leaf alive）；恢复 PASS（`raw/mut/`）。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
process pkg PASS；full watch PASS；`-race` process+watch rc0 / **0 DATA RACE**；vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 process 70.4% / watch 88.0%；preview 未改、单跑 PASS（不交叉）。台账 `commands.jsonl` verify PASS。

## 已修 vs 未验证
- **已修**：Manager `stopJob` 顺序——先 Job 句柄终止 + 退出证明，再异步有界 taskkill；消除 taskkill 挤占单一 StopTimeout 导致 `ActiveProcesses 未归零 / exit proof expired`。
- **未改**：单一 StopTimeout、stop-old→start-new、失败不交接、Preview 5s、CLI/API/跨平台；未放大超时/放松守卫/吞错。
- **未验证**：原生窗口、跨平台运行、整应用性能采样；全树 `go test ./...` 组合门禁由协调者统一执行（本机全树红为负载竞争，本线用确定性垫片复现并在修复后绿）。process 覆盖率 70.4% 为既有基线（本线为窄 bugfix，未新增无判别力测试）。
