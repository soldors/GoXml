# R29-C R3：宿主注册/启动顺序（S1 修复）

## 反例（先红）

协调者 `c-r2-rejected-host-probe_test.go` 原样重放，台账 `r3-replay-rejected-host-red`
observed rc=1：第二个宿主 `Open` + `Run` 被正确拒绝，但 `Open` 已经启动事件泵，
Gio 首次 `Event()` 就会创建平台窗口。

修复前字节基线：`pkg/runtime/app/window_host.go` 16795 B
（SHA-256 `225d8a2c056812c13903bd320e163a2e78f00bc0532bfae8661c3f7a020e6faf`）。

## 修复

- `Open` 只注册句柄（label 唯一、view/theme 非空），不创建事件泵、不读事件。
- `Run` 收敛为：宿主状态检查 → `claimGioMain` → 启动每窗事件泵 → 调用一次 Main。
  被拒宿主在第一步就返回：`reject()` 只标记句柄（`Rejected`、`Err`、`Done`），
  不启动 goroutine、不触后端、不遗留等待者。
- `ExitAll` 补齐 `hostStarting` 分支：并发 Run 正在做所有权判定时，只把每个
  已注册窗口的关闭请求排队，由 Run 启动的 loop 在首个事件就绪后交付；否则
  该窗口期的 `ExitAll` 会被忽略，`Wait()` 永久等待。
- `Cancel` 明确为 **Run 前**取消入口：不启动、不触后端，`Err = errWindowCanceled`。

## 测试

受控启动辅助 `r3StartHost`（`r3_test_helpers_test.go`）只把 `mainFunc` 换成
「立即返回」的桩并隔离包级所有权状态；旧受控用例（协调者 R1/R2 探针、S2 用例）
按「注册/启动分离」补一行显式启动，**断言未改**。

新增（`r3_host_startup_test.go`）：被拒宿主无 Event/无后端/无遗留句柄、旧入口
先行、两个 Host 竞争、重复 Run 不计入 Main 次数、Run 前取消、零窗、并发
Open/Run/ExitAll（有界收口、计数守恒、无重复关闭）。

## 变异取证

首个变异「把 claim 移到启动事件泵之后」**未命中**（rc=0，如实保留在
`r3-mutation-red`）：泵在读取事件前先看到 `done` 关闭，读到事件的机会被掩盖。

改用组合变异（启动先于认领 + 泵启动即无条件读一次事件），`r3-mutation-red-2`
rc=1，逐字复现缺口：

```
coord_r3_rejected_host_probe_test.go:57: rejected second host entered its window Event source while first host owns Main
```

按字节 SHA-256 恢复为 `225d8a2c…6faf`（`MATCH True`），`r3-mutation-restored` rc=0。

## 未验证

受控探针只证明生产宿主的调用顺序，不证明真实原生窗口的创建时机；真实 Windows
双窗执行仍待协调者组织。
