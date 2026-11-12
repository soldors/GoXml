# R28-C2 Lifecycle 原生关窗 fixture（**只编译，默认不执行**）

## 0. 本轮状态（必须先读）

- 本目录在 `pkg/runtime/app/testdata/` 下，Go 的 `./...` 默认忽略；`main.go`
  / `judge.go` / `r1_repro_test.go` / `judge_test.go` / `r2_contract_test.go`
  / `r2_readiness_probe_test.go` 均带 `//go:build nativewindow`，显式构建/测试
  也必须给该标签。
- **本轮只编译普通与 race 二进制、只跑无窗口受控回归，没有执行任何一个窗口程序**。
  共用约束：「不开窗、不控制桌面、不执行原生场景」「编译原生 fixture 不等于执行」。
- 因此下面的「自然关闭步骤」「证据字段」都是**给授权执行者的准备**，不是已有的
  原生验收结论。来自原生窗口的真实 `DestroyEvent`、真实 native race、runner rc、
  进程树无残留、跨平台运行仍是**验收结论：未验证；复核状态：未要求**。
- `log_judge` 是**日志/状态判据**结论，不是原生验收结论；`judge_test.go` 与
  `r2_contract_test.go` 的受控回放同样**不得冒充**原生 `DestroyEvent`。

## 1. 它证明什么 / 不证明什么

| 层 | 结论 |
|---|---|
| 公开接线 | 只用公开 `app.WithOnDestroy` + `app.RunSingleWindowProcess` + `app.ProcessOptions{Observer}` + `app.ExitResult` + `router.Context()`，组合真实 `router.Router`。**未**新增公开 API，**未**调用 `NotifyOwnerDestroyed` |
| 真实关闭路径 | 生产 `RunSingleWindowProcess` 在 `opts.next == nil` 时取 `next = a.w.Event`（`process_entry.go`）；公开 `ProcessOptions` 没有事件/错误/退出注入字段。销毁事件只能来自 Gio 真实窗口事件泵。**fixture 不手工触发回调、不合成 `gioapp.DestroyEvent`** |
| 确定性帧 | `nativePage.Layout` 用公开 `gtx.Execute(op.InvalidateCmd{})` 请求后续**真实 FrameEvent**（Gio 重绘命令，不是事件注入、不合成帧）；`shutdown-first` 需要 3 帧，第 2、3 帧由 fixture 请求，不依赖操作员碰巧移动/缩放窗口 |
| 唯一就绪发布点 | `recorder.publishReady()` 在**同一临界区**内把 `ready-to-close` 写入 `marker.txt`，成功后才置发布态；不存在「flag 已置但 marker 未写」的中间态（见 §3） |
| 真实 Router 行为 | 用公开 `router.Context()` 在 `LifecycleFinalize` 前后观察根 Context 仍活跃，并与真实 `r.Shutdown()` 取消根的可区分对照证明非隐式 Shutdown（见 §4） |
| 受控证据 | `judge_test.go` + `r2_contract_test.go` + `r2_readiness_probe_test.go`（日志/状态回放与真实 Router 组合守卫）+ 既有 `pkg/runtime/app/*_test.go` 的两条生产循环守卫，属受控回归，**不得**冒充原生证据 |
| 不证明 | 窗口真的显示、Gio 真的投递 `DestroyEvent`、真实 FrameEvent 确实到达、回调线程归属、真实 native race、进程/句柄收口、跨平台运行 |

## 2. 真实轨迹：手册 ↔ 源码 ↔ 机器判据逐字一致

`WithOnDestroy` 回调**同步**执行 `r.LifecycleFinalize()`；后者经 `unmountCurrent()`
在回调返回**之前**触发一次 `OnUnmount`。`finalize` 标记只能在返回之后写入，
表示「finalize 完成」。

下面两行是**机器可读声明**，由 `r1_repro_test.go` 与 `judge.go` 的
`ExpectedMarkerOrder` / `ExpectedStepsOrder` 逐项对账（改动任一处都会转红）：

