# 2026-11-08 R26-A 集中返修 R2（WATCH-REBUILD-RESTART）开发日志

返修父 = R1 冻结 `2bbc848a1067309272b3042681328d791d5e8090`。新分支 `codex/r26-r2-deepseek`，树 `r26-r2-deepseek`。主库 `1505fd4` 只读未动。只闭合 F-A1-R1/F-A3a-R1/F-A3b-R1/F-A4-R1/F-A7-R1/F-A8。
提交：`2bbc848`（父）→ `a878603`（6 项修复）→ 本 dev-log。

## 逐项：原红 → 新绿（同协调者材料，先红后绿）
- **F-A1-R1 / S1**（Source 不符仍被 owned）：`guardHeaderOwned` 由“仅 HasProtectedHeader”改为 **HasProtectedHeader ∧ 第2行 `// Source:` 与 Manifest 登记源 basename 精确一致**（更正之前“前缀/与--clean一致”的错误说明；cli `headerSourceMatches` 实际比对 Source）。原红 `TestCoordR1SourceMismatchRemainsUserOwned` FAIL（trigger=[] owned=[home_gen.go]）；新绿 PASS。
- **F-A7-R1 / S1**（Run 构建期间变更、发布落后代）：`Run` 维持；`runGeneration` 以构建前快照 vs 构建后重扫比对，**用户输入变化**（经所有权过滤，排除本代生成写入）则本代 **stale 不发布**、重排 latest。原红 `TestCoordR1RunDiscardsChangeDuringBuild` FAIL（first 仍发布）；新绿 PASS（first 不发布）。
- **F-A3b-R1 / S2**（Config.BuildTimeoutMS 未进 Builder ctx）：`runGeneration` 派生每代 ctx = min(外部 deadline, now+BuildTimeoutMS)，贯穿 generate+build 传给 Builder。原红 `TestCoordR1ConfigTimeoutReachesBuilder` FAIL（无 deadline）；新绿 PASS。
- **F-A3a-R1 / S2**（取消只是 join，不可有界取消）：`CLIGenerate` 改为**可取消工具子进程**（`go run ./cmd/goxml generate <dir> --check`，`exec.CommandContext`+`WaitDelay`），ctx 取消即终止子进程、非阻塞返回、无迟到写盘。原红 `TestCoordR1CLIGenerateCancellationIsBounded` FAIL（代理阻塞 >1s 才因外部释放返回）；新绿 PASS。
- **F-A4-R1 / S3**（taskkill 在停止时限外、Shutdown 可无限阻塞）：整个 stop 链（taskkill tree / job 终止 / 等待）由**同一 StopTimeout deadline** 覆盖；`killProcessTree` 用 `CommandContext`。Job create/assign/terminate/query/超时错误**如实返回**（不伪装成功）。保持 stop-old→start-new。原红 `TestCoordR1ManagerStopTimeoutIncludesTreeCommand` FAIL（>1s）；新绿 PASS。
- **F-A8 / S3**（Windows Job 未隔离，Linux 包编译失败）：Windows 实现移入 `proc_windows.go`（`//go:build windows`）；`proc_other.go` 明确 **unsupported**；公共包 `GOOS=linux CGO_ENABLED=0 go build ./internal/devx/watch ./internal/devx/watchadapter` **rc0**（仅交叉编译，不是 Linux 运行验收）。原红 go build 报 x/sys/windows 约束；新绿 rc0。

## 两条可编译行为变异（独立副本 + Go overlay，红→恢复绿）
- **MUT-1**（snapshot 回退忽略 Source）→ `TestCoordR1SourceMismatchRemainsUserOwned` **FAIL**。
- **MUT-2**（watch 去掉 BuildTimeoutMS deadline）→ `TestCoordR1ConfigTimeoutReachesBuilder` **FAIL**。
- 恢复（无 overlay）→ watch+adapter 全 PASS。真红非编译/flags。证据 raw/mutations-*.txt。

## 测试适配（仅 helper/接口，判据不降级）
协调者 R1 探针 `zz_coord_r1_boundaries/stop/cancel` 保留为持久回归（现全绿）；未删健康断言、未加 Skip。旧 R1 原生探针仍绿。

## 门禁（Windows/amd64，CGO=0；race MinGW；含真实 Run 并发）
watch+adapter test 全绿；`-race` 两包 rc0/0 DATA RACE；`go build ./...` rc0；**GOOS=linux 包编译 rc0**；vet rc0；gofmt 空；docgate ok；覆盖 watch 79.4% / adapter 88.9%；台账见交付。真实 Go 持公共锁、真实 observed rc、原始双通道。

## 接线建议（修正后）
主 CLI 分发由协调者保留。**不得让 cli import 再 import cli 的 watchadapter**；应由 **cmd/ 同级调度层**（或 cli 之外的组合入口）import watchadapter + watch 并调用 `watch.New(... Watch.Run(ctx))`。`watchadapter.NewRealBuilder`/`CLIGenerate` 即真实可用实现；`goxml watch` 参数/默认见原 candidates（仍未批准）。

## 未证边界
无原生窗口/视觉/IME；**无跨平台运行**（仅 GOOS=linux 交叉编译，POSIX 进程树收口未实现，明确 unsupported）；无性能采样；真实 e2e 用阻塞 console（非 GUI）。冻结≠批准。

## 五态
实现 done(`a878603`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
