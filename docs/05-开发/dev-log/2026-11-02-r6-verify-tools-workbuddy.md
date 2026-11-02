# R6-C VERIFY-TOOLS 开发日志（WorkBuddy）

- 工作树：`C:/Users/admin/.codex/worktrees/night-r6-workbuddy-20261102/Goxml`
- 分支：`codex/night-r6-workbuddy-20261102`
- 基线：`455da26f25e7ae5781e0e8d88bb64ea0f60c9b56`（`feat: integrate reviewed R5 padding, safe cache, cleanup and workbench`）
- 提交（本线）：`8ca4bd6` → `c770a24` → `f93ad63` → `1adf15c`
- 交付：`D:/Codes/Goxml-agent-rounds/2026-11-02/night-product-r6/workbuddy/`
- 本线**未 push、未动主仓、未改** `scripts/verify-examples.ps1` 与 `scripts/verify-examples-lock.py`

## 0. 复核快照对照（先核"是否同一版本"）

协调者快照 `coordinator/review/tools-checkpoint/snapshot.json` 的 4 个文件 SHA 与本次开工前的作者树
**逐一相同**（`68cd5936…` / `de08f9df…` / `0f4499f0…` / `2856e6a6…`），因此复核报告的 4 类缺口
在开工时**确实未关闭**，不是"已修但被看旧版本"。

## 1. 复核 4 条缺口：逐条处置

### 1.1 `run_capture.ok` 未消费全部收口事实（4 个毒化形状被放绿）

旧表达式为 `... and result['activeAfter'] in (0, None)`，外加完全没有 `closeErrors` / `contained` 项。
现改为 `compute_ok(result)` 逐字段、带类型判定（`verify_examples_proc.py`）：

| 字段 | 要求 | 旧行为 |
|---|---|---|
| `contained` | `is True` | 未消费 |
| `activeAfter` | `type(...) is int and == 0` | `in (0, None)`，None 与 False 一起放绿 |
| `activeKnown` | 不得 `is False` | 未消费 |
| `stillRunning` | `is False`（None 不算已退出） | `not stillRunning`，None 被放绿 |
| `closeErrors` | `== []` | 未消费 |
| `cleanupError` | `is None` | 已消费 |
| `timedOut` | `is False` | 未消费（本轮自查补） |
| `restrictedSafe` | 不得 `is False` | 未消费 |

`test-verify-examples-proc-consumer.py`（纯 mock，`nativeProcessesStarted: 0`）对 **18 种**形状
逐个单独毒化、其余保持健康，并保留旧公式 `old_ok()` 逐字复刻，断言
`ok.old-formula-reproduces-gaps` 覆盖 `close-errors-only / active-none / active-false / contained-false`
四条——与复核表格一一对应。

### 1.2 `auto` 在非 Windows 先启动 portable 再标 failClosed

现 `_resolve_containment()` 先解析再决定是否启动：`auto` / `win-job` 在能力不可用（非 Windows、
Job Object 缺失、取值未知）时返回 `refused`，`run_capture` **不发起任何 Popen/CreateProcess**。
`none` 只允许显式声明，并标 `restrictedSafe=False`，`compute_ok` 必定拒绝。

证据：`refuse.auto-popen-zero`（Popen 调用数 0）、`refuse.auto-not-started`（pid/rc 均 None）、
`refuse.auto-reason-surfaced`；变异 `C6b tree-unavailable-degrades` 把该分支改回"静默降级"后，
`refuse.auto-popen-zero` 立刻翻红。

### 1.3 Job 侧"未知当成功"

- `_active()` 查询失败返回 `None`；收口判断改为 `need_reap = timedOut or active is None or active > 0`，
  不再 `(active or 0)`。
- `WaitForSingleObject` 结果分类为 `object0 / timeout / failed / other-0x…`；`failed` 置 fail-closed。
- 归属失败分支分三路取证：`terminateOk` / `waitResult` / `exitQueryOk`，`verdict ∈
  {proven-exited, proven-alive, unknown}`；`stillRunning` 未知时为 `None`，**不得**写成已退出。
- `timeout<=0` / NaN / 非数值：`validate_timeout()` **启动前拒绝**，`CreateJobObjectW` 与
  `CreateProcessW` 调用数均为 0（旧实现把 0 变成 `0xFFFFFFFF`＝INFINITE）。

证据：`FakeK32` 驱动（不创建真实进程）— `fakek32.query-fail-active-unknown`、
`fakek32.wait-failed-classified`、`fakek32.timeout-{zero,negative,nan,str}-refused`、
`fakek32.assign-unknown-not-exited`、`fakek32.assign-alive-verdict`、`fakek32.exit-query-failed-source`。

### 1.4 见证进程清理未覆盖外层 finally

`selfcheck-verify-examples-proc.py` 的检查主体整体移入 `try`，见证进程在 `finally` 按自持对象
`terminate()` → `wait(10)` → 失败再 `kill()` 有界收口；异常写入 `report['exception']` 且
**不覆盖**已有断言结果。变异 `M9 witness-cleanup-on-exception` 在检查中途 `raise`，
`witnessCleanup.exitCode` 仍非空、整体 0.7–0.9 s 结束，`harness.completed` 翻红。

## 2. 自查新增的两处（复核未提但同源）

