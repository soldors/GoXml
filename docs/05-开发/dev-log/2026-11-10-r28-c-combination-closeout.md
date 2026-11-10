# 2026-11-10 · R28-C Lifecycle combination 后续收口（独立复核线）

范围：Lifecycle / Router / App。不改主库、不改已有冻结树、不改 Hydration /
DataGrid 所有权文件、不安装依赖、不启动原生窗口、不合并、不 push。

| 项 | 值 |
|---|---|
| 组合验收基线 | `8f0f33fe0230cd9c7e16e1f46bc06844bdae78ea`（`codex/r28-combined-acceptance` HEAD） |
| 本线基点父 SHA | `8f0f33fe0230cd9c7e16e1f46bc06844bdae78ea`；其父 `67894e1` |
| 独立树 / 分支 | `C:/Users/admin/.codex/worktrees/r28-c-combination-closeout/Goxml` / `codex/r28-c-combination-closeout` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-09/product-r28/closeout-c/delivery/` |
| 主库 | `255c39251127c39d1a684baa881767a5fb7677b9`，status 空，全程只读 |

## 本线真实提交（docgate 登记）

- `8b77903594a704a661ad93a76414d6cb34c873d6`（`8b77903`）
  `test(lifecycle): R28-C 组合收口独立合同探针（生产代码零改动）`
  —— 新增 `pkg/runtime/lifecycle/closeout_contract_test.go`、
  `pkg/runtime/router/closeout_contract_test.go`、
  `pkg/runtime/app/closeout_destroy_test.go`。父提交即组合基线 `8f0f33f`。
- 本提交（dev-log 维护提交，按 docgate 自引用规则排除 HEAD）。

**生产代码零改动**：`app.go`、`router.go`、`lifecycle.go`、
`router/lifecycle.go`、`contracts/runtime_api.go` 与组合基线逐字节一致
（SHA-256 见 `raw/audit/identity.json`）。未改 opt-in 默认、未扩展公开语义、
未应用 `NotifyOwnerDestroyed` proposal。

## 组合保真实证

C 候选 `2f4ae2b` 的 11 个 Lifecycle/Router/App/Contracts 文件在 `8f0f33f`
中逐字节一致；组合树相对 C 只多 A（Hydration）/B（DataGrid）文件与 dev-log。
主库、组合验收树 `8f0f33f`、HY4 树 `2f4ae2b`、作者树 `363c9e7` 均未改动。

## 独立审查结论

- **公开 API**：枚举 `lifecycle` / `router` 生命周期 / `app` 的公开符号
  （`go doc -all` 快照 + `raw/audit/contract-surface.json`），静态审计 35/35：
  opt-in 默认关闭、`Run`/`RunWindow` 等签名未改、复用
  `contracts.LifecycleMount/Unmount`、零 `reflect`、proposal 未应用。
- **状态转换 / 线程所有权**：18 个新写独立探针（含 2 子测试共 20 个
  `--- PASS`）覆盖 Created→Mounted→Unmounted、Owner closed/cleanupDone、
  bridge pending/finalized、CancelOnly 只取消不跑 UI 回调、Finalize 幂等
  （含并发）、Post/RunPosted 执行时复查、关闭态失败语义。
- **App 销毁回调恰好一次**：两条生产事件循环 `loopEvents` /
  `runWindowEvents` 的 `app.DestroyEvent` 分支分别验证 —— 各自恰好一次
  `WithOnDestroy → LifecycleFinalize → Owner.Finalize`，重复销毁幂等，
  首个 DestroyEvent 结束循环、不双触发。
- **错误路径不误跑 UI 回调**：真实 App 连续帧内失败工厂后，旧页在失败帧
  的 Layout 中记录的 `unmounts` 仍为 0；只有后续 DestroyEvent 才清理一次。
- **判别力**：三条外部 Go overlay 变异 K1（删 DestroyEvent 接线）/ K2
  （Mount 提前到工厂构造）/ K3（错误路径误执行 UI OnUnmount），每条
  `go build -overlay ./...` rc=0 → 指定守卫 rc=1（真实 `--- FAIL`）→
  移除 overlay rc=0；生产文件字节 SHA-256 恢复 `byteIdentical=true`。
- **自我纠错（首败保留）**：K2 首轮顺序守卫在同一帧内无法区分构造期挂载，
  rc=0 而非期望 1；未降低判据，补上「失败工厂 + 下一帧」判别步后 `-r2`
  复跑转红。首败台账与原始双通道保留。

## 发现但不擅自修改的边界（交负责人裁决）

`Owner.CancelOnly()` 只调用 `o.rootCancel()`：当 `MountHandle` 的 `parent`
为独立于 owner 根的 Context（Router 传页面 Context）时，`Handle.Context`
不由 CancelOnly 取消，而由 `Router.Shutdown()` 的 `r.rootCancel()` 或
`Finalize` 的 `unmountCurrent()` 取消。`Owner.CancelOnly` 文档「立即取消
根与页面 Context」在该情形下不精确。独立探针
`TestCloseoutCancelOnlyContextOwnershipBoundary` 精确记录行为。
改实现会改变公开可观察语义 → 列为 `decision-matrix.md` D-2，建议先改文档，
本线**不改**。

其余裁决项 D-1/D-3…D-9（正式 API、proposal、切换先后、
`LifecycleFinalize` 是否合并 Shutdown、`RunPosted` 窄 TOCTOU、
原生验收授权、文档同步、合并发布）见交付的 `ADR-proposed.md` /
`decision-matrix.md`。两者均为 **PROPOSED / 未批准**，故意不写入
`docs/03-架构/ADR/`（归协调者）。

## 门禁与证据

全部正式命令经公共锁 `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock`
+ 随仓 `scripts/record-command.py`，单层 acquire、仅释放自有 token。
原始 stdout/stderr 在 `raw/cmdlogs/<id>/c1/`。

- 三包联合 **230 PASS / 0 FAIL / 1 既有 SKIP**；独立守卫 20 PASS。
- **MinGW race 真实完成**：`gcc.exe MinGW-W64 x86_64-ucrt-posix-seh 16.1.0`、
  `CGO_ENABLED=1`、`GORACE=halt_on_error=1`；全量与守卫两条 race rc=0、
  **0 DATA RACE**（不是「CGO 环境不可用」的未完成项）。
- CGO=0 `go build ./...`、`go vet`、`gofmt -l` 空、diagdoc、docgate
  （提交前与冻结态）全 rc=0。
- 覆盖率（同源码分包 profile）：lifecycle **93.8%** / router **95.0%** /
  app **92.0%**。
- `git apply --check integration-proposal.diff` rc=0；生产 `app.go` 与
  R27 基线逐字节一致、无 `NotifyOwnerDestroyed` → **proposal 仍未应用**。
- `verify-evidence.py` 的 PASS 只代表记录完整性，不代表业务全绿或语义批准。

## 五态与未验证边界

| 状态 | 结论 |
|---|---|
| 实现 | 完成（独立合同探针 + 提案文档；生产代码零改动） |
| 自测 | **验收结论：通过**（限无窗口受控集合）；**复核状态：待独立复核** |
| 独立复核 | 本线即复核线，对组合基线的公开 API/合同/DestroyEvent/错误路径已重放；协调者对 readyRef 的再复核**验收结论：未验证；复核状态：待独立复核** |
| 主库 | 未合入；HEAD 仍 `255c3925`、status 空 |
| 远端 | 未 push；本轮未执行 `ls-remote`，远端状态**无法核对** |

**验收结论：未验证** 单列：来自原生窗口的真实 DestroyEvent、真实窗口
可见性/渲染/原生退出、真实 native race、跨平台运行、多窗口/DPI/IME、
人工视觉验收、整应用性能采样。本线「真实 DestroyEvent 接线」只表示生产
代码分支被**受控合成事件**执行，不等于原生销毁事件已被验收。
`ADR-proposed.md §9` 给出必要性与 7 步最小步骤；**本线未启动窗口**。

新 API、opt-in、切换先后策略、`LifecyclePage`、`NotifyOwnerDestroyed`
是否应用均为隔离候选 / 待裁决，技术通过不自动代表语义获批。冻结后停止，
不合并、不推送、不接下一线。
