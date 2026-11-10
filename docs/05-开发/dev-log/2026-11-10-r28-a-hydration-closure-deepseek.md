# 2026-11-10 R28-A Hydration 后续收口（generation 语义核对 + 决策包）开发日志

基线：组合验收分支 `codex/r28-combined-acceptance` @ `8f0f33f`（含 Hydration generation guard 候选）；主库 `255c392` 只读未动。
本线分支 `codex/r28-hydration-closure`，独立树 `r28-hydration-closure`（从 `8f0f33f` 起）。所有权：`internal/devx/hydrationchannel/`（仅测试增补）+ 本 dev-log + `delivery-closure/` 决策材料。**不改生产语义、不改默认关闭、不改 watch/DataGrid/Lifecycle 所有权文件**。
提交：`8f0f33f`（父）→ 本提交（generation 并发/迟到回归 + dev-log）。

## generation 语义独立核对（结论：实现正确，无需改生产代码）
- **Endpoint 守卫**（`endpoint.go:180`）：`gen != req.ServingGen → ErrMismatch`，不返回 payload。
- **Client 守卫**（`client.go:135`）：`resp.Gen != oldGen → ErrMismatch`，不接受 payload。
- 编号域单一（`watch` 单调代次：ServiceObserver.ServingGeneration → Watch.servingGeneration → Hook.BeforeSwap(oldGen) → adapter ClientFor → Client.Capture(ServingGen) → Endpoint CaptureFunc thisGen）。真实两进程 e2e 在守卫下 PASS。
- 既有测试已覆盖：replay、cancel 有界、busy/并发上限、错 session/token、owner-closed、capture-fail、stop/双 Stop、applier cancel-no-apply/no-state、端点&客户端 gen 不符（含 payload 缺失）与一致正控制、真两进程 e2e。

## 本轮新增回归（补覆盖缺口：并发 + 迟到响应）
- `TestEndpointConcurrentGenerationGuard`：24 并发混合正确(ServingGen=5)/错误(=4)代次；错误代全部 `ErrMismatch` 且**无 payload 泄漏**，正确代全部返回且 payload 正确——并发下守卫不串代。
- `TestClientLateWrongGenResponseRejected`：服务端延迟回错代(G=99)响应；客户端**有界**返回错误且 `got==nil`（不接受字节）。
- 断言只约束合同（无 payload / 有界），不过度约束底层错误类型（IO/超时/ErrMismatch 均可），不弱化产品承诺。

## 决策包
见 `delivery-closure/hydration-decision-package.md`：TCP 回环传输 / DTO schema / session-app 标识 / 状态目录 / 超时大小限制 / 失败策略 / 公开 API 与默认策略，逐项推荐+替代+兼容+安全边界+**需负责人裁决**清单；含最小接线方案与回滚方案。**候选未批准**。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0 64bit）
hydration/hydrationchannel/watch 三包 test 全绿；`-race` 三包 rc0 / 0 DATA RACE；`go build ./...`（CGO=0）rc0；vet rc0；gofmt 空；diagdoc/docgate ok；覆盖 hydration 86.0% / channel 87.6% / watch 88.4%。

## 已观察的既有偶发（非本线引入，未修）
`internal/devx/watch` 的 `TestReviewedCommandTimeoutIsHonored`（40ms 超时/800ms 预算的真实进程计时用例）在负载下偶发 FAIL，单独重跑与后续全量均 PASS。该用例属 watch 所有权、与本线改动无关（本线对 watch 零改动），未修改以免越界；登记为既有环境计时偶发。

## 未验证边界
原生窗口、跨平台运行（仅 Windows 运行）、整应用性能采样未做；TCP 传输/DTO 协议/API/默认策略最终裁决未决；生成编号域假设请求/owner 同域，跨命名空间须先映射。