1. **超时不算基础设施 ok**：超时后 `rc=8` 是我们给的 `forced-on-timeout`，不是被测进程的真实退出码；
   旧判据会把它当成功记录，上层可能当成业务退出码。现要求 `timedOut is False`，并拆
   `timeout.record-trustworthy` 单独核对 `rcSource/activeAfter/cleanupError`。
2. **基础设施门收敛到单点**：原本内联在 10 处 `pass` 表达式里；现统一为
   `verify_examples_selcase.case_passed(row, business)`（两参数都要求 `is True`，缺字段不放行）。

附带一处真实行为修正：父进程退出瞬间 `ActiveProcesses` 会读到 1（Job 记账滞后），旧代码靠
"顺手 Terminate 一次再轮询"掩盖。现先有界等归零（`SETTLE_DEADLINE_SEC=2.0`），
实测 `ok-quick` 15 ms、`nonzero` 30 ms 归零且**不触发终止**，`detach` 才 2014 ms 后判为真有活口。

## 3. 片 3：两个选择器接入与实跑

接入方式：`run_case` 换成 `verify_examples_selcase.Runner`，W1-W5 / F1-F5 的**业务判据一字未改**，
只在末尾统一施加基础设施门。每 case 独占目录 `<log-dir>/<caseId>`，stdout/stderr 原始字节落盘 +
SHA-256，`outTail` 由 raw 解码展示。

| 运行 | 结果 | 关键事实 |
|---|---|---|
| W1-W5（默认 5.1 宿主） | 全 PASS，rc 0/0/2/7/2 | 每 case pid/rc/ok/raw 字节+SHA；锁 release ok、afterRelease absent |
| F1-F5（默认 5.1 宿主） | 全 PASS，rc 0/0/2/7/2 | 同上 |
| W1-W5（7.6.9 宿主，`--ps-exe`） | 全 PASS | 同一实现两个宿主均通 |
| `--inject-fault assign-fail` | 脚本 rc=1，5 case 全 `infraFalse` | raw 0 B（进程未 resume，属正确）；锁仍释放 |
| `--case-timeout 3` | 脚本 rc=1 | W1/W2/W4 `timedOut=True`、partial raw 214/237/404 B 保留；W3/W5 快速路径仍 PASS |

Go 侧：`go build ./...` rc=0（3.3 s，`CGO_ENABLED=0 GOPROXY=off GOFLAGS=-mod=readonly` 等六项固定）。
本轮**未运行**任何窗口入口 / 诊断 exe / 采样 / pprof / trace / install。

## 4. 自纠记录（未覆盖原始失败）

1. **故障注入静默失效**：CLI 传 `assign-fail`（连字符），`tree.run` 读 `assign_fail`（下划线），
   注入被吞 → attempt1 五个 case 全绿。已改 `replace('-','_')`；失败记录
   `raw/wb-fault-assign.out` 保留，成功记录在 `.attempt2`。
2. **重排脚本锚点撞车**：按 `    try:` 找锚点命中 `alive()` 里的 try，把文件中间整段吃掉；
   改用"`try:` 且下一行是 `_checks(`"的成对锚点。两次失败文件均已从备份恢复，未提交中间态。
3. **变异期望写反**：`gate.infra-truthy-not-true` 原本期望 `infraOk=1` 放行，实际严格判定应拒绝；
   已改名为 `gate.infra-int-one-rejected` 并修正期望（这是测试错，不是实现错）。
4. **变异清单滞后**：`M1`/`M4`/`M7` 的锚点与期望在重构后失效，逐条更新后才重跑；
   `M4` 现在期望 `timeout.record-trustworthy`（`timeout.ok` 已不存在）。
5. **变异脚本删除触发安全护栏**：`shutil.rmtree` 一次删 98 个文件被拦；改为时间戳目录、不删历史。

## 5. 五态（不自我宣布通过）

| 维度 | 状态 |
|---|---|
| 源码实现 | 已完成并本地提交（`8ca4bd6`…`1adf15c`） |
| 自测 | selfcheck 48 条真实进程断言 PASS；consumer mock 64 条 PASS；变异 9+9 条全红；W/F 两矩阵 PASS |
| 独立复核 | **未进行**（本文件全部为作者自测，不构成外部审核结论） |
| 主仓 | **未合入**（仍在 `codex/night-r6-workbuddy-20261102`） |
| 远端 | **未推送** |

未覆盖：真实非 Windows 平台、真实"归属失败/终止失败"的 OS 级故障（仅安全注入与 FakeK32）、
组合树（缓存 + Padding + 本轮工具改动）下的复验、`-Example all` 长跑矩阵的重复跑稳定性。

## 6. 证据索引

- `out/selfcheck-proc.attempt8.json`（48 checks / 0 failed）
- `out/consumer-mock.attempt6.json`（64 checks / 0 failed）
- `out/mutate-selfcheck-proc.attempt4.json`（9/9 红，源文件 SHA 不变）
- `out/mutate-consumer.attempt6.json`（9/9 红，源文件 SHA 不变）
- `out/workbench-selector-r6.final.json`、`out/forms-selector-r6.final.json`、`out/wb-ps7.json`
- `out/wb-fault-assign.attempt2.json`、`out/wb-fault-timeout.attempt2.json`
- `raw/` 下每个 case 的 `stdout.raw` / `stderr.raw` / `result.json`
