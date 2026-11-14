# 2026-11-14 R29-A 最终发布合同与兼容验收 — 开发日志（DeepSeek）

基线 `90a0ac750af63886191ded2bfb56c9a3fc6ca289`（R29 R5 组合受验，含协调者 `1e52179`）。分支 `codex/r29-closeout-deepseek-hydration`，独立树 `r29-closeout-deepseek-hydration`（开工 HEAD==基线、clean）。主库 `d20714d`、组合树、R5 原件只读。写范围：Hydration、Watch CLI/adapter 与必要回归、本 dev-log；发布文档放共享交付目录（不在树内）。

## S1 公开接入与 Windows 合同（事实核对，见交付 hydration-contract-draft.md / decision-matrix.md）
- `pkg/hydrationapp` 实导出面：`Session/SessionConfig/OwnerRunner/CaptureFunc/ApplyFunc/ReplayPolicy` 类型别名 + `NewSession`、`SchemaString` + 常量 `ReplayEvictOldest/ReplayRejectOnFull/DefaultReplayCapacity(4096)/DefaultCaptureTimeout(5s)`。外部 module **不 import internal**，经此前缀接入。
- CLI flag：`--hydration`（默认关）、`--hydration-session`(watch)、`--hydration-app`(取 entry)、`--hydration-schema`(watch.v1)、`--hydration-replay`(reject 默认)、`--hydration-replay-cap`(0=默认容量)。
- 子进程 env（token 仅此处、不入 argv/诊断/发现文件）：`HYD_PROJECT/SESSION/APP/SCHEMA/TOKEN/GEN`、`HYD_MODE=serve|restore`、`HYD_STATE`(gen≥2)、`HYD_REPLAY_POLICY/CAP`。
- 状态路径：`<project>/.goxml/hydration/<session>.<runID>.state`（runID=sha256(token)[:8]；逻辑 SessionID 不变）；发现 `<session>.<runID>.port.json`；旧共享 `<session>.state`/`<session>.port.json` 只读回退、不删除。

## S2 真实 CLI 发布证据
- **真实 CLI 双 Watch（本树重建，同一构建输入）**：`raw/two-watch`：`independent=True`（`run/e33fddf5…` vs `run/f0abf72d…`），`independent restore: first=True, second=True`，rc=0。与组合 `coordinator-r5-integration/raw/a-two-watch` 对账一致（DTO 7/19 各自恢复）。
- **受控对照（区分共享生成串行化 vs 仅输出隔离）**：新增 `internal/devx/watchadapter/closeout_contract_test.go`：
  - `TestCloseoutUnlockedGenerateOverlaps`：无锁并发 generate 临界区可重叠（max>1）——证明仅给各运行不同输出目录不足以防共享 `.goxml` 生成写入交错。
  - `TestCloseoutLockedGenerateSerializesDefaultPath`：`withGenerateLock` 对同一项目并发 generate 串行化（max==1），且**默认（非 Hydration）路径**创建 `.goxml/generate.lock`。
- **锁覆盖核实**：`CLIGenerate` 与 `generateWithTool` 均包裹 `withGenerateLock`（`adapter.go` L121/L132），故**有/无 Hydration 的生成路径都被锁覆盖**。
- **默认关闭目录/文件影响（合同）**：不启用 `--hydration` 时仍产出 `.goxml/watch-bin-genN.exe`（R28）**并新增 `.goxml/generate.lock`**（generate 串行锁）；**不**创建 `run/<runID>/` 目录、不创建状态/发现文件。启用 Hydration 才加 `run/<runID>/` 专属产物与状态/发现文件。
- 端点 replay 三态（reject/evict/小容量）、取消、失败保旧、瞬时扫描冲突保旧、真实错误终止：沿用 R4/R5 已验守卫（本轮回归全绿）。

## 本轮验证（Windows/amd64，CGO=0；race MinGW-W64 16.1.0）
受影响包 hydrationchannel/hydration/watch/watchadapter/cli 全绿；`-race` rc0 / 0 DATA RACE（见交付 raw/race）；build/vet/gofmt/docgate/diagdoc ok。

## 结论与边界
- 无新增可复现且影响本轮合同的缺陷需修（对照测试为**新增合同证明**，非缺陷修复）。
- 发布统一口径见交付 status.json / handoff.md：**WAIT_NATIVE**（原生三场景由协调者排程，串行各留 run ID；本轮未执行原生窗口）。
- 未验证：原生 DataGrid/WindowHost、IME/DPI、跨平台运行、整应用性能采样。协议/公开 API/策略仍为候选。

## 提交
- 源码：新增 `closeout_contract_test.go`（合同对照测试）。本 dev-log。
