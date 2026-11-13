# 2026-11-13 R29-A R3 运行实例所有权与 CLI 真实配置 — 开发日志

返修父 `6da61047160ee09f071400d662240e992c61c304`。分支 `codex/r29-r3-deepseek-hydration`，独立树 `r29-r3-deepseek-hydration`（开工 HEAD==父、clean）。主库 `d20714dd…`（R28 发布基线），与 R29 作者树分开、只读。写范围：`internal/devx/hydrationchannel`、必要状态存储 `internal/devx/hydration`、Hydration CLI（`internal/cli/watch*`）、`pkg/hydrationapp`/示例、本 dev-log。R28 稳定入口与默认关闭回归保留。

## 1. 同材料复现两红 → 修复 → 绿（`coordinator-r3/a-r2-session-probe_test.go`）
- **红① 旧运行删掉新状态**：`TestCoordR29R2OlderSessionKeepsNewerState`——旧 Session.Close 删除了新运行写入的合法状态。
- **红② 缺 ownerId/gen 的匹配发现记录仍被覆盖**：`TestCoordR29R2InvalidMatchingDiscoveryIsForeign`——session/app/schema 匹配但无 ownerId/generation 的记录被发布覆盖。
- 两条复现后修复，现均 PASS（`raw/red-r3`）。

## 2. 修复：实例级命名空间 + 代次区分（`store.go` + `session.go`）
- **Store 实例命名空间**：`NewStoreWithRun(projectDir, sessionID, runID)`；物理 `<session>.<runID>.state`，逻辑 SessionID 不变（Envelope/Expect 兼容）。旧 `NewStore` 保持 R28 路径 `<session>.state`。
- **runID 由 token 派生**（`sha256(token)[:8]`，非机密、不可逆推 token、不写日志）：**同一运行的三代共享同 runID/同状态路径**（可交接）；**不同运行 runID 不同 → 状态路径隔离**（新运行不能接管/误删他运行状态，旧运行 Close 仅触及自己的实例路径）。
- **Close**：仅删本实例命名空间路径且可校验的状态；legacy `<session>.state` 从不删除（只读回退）。
- **Restore 兼容**：实例路径无状态时**回退读取 R28 旧格式** `<session>.state`（只读，不迁移不删除）；`ErrNoState`（冷启动）与真实 I/O 错误区分。
- **发现记录外来判定收紧**：`portRecord` 视为“本会话合法”需 session/app/schema 匹配 **且 OwnerID!="" 且 Gen>=1**；缺 ownerId/gen 的同会话记录视为**外来/旧格式**——发布**拒绝覆盖**（保留原字节），`ReadPortInfo` 不匹配（保留）。旧发现格式 `<session>.port.json`（无 ownerId/gen）作为外来保留，需迁移。
- **替换/删除无并发误窗口**：发布用独占临时名（R2）+ 目标非本会话合法记录拒绝覆盖；Close 只删本实例记录与状态；不同实例路径隔离 → 并发替换/删除不误覆盖/误删（`TestR3ConcurrentWriteAndCloseNoCrossWindow`）。

## 3. 绿证（R3 新增回归 `session_instance_r3_test.go`，6 例全绿）
新运行不能接管外来有效状态；旧运行不能删新运行状态（字节+mtime）；同一运行三代交接（同 token 同路径可恢复）；R28 旧格式只读兼容（Close 不删 legacy）；并发写/Close 无跨实例误删；状态无状态=ErrNoState 与真实 I/O 错误可区分。协调者两红现 PASS。

## 4. CLI replay 参数到达真实应用端点（item 2）
- 发现缺陷：父进程 Session 接收 policy/cap，但**应用（子进程）Session 构造未使用该配置**。
- 修复：CLI 启动前**校验** `--hydration-replay`（空=默认 reject；非法值拒绝、cap<0 拒绝），并经子进程 env 注入 `HYD_REPLAY_POLICY`/`HYD_REPLAY_CAP`；示例（应用）读该 env 构造 Session 的 `ReplayPolicy`/`ReplayCap`，端点据此 SetReplayBounds。**命令参数能影响真实应用端点**。
- 测试：`TestR3HydrationReplayValidationAndInject`（非法值启动前拒绝、合法值注入 env）；`TestR1HydrationReplayDefaultFailClosed`（默认 reject）。帮助/示例/退出码对齐；env 日志/报告不含 token（`TestBuildHydrationHookAndEnvNoTokenInArgv`、`TestR2NoTokenInDiscovery`）。
- 真实小容量实测：`raw/green-replay-concur`——`--hydration-replay evict --hydration-replay-cap 2` 下 gen1→gen2 恢复仍工作（参数被接受、到达端点）；真实双进程同名会话并发两 `goxml watch` 均启动（实例命名空间状态隔离）。

## 5. 保留正/反控制
- R2 原三项守卫（外来发布/`.tmp`/旧删新）仍绿；真实外部 module 三代恢复正控制 `raw/green-3gen`（gen1→2→3 全不同 PID、每代 count=7/nonDefault=seed-7、gen1 restored=false、gen2/3 true、构建产物存在、收口）。
- 真实双进程同名并发反例：`raw/green-replay-concur`（两进程均启动；状态实例隔离）。mtime 保留由 `TestR3OldRunCannotDeleteNewRunState` 断言。

## 门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0）
hydrationchannel/hydration/watch/cli 全绿；`-race` hydrationchannel+hydration+watch rc0 / 0 DATA RACE；`go build ./...` rc0；vet rc0；gofmt 空；docgate/diagdoc ok。

## 可判别变异（compile→行为红→恢复绿）
- **MUT-norunid**（`store.go` Path 忽略 runID，回退无实例命名空间）→ `go test -c` rc0 → `TestR3OldRunCannotDeleteNewRunState`、`TestCoordR29R2OlderSessionKeepsNewerState` **FAIL**。

## 候选/未证边界
- 协议/API 仍为候选；正式公开 API、nonce 容量与 `--hydration-replay` 策略待裁决；标志**默认关闭**。
- runID 由 token 派生（同运行同 token）；跨进程 CLI 与子应用共享同一 token（env 注入）。
- 旧发现格式（无 ownerId/gen）作为外来保留、需迁移；未自动迁移。
- 原生窗口、跨平台运行、整应用性能采样未做。

## 提交
- `a10063f` fix(hydration): 运行实例命名空间 + 发现外来守卫 + CLI replay 到真实端点（含回归与变异）
- 本 dev-log（含提交引用）
