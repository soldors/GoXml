# 2026-11-08 R26-A 集中返修 R3（WATCH-REBUILD-RESTART）开发日志

返修父 = R2 冻结 `25e1e6b538565d48b29fd0ae649f016883c12ddf`。分支 `codex/r26-r3-deepseek`，树 `r26-r3-deepseek`。主库 `1505fd4` 只读未动。只闭合 F-A9/F-A3-R2/F-A10/F-A7b-R2；**不推翻**已通过的 Source/deadline/停止链/平台编译。
提交：`25e1e6b`（父）→ `d712525`（四项修复）→ `cf0aabe`（工具身份测试）→ 本 dev-log。

## 逐项：原红 → 新绿（同协调者材料）
- **F-A9 / S1 工具身份与普通工程落点**：`CLIGenerate` 不再从 cwd 向上找 `cmd/goxml`。改为由 `runtime.Caller` 定位本仓库根（与 cwd 无关，非作者工作树/主库硬编码）、`go run ./cmd/goxml`（`Dir=仓库根`）运行真实 generate --check + 相对入口。新增内部 adapter API：`NewRealBuilder(...,toolExe)`、`SetToolExe`、`ToolIdentity`（协调者接线时传显式工具身份，优先于自动解析）。原红 `TestCoordR2GenerateFromUserWorkingDirectory` FAIL（用户 cwd 找不到源码根）；新绿 PASS。
- **F-A3-R2 / S2 取消收整棵树（未闭合的 F-A3a）**：新增 `watch.RunCommandTree`——以 Windows Job(KILL_ON_JOB_CLOSE) 拥有**整棵生成树**；ctx 取消/超时有界 `taskkill /T` + `TerminateJobObject` + **有界等待 ActiveProcesses 归零**后才返回。`CLIGenerate` 用它运行 `go run ./cmd/goxml`（保留 toolexec→go build→generator→go run 完整真实父链）。原红：取消后 4 条父链仍 3 个活跃、放行后 home_gen.go 迟到写盘（rc=-1 但未收口）；新绿：~6s 内返回，**active owned descendants=0、无迟到写盘**、正常对照 rc0。**撤销 R2“仅 CommandContext+WaitDelay 即无迟到写”的错误结论**（旧 raw 保留）。
- **F-A10 / S3 Run/Stop 数据竞争**：`Watch.stopped` 改 `atomic.Bool`；`Stop/Stopped` 用 `Store/Load`，`Run` 用 `stopped.Load()`；无阻塞锁、保留退出活性。原红：MinGW -race 真实 DATA RACE（run.go:26 读 / watch.go:219 写）；新绿：`TestCoordR2RunAndStopConcurrent` -race rc0 / 0 DATA RACE。
- **F-A7b-R2 / S3 时钟坐标混用致 latest 饿死**：`Run` 改用 `w.runStart` 作**统一时钟原点**（与 Note/Pump/stale 同一坐标），不再每次 Run 从 `time.Now()` 重起算。原红 `TestCoordR2DelayedRunStillPublishesLatest` FAIL（first stale 后 latest 在 1s 内不发布，1h 偏移把 latest 排到未来）；新绿 PASS（first stale 正确，latest 稳定发布）。

## 两条可编译行为变异（独立副本 + Go overlay，红→恢复绿）
- **MUT-1**（`stopped` 回退为普通 bool，run.go 去 `.Load()`）→ `TestCoordR2RunAndStopConcurrent` -race **WARNING: DATA RACE**。
- **MUT-2**（run.go `start := time.Now()` 回退）→ `TestCoordR2DelayedRunStillPublishesLatest` **FAIL**。
- 恢复（无 overlay）→ watch+adapter 全 PASS。真红非编译/flags。证据 raw/mutations-*.txt。

## 测试适配（仅 helper/接口，判据不降级）
`adapter_e2e_test.go` 的 `NewRealBuilder` 增 toolExe 形参（空=自动解析）；新增 `tool_identity_test.go`（真实测试 SetToolExe/ToolIdentity、runtime.Caller 构建工具、missing-tool 非零、buildError）。协调者 R2 探针 `zz_coord_r2_{adapter,race,clock}` 保留为持久回归（现全绿）；R1 原探针仍绿。**未删健康断言、未加 Skip**。

## 门禁（Windows/amd64，CGO=0；race MinGW）
watch+adapter test 全绿；`-race` 两包 **rc0 / 0 DATA RACE**（含真实 Run 并发）；`go build ./...` rc0；**GOOS=linux CGO=0 build watch+adapter rc0**（仅交叉编译，非 Linux 运行）；vet rc0；gofmt 空；docgate ok；覆盖 watch 73.1% / adapter 71.7%（新增真实工具身份测试后由 38.3% 升；按实际记录不凑数）。

## 接线建议（更新）
主 CLI 分发归协调者。不得让 cli import watchadapter（环）。由 cmd/ 同级调度层 import watchadapter+watch，调用 `watchadapter.NewRealBuilder(proj,entry,out,toolExe)`（toolExe 由调度层给出真实工具二进制身份）或 `SetToolExe`，再 `watch.New(...).Run(ctx)`。参数/默认仍属候选未批准。

## 未证边界
无原生窗口/视觉/IME；无跨平台**运行**（仅 GOOS=linux 交叉编译；POSIX 进程树收口在 proc_other.go 明确 unsupported）；无性能采样；`go run` 每次编译（性能未优化，工具身份已可注入以规避）；真实 e2e/取消夹具用阻塞 console/自有无窗口 helper，非 GUI。冻结≠批准。

## 五态
实现 done(`cf0aabe`+dev-log) / 自测 pass / 独立复核 PENDING（协调者）/ 主仓未合 / 远端未推送。
