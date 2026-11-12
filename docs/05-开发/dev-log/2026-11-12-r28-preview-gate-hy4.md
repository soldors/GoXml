# 2026-11-12 · R28 共享门禁：Preview Windows 终止诊断（HY4）

遵守 `coordinator/review-r2-2026-11-12/preview-gate-hy4.md` 与继承的
`coordinator/review-2026-11-12/r1-common.md`。**不返修 C2**（冻结 `bed1525`）。
仅操作本线自建 console helper；不开窗、不控制桌面、不安装、不执行用户业务、
不杀不归本线所有的 PID；不改主库/组合树，不合并、不推送。

| 项 | 值 |
|---|---|
| 组合基线 | `611948d49525744cbb4127265983fd24236e6bf5`（父 `b3e502c`） |
| 分支 / 独立树 | `codex/r28-preview-gate-diagnosis` / `C:/Users/admin/.codex/worktrees/r28-preview-gate-diagnosis/Goxml` |
| 代码提交 | `a41bc1d70609e38a7626a7848aac7f5f682b513d`（1 个文件：`internal/devx/preview/supervisor.go`） |
| 本提交 | dev-log 维护提交（按 docgate 自引用规则排除 HEAD）；冻结 readyRef 见交付 `handoff.md` |
| 交付 | `D:/Codes/Goxml-agent-rounds/2026-11-11/product-r28-release-followup/preview-gate-delivery/` |

## 根因（三类逐项区分）

1. **系统环境约束：排除**。`C:/Windows/System32/taskkill.exe` 与
   `C:/Windows/System32/tasklist.exe` 绝对路径存在；协调者 `taskkill /?` rc0
   只读保留。本线用自建 helper 的足量有界超时档（5000ms）2/2：
   `taskkill /PID <自家 root> /T /F` **rc=0**、输出为 GBK 本地化
   「成功: 已终止 PID …（属于 PID … 子进程）的进程。」、root/leaf 均退出、
   leaf marker 未出现（真正收口）。实测耗时 **685–711ms**。
2. **测试夹具启动/终止时序：排除**。两次实验在终止命令执行前，root/child PID
   均由绝对路径 `tasklist /FI "PID eq …" /FO CSV /NH` 独立证实存活，
   不存在 helper 提前退出或 PID 不存在。
3. **Windows 终止实现缺陷：证实（同合同内）**。`killProcessTree` 的
   `killCommandTimeout = 500ms` 小于 taskkill 真实耗时；
   `exec.CommandContext` 在命令返回前触发 deadline，把 taskkill **自身**杀掉：
   500ms 档 2/2 均在 **503–504ms** 返回 `exit status 1`、
   `CombinedOutput` 为空、ctx deadline exceeded。产品因此把真实可用的
   终止命令误报失败（错误链「终止命令失败（已观测，不视为成功）」），
   且自有进程树未被及时收口——**500ms 档 leaf 在 3s 后写出 marker**。
   注意 `aliveAfter=False` 只是进程随后自然结束，**不能**以外层
   record-command 的 `activeAfter=0` 替代产品自身的进程树收口证据。

协调者定向反例 `raw/preview-combo-control/c1/stdout.raw` 与 C2 全树首败、
主库对照全程只读；本线按同一精确选择器
`^(TestSupervisorLifecycle|TestR1HostRunnerCancelAndReentrancy)$`
复现 rc1（`pg-repro-combo`：三个失败子例、`exit status 1 ()`、无 SKIP、
无编译失败）。

## 单一改动与保护项

只改 `internal/devx/preview/supervisor.go` 的**一个常量**：
`killCommandTimeout 500ms → 5s`，并写明实测依据。5s 仍是**有界**预算，
不是等待/凑绿 sleep；不改错误包装、不改 `CombinedOutput`、不改 `/T`、
不改 `stopOnce`、不改 `reapBound`/`finalReapBound`、不改兜底 Kill。
「终止命令真正不可用」守卫保持不变（`disableTaskkill` 下启动失败仍立即
上报并保留在 `StopError()`），不按错误文本吞错、不全局扫杀、不加长 sleep。

## 证据链（命令台账 + 原始双通道）

- 红→绿：`pg-repro-combo` rc1 → `pg-fix-repro-green` rc0（同一选择器，
  两个测试均 `--- PASS`、无 taskkill 误报、无 SKIP）。
- 完整 preview 包：`pg-fix-preview-suite` **51 PASS / 0 FAIL / 0 SKIP**，
  含 `TestR1HostKillFailureIsObserved`（真实终止失败诊断）、
  `TestCoordHostKillFailureIsBounded`（有界）、
  `TestSupervisorLifecycle`（幂等/超时/进程树收口）、
  `TestR1HostRunnerCancelAndReentrancy`（取消/重入）。
- MinGW race：`pg-fix-preview-race` rc0、**0 DATA RACE**、51 PASS。
- vet / gofmt / diagdoc / docgate 全 rc0，docgate 0 FAIL。
- 单一改动变异取证：`pg-mut-timeout-compile` 编译 rc0 →
  `pg-mut-timeout-guard-red` 把常量改回 500ms 的**树外 overlay** 令
  `--- FAIL: TestSupervisorLifecycle` 命中（rc1、无编译失败、无 SKIP）→
  `pg-mut-timeout-restore-green` rc0；恢复判据为树内 `supervisor.go`
  字节 SHA-256 `9c68c37e…356` 不变。
- 自建 helper/driver 四档报告：`raw/gates/probe-*.json` 与汇总
  `probe-summary.json`；每个 PID 自报身份、存活/退出原始输出、
  taskkill argv/rc/输出/耗时、marker 判据均在案。
- 两次脚本层判定修正（字段名 `TimeoutMs`→`timeoutMs`、成功文本不能按
  ASCII `SUCCESS` 匹配）如实登记在 `summarize_probe.py`，**不重跑实验凑绿**。

## 未证边界 / 五态

- 本线只跑本线相关无窗口门禁；**未跑全树 `go test ./...`**，由协调者在
  最终候选上统一复跑。C2 冻结树、A1/B2/B3、主库与组合树均未改动。
- 未验证：真实原生窗口、桌面控制、跨平台（Linux/macOS 无 taskkill，
  走精确自有 PID Kill）、其它机器上的 taskkill 耗时分布、
  5s 预算在极端负载下是否仍足够；未提出新进程所有权策略或公共合同变更。
- 五态：实现完成（单一改动）/ 自测通过（仅限本线 preview 定向与包级
  无窗口门禁）/ 独立复核未验证 / 未合主库 / 未 push 且远端无法核对。
  冻结 ≠ 批准；文档更新完成 ≠ 验收收口完成。

到此停止，由协调者统一复跑完整门禁并裁定发布。