MACHINE_MARKER_ORDER: callback > unmount:home > finalize > observer

MACHINE_STEPS_ORDER: construct:home > mount:home > layout:home > callback > unmount:home > finalize

| 序 | marker.txt 行 | 谁写 | 含义 |
|---|---|---|---|
| 0 | `background-shutdown:unmounts=0 cleanup=false` | 后台 goroutine（仅 shutdown-first） | 后台 `Router.Shutdown()` 已完成，观测到 0 次 OnUnmount、`CleanupDone=false` |
| 1 | `ready-to-close` | **唯一发布点** `publishReady()`（仅 shutdown-first） | 三条件同时满足且**写盘成功**后的可关窗 marker |
| 2 | `callback` | 生产 `DestroyEvent` → `WithOnDestroy` | 显式回调入口 |
| 3 | `unmount:home` | `LifecycleFinalize` → `unmountCurrent` | `OnUnmount` 恰一次 |
| 4 | `finalize` | 回调中 `LifecycleFinalize()` 返回之后 | **finalize 完成** |
| 5 | `observer:callback=… cleanup=… code=…` | `ExitResult` Observer（cleanup 之后） | cleanup 后 observer |

三个独立证明（由受控 recorder 驱动并交给 `Judge` 验证）：

| 证明 | 字段 | 判据 |
|---|---|---|
| 首次业务 Layout 前 Mount | `mount_before_first_layout` | 第 1 次 `layout:home` 时 `mounts==1` |
| 卸载前页面 Context 已取消 | `page_context_canceled_before_unmount` / `page_context_err_at_unmount` | `errors.Is(pageCtx.Err(), context.Canceled)` 且文本为 `context canceled` |
| 卸载恰一次 | `mounts` / `unmounts` | 均等于 1 |

## 3. 唯一的就绪发布点（R28-C2 核心）

R1 曾「先置 ready flag、再另一次持锁写 marker」，协调者受控探针在两个操作
之间调用关窗检查，实得 `early=false` 且 `marker.txt` 尚无 ready；后补的
ready 还能排在 callback 之前掩盖早到。本轮修复：

- **发布点唯一**：`recorder.publishReady()` 持有 `mu`，在**同一临界区**内
  调用 `writeMarkerLocked("ready-to-close")`；只有 `os.OpenFile` / `WriteString` /
  `Close` 全部成功返回，才把该行追加到 `markers` 并置 `readyPublished=true`。
  **没有任何可提前置位的就绪 flag**；`setReadyWritten()` 是显式 no-op，
  仅为兼容协调者 R1 探针保留，用来模拟「后台观测完成、尚未发布」的交错。
- **写盘失败不发布**：`publishReady()` 返回 error，记录 `ready_publish_err`
  与 `ready_refused_reason`，不置发布态；后台另写 `ready-refused:<原因>`。
- **关窗检查序列化**：`checkCloseArrival()` 在同一把 `mu` 里只读
  `shutdownObserved` 与 `readyPublished`；任一不满足即写
  `close-arrived-early:<原因>` 并置 `close_arrived_early=true`。
- **早到不可逆**：`publishReady()` 绝不清除 `closeArrivedEarly`。即使随后
  marker 写成功，它也排在 callback / observer 之后，`Judge()` 仍因
  `close-arrived-early` 与「ready 晚于 callback」拒绝。**不得靠随后补写
  marker 修复先到的关闭。**
- 兼容探针 `r2_readiness_probe_test.go`（来源：协调者
  `lifecycle-readiness-probe_test.go`）修前 rc=1、修后 rc=0，同一精确选择器。

受控回归（§3 四类，`r2_contract_test.go`）：

