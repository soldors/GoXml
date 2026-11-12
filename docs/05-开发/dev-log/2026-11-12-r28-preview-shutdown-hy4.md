# R28 全树阻断 2 · Preview 两个终止边界判别与夹具修正（HY4，2026-11-12）

> 任务卡：`coordinator/watch-final-2026-11-12/preview-shutdown-hy4.md`。
> 分支 `codex/r28-preview-shutdown-gate`（父 `2c16e37109c017fb025d5ded5b8f88530f7da0c9`），
> 独立 worktree `C:/Users/admin/.codex/worktrees/r28-preview-shutdown-gate/Goxml`。
> 交付：`preview-shutdown-delivery-hy4/`。不重做 C2，不改主库/候选，不合并、不推送。

## 结论先行

| 项 | 结论 |
|---|---|
| 阻断 2-A（进程树收口 rc128） | **夹具时序前提缺陷，产品无缺陷**。consolehelper tree 分支父进程 `child.Wait()` 后退出，整树自然寿命只有子进程的 ~3s，与测试 2s 超时 + taskkill 执行（负载 2–2.4s）重叠；宿主以退出码 0 自然消亡，taskkill 如实 rc128「没有找到进程」，产品按合同上报终止失败，测试误判。四假设判别：「目标已自然退出」成立（30/30），PID 识别 / 后代遗留 / 命令失败均排除 |
| 阻断 2-B（Bounded 1s 守卫红） | **夹具前提失效，产品无缺陷**。只清 PATH 无法禁用绝对路径 taskkill（probe-b：旧前提 8/8 次 1111–1354ms、错误链无 taskkill），1s 守卫量的是 taskkill 成功路径耗时（必超）；真实不可用（disableTaskkill）实测 273–276ms、0/8 超 1s、错误链保留 taskkill |
| 生产代码改动 | **零**。全部改动在测试与 testdata helper（3 文件） |
| 提交 | `8f364fb`（2-A：consolehelper + host_test）、`0b455bb`（2-B：r26 Bounded 夹具 + 正对照） |

## 判别实验（raw/probe/，探针源码已存档、未入提交）

- **probe-a**（负载下 30 场景复刻「进程树收口」）：修复前 30/30 taskkill rc128、宿主全部退出码 0 自然退出、marker 30/30 出现（子进程走完自然寿命）、done 在 kill 调用前仅 3/30 关闭（其余为 kill 执行中自然死亡）、输出原始字节与全树首败逐字同款。修复后 30/30 rc0、marker 0/30。
- **probe-b**（三前提 ×8）：old-path-only 1051–1227ms 全超 1s；disable-all 273–276ms 全绿且错误可见；normal-tools 995–1342ms 全超 1s（正对照不能沿用 1s 守卫的依据）。
- 中间对照（探针同轮发现）：仅修父进程寿命、子进程窗口仍 3s 时，负载下 taskkill rc255「无法终止（没有此进程的实例）」+ marker 出现——终止路径负载下可达 4.5s+（定时器唤醒延迟 ~2.5s + taskkill 2–2.4s），3s 写入窗口结构性不足，据此将子进程窗口改为 10s（killCommandTimeout 5s 的 2 倍），等待窗 11.5s。有判别依据，非凑绿。

## 修复要点

1. `consolehelper tree <seconds>`：父进程睡 `<seconds>`（原实现忽略参数、寿命被子进程 3s 绑定）；子进程 marker 窗口 3s→10s。
2. `TestSupervisorLifecycle/进程树收口`：READY 就绪门闩（子进程 spawn 后才开始计时）+ `StopError()==nil` + 有界退出证明 + marker 11.5s 判据；保留 `werr != ErrHostTimeout` 严格比较（终止失败敏感性不放松）。
3. `TestCoordHostKillFailureIsBounded`：`disableTaskkill(t)` 替换只清 PATH；补「错误可见」断言（超时类别 + taskkill 点名 + 首返回值 nil）；新增「正常工具健康根与后代正对照」子测试（tree 模式，不沿用 1s 守卫）。

## 门禁（commands.jsonl 逐条，含锁）

| 命令 | rc | 说明 |
|---|---|---|
| 定向（两测试 + 探针，-v） | 0 | 首轮红（rc255/marker 假阳性）→ 修窗口后全绿 |
| 定向 count=3 | 0 | 107.3s，两测试 ×3 全 PASS |
| 包全量 -count=1 | 0 | 32 PASS / 0 FAIL / 0 SKIP（另 21 子测试 PASS，59.1s） |
| MinGW race（CGO=1, halt_on_error=1） | 0 | 0 DATA RACE（62.7s） |
| go vet / gofmt（3 文件）/ diagdoc / docgate | 0 | 全过 |

## 未证边界

- 未跑全树（按任务留给协调者统一复跑）；未验证 Linux/macOS、真实原生窗口。
- rc128/rc255 的**产品语义**（「目标已自然退出」是否可凭真实退出证明判为成功）本轮**未裁决未改动**：现行合同「不把找不到当成功、失败必须被观测」保持原样，如需语义变更应由协调者单列裁决。
- 10s 子进程窗口的跨机器适用性：基于本机负载实测（终止路径 ≤4.5s+ 与 5s 预算）；更慢机器上若终止路径逼近 10s，判据仍可能假阳性。
