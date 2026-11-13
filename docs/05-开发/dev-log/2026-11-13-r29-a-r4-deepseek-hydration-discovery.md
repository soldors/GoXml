# R29-A R4 · Watch 运行级发现与真实防重放 — dev-log (DeepSeek)

起点：`788350dd533d863e8507afa4625673f970f0408a`；分支 `codex/r29-r4-deepseek-hydration`。

## 缺陷（原红）
同名会话（同 session/app/schema）不同 token 的两个运行共享 `<session>.port.json`：后启动者覆盖，
先启动运行的客户端 `NewClientFromPort` 读到后者端点 → `ErrUnauthorized`。
原样重放 `a-r3-shared-discovery-probe_test.go` 在基线 session.go 上真实翻红：
`discovery sent the first run to the other live endpoint: hydrationchannel: token/session 未授权`。

## 最小修复（能证明所有权）
1. 发现文件按**运行身份 runID**（由 token 派生，同一运行三代共享）隔离：
   `<session>.<runID>.port.json`。发布/读取/清理都作用于本运行文件；不同运行互不覆盖/误删。
2. 读兼容：运行文件不存在时，**只读回退** R28 旧 `<session>.port.json`，且仅接受无 ownerId 的旧格式，
   绝不删除旧文件（保持 R28 默认入口）。
3. `storeOwning` 由 `os.ReadFile` 改为**有界读取**（上限 `MaxEnvelopeBytes+1`），区分「不存在」与「真实读取错误」；
   超限/外来/坏文件一律不拥有并保留原字节与 mtime；Close 对真实读取错误如实上报且不删除。

## 绿证
- `go test ./internal/devx/hydrationchannel` 全绿（含原 probe 回绿 + 4 条 R4 判别性回归）。
- `go vet` / `go build` rc0；gofmt 无差异。
- 记录见交付 `commands.jsonl`（5 条 completed，rc0，锁已释放）。

## 边界 / 未验证
- `-race` 未执行：本机无 gcc（MinGW），CGO race 构建失败（环境缺失，非代码失败）。
- item3 真实 CLI 容量边界（默认reject/显式reject/evict、外部 module 三代、默认关闭、取消、失败保旧）：
  本次未新增真实 CLI 夹具，仅沿用/引用既有 e2e 证据；由协调者判定是否需补。
- 组合 combined-affected 的 Windows 共享冲突：见交付 handoff（判定为短暂文件共享冲突，非确定性扫描错误）。

## item3 真实 CLI / 容量边界（本轮补）
- 新增 `zz_coord_r29r4_cli_capacity_test.go`：**真实 exampleapp 进程 + 自有端点 + cap=2**，raw loopback TCP 填满预算，
  分三态断言：
  - 默认(reject) / 显式 reject：第三个新 nonce → `ErrReplayWindowFull`；窗口内重复 → `ErrReplay`；
  - evict：第三个新 nonce 被接受（淘汰最旧）；被淘汰旧 nonce 重复不再判重放（ok），窗口内另一 nonce 仍 `ErrReplay`。
  证据：`commands.jsonl` id `r4-capacity`（rc0）。
- **外部 module 真实 CLI 三代恢复**复跑（`r29r1-cli-3gen.py`，真实 `goxml watch` 二进制 + `example.com/...` 外部 module）：
  PID 22900/13696/24464 三代互异，每代 count=7/nonDefault=seed-7，gen1 restored=false、gen2/3 restored=true，
  `watch-bin-gen1/2/3.exe` 真实构建产物齐。证据：`commands.jsonl` id `r4-cli-3gen`（rc0）、`raw/r4-cli-3gen/`。
- S4 边界组（默认关闭/两代恢复/构建失败保旧/协议失败拒绝/退出清理）复跑全绿：id `r4-s4`（rc0）。
- 全包终态：id `r4-pkg-full-final`（rc0）。
- 仍**未验证**：`-race`（本机无 gcc/MinGW）；真实业务 app（非 exampleapp/外部 module fixture）的 CLI 原生端到端。

## 提交
- `1a2def0` fix(r29-a r4): 运行身份隔离发现文件 + storeOwning 有界读取
- `261e79a` test(r29-a r4): 真实进程+固定小容量防重放三态边界（默认/显式 reject 与 evict）

## 组合门禁观察：无 Hydration 的既有 Watch 共享冲突（本轮修复）
- 组合 `combined-affected` 首轮 `TestDevWatchActualCLIRebuildFailureRecoveryAndCancel` 在删除 `broken.go` 时
  `cmd\app\broken.go: The process cannot access the file because it is being used by another process`，
  触发 `goxml watch: 监听/清理失败` 终止。R28 基线与组合定向复跑**均绿**（`raw/r28-watch-control`、`raw/combined-watch-recheck`）。
- 判定：**瞬时 Windows 文件共享冲突**（并发构建/编辑器短暂持有文件），非确定性扫描/代码生成错误
  （旁路证据：stderr 中 build 失败为 `undefined: missingApplicationSymbol`，共享冲突发生在扫描 `os.ReadFile` 与删除同一路径并发时）。
- 但 `watch.Run` 轮询循环对 `Scan` 任一错误 `return err` 终止 Watch（`internal/devx/watch/run.go`），
  属产品健壮性缺陷：瞬时共享冲突应**有界保旧重试**而非终止。
- 最小修复（有界、保旧）：`Run` 循环对**瞬时**共享/锁冲突（`ERROR_SHARING_VIOLATION`/`ERROR_LOCK_VIOLATION`，
  `scan_transient_windows.go`）跳过本 tick 保旧继续；**连续**超过 `maxTransientScanSkips=40` 才以真实错误终止
  （保留首败诊断）；非瞬时错误仍立即终止。注入 seam `scanFn` 便于受控测试。
- 受控测试 `scan_transient_r4_test.go`：瞬时连续冲突 → Run 返回 nil（不终止）；非瞬时 → 终止并返回错误。
- 真实 `TestDevWatchActualCLIRebuildFailureRecoveryAndCancel` 修复后连跑 2 次均 PASS（`raw/green-watch-recovery.txt`）。

## 本轮门禁（Windows/amd64，CGO=0；race MinGW-W64 16.1.0）
- 受影响包全绿：hydrationchannel / hydration / watch / cli；`go build ./...` rc0；vet rc0；gofmt 空。
- **MinGW `-race`**（CGO=1）：hydrationchannel+hydration+watch rc0 / 0 DATA RACE（本轮补，前次环境缺 gcc 未跑）。
- docgate/diagdoc ok。
- 原红→绿：`a-r3-shared-discovery-probe_test.go` 在父 `788350d` RED（ErrUnauthorized）→ 本 HEAD GREEN。
