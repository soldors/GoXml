# 2026-11-07 R26-A 集中返修 R1（WATCH-REBUILD-RESTART）开发日志

返修基点 = 本线原冻结 `7e3350c6f3d2a2af6e9a1a7b13ebc5a1f519ca23`（独立复核 BLOCK）。新分支 `codex/r26-r1-deepseek`，树 `r26-r1-deepseek`。主库只读（`1505fd4`，未动）。只闭合 F-A1~A7，不接下一线。
提交：`7e3350c`（父）→ `f3ace4c`（F-A1/A2/A3/A4/A5/A6 核心修复）→ `f27afc5`（F-A7 Run 驱动 + 真实 e2e）→ 本 dev-log。

## 逐 finding：原红 → 新绿（真实命令/证据）
- **F-A1**（owned 头口径漏判/误判）：`snapshot.guardHeaderOwned` 改用 `codegen.HasProtectedHeader`（真实三行头）+ Manifest 登记；不复制错误头。原红：`TestCoordActualProtectedHeaderOwnership` real-output/not-protected 均 FAIL；新绿：PASS。证据 raw/red/probes-frozen-red.txt、raw（终绿）。
- **F-A2**（真实构建未进入 ProjectDir）：`realGoBuild` 设 `cmd.Dir=ProjectDir`，`BuildFn` 携 workDir，相对入口 `./cmd/app`。原红：`TestCoordRealBuilderUsesProjectDirectory` FAIL（产物非 MZ）；新绿 PASS（真实用户 main）。
- **F-A3**（取消/超时未阻止发布/停止生成）：`runGeneration` 构建前与发布/交接前检查 `ctx.Err()`；`RealBuilder` 落实 `BuildTimeout`（ctx.WithTimeout）；adapter `CLIGenerate` 在返回前 join cli.Run goroutine（无迟到写盘）。原红：`TestCoordCanceledContextCannotPublish` FAIL（仍发布 gen=1）；新绿 PASS。
- **F-A4**（进程树收口失效 + 交接顺序）：`ProcessManager` 用 Windows **Job Object**（KILL_ON_JOB_CLOSE）在 start 即 assign，关闭 Job 即杀整棵树（含后代），不依赖根存活；**Swap 改为 stop-old→start-new**（任务 A2；构建在 Swap 前完成，构建期间旧继续服务）。原红：`TestCoordProcessManagerCountsDescendants` FAIL（leaf 存活）；新绿 PASS。
- **F-A5**（Pump 清空本代触发路径）：`Debouncer` 在 Pump 清空前**冻结本代路径快照**（`GenPaths()`），Builder 收冻结列表。原红：`TestCoordChangedPathsSurvivePump` FAIL（paths=[[]]）；新绿 PASS。
- **F-A6**（CLI 接线导入环）：`internal/devx/watch` **不再 import internal/cli**；新增 `internal/devx/watchadapter` 仅此包 import cli+watch（`CLIGenerate`/`NewRealBuilder`）。证据：`go list ./internal/devx/watch` deps 不含 cli；adapter 编译通过；`go build ./...` rc0。原为编译阻断（a-cli-import-cycle go list rc1），现解除。
- **F-A7**（缺可运行驱动/真实整链判据）：新增 `Watch.Run(ctx)` 真实轮询驱动；新增 `watchadapter` 真实 e2e：临时 module（复制仓库 go.mod + replace 自身）+ 真实 gxml 页，**真实 `cli.Run generate --check`** + **真实相对 `./cmd/app` 构建**阻塞 console + **真实进程切换**。断言：初始发布(gen1,console 运行)→坏 GXML 真实 --check 失败(stage=generate)不重启(旧 PID 保留)→恢复发布新代(新 PID)→Shutdown 后 ActiveAfter=0。原红：无驱动（源码确证）；新绿：真实 e2e PASS。删除原 `-100000` 哨兵测试。

## 两条可编译行为变异（独立副本 + Go overlay，红→恢复绿）
- **MUT-1**（process.go 不 assign Job）→ `TestCoordProcessManagerCountsDescendants` **FAIL**（后代存活）。
- **MUT-2**（snapshot 回退错误两行判定）→ `TestCoordActualProtectedHeaderOwnership` **FAIL**。
- 恢复（无 overlay）→ watch+adapter 全 PASS。真红非编译/flags。证据 raw/mutations-mut1-red.txt、mut2-red.txt、restore-green.txt。

## 测试适配（仅 helper/接口，行为判据不降级）
- 夹具改用真实三行保护头（F-A1 要求）。
- `BuildFn`/`realGoBuild` 增 workDir 形参（F-A2）。
- `TestProcessManagerSpawnFailKeepsOld` 重写为 `TestProcessManagerSpawnFailAfterStop`（stop-old→start-new：失败后无当前进程、旧根收口；不承诺旧 PID 仍在——F-A4/A2 任务要求）。
- 协调者探针 `zz_coord_a_test.go`/`zz_coord_aprocess_test.go` 保留为持久回归（现全绿）。**未删除任何健康断言、未加 Skip**。

## 门禁（Windows/amd64）
`go build ./...` rc0；`go vet` watch+adapter rc0；`gofmt -l` 空；`go test` watch+adapter 全 PASS；`-race` 两包 rc0 / 0 DATA RACE；覆盖 watch 76.3% / adapter 81.8%；docgate 见提交后复验。真实 Go 命令持公共锁、真实 observed rc、原始双通道。

## 候选（不自动批准）
交接**回滚语义**（先起新代健康探测通过再停旧代，存在短暂重叠，非“串行无重叠”）单列候选，未实现为默认（默认遵循任务 stop-old→start-new）。未定策略见原 `delivery/candidates.md`，返修不改变其“未批准”状态。

## 未证边界
无原生窗口/视觉/IME；无跨平台（Job Object 仅 Windows；POSIX 路径未在真机验证）；无正式性能采样；完整编译器全包与正式字体验收不在本线。冻结≠批准。

## 五态
实现 done(`f27afc5`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。

追加提交：`d03973a`（F-A4 进程树收口硬化：根存活时先 taskkill /T 再 TerminateJobObject+有界等待；根已退出仍用 Job 句柄终止后代；CoordProcessManagerCountsDescendants 稳定 3/3）。
