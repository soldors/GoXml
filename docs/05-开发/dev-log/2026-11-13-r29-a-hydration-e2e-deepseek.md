# 2026-11-13 R29-A 让 Watch 跨进程状态恢复可供示例应用使用 — 开发日志

共同起点 `ddef3a4d52588c3edebcb0dfa999c048addecd15`（R28 已验 `35b61b5` + 验收文档）。分支 `codex/r29-deepseek-hydration-e2e`，工作树 `r29-deepseek-hydration-e2e`（开工核对 HEAD 精确==起点、status/index 空）。主库 `255c392` 只读未动。
本线写范围：`internal/devx/hydration*`、Watch/Watch adapter 与 `internal/cli/watch*` 相关接线、`pkg/runtime/hydration`、专属示例/夹具、本 dev-log。App/Router/Lifecycle/DataGrid/公共进程所有权代码未改。
切片提交：`6b17aa8`（S1）→ `b1e2b5b`（S2）→ `e30d336`（S3）→ S4（测试，本提交）。

## S1 有界 nonce 防重放（`6b17aa8`）
- 复现缺陷：原 `seen map[string]struct{}` 随端点生命周期**无上限增长**。
- 修复：`replay.go` 有界 `replayGuard`（FIFO 顺序 + O(1) 判重）。两策略（`Endpoint.SetReplayBounds` 可配，默认 `ReplayEvictOldest`/容量 `DefaultReplayCapacity=4096`）：
  - `ReplayEvictOldest`：满则淘汰最旧 → 内存有界；**重放保护对已淘汰 nonce 弱化（过期）**——已文档化。
  - `ReplayRejectOnFull`：满则拒绝新 nonce（`ErrReplayWindowFull`）；保留窗口内重放仍严格拒绝。
  - 会话结束回收：随 Endpoint.Stop 释放（无全局状态）。
- 真实 TCP 测试（`replay_s1_test.go`）：累计 nonce 仍 ≤ 容量（有界）；重复→ErrReplay；两策略对同会话重放影响对比（淘汰后旧 nonce 可再用 vs 满额拒绝新请求但保留窗口严格）；错代 ErrMismatch 无 payload；错会话 ErrWrongSession；并发不同 nonce 有界；客户端取消有界；慢回调有界。测试不写 token/user 数据到输出。原 obsolete 风险用例 `TestEndpointNonceStateGrowsRiskEvidence` 替换为有界断言 `TestEndpointNonceWindowBounded`。

## S2 应用接入（`b1e2b5b`）
- `session.go`：可消费单一入口 `Session`——绑定 app/session/schema/token；旧代 `NewOldGenEndpoint`（owner 串行 Capture + portfile 发现，portfile 不含 token）；新代 `Restore`（读+校验+owner 应用，复用 Applier/Protocol）；`Close` 仅删本会话状态+portfile。runtime 不反向 import devx。
- `exampleapp/main.go`：自有有界 console 演示应用如何注册（旧代端点+owner 捕获）与恢复（新代），**只用既有 API**，不重写协议。
- `session_s2_test.go`：状态文件归属/清理隔离他会话；portfile 会话绑定且无 token；RestoreOnOwner 应用精确 DTO。

## S3 Windows CLI opt-in（`e30d336`）
- `internal/cli/watch_cmd.go`：新增 `--hydration`（默认关）+`--hydration-session/-app/-schema`。**无标志路径不变**（`cfg.Hydration=nil`、`pm.EnvFor` 未设）。
- 启用：`buildHydration` 构造 `Session`，随机 token（crypto/rand）**仅经子进程环境注入**（`HYD_GEN/HYD_MODE(serve/restore)/HYD_STATE/HYD_TOKEN`），**绝不出现在 argv 或诊断**；钩子 Capture 经 portfile 发现旧代；捕获失败/取消保旧；新代启动失败沿用既有次序（如实报告无服务，不自动复活旧代）。停旧/启新次序、Windows Job 所有权不变。
- `watch_hydration_s3_test.go`：默认关闭（nil hook）、opt-in 解析+配置来源默认/覆盖、token 不在 argv/诊断、gen>=2 注入 restore env。

## S4 全链验收（本提交）
- `s4_fullchain_test.go`（Windows，无原生窗口）：用 **S2 exampleapp** + 真实 `watch.New` 驱动两代应用进程——gen1 非默认 DTO→Watch opt-in 快照→停旧→gen2 **新 PID** 恢复**精确 DTO**（取自子 `APP_REPORT`，父不重读状态文件）。
- 对照：默认关闭（gen1 发布、子 restored=false）、构建失败保旧（gen2 失败 gen1 仍服务）、协议失败（坏状态 Restore 拒绝不部分应用）、Shutdown 清理（ActiveAfter=0）、代次错配（由错代守卫在 S1/S3 覆盖 + 本链 gen 绑定）。
- **两变异（compile→行为红→恢复绿）**：
  - MUT-1（exampleapp 跳过恢复，overlay `mut1-exampleapp.overlay.json`，经 `S4_EXAMPLE_OVERLAY` 传入嵌套 build）→ `TestS4FullChainTwoGenerations` **FAIL**（`restored:false`/`nonDefault:default`）——破坏真实恢复证据。
  - MUT-2（`replay.go` 忽略容量，overlay `mut2-replay.overlay.json`）→ `go test -c` rc0 → `TestReplayWindowBounded_NoUnboundedGrowth` **FAIL**（`size=200 > cap=16`）——破坏容量/重放守卫。
- 微基准（复用既有 `bench_test.go`：通道 RTT / DTO / 状态读写）与回退见交付。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
hydrationchannel+hydration+watch+cli 受影响测试全绿；`-race` hydrationchannel+hydration+watch rc0 / 0 DATA RACE；`go build ./...` rc0；vet rc0；gofmt 空；docgate/diagdoc ok；覆盖 hydrationchannel 86.3% / hydration 86.0%。

## 候选/未证边界
- 三策略/接口为**候选**（nonce 容量与满额策略、Session API、CLI flag 名与 env 契约），需负责人裁决。
- 全链为**程序化 `watch.New` + exampleapp** 真实两进程；真实 `goxml watch` 二进制对用户自定义应用的 CLI 端到端需用户应用实现 `hydrationchannel` 契约——**本线未构建用户应用的 CLI 原生运行**（无业务 app），记为未验。
- 原生窗口、跨平台运行、整应用性能采样未做。公共 CLI 接线/共享 ADR 由协调者处理。
