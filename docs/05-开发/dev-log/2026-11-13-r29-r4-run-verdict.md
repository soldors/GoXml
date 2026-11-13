# R29-C R4：并发 Run 的所有权结果线性化（修复）

## 反例（先红）

协调者 `c-r3-concurrent-run-probe_test.go` 原样重放，台账
`r4-replay-concurrent-run-red` observed rc=1：

```
coord_r4_concurrent_run_probe_test.go:73: repeat Run reported <nil> for a host whose initial Run was rejected
```

缺口：R3 的 `Run` 在 `hostStarting` 状态下**提前返回 nil**；首个 Run 稍后被拒，
同一宿主的并发调用给出互相矛盾的结果（一 nil 一 error）。

修复前字节基线：`pkg/runtime/app/window_host.go` 18825 B
（SHA-256 `a41898f5ea475976a2da46cfc0ed509677b08ee3db7416d37e4927a1d530a93b`）。

## 修复：启动结果发布点

- `WindowHost` 新增 `runVerdict`（恰关闭一次）与 `publishRunVerdict()`。
- `Run` 的胜者路径：状态检查 → `claimGioMain` → 启动事件泵 → **发布结果** →
  调用 Main。**发布不等待 Gio Main 返回**（生产 Main 永不返回，等待者不能依赖
  它的返回）。
- 等待者路径：`hostStarting` 时等待 `runVerdict` 发布，之后按同一结果返回：
  `hostRunning` → nil；`hostRejected` → 同一 `ErrGioMainAlreadyOwned`；
  `hostDrained/hostExiting` → `errHostNotAcceptingRun`。
- `reject()` 同步发布拒绝结果；线性化点：
  - **成功**＝事件泵启动完成 + `publishRunVerdict`（先于 Main）；
  - **拒绝**＝`claimGioMain` 失败 + `reject()` 内发布；
  - **取消**＝`ExitAll`/`Cancel` 在 `hostAccepting` 下经 `mu` 转移到
    `hostDrained`（之后的 Run 返回 `errHostNotAcceptingRun`，之前的照常启动）。

## 新增受控测试（r4_host_run_test.go）

1. `TestR4RunVerdictPublishedBeforeMainReturns`：mainFunc 阻塞，胜者停在 Main
   内；等待者在 Main 未返回时即拿到 nil；释放后零窗宿主完整收口。
2. `TestR4TwoWaitersShareSameRejection`：持所有权锁把胜者固定在 claim 内，
   两个等待者与胜者共享同一拒绝；被拒宿主不进入事件源（R3 边界保持）。
3. `TestR4CancelConcurrentWithRunIsLinearized`：Cancel(A) 与 Run 并发的两种
   交错都有明确结果（取消赢：A 无事件源、Canceled=1；Run 赢：Cancel 明确
   失败 `errWindowNotCancelable`），计数守恒、收口有界。
4. `TestR4ExitAllDuringStartingDeliversQueuedCloses`：胜者在 claim 内时
   `ExitAll(7)` 只排队；Run 成功启动后由各窗 loop 交付，Explicit/Code=7、
   两窗各恰一次关闭请求、有界收口。

## 变异取证

变异＝把 `hostStarting` 分支恢复为「提前 return nil」（R3 旧行为），可编译，
`r4-mutation-red` 预期 rc=1（与协调者探针同款首败）；按字节 SHA-256
`a41898f5…a93b` 恢复后 `r4-mutation-restored` 预期 rc=0。

## 真实退出码对照（OnMainConflict 独立候选切片）

在既有 R27 console 子进程范式上新增两个场景
（`TestExitHelperOwnershipConflictExitCodes`）：

- `conflict`：子进程先由宿主认领 Main，旧入口默认策略 → **真实 `os.Exit(1)`**
  退出码由父进程观察（observer 先于退出，步骤尾 `observer,exit`）。
- `conflict-return`：显式 `MainConflictReturn` → 入口**返回**，子进程正常
  结束 rc=0（marker `returned-without-exit`，步骤尾 `observer,return`，
  不含 `exit`）。

公开形态与回退：字段/类型/分支/白名单条目整体回滚，默认契约（R28）不变。

## 未验证

受控探针只证明生产调用顺序与真实子进程退出码；真实 Windows 双窗、
Linux/macOS 仍由协调者组织。