| 用例 | 期望 |
|---|---|
| `TestUnpublishedCloseIsEarly` | 未发布 → 关窗判早到，磁盘无 ready |
| `TestPublishedCloseIsAccepted` | 发布成功 → 关窗通过，磁盘与快照同一行 |
| `TestReadyPublishFailsOnWriteError` | 真实不存在目录触发写失败 → 不发布、仍判早到、磁盘无 ready |
| `TestLateReadyPublishCannotRescueEarlyClose` | 早到后再发布 → 不可逆，`Judge` 仍 FAIL |

## 4. 非隐式 Shutdown：真实 Router 的可观察证明（R28-C2）

R1 的 `shutdown_calls` 只统计 **fixture 自己**调用 `r.Shutdown()` 的次数，
不能观察 Router 内部是否隐式触发 Shutdown。本轮据实收窄口径并补真实证据：

- **可观察状态**：公开 `router.Context()` 返回 Router 级根 Context。
  `r.Shutdown()` 会 `rootCancel()` 取消它；`r.LifecycleFinalize()` 只做
  `life.finalize()` → `owner.Finalize()`，**不**调用 `Shutdown`。
- **fixture 记录**：销毁回调在 `LifecycleFinalize()` 前后各读一次
  `r.Context().Err()`，写入 `router_root_err_before_finalize` /
  `router_root_err_after_finalize`。`natural` 场景两者必须都是 `""`（根仍活跃）；
  `shutdown-first` 场景后台已显式 `Shutdown`，根必然已取消，故该判据不适用。
- **真实组合守卫**（`r2_contract_test.go`，无窗口）：
  `TestRealRouterFinalizeKeepsRootContextActive` 用真实
  `New + EnableLifecycle + Register + Navigate` 后调用 `LifecycleFinalize()`，
  断言根仍活跃；`TestRealRouterShutdownControlCancelsRootContext` 用另一个真实
  Router 调 `Shutdown()`，断言根变为 `context.Canceled`。两者状态**可区分**：
  若 Finalize 隐式触发 Shutdown，前一个测试必红。
  `TestRealRouterObservationFeedsJudge` 把真实观察值喂给 `Judge` 仍 PASS；
  `TestJudgeRejectsRouterRootCanceledAfterFinalize` 证明日志一旦显示根已取消
  就会被拒绝。
- **外部 overlay 反例**：变异取证 M4 只在**树外副本**给生产
  `Router.LifecycleFinalize()` 注入一次 `r.Shutdown()`，证明该守卫确实能变红；
  树内生产文件字节不变，**不构成生产行为改动**。
- **口径收窄**：`shutdown_calls*` 字段与
  `fixture-shutdown-call-count-changed` 判据只校验 **fixture 日志自洽**，
  `result.json` 的 `evidence_scope` 明确写出这一点；Router 内部行为以上述
  真实根 Context 观察为准。

## 5. 自然标题栏关闭步骤（授权后由人工执行，本轮未执行）

1. 取得桌面控制/原生窗口的**明确新授权**（旧轮次授权不可沿用）；确认公共锁
   `D:/Codes/Goxml-night-reports/2026-10-27/heavy.lock` 空闲，在专用验收树操作。
2. 为本轮**独占新建**证据目录（已存在即拒绝，旧 PASS 不可复用）；核对
   普通/race 二进制 SHA-256、源码清单与构建 argv/env/rc（见 §8）。
3. 以 `natural` 场景启动已编译二进制：`lifecyclenative <new-dir> natural`。
   记录父进程观察到的 `runner_observed_rc`。窗口标题含 PID。
4. 让窗口稳定渲染若干帧；**不做任何鼠标/键盘注入、不向别的进程发消息、不强制终止**。
5. 用**自然方式点击该窗口标题栏的 X** 关闭（不是代码内 `WM_CLOSE`、不是
   `system.ActionClose`、不是注入事件）。这是「真实关闭路径」的唯一来源。
6. 进程退出后读取 `<dir>/marker.txt` 与 `result.json`：确认 `runner_observed_rc`
   与 `process.code` 一致；确认 `log_judge.verdict=PASS` 且 `reasons=[]`；
   确认 `router_root_err_after_finalize=""`；确认没有残留进程/窗口、
   `activeAfter=0`、临时目录归属清晰。
