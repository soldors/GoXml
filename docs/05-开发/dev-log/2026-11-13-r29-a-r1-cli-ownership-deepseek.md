# 2026-11-13 R29-A R1 真实 Watch CLI 与会话所有权 — 开发日志

返修父 `e96d32aa0946cf2923aa314b51ddcdf66b5b20e1`。分支 `codex/r29-r1-deepseek-hydration`，独立树 `r29-r1-deepseek-hydration`（开工核对 HEAD==父、clean、index 空）。原冻结树与主库 `255c392` 只读。写范围沿用大任务：`internal/devx/hydration*`、Watch/Watch adapter 与 `internal/cli/watch*` 接线、`pkg/runtime/hydration`、新增**公开入口** `pkg/hydrationapp`、专属示例、本 dev-log。App/Router/Lifecycle/DataGrid/公共进程所有权未改。

## 同材料复现红
- **CLI 红**（`raw/red-cli`，coordinater `a-cli-probe-r2.py`）：候选 `goxml watch --hydration` 驱动外部 module 应用，gen1 实际启动，改 GXML 后 gen2 报 `无可用旧代端点（gen=1）`，保旧不切换；Ctrl+Break 后退出 0、树已收口。
- **会话所有权红**（`raw/red-ownership`，coordinater `a-probe_test.go`）：Session 从未写、同名现存外来文件，Close 后文件消失（`no such file`）。

## 根因与最小修复
1. **环境协议不一致**：CLI 注入 `HYD_*`，示例只读 `APP_*` → 应用不按 CLI 会话建端点 → portfile 不符 → gen2 发现失败。
   修复：`exampleapp` 统一读 `HYD_*`（回落 `APP_*`，`envOr2`），与 CLI 会话/代次/状态/令牌对齐。
2. **每代缺“下一次捕获能力”**：原示例仅 gen1 起端点，gen2/3 无端点供下一代发现。
   修复：**每一发布代**在（可选）恢复后都起端点（servingGen=本代），gen1→gen2→gen3 连续可捕获。
3. **外部 module 不能 import `internal/`**（Go 规则）：公共接入无法用 `internal/devx/hydrationchannel`。
   修复：新增**公开入口** `pkg/hydrationapp`（类型别名 + 函数转发，单向依赖 internal，不复制协议）；示例改为 import 公开包。外部普通 module `example.com/extconsumer/app` 经 `replace` 接入验证（**非项目前缀**）。
4. **会话所有权**：Close 无条件删除同名状态文件 → 外来字节丢失。
   修复：`Session.Close` **删除前证明归属**——状态文件须能被本会话 Expect(schema/app/session) 完整校验通过才删；否则保留**原字节与 mtime**。portfile 须内容会话匹配才删。错误路径不改动文件。
5. **防重放 fail-closed 默认**：CLI 默认 `--hydration-replay=reject`（满额拒绝，fail-closed），可选 `evict`；容量 `--hydration-replay-cap`。窗口淘汰取舍与“持续重建安全开新会话”登记于 handoff（建议：长时多次重建时按预期 nonce 上限设容量，或每次重建轮换会话）。

## 绿证（真实 CLI 三代，`raw/green-3gen`）
自写探针 `raw/r29r1-cli-3gen.py`：真实候选 `goxml.exe` + **外部 module** `example.com/extconsumer/app`（`replace` 指向本树）+ `HYD_SEED=7`，逐代改 GXML 驱动 gen1→gen2→gen3：
- **R29R1_3GEN_PASS**：三代 PID 全不同（265764→268584→267924）；每代精确 DTO `count=7 nonDefault=seed-7`；gen1 restored=false、gen2/3 restored=true；真实构建产物 `watch-bin-gen1/2/3.exe` 均出现。
- 默认关/取消/清理：既有 `TestWatchParseNoHydrationDefault`、`TestR1CancelRestoreSafeClose`、probe Ctrl+Break 退出 0 + 记录器 activeAfter=0。

## 回归测试（无窗口）
- `session_ownership_r1_test.go`：外来字节+mtime 保留、拥有态删除、两个并发同名会话、portfile 归属、取消后安全 Close。coordinater `a-probe_test.go` 复放亦 PASS。
- `watch_hydration_s3_test.go`：`TestR1HydrationReplayDefaultFailClosed`（默认 reject=fail-closed）等。

## 可判别变异（compile→行为红→恢复绿）
- **MUT-ownership**（Close 无条件删，overlay `mut-ownership.overlay.json`）：`go test -c` rc0 → `TestR1ClosePreservesForeignBytesAndMtime` **FAIL**（外来字节被删）。
- **MUT-unbounded**（replay 忽略容量，overlay `mut2-replay.overlay.json`，见 R29-A 原始）：→ `TestReplayWindowBounded_NoUnboundedGrowth` FAIL。
（CLI 环境协议红→绿由原始 `raw/red-cli` 与 `raw/green-3gen` 对照证明。）

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0）
hydrationchannel+hydration+watch+cli+pkg/hydrationapp 全绿；`-race` hydrationchannel+hydration+watch rc0 / 0 DATA RACE；`go build ./...` rc0；vet rc0；gofmt 空；docgate/diagdoc ok。

## 候选/未证边界
- **公开入口形态**（`pkg/hydrationapp` 包名与导出面）、CLI flag 名与 `HYD_*` env 契约、`--hydration-replay` 语义、nonce 默认容量：**候选，待负责人裁决**。
- 原生窗口、跨平台运行、整应用性能采样未做；并发同名会话“双方均合法状态”时删除属同会话幂等（已登记）。

## 提交
- `711c102` fix(hydration): 环境收敛 + 每代捕获 + 公开入口 + 会话所有权（含回归测试与变异）
- dev-log 提交（本文件）
