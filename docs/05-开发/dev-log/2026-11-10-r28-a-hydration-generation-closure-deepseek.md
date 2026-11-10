# 2026-11-10 R28-A HYDRATION-GENERATION-CLOSURE 开发日志

主库基线 `255c39251127c39d1a684baa881767a5fb7677b9`（开工核验 HEAD 同、porcelain 0、索引空）。本线冻结参考：作者 `d82c32f`（通道引入）、协调者修复 `abe7edd`、复核结果参考 `e3b5942`。
分支 `codex/r28-deepseek-hydration-generation-closure`，独立树 `r28-deepseek-hydration-generation-closure`（从 `d82c32f` 起，链自 `255c392`）。所有权：`internal/devx/hydrationchannel/` + 本 dev-log。
提交：`d82c32f`（父）→ `551534e`（两道代次守卫 + 守卫测试）→ 本 dev-log。

## 目标：独立验证并冻结 Hydration 通道的 generation 关联合同
- **一致性核对（item 2）**：Watch `ServiceObserver.ServingGeneration()`（`ProcessManager.Current().Gen`）→ `watch.servingGeneration()` → `HydrationHook.BeforeSwap(ctx, gen, oldGen)` → `adapter.ClientFor(oldGen)` → `Client.Capture(oldGen)` 发 `ServingGen=oldGen` → Endpoint `CaptureFunc` 返回 owner 的 `thisGen`。**全部同一 `watch` 单调代次编号域**（`GenerationResult.Gen` / `Current().Gen` / console `HYD_GEN`），无第二套计数器。真实两进程 e2e `TestWatchChannelTwoProcessRecovery` 在守卫存在下仍 PASS（gen1→gen2 恢复），证明端到端同域。

## 两道独立守卫（合同）
- **Endpoint（`endpoint.go`）**：owner 实际捕获代次 `gen` 必须等于 `req.ServingGen`；不等即 `ErrMismatch`，**绝不返回 payload**。
- **Client（`client.go`）**：成功响应 `resp.Gen` 必须等于请求 `oldGen`；不等即 `ErrMismatch`，不接受 payload（双保险：端点被绕过时客户端仍拒错代字节）。

## 验证
- 守卫测试（独立编写）：`TestEndpointRejectsServingGenerationMismatch`（端点不符→ErrMismatch 且 payload 缺失）、`TestEndpointMatchingGenerationReturnsPayload`（正控制一致才返回）、`TestClientRejectsResponseGenerationMismatch`（客户端不符→ErrMismatch 不接 payload）。
- **两条 Go overlay 变异**（独立副本）：`mut-endpoint`（移除端点守卫）、`mut-client`（移除客户端守卫）。各自 `go test -c` **rc=0**（编译成功），行为测试 **rc=1**（真红）；恢复（无 overlay）→ 全 PASS。
- 门禁：hydration/hydrationchannel/watch 全绿；MinGW-w64 16.1.0 64bit `-race` rc0/0 DATA RACE；`go build ./...`（CGO=0）rc0；vet rc0；gofmt 空；diagdoc/docgate ok；覆盖 hydration 86.0% / channel 87.6% / watch 88.4%。台账见交付。

## 未验证 / 未决
无原生窗口、无跨平台运行（仅 Windows 运行 + 常规 CGO=0 build）、无整应用性能采样。**TCP 传输 / DTO 协议 / Watch 接线 / 公共 API / 默认策略最终产品裁决未决**——本线只关闭「generation 关联合同」这一隔离缺陷，不批准传输/协议/API。生成编号域假设请求方与 owner 回调同域；跨命名空间集成须先映射到共享标识再调用。
