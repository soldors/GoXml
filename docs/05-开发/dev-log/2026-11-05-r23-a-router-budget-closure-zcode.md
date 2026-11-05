# 2026-11-05 R23-A ROUTER-BUDGET-CLOSURE（ZCode）dev-log

基线 `a8a40e3bba30701939bc5804b5e7e53fdad5f4ec`；树
`C:/Users/admin/.codex/worktrees/r23-zcode-budget/Goxml`。
**readyRef = 本分支**（守卫/设计/记录，无生产改动——基线的调度器守卫与
R21/R22 加严守卫原样保留）；**candidateRef = 分支
codex/r23-zcode-router-budget-candidate**（预算移植 + C1/C2 修复 + 矩阵）。
主仓/WIP/R22 冻结树只读。

## A1 关闭 R22 协调者已证缺陷（candidateRef 35c9ff3）

- **C1（关闭后空队列仍请求一帧）**：余量唤醒**后置**到回调全部完成后——
  锁内重查 `len(queue)>0 && rootCtx.Err()==nil`（Shutdown 清空队列 → 不唤醒；
  后台入队竞态 → 照常推进），失效回调锁外调用。
  反例重放：budget=8、9 任务、第 8 个回调 Shutdown → 恰 1 帧消费 1 请求、
  DroppedCount=1、Current 保持（TestR23C1ShutdownOnEighthStopsWakeup）；
  缺陷副本（缓存进入循环时的快照唤醒）红已录（MUT-C1）。
- **C2（锁重入守卫未触达 drain）**：重写为「先入队 9 个超预算任务，再启用
  Current 回查」——drain 余量路径真实触发失效回调；健康 PASS；
  持锁调用的可编译副本在 10s 墙钟内 FAIL（MUT-C2 有界红已录；
  frame 驱动走 goroutine+截止探针，死锁有界可测——首版内联驱动会挂死
  测试进程，已修正并记录）。
- **同步纪律**：entered/release 信号替代 sleep；工厂进入/释放/完成均有
  10s 墙钟上限（TestR23FactorySignalsWithDeadlines）。
- 保留 R21/R22 已加严守卫：r21_liveness_test.go（协调者加严版）、
  r22_scheduler_test.go 原样在基线与候选两分支，全套绿。

## A2 完整判别矩阵（candidateRef，九项）

- 轨迹精确断言（FIFO id 序列 + 精确帧数 + 帧末路由 + 排空后无残留请求 +
  空闲帧零执行）：budget=1/total=3 → [0][1][2]；8/9 → [0..7][8]；
  8/17 → [0..7][8..15][16]；8/8 → [0..7]（恰好排空无余量请求）。
- budget=0：显式 fixpoint（B1a 修复绿证）+ 既有 FIFO/链守卫不变。
- 场景：无新 Dispatch 的留队推进（64 回调恰 8 帧）、回调再入队跨帧、
  后台入队、回调关闭（守恒 8+1=9）、外部关闭（0+20=20）、在途工厂
  （entered/release + 墙钟）、双实例隔离。
- 调度器只在有未消费失效请求时驱动帧；空闲帧零执行。

## A3 成本、退化与裁决包

- **双退化红（可编译变异，已录已还原）**：C1 陈旧快照唤醒 →
  TestR23C1ShutdownOnEighthStopsWakeup 红；C2 持锁唤醒 →
  TestR23C2LockReentryReachesDrainLeftover 墙钟内红。健康材料恢复 PASS。
- **bench（3000×；边界：仅路由侧 drain+renderCurrent，不含 Gio 窗口/页面
  渲染；首帧预热不计入）**：Idle Layout **83.7 ns/op 0 B/op 0 allocs/op**；
  Active（8 fn/帧 Dispatch+Layout）**395.8 ns/op 120 B/op 4 allocs/op**——
  120B/4 allocs 来自队列切片增长与测试闭包，空闲帧稳态零分配
  （INV-PERF-2 口径按空闲帧成立，不外推首帧）。
- **门禁（锁内台账）**：candidate 全套 rc=0（cover 见 raw）；
  MinGW race `-race -count=3` rc=0；docgate rc0；gofmt 空、vet 干净。

## 决策表（技术通过后交负责人裁决；候选不自动成为生产接纳）

| 决策点 | 选项 | 建议 |
|---|---|---|
| 默认预算 | 0（基线 fixpoint，缺省）/ 8（有界） | **0**——无界 fixpoint 语义可辩护且实测健康；有界语义仅在出现真实活性投诉时启用 |
| 是否开放配置 | 不公开（白盒）/ Config 字段 / Router setter | **不公开**——按「不新增公开选项」约束，内部保留白盒；确有需求时随 B2 生产接线一并提案 |
| 预算耗尽时导航规则 | 每帧照常应用（现候选）/ 推迟到排空 | **每帧照常应用**——导航活性优于基线，且实现简单（applyNavigation 在预算循环外） |

## 未验证边界

- 原生窗口形态（默认选择器排除，未授权不跑）；race 覆盖 `count=3` 非
  长时间压测；预算值 8 为原型试验值。

## 门禁数字

- candidate router 全套 rc=0（cover 见 raw/r23a-suite-cand）；
- MinGW race `-race -count=3` rc=0（raw/r23a-race-cand）；
- docgate rc0（raw/r23a-docgate）；gofmt 空、vet 干净。
