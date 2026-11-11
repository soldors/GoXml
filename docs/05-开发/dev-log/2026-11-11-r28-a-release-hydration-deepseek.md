# 2026-11-11 R28-A Hydration 错代合同与发布边界（release follow-up）开发日志

组合基线 `611948d49525744cbb4127265983fd24236e6bf5`（主库 `255c392` 只读未动）。分支 `codex/r28-release-hydration`，独立树 `r28-release-hydration`。所有权：`internal/devx/hydrationchannel/`、`internal/devx/hydration/`、`pkg/runtime/hydration/` + 本 dev-log；Watch/CLI/App/Router/共享文档只读。
已批准 ADR-0016 范围：回环 TCP、显式 DTO、默认关闭、错代拒绝且不返回 payload。
提交：`611948d`（父）→ 本提交（错代错误映射修复 + 真实回环合同回归 + 安全边界 + 微基准）+ dev-log。

## 1. 错代错误传播（缺陷修复，先红后绿）
- **根因**：Endpoint 对错代返回 `ErrMismatch` 错误响应；`client.go` 错误映射 switch 缺 `ErrMismatch` 分支 → 落到 `default: ErrCaptureFail`（`hydrationchannel: 应用侧捕获失败`）。
- **先红**：将协调者真实回环探针重定位为本树 `zz_coord_generation_probe_test.go`，冻结态复现同一红：`want ErrMismatch and no payload, got err=...应用侧捕获失败 payloadLen=0`（`raw/red/probe-red.txt`）。
- **最小修复**：`client.go` 错误映射补 `case ErrMismatch.Error(): return nil, ErrMismatch`（仅此一行，不改协议/API/默认）。
- **后绿**：`TestCoordinatorRealEndpointMismatchError` PASS（同代正控制通过；错代 ErrMismatch 且 payloadLen=0）。
- **真实回环合同回归**（`release_contract_test.go`，真实 Endpoint+Client，非伪端点/raw 客户端）：
  - `TestRealEndpointSameGenerationControl`：同代返回 payload；`TestRealEndpointWrongGenerationIsErrMismatchNoPayload`：错代 ErrMismatch 且无 payload；`TestRealEndpointCaptureFailureIsErrCaptureFail`：真实捕获失败仍 `ErrCaptureFail`（与错代区分）。
- **真实 Watch 两进程同代恢复不退化**：`TestWatchChannelTwoProcessRecovery` PASS（gen1→gen2 restored=true 精确值）；错代绝不提交/切换（错代在 `BeforeSwap` 前以 ErrMismatch 拒绝→保旧不 swap，见 release_contract + e2e）。
- **变异（只撤本次错误映射）**：`mut` overlay（移除 `case ErrMismatch`）→ `go test -c` **rc=0**（编译成功）→ 两个错代守卫 **rc=1 FAIL**（`raw/mut/mut-*-compile.txt` rc0、`raw/mut/mut-*-red.txt` rc1）。恢复（无 overlay）→ 全 PASS。

## 2. 安全与资源边界（有界自有回环，区分缺陷/风险/已防护）
- **已防护（红绿证据）**：`TestClientRejectsMalformedResponse`（畸形非 JSON 响应→错误无 payload）；`TestClientRejectsOversizeResponse`（>MaxResponseBytes 有界拒绝）；`TestEndpointSurvivesHalfOpenAndAbruptClose`（未授权半连接/异常关闭后端点仍正常服务）；`TestEndpointBindsLoopbackOnly`（Start 仅绑 127.0.0.1，不监听公网）。拨号/已连接取消有界：`TestCaptureCancelBounded`（既有）。状态文件所有权/失败保旧：`internal/devx/hydration` 既有 `TestCoordR27ForeignExistingFilePreserved`、`TestStoreOwnFileOverwriteAndUnparsableReject`（引用，未改）。
- **已登记风险（非本轮修复；需负责人裁决是否加界）**：
  - **nonce 防重放集合 `seen` 无上限增长**：`TestEndpointNonceStateGrowsRiskEvidence` 记录每次不同 nonce 请求增长 1，端点生命周期内无上限（内存随请求数增长）。加界属协议/预算变更 → 单列方案（建议加入有界 LRU/时间窗，容量与淘汰策略待裁决），本线不改。
  - **token 仅会话绑定非认证**：恒定时间比较、不落日志；不宣称抗本机恶意进程/鉴权（ADR-0016 已定）。
- **微基准入口**（`bench_test.go`，不替负责人定阈值、非整应用采样）：`BenchmarkChannelCaptureRTT`（回环一次捕获往返 ~492µs / 9163B / 83 allocs）；`BenchmarkDTOSize`（16/256/4096 ~547ns/4.1µs/61µs，5 allocs）；`BenchmarkStateWriteRead`（write ~10.7ms 含 fsync/54 allocs；read ~39µs/22 allocs）。原始见 `raw/bench.txt`。

## 3. 平台矩阵
- **Windows/amd64（真实运行）**：三包 test 全绿；真实 console 两进程 e2e PASS；MinGW-W64 16.1.0 64bit `-race` 三包 rc0/0 DATA RACE。
- **Linux/Darwin**：`GOOS=linux` 与 `GOOS=darwin` 对 `pkg/runtime/hydration`、`internal/devx/hydration`、`internal/devx/hydrationchannel` **build+vet rc0**（**仅交叉编译，非运行**）。**真实运行阻塞**：本机 WSL(Debian) 无 Go 工具链（`command -v go` 空、无 `/usr/local/go/bin`），按约束**不安装依赖**；Watch 进程切换在非 Windows 明确 **unsupported**（进程树/Job 为 Windows 专有，POSIX 未实现）。可复跑命令见 handoff。

## 门禁（Windows/amd64，CGO=0；race MinGW）
vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 hydration 86.0% / channel 89.4% / watch 88.4%。台账 `commands.jsonl` verify PASS。

## 未验证 / 需负责人裁决
原生窗口、跨平台**运行**（仅 Windows；Linux/Darwin 仅交叉编译，WSL 无 Go 阻塞）、整应用性能采样未做。nonce 集合加界策略、微基准阈值、token 安全假设边界、是否扩大协议/预算：待裁决。错代合同修复本身在 ADR-0016 已批范围内。
