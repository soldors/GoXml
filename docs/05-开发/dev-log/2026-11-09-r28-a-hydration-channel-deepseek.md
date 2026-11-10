# 2026-11-09 R28-A HYDRATION-APPLICATION-CHANNEL 开发日志

固定 base `255c39251127c39d1a684baa881767a5fb7677b9`（开工核验：主库 HEAD 同、porcelain 0、索引空）。分支 `codex/r28-deepseek-hydration-channel`，树 `r28-deepseek-hydration-channel`。所有权：`pkg/runtime/hydration/`、`internal/devx/hydration/`、新 `internal/devx/hydrationchannel/`、本 dev-log；Watch/App/Router/CLI/进程助手只读（仅引用导出类型接线）。
提交：`255c392`（父）→ `e578cc2`（S1）→ `a81b72b`（S2）→ S3（本片）。

## S1 通道选择/端点/owner 边界（`e578cc2`）
- **传输选择**：**仅回环 127.0.0.1:0 的显式 TCP 端点**（标准库 `net`，无安装/新依赖、Windows 可用、可交叉编译）。备选（会话目录邮箱/匿名管道/命名管道）或平台特定或需 fd 传递，非选。
- `Endpoint`（旧应用进程）：仅暴露 **Capture**（无 apply/提交能力），经注入 `OwnerRunner` 在 **owner 串行点**调用应用 `CaptureFunc`；token 恒定时间比较（不写日志）、session/app/schema 绑定、reqID/nonce 关联 + 重放拒绝、请求/响应大小上限、IO deadline、在途上限 ErrBusy、owner 关闭/取消有界。
- `Client`（Watch/worker 侧）：拨号→发请求→取不可变字节；错误关联（reqID/session）、超时/取消有界。
- `Applier`（新应用进程）：经 `devx/hydration.Protocol` 读取+校验，再经 `OwnerRunner` 在 owner 点 `ApplyFunc`；校验失败/取消/无状态不 Apply。
- S1 测试：roundtrip、owner 运行证明、错 session/token、重放、owner-closed、capture-fail、cancel 有界、Stop 无遗留、applier apply/cancel-no-apply/no-state + 错误路径/过大/busy/版本/readUpTo 覆盖。

## S2 真实 Watch→旧应用→新应用整链（`a81b72b`）
- `adapter.NewWatchHook`：把 `ClientFor(oldGen)` 的 `Client` 接到现有 `watch.HydrationHook.Capture`；真实 `watch.New`+NotifyChange/Tick 进入 runGeneration→BeforeSwap→stop-old/start-new。
- `testdata/consoleapp`：旧代持非默认 DTO + 单一 owner 循环，用 `Endpoint` 回环暴露快照并把 addr 写 portfile；新代经 `Applier` 在 owner 恢复并自报 pid/session/gen/nonce/精确 DTO/restored；随后启动本代端点。
- E2E `TestWatchChannelTwoProcessRecovery`：gen1 非默认 DTO → 通道捕获 → gen2 新 PID 恢复精确值；父端核验**子报告**（不重读状态文件）。实测 gen1 pid 非默认、gen2 新 pid restored=true 精确 count=7/items=[a b c]/nonDefault=OLD-NON-DEFAULT。

## S3 特异性/成本/门禁（本片）
- **同一健康 E2E 两种可编译生产退化真实 FAIL**：OVL-A（adapter 绕过真实 Client/端点返回常量 → 新代恢复 BYPASS 错值）；OVL-B（consoleapp 不恢复 → restored=false default）。raw/mutations-ovl{A,B}-*.txt。
- **定向行为守卫**：MUT 移除 token 校验 → `TestWrongTokenRejected` FAIL（证明 token 守卫具判别力）；S1 覆盖错 session/重放/cancel-no-apply 等。
- **race**：`-race` hydrationchannel+hydration+watch+runtime 全 rc0/0 DATA RACE（含 owner 队列/取消/并发 capture/Stop/双 Stop）。
- **成本**：见 closure-matrix 单列（端点每请求一次本机环回 RTT；单 DTO 成本=捕获+JSON；未启用路径无监听/无新线程/I/O）。
- **门禁**：受影响包 test/vet/build/gofmt/docgate/diagdoc 全绿；`-race` 干净；`go build ./...` rc0；**GOOS=linux 包编译 rc0**（交叉编译非运行）；覆盖 hydrationchannel 87.3% / devx 86.0% / watch 88.4% / runtime 88.7%（均≥85%）。

## 变异/退化（独立副本 + Go overlay，健康→编译→行为红→字节恢复→守卫绿）
- OVL-A bypass（adapter 源）→ E2E FAIL；OVL-B restore-off（consoleapp 源，嵌套 build 经 `HYD_CONSOLE_OVERLAY`）→ 同 E2E FAIL；MUT-token（endpoint 源）→ wrong-token 守卫 FAIL；恢复全 PASS。

## 未证边界
无原生窗口/桌面控制/安装/采样/业务 main；无跨平台运行（仅 GOOS=linux 交叉编译）；协议/API/默认值为候选；不反射任意 VM；不宣称回滚任意用户回调；helper 仅自有有界 console、不开窗。