7. 再以 `shutdown-first` 场景重复 3–6：fixture 会自行请求后续真实 FrameEvent，
   **必须等 `marker.txt` 出现 `ready-to-close` 之后**才点击标题栏 X。
   若出现 `close-arrived-early` 或 `ready-refused`，该次判为**证据不足**，
   不得计入通过；**不得事后补写 marker**。
8. 概率性判据重复 N 次逐次留痕；授权范围内再跑真实 native `-race` 二进制。
   `-tags nativewindow` 集合与默认受控集合**分开记录**，不得互相冒充。

> 步骤 3–8 全程需 `scripts/record-command.py run --lock-mode acquire` 留痕；
> 原始 stdout/stderr 字节、argv、env、rc、timeout、进程 containment 都落盘。

## 6. 原始证据字段（`result.json` / `marker.txt`）

| 字段 | 由谁写 | 判据 |
|---|---|---|
| `marker_lines` / `marker.txt` | fixture | 与 `MACHINE_MARKER_ORDER` 一致；`callback → unmount:home → finalize → observer` |
| `steps` | fixture | 与 `MACHINE_STEPS_ORDER` 一致（子序列匹配，允许多帧 `layout:home`） |
| `mount_before_first_layout` | fixture | **首次业务 Layout 前 Mount** |
| `page_context_err_at_unmount` / `page_context_canceled_before_unmount` | fixture | **卸载前页面 Context 已取消** |
| `mounts` / `unmounts` | fixture | `OnUnmount` **恰一次** |
| `ready_published` / `ready_publish_err` / `ready_refused_reason` | 唯一发布点 | 只有写盘成功才 `ready_published=true`；失败留原因且不发布 |
| `close_arrived_early` / `close_early_reason` | 关窗检查 | 早到记录**不可逆**，随后补写不翻案 |
| `router_root_err_before_finalize` / `router_root_err_after_finalize` | 公开 `router.Context()` | natural 场景两者必须为 `""`（根仍活跃） |
| `shutdown_observed` / `shutdown_unmounts` / `shutdown_cleanup_done` | 后台 goroutine | **后台 Shutdown 只取消**：0 次卸载、`CleanupDone=false` |
| `shutdown_calls` / `shutdown_calls_before_finalize` / `shutdown_calls_at_finalize` | fixture | **仅 fixture 显式调用计数**，只校验日志自洽，**不证明** Router 内部行为 |
| `frame_requests` / `frames_needed` / `layouts` | fixture | shutdown-first 有 `frames_needed-1` 次公开的后续帧请求 |
| `log_judge.{verdict,reasons,scope}` | `Judge()` | `PASS` 且 `reasons=[]`；**scope 明确不是原生验收** |
| `evidence_scope` | fixture（静态事实） | 明确写出上述计数口径与 Router 根观察的证据边界 |
| `observer_after_cleanup` / `process_code` | `ExitResult` | cleanup 后 observer；正常关窗 `process_code=0` |
| `event_source` / `frame_event_source` | fixture（静态事实） | 生产 `app.Window.Event` 无注入；`op.InvalidateCmd` 是公开重绘请求 |
| `real_close_path.*` / `process.*` | `ExitResult` | 正常人工关窗 `destroy_err=""`、`callback_done=true`、`cleanup_done=true` |
| `runner_observed_rc` | **授权执行者** | 父进程实测 rc，与 `process.code` 一致 |
| `no_residual_process` | **授权执行者** | 进程树、窗口句柄、临时目录收口；`activeAfter=0` |
| `natural_titlebar_close_steps_performed` | **授权执行者** | §5 步骤逐条真实完成，非代码内关闭 |
| `plain_binary_sha256` / `race_binary_sha256` | 授权执行者按 §8 复核 | 二进制身份 |
| `native_race_result` | **授权执行者** | 真实 native `-race` 结果（本轮未跑） |
| `cross_platform_run_result` | **授权执行者** | Linux/macOS 运行（本轮未跑） |
| `native_destroy_event_verdict` | fixture（常量） | 恒为「未验证」；编译与 `log_judge` 都不是原生证据 |

