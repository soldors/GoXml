# R29-C R5 · 宿主启动与 Wait 终态（HY4，2026-11-13）

> 分支 `codex/r29-r5-hy4-window-host`，基线 `6f3253548724615047479e9f60f81ec6f8786bc7`。
> 交付：`coordinator-r5/delivery-c/`。原生双窗执行由协调者组织。

## 首败（原样重放）

协调者 `c-r4-zero-window-verdict_test.go` 原样复制入 `pkg/runtime/app/`（字节
一致），精确选择器 rc=1：`early.Rejected=false final.Rejected=true`。另一宿主
持有 Main，candidate 零窗 Run 卡在 claim；ExitAll 先关闭 finished 使 Wait 读到
未判定的 result；释放锁后 reject 改写 h.result.Rejected=true，第二次 Wait 变成
另一终态。与 R4 组合验收 C 线首败逐字一致。

## 修复（生产代码 window_host.go，公开面不变）

1. **不可变终态**：`signalFinished` 在 `close(h.finished)` 前的同一 h.mu 临界区
   冻结 `h.terminal = h.result`（Errors 深拷贝）；`Wait` 只返回冻结值。收口后
   reject / onWindowClosed 对 h.result 的后续写入只服务 Run 与句柄，不改写终态。
2. **启动结果脱钩**：`publishRunVerdict(err)` 在关闭 `runVerdict` 的同一临界区
   写 `runErr` 快照；hostStarting 等待者唤醒后直接返回 `runErr`，不再按当前
   `h.state`（Drained/Exiting）推断。发布点仍先于 `gioMain.once.Do(mainFunc)`，
   赢家不等 Main 返回；R4「同宿主并发 Run 同一结果」「拒绝可判别」守卫保留。
3. 未改 `OnMainConflict` 候选形态、R28 默认行为、`ProcessOptions` 公开字段；
   `TestProcessOptionsExportsProductionFieldsOnly` 通过。

## 回归（r5_wait_terminal_test.go）

- 四路径表驱动：claim 成功/拒绝 × 零窗/有窗；两次 Wait 逐字段一致；
  冻结后改写 h.result 仍返回同一终态；被拒宿主 0 次 Event/后端调用、
  句柄 Rejected/Done、`Active()==0`；成功有窗路径生产 loop 收口 Closed=1。
- 取消状态：Cancel + ExitAll 的 Canceled=1/Explicit/Code/Drained 保留。
- 并发等待者脱钩：exit-all 与 fast-destroy 各 8 次，胜者与 4 个等待者结果
  全一致；树外 overlay 变异（恢复按状态推断）令 exit-all 8/8 转红
  （`errHostNotAcceptingRun`），证明断言能变红。

## 门禁与证据

全部正式命令经 `record-command.py run --containment win-job --lock-mode acquire`
串行持公共锁，原始双通道/argv/env/rc/planned/result/verdict 落 `raw/cmdlogs/`：

- 红→绿：`r5-c-red-replay` rc=1 → `r5-c-targeted-green` rc=0
  （51 PASS / 0 FAIL / 0 SKIP）；变异红 `r5-c-mutation-red` rc=1。
- App/Router/Lifecycle + 双窗 fixture 受控回归 rc=0；app 全包 -v
  148 PASS / 0 FAIL / 1 既有 SKIP（`TestNightM1CollectSample` 显式 opt-in
  采集器，非本轮引入、非冻结兜底）。
- MinGW race（CGO=1、MinGW gcc、GORACE=halt_on_error=1）0 DATA RACE；
  CGO=0 默认 build、vet、gofmt（无输出）、docgate、diagdoc rc=0。
- 双窗普通/race 二进制在冻结 readyRef 上重绑并逐字节复核 SHA-256，
  见 `raw/binary-binding.json`；只编译不运行。
- 全树：首份 `-p 1` 被工具层 SIGTERM 中断且 `internal/cli` 出现已知
  Watch CLI 共享冲突，已用 `recover` 如实登记 incomplete 并释放自有锁
  token；另以 `-p 2` 新 id 重跑 `go test -p 2 -count=1 -timeout 25m ./...`
  **rc=0、849.794s、24 包通过 / 4 包无测试 / 0 FAIL**，contained=true、
  activeAfter=0、未超时、锁已释放。

## 未证边界

- 原生双窗实机执行、真实 DestroyEvent、跨平台运行仍未验证；本轮全部为
  无窗口受控证据与编译身份重绑。
- 冻结终态语义为「finished 关闭瞬间的结果」，之后启动判定仍可供 Run 观察；
  若未来要求 Wait 反映启动判定，属合同变更需协调者裁决。
