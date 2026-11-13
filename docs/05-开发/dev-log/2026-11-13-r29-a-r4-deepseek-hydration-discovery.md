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
