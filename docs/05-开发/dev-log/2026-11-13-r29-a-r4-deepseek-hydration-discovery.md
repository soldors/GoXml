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
