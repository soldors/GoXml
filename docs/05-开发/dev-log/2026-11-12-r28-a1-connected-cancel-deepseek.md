# 2026-11-12 R28-A1 Hydration 已连接取消（release r1）开发日志

冻结起点 `43516a550ed3aafc18addf88f7e160e5b34c003b`（父 SHA）。分支 `codex/r28-release-hydration-r1`，独立树 `r28-release-hydration-r1`。所有权：`internal/devx/hydrationchannel/` + 本 dev-log；Watch/Runtime/App/CLI/其他线只读。主库 `255c392` 只读未动。
提交：`43516a5`（父）→ 本提交（已连接取消修复 + 回归 + 变异 + valid-JSON 超限）→ dev-log。

## 1. 先立原红（同材料）
- 将协调者真实 TCP 探针 `capture_cancel_probe_test.go` 重定位为本树 `zz_coord_cancel_probe_test.go`（不编辑审查树）。
- 冻结态实测同一红：`canceled capture: want context.Canceled and no payload, got err=<nil> payloadLen=32`（`raw/red/connected-cancel-red.txt`，rc=1）。
- 根因：`client.go` `Capture` 拨号用 `net.Dialer.Dial`（非 ctx），连接后仅 `conn.SetDeadline`，读写不吃 ctx 取消；取消后仍读完响应并交出 payload（err=nil）。

## 2. 修复（贯穿拨号与连接后 I/O 的 context）
`internal/devx/hydrationchannel/client.go`：
- 拨号改 `net.Dialer.DialContext(ctx, ...)`（拨号期取消有界返回 ctx 错误，不遗留拨号 goroutine）。
- 连接后 I/O：watcher goroutine 监听 `ctx.Done()`→**有界关闭自有连接**使阻塞 Write/readUpTo 立即返回；正常完成经 `sync.Once` 回收 watcher（不泄漏）。
- I/O 错误若由取消导致，优先还原为 `ctx.Err()`（`errors.Is(context.Canceled/DeadlineExceeded)` 可判）。
- **交出 payload 前最后一次复查取消**：已取消则不交出 payload、返回 ctx 错误。
- **保留**：正常成功返回 payload；错代 `ErrMismatch` 无 payload；真实捕获失败 `ErrCaptureFail`；原 deadline 行为（`context.DeadlineExceeded`）。**不承诺强杀远端任意 owner 回调**（仅关闭自有客户端连接）。

## 3. 守卫与边界
- 两条取消回归（本树独立编写，真实回环）真实翻红→转绿：
  - `TestConnectedCancelNoResponseReturnsEarly`：对端**永不返回**，仅显式 cancel（握手同步、墙钟 2s 仅作上限、**不用短 deadline 代替**）→ 提前收口 `context.Canceled` 无 payload。
  - `TestConnectedCancelAfterReleaseRejectsPayload`：进入后先 cancel 再放行同代响应 → 不交出 payload。
  - 正控制 `TestConnectedCaptureNormalUnaffected`：未取消正常捕获仍返回 payload（不误杀）。
- **变异**（独立源码 overlay：用父 `43516a5` 的 client.go 回退取消处理）→ `go test -c` **rc=0**（编译成功）→ 三条取消守卫 **rc=1 FAIL**（`raw/mut/mut-revert-cancel.overlay.json`、`mut-compile.txt` rc0、`mut-red.txt` rc1, 3 FAIL）。恢复（无 overlay）→ 全 PASS。
- **真实 Watch 两进程恢复继续通过**：`TestWatchChannelTwoProcessRecovery` PASS；相关包 MinGW race rc0/0 DATA RACE（不退化）。
- **有效 JSON 超限**（不以畸形全零代替）：`TestClientRejectsOversizeValidJSONResponse`——服务端回**合法 JSON** captureResponse，payload 为合法 JSON 字符串大值使总字节 > MaxResponseBytes；客户端解析前按上限拒绝，确定错误类别 `ErrTooLarge`，无 payload。
- 未改：nonce 状态上限、token 认证、非回环策略、跨平台运行、协议、默认值、公开 API（仍单列/待裁决）。

## 已修 vs 仍是发布风险
- **已修（本轮）**：已连接（及拨号）取消时客户端交出 payload 的错误——取消后返回 `context.Canceled` 且无 payload，可 `errors.Is`。
- **仍是发布风险（未改，单列）**：nonce `seen` 防重放集合无上限增长；token 仅会话绑定非认证；仅回环（不跨机/无 TLS）；非 Windows Watch 进程切换 unsupported；跨平台真实运行未验证（本机 WSL 无 Go，安装禁止）。

## 门禁（Windows/amd64，CGO=0；race MinGW）
vet rc0；gofmt 空；`go build ./...` rc0；diagdoc/docgate ok；覆盖 hydration 86.0% / channel 89.0% / watch 88.4%；MinGW-W64 16.1.0 64bit `-race` 三包 rc0/0 DATA RACE。台账见 `r1-delivery-a/commands.jsonl`。

## 未验证边界
原生窗口、跨平台运行（仅 Windows 运行）、整应用性能采样未做。nonce 上限/token 认证/非回环/跨平台：待裁决。
