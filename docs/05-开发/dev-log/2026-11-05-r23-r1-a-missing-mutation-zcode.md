# 2026-11-05 R23-R1-A ROUTER-MISSING-MUTATION（ZCode）dev-log

主基线 `bab7a35fb6ee77be000221c6d39dc22b943df7f5`；readyRef 树
`C:/Users/admin/.codex/worktrees/r23r1-zcode-ready/Goxml`（分支
codex/r23-r1-zcode-router-evidence）；验证树（detached）
`C:/Users/admin/.codex/worktrees/r23r1-zcode-verify/Goxml` @ 候选精确 SHA
`35c9ff343d24f5ba37c5ff3781706b0a2eb4a0bd`。本轮**无生产代码改动**：
变异经 Go overlay 外部源码副本实施，两棵树源码零触碰。

## A1 遗漏锁定（completion-audit 缺口 R23-A3-NO-LEFTOVER-WAKE-MUTATION）

R23 原材料已有两条红，**均不能代替「完全移除余量唤醒」**：
- 陈旧快照唤醒（MUT-C1）：仍**会**唤醒（多一次请求/帧），错在时机；
- 持锁唤醒（MUT-C2）：仍**会**唤醒（持锁死锁），错在调用上下文。
缺失的是「**完全不唤醒** → 留队任务饿死」这条变异——A3 原文要求
"移除余量帧失效以及持锁失效"中的前者，此前无人执行。

**选定健康守卫**：`TestR23LeftoverAdvancesWithoutNewDispatch`
（r23_budget_matrix_test.go @35c9ff3，sha256 见 raw）——经可合并失效
调度器**只在有未消费请求时驱动帧**：64 独立回调 + 一次请求 → 恰 8 帧清空。
候选健康用例集合（9 项）：BudgetTrajectories、LeftoverAdvances、
CallbackReenqueue、CallbackShutdownAndExternalShutdown、
TwoInstanceBudgetIsolation、C1ShutdownOnEighth、C1BackgroundEnqueue、
C2LockReentry、FactorySignalsWithDeadlines。

## A2 缺失变异（一次补齐）

- **变异（外部副本 overlay）**：复制 35c9ff3 的 router.go 到独立目录，
  仅移除 drain 的余量 Invalidate 调用（保留入队 Invalidate、取消检查、
  锁序、预算与计数；`_ = leftover; _ = inv` 保持可编译）；
  **编译成功依据**：overlay 下 `go vet ./pkg/runtime/router/` rc=0
  （raw/r23r1-mut-compile）。
- **行为红**：同一健康守卫在 overlay 下 → **FAIL：executed=8, want 64**
  （调度器 1 帧后无请求停摆，留队 56 饿死；raw/r23r1-mut-red，rc=1，
  FAIL 行已存）。未用手动再 Layout 驱动到绿。
- **恢复**：去 overlay 同材料同守卫 → PASS（raw/r23r1-restore）。
- **budget=0 保留**：全套（含 BudgetTrajectories 的 fixpoint/FIFO 守卫与
  C1/C2/计账/后台/逐帧路由守卫）窄测回放 rc=0（raw/r23r1-suite，-v 全量，
  0 SKIP）。守卫未拒绝变异的问题不存在——现有守卫直接红，无需补断言。

## A3 补证包

- 复跑入口：验证树 `go test ./pkg/runtime/router/ -run
  'TestR23LeftoverAdvancesWithoutNewDispatch' -count=1 -v`（健康）；
  加 `GOFLAGS=-overlay=…/overlay-copy/overlay.json`（变异红）。
- 三阶段：healthy rc=0 → mutation rc=1（8/64）→ restore rc=0，
  argv/cwd/rc/双通道 raw 均在 raw/。
- 哈希：候选 router.go、变异副本、overlay.json 见 raw/r23r1-*-hashes.txt
  与本报告附录。
- 用例名/完成标记/0SKIP/rc：见 raw/r23r1-suite（-v 全量）。
- 持久测试：**无需新增**——既有守卫已拒绝该变异，不制造重复；
  candidate 分支无新提交。
- 边界：本轮只补技术证据；不请求预算默认值/公开配置/导航语义裁决；
  不接其他线、不合主库、不推送。

## 覆盖率与门禁

验证树窄测（守卫级）+ 全套回放 rc=0；ready 树 docgate rc0（raw/
r23r1-docgate）；未重复全仓产品测试（改动为零）。
