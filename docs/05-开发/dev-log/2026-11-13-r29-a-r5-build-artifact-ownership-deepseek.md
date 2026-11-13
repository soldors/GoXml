# 2026-11-13 R29-A R5 同项目 Watch 构建产物所有权 — 开发日志

返修父 `f40c7c772af821efd8de2f83ce033bb5c6749be4`。分支 `codex/r29-r5-deepseek-hydration`，独立树 `r29-r5-deepseek-hydration`（开工 HEAD==父、clean）。R28 主库 `d20714d`、组合 `2870ae9`、旧证据只读。写范围：Hydration、相关 Watch CLI 与必要受控 fixture、`internal/devx/watch`/`watchadapter` 构建产物路径与 generate 串行化、本 dev-log。

## 1. 原样复现红（`coordinator-r5/a-two-watch.py`，`raw/red-two-watch`）
同项目、同入口、同会话名两 Watch：外部 module `example.com/r29/watchtwo`，seed 7 / 19 不同 DTO。
- 红：两者 gen1 二进制同路径 `<project>/.goxml/watch-bin-gen1.exe`（`independent=False`）；第二 Watch gen2 恢复失败（`second=False`），probe rc=1。
- 根因：`RealBuilder.BinaryPath(gen)` 默认固定 `<ProjectDir>/.goxml/watch-bin-genN.exe`，两运行同代互相覆盖；且共享 generate 事务（`.goxml` manifest/cache/资产）并发无串行化。

## 2. 最小安全候选（保留并行；可证明所有权）
- **每运行专属二进制**：`RealBuilder.OutDir`（新）。非空时 `BinaryPath(gen)=<OutDir>/watch-bin-genN.exe`。CLI 在启用 Hydration 时设 `b.OutDir=<root>/.goxml/run/<runID>/`（runID=sha256(token)[:8]，与 session 一致）。不同运行 runID 不同 => 同代二进制互不覆盖，运行进程持有的二进制不被他人替换；同运行内不同代次仍各自文件。**默认（无 OutDir，及无 Hydration 路径）保持 R28 `.goxml/watch-bin-genN.exe` 兼容。**
- **共享 generate 事务串行化**：`watchadapter.withGenerateLock`（新）用 Windows `LockFileEx` 对 `<project>/.goxml/generate.lock` 取**跨进程独占锁**串行化 `generate --check`（manifest/cache/资产写）。进程异常退出由 OS 自动释放（无需清理陈旧锁）；获取受 ctx 有界（取消即返回）。`generateWithTool` 与 go-run fallback 均包裹。非 Windows 为 passthrough（Windows 首发范围）。
- 结果：两真实 Watch 同时完成重建（gen1→gen2）并**各自恢复原 DTO**（seed 7 与 19，restored=true），二进制独立。

## 3. 绿证（`raw/green-two-watch`）
`a-two-watch.py` 同材料复跑：
- `gen1 binary paths: first=…/run/38079e2ce1d1cc23/watch-bin-gen1.exe, second=…/run/96075046a28cff11/watch-bin-gen1.exe, independent=True`
- `independent restore: first=True, second=True`；probe rc=0。
两运行各自 `APP_EXE` 指向各自 `run/<runID>/` 下的 genN 二进制；gen2 各自恢复自身 DTO。

## 4. 回归/补充测试（无窗口）
- `internal/devx/watch/realbuilder_r5_test.go`：默认路径 R28 兼容；OutDir 每运行隔离（不同 run 二进制不同；同运行不同代不同）。
- `internal/devx/watchadapter/generate_lock_r5_test.go`（Windows）：同项目 6 并发 `withGenerateLock` 观测 maxConcurrent==1（串行化）；持锁时取消有界返回（不进入临界区）。
- R4 回归全绿：三代恢复、真实端点小容量 reject/evict（`zz_coord_r29r4_cli_capacity_test.go`）、瞬时扫描冲突保旧及真实错误终止（`scan_transient_r4_test.go`）。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0）
hydrationchannel/hydration/watch/watchadapter/cli 全绿；`-race` hydrationchannel+hydration+watch rc0 / 0 DATA RACE；`go build ./...` rc0；vet rc0；gofmt 空；docgate/diagdoc ok。

## 候选兼容影响（负责人未批准最终策略）
- 采用**并行**可组合候选：运行身份（runID=token 派生）隔离发现/状态（R4）与**构建产物**（本轮）；**共享 generate 事务**用跨进程锁串行化（写序证明/串行化要求满足）。
- 兼容：无 Hydration 路径保持 R28 默认 `.goxml/watch-bin-genN.exe` 与既有行为；启用 Hydration 才启 per-run `run/<runID>/` 输出与 generate 串行锁。旧发现/状态只读兼容不变。
- 若负责人后续选择**单项目互斥**，本轮 per-run 隔离与 generate 锁可作为基础；互斥拒绝需另加“第二条在任何构建/端点副作用前原子拒绝”（本轮未采用）。

## 真实未证边界
API/策略仍候选；真实业务 app CLI 原生端到端未做（fixture/外部 module）；原生窗口/跨平台/整应用性能采样未做；generate 锁为 Windows LockFileEx（首发范围）。

## 提交
- fix（本轮）+ 本 dev-log（见交付 readyRef）。

- `f6c7e3b` fix(watch): 每运行构建产物 + 共享 generate 串行化（含回归测试）
- dev-log 提交（本文件）
