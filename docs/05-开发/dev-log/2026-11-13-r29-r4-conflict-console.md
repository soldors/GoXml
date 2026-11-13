# R29-C R4：OnMainConflict 的真实子进程退出码（独立候选切片）

复用 R27 的 console 子进程范式（`exit_helper_test.go`），新增两个子场景：

| 场景 | 子进程动作 | 父进程观察 |
|---|---|---|
| `conflict` | 宿主先认领进程级 Main（包内接缝，不启动窗口、不调用 Main），旧单窗入口默认策略收口 | **真实退出码 1**（`os.Exit` 结束子进程）；结果 Code=1、Err 含 `Gio Main is already owned`；步骤尾 `observer,exit`；marker 只含收口行 |
| `conflict-return` | 同上，但显式 `OnMainConflict: MainConflictReturn` | 入口**返回**（marker `returned-without-exit`），子进程正常结束 **rc=0**；步骤尾 `observer,return` 且不含 `exit` |

判定口径：`runExitHelper` 以 `exec.ExitError.ExitCode()` 观察**真实**退出码，
不注入 `ProcessOptions.exit`；父测试进程存活（不误杀调用方）。

## 公开形态与回退

- 公开形态：`ProcessOptions.OnMainConflict`（`MainConflictExitProcess` 零值＝
  R28 契约；`MainConflictReturn` 显式返回）。白名单登记在
  `process_api_test.go`。
- 回退方式：删除 `MainConflictPolicy` 类型与常量、`ProcessOptions.OnMainConflict`
  字段、`process_entry.go` 的 `MainConflictReturn` 分支与 `deliverProcessResult`
  的 `return` 尾步，恢复白名单三项；同时删除本切片两个子场景与
  `TestExitHelperOwnershipConflictExitCodes`。

## 边界

- 子进程认领用包内接缝 `claimGioMain("helper-host")`（等价于宿主已认领，
  不启动窗口、不调用 Main）；真实宿主 + 真实旧入口同进程共存的端到端形态
  属于组合验收范围。
- Windows 真双窗自然关窗与取消后关窗留统一组合验收（协调者组织）。
