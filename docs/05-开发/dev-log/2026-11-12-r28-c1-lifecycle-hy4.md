# 2026-11-12 · R28-C1 Lifecycle 原生关窗证据修正（HY4）

遵守 `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/coordinator/review-2026-11-12/r1-common.md`
与 `r1-hy4.md`。范围只含 `pkg/runtime/app/testdata/lifecyclenative/`、它的验收测试
与本 dev-log；Owner/Router/App 生产行为不改。不开窗、不执行原生程序、不安装依赖。

| 项 | 值 |
|---|---|
| 返修起点（作者冻结） | `fe3a6aa127bbdf88f572cf40ac98c9590116d7e8`，父 `7aba43ce90ca34d02e8fcec25f3522fabdaab6b1` |
| 组合基线 / 主库 | `611948d49525744cbb4127265983fd24236e6bf5` / `255c39251127c39d1a684baa881767a5fb7677b9`（只读） |
| 分支 / 独立树 | `codex/r28-release-lifecycle-r1` / `C:/Users/admin/.codex/worktrees/r28-release-lifecycle-r1/Goxml` |
| 代码提交 | `d0e2a79eaf710e6522576fa800f2e6a4a817c57c`（5 个文件，父 `fe3a6aa`） |
| 本提交 | dev-log 维护提交（按 docgate 自引用规则排除 HEAD）；冻结 readyRef 见交付 `handoff.md` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/r1-delivery-c/` |

## 协调者两项 P2 的闭合

1. **手册顺序与源码反序**：真实轨迹是
   `callback → unmount:home → finalize 完成 → observer` —— `WithOnDestroy`
   回调同步执行 `LifecycleFinalize`，`unmountCurrent` 在回调返回**之前**触发
   `OnUnmount`，`finalize` 只能在返回之后写入。手册的
   `MACHINE_MARKER_ORDER` / `MACHINE_STEPS_ORDER` 与 `judge.go` 的
   `ExpectedMarkerOrder` / `ExpectedStepsOrder` 逐字一致，由 `r1_repro_test.go`
   做「手册 ↔ 源码 ↔ 机器判据」三方一致性守卫。
   三个独立证明：首次业务 Layout 前 Mount、卸载前页面 Context 已取消
   （`errors.Is(pageCtx.Err(), context.Canceled)`）、`OnUnmount` 恰一次；
   `shutdown_calls_before_finalize == shutdown_calls_at_finalize` 保证
   `Finalize` 不隐式 `Shutdown`（生产语义仍是已冻结的 `lifecycleBridge.finalize()`）。
2. **确定性就绪与缺证拒绝**：`shutdown-first` 由 `nativePage.Layout` 用公开
   `gtx.Execute(op.InvalidateCmd{})` 请求后续真实 FrameEvent（Gio 重绘命令，
   不是事件注入），3 帧后通知后台 `Shutdown`，不依赖操作员碰巧移动窗口。
   只有「后台 Shutdown 已完成 + 未发生 OnUnmount + `CleanupDone=false`」
   三条件同时满足才写 `ready-to-close`；关窗早到或后台观测缺失写
   `close-arrived-early` 并由 `Judge()` fail-closed 拒判通过。
   `judge_test.go` 的 16 个受控状态/日志回放覆盖早关闭拒绝、ready 晚于关窗拒绝、
   后台观测缺失拒绝、`ready-refused` 拒绝、旧反序拒绝、三证明缺一不可、
   隐式 Shutdown 拒绝、缺帧请求拒绝、完整顺序通过。**这些回归不冒充原生
   `DestroyEvent`**；`log_judge` 只是日志判据，`native_destroy_event_verdict`
   恒为「未验证」。

## 证据纪律（先立红、首败保留）

- `r1-red-probe`：同一精确选择器 `^TestR1ReproduceReviewFindings$` 在修前
  rc=1，8 项检查全部命中（含 F1-a 旧反序、F2-a 无帧请求、F2-b 无 ready
  marker、F2-c 无早关闭守卫、F2-d 无机器判据），`--- FAIL` 命中真实用例、
  无 SKIP、无编译失败；修复后 `r1-red-probe-green` rc=0，`--- PASS` 命中同一用例。
- 三条**外部 Go overlay** 变异（树外副本，不改冻结源）：K1 删
  `readyToClose` 的 OnUnmount 守卫、K2 删「ready 晚于 callback」顺序守卫、
  K3 删 Finalize 隐式 Shutdown 判据。每条 `go test -c -overlay` 编译 rc=0 →
  `-run` 精确选择器守卫 rc=1（命中 `--- FAIL`、无编译失败、无 SKIP）→
  去掉 overlay 恢复 rc=0。恢复判据是树内 `judge.go` **字节 SHA-256**
  `4c59e9b4…bd` 三条全一致（`raw/gates/mutations.json`）。
- 一次前台工具 600s 上限造成的 SIGTERM 中断（`r1-full-tree`）如实登记在
  `raw/gates/interrupted-run.json`：`result.json` 保持 prepared/started=false，
  部分 stdout 原样保留；本线自己的残留锁按官方 `Lock.release()` + 自有
  token 校验归还；以独立 id `r1-full-tree-r2` 后台重跑 rc=0、28 包行、
  0 `--- FAIL`。**未启动/中断的命令不计入门禁通过**。
- 阶段 1 门禁：红转绿 rc=0；受控回归 17 PASS / 0 FAIL；MinGW race 真实完成、
  0 DATA RACE；默认 `go list ./...` 不含 fixture、显式标签可选中；vet / gofmt /
  受影响包窄测 / 全树 / CGO=0 构建全 rc=0。
- 冻结态门禁（提交后复跑）：`go test` 受控回归与 race、受影响包、
  `go build ./...`、vet/gofmt、diagdoc、docgate、普通/race 二进制构建、
  `go version -m`、源码依赖清单、verify-evidence、clean —— 详见
  `raw/gates/final-report.json` 与交付 `handoff.md`。

## 固定可复核身份

从**干净冻结树**构建普通/race 二进制，**只编译不运行**。
`raw/fixture/source-manifest.json` 记录冻结 commit / tree SHA、`go.mod`/`go.sum`、
fixture 全部文件、`go list -deps` 得到的本模块构建输入逐文件 SHA-256 与聚合
SHA-256；`raw/fixture/build-identity.json` 记录每条构建的 argv/env/observed rc、
二进制字节与 SHA-256、`go version -m` 原文。若 `go version -m` **缺
`vcs.revision`**（本环境只见 `vcs.modified=true`），以可复核的源码/构建绑定
补证，不把 VCS 元数据当唯一证明、不据 `vcs.modified` 推断提交状态、
**不声称编译即原生通过**。

## 五态与未验证边界

| 状态 | 验收结论 | 复核状态 |
|---|---|---|
| 实现 | 完成（fixture/判据/手册修正；生产代码零改动） | 待独立复核 |
| 自测 | 通过（**仅限**无窗口受控集合 / 静态与编译门禁） | 待独立复核 |
| 独立复核 | 未验证（协调者对本线冻结 SHA 的再复核尚未进行） | 待独立复核 |
| 主库 | 未合入（HEAD 仍 `255c3925`） | 未要求 |
| 远端 | 未 push；本轮未执行 `ls-remote`，远端状态无法核对 | 未要求 |

**验收结论：未验证 / 复核状态：未要求（待授权后转待独立复核）** 单列：
来自 Gio 真实窗口的 `DestroyEvent`、真实窗口显示与原生退出、真实 FrameEvent
按请求到达、人工标题栏关闭 8 步、`runner_observed_rc`、
`no_residual_process`、真实 native race、Linux/macOS 实机运行（需
`CGO_ENABLED=1` 与系统库）、IME/DPI/多窗口、人工视觉验收。
`runner_fields_required` 中只能由授权执行者填写的字段本轮**全部未填**。
Image v1 对账已独立通过 18/18，本线不重做资产管线、不关闭整张任务卡。

冻结后停止，不接下一线；由协调者独立复核、组合验收并决定发布。
