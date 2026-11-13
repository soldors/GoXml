# R29-C R3：旧单窗进程入口的冲突收口（候选）

## 风险事实

R2 的 `RunSingleWindowProcess` 在启动事件循环前认领进程级 Main；Host 已占用时
拒绝路径走 `finishProcess` → 默认 `os.Exit(1)`。也就是说：**在宿主仍开窗服务
的进程里误用旧入口，会终止整个进程**。这是两个进程级入口混用的编程错误，
但「明确失败」与「意外终止」必须能区分。

## 候选（默认行为不变）

`ProcessOptions.OnMainConflict`：

| 取值 | 收口 | 兼容 |
|---|---|---|
| `MainConflictExitProcess`（零值） | Observer 后退出进程，code=1 | 与 R28 逐字一致 |
| `MainConflictReturn` | Observer 后直接返回：不退出、不关宿主窗口、不进 Main | 仅显式选择 |

包内步骤诊断如实区分：默认 `observer → exit`，候选 `observer → return`；
候选路径不写 `exit`，避免把「返回」冒充成「已退出」。

公开面：`process_api_test.go` 的 `TestProcessOptionsExportsProductionFieldsOnly`
原本只允许 `Theme/Lazy/Observer`。新字段走白名单登记（附候选/回滚说明），
未放宽其他校验。这是需要负责人裁决的公开 API 追加。

回滚点：删除 `MainConflictPolicy` 类型与常量、`ProcessOptions.OnMainConflict`、
`deliverProcessResult` 的 `return` 调用点，并把白名单恢复为三项。

## 证据（两套对照 + 健康对照）

- `TestR3LegacyProcessEntryConflictExitsByDefault`：Host 持窗时调用旧入口，
  exit 桩被调用一次、code=1、Observer 看到 `ErrGioMainAlreadyOwned`、
  步骤含 `observer/exit`、宿主窗口仍在服务 → 说明默认策略下真实进程会带着
  仍开着的宿主窗口退出。
- `TestR3LegacyProcessEntryConflictCanReturnWithoutKillingHost`：显式选
  `MainConflictReturn` 时不调用 exit、步骤含 `observer/return` 且不含 `exit`，
  之后宿主继续处理帧、关窗并完整收口。
- `TestR3LegacyProcessEntryHealthyControl`：无 Host 时旧入口仍正常退出 0。

被拒的旧入口在两种策略下都不触后端（Event/Perform 调用前后无增量）。

## 同宿主重复 Run

`gioMain.calls` 只统计被允许进入 Main 的次数；同宿主重复 Run 由宿主状态机在
claim 之前幂等拦截，`TestR3TwoHostsCompeteAndRepeatRunIsNotAnotherMainCall`
断言 `Run` 调用 3 次而 `calls == 1`、`blocked == 1`。诊断 `gioMainState()`
保持包内，不新增公开 API。

## 未验证

真实进程级退出（`os.Exit`）未在子进程里执行；本轮用注入的 exit 观察调用序。
原生双窗与各平台执行仍待协调者安排。