`runner_fields_required` 显式列出只能由执行者填写的字段，fixture 编译成功
**不填**这些字段，避免把「编译过」写成「跑过」。

## 7. 后台取消 vs UI owner 清理（两个场景的区别）

| 场景 | 后台 `Router.Shutdown()` | UI owner `LifecycleFinalize` |
|---|---|---|
| `natural` | 不调用 | 真实 `DestroyEvent` 的 `WithOnDestroy` 回调里调用一次 → `Owner.Finalize` → `unmountCurrent` 一次 `OnUnmount`；Router 根 Context 仍活跃 |
| `shutdown-first` | 确定性 3 帧后调用：`CancelOnly` 只取消 owner 根/拒绝排队，**不执行 `OnUnmount`**、`CleanupDone=false`；三条件满足且写盘成功才发布 `ready-to-close` | 随后人工关窗时调用一次，补上**恰好一次**清理 |

`Finalize` **不隐式** `Shutdown` 的证据以 §4 的真实 Router 根 Context 观察为准；
`shutdown_calls` 只作日志自洽校验。生产语义以已冻结的
`lifecycleBridge.finalize()`（`owner.Finalize()`，不调用 `Shutdown`）为准，
本轮**不改** Owner / Router / App 生产行为。

## 8. 固定可复核身份（本轮已做，未执行）

从**干净冻结树**（`git status --porcelain` 为空）构建，普通与 race 二进制
都**只编译不运行**。交付 `r2-delivery-c/raw/fixture/` 中：

- `source-manifest.json`：冻结 commit / tree SHA、`go list -deps` 得到的本模块
  源码文件相对路径 + 字节 + SHA-256 清单、聚合源码 SHA-256；
- `build-identity.json`：每条构建的 argv / env / observed rc、二进制字节与
  SHA-256、`go version -m` 原始输出；
- `commands.jsonl` + `raw/cmdlogs/`：全部正式命令的原始双通道与锁记录。

`go version -m` 若**缺 `vcs.revision`**（本环境实测只见 `vcs.modified=true`），
以「冻结 commit/tree SHA + 源码文件 SHA-256 清单 + 构建 argv/env/rc +
二进制 SHA-256」的**可复核绑定**补证；**不声称编译即原生通过**，也不据
`vcs.modified` 推断提交状态。

构建命令（Windows/amd64；race 需 MinGW gcc）：

```
CGO_ENABLED=0 go build -tags nativewindow -o <delivery>/raw/fixture/lifecyclenative.exe ./pkg/runtime/app/testdata/lifecyclenative
CGO_ENABLED=1 CC=<mingw64 gcc> go build -race -tags nativewindow -o <delivery>/raw/fixture/lifecyclenative-race.exe ./pkg/runtime/app/testdata/lifecyclenative
```

受控回归（无窗口、不执行原生场景）：

```
CGO_ENABLED=0 go test -count=1 -v -tags nativewindow ./pkg/runtime/app/testdata/lifecyclenative
```

## 9. 跨平台运行缺口

- 本轮只对 Windows/amd64 做**编译**（普通 CGO=0 与 MinGW race CGO=1）。
  Linux（X11/Wayland/EGL/Vulkan）与 macOS（Cocoa/Metal）**必须 CGO=1** 且需
  系统 `-dev`/运行库，Gio 使用 `#cgo` 不是 purego；不得据此声称三端零 CGO。
- **交叉编译只证明编译性，不证明运行**。真实窗口销毁事件、DPI/IME、多窗口、
  原生退出与进程收口、native race、人工视觉验收、跨平台实机运行均
  **未验证**；旧轮次授权不自动扩展。
