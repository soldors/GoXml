# 2026-11-05 R22-B ROUTER-BOUNDED-DISPATCH（ZCode）dev-log

基线 `8d7aa44b3a121af3e2b5f9d2c934086aea1fe0cb`（A 线接纳版，主库快进后）；
树 `C:/Users/admin/.codex/worktrees/r22-zcode-budget/Goxml`。
readyRef 分支 `codex/r22-zcode-router-budget`；candidateRef 分支
`codex/r22-zcode-budget-candidate`。主库/WIP/R18-R21 冻结树与证据只读。

## B1 双反例独立重放（先立红）

- **B1a budget=0 只跑首批**：旧候选 5251ba5（R21）构造器置 0 + 当前单帧守卫
  （8d7aa44b 协调者加严版）→ **executed=1, want 64**（rc=1，raw/r22b-replay-budget0）。
  根因：旧实现每批后 `if budget == 0 { break }` 同时击穿「无界模式」——
  batch=1 节后即退出，与「budget==0 保持基线 fixpoint」承诺冲突。
- **B1b 余量饿死**：旧候选 budget=8，集中入队 64 个独立回调 + 一次 Invalidate，
  可合并失效调度器只在有未消费请求时驱动帧 → **首帧消费 8、留队 56、
  请求余量=0、调度器停摆**（rc=1，raw/r22b-starvation-replay）。余量活性
  不能依赖入队时 Invalidate（合并语义下该请求已被首帧消费）。

重放树 `r22-replay-5251ba5`（detached，只读冻结证据用，验证文件仅存于该树）。

## readyRef（4e74ec7 + b/m）/ 无生产改动

- 调度器夹具（r22_scheduler_test.go）：失效请求可合并、帧只在有未消费请求时
  驱动——**不能无条件手动 Layout 后就声称调度活性成立**。
- 守卫 TestR22SingleRequestDrainsAll：一次请求 → 基线同帧排空 64 回调 →
  空闲（无残留请求、空闲帧零执行）。

## candidateRef（7198a18）/ 修订预算实现

- **budget==0（缺省）**：显式 fixpoint continue，与基线同构（B1a 修复绿证
  TestR22Budget0Fixpoint：64 节链一帧完成）；全套既有测试不变绿转红。
- **budget>0**：每帧至多 N 个 fn、批内快照 FIFO；预算耗尽即结束本帧、余量
  留队并**显式请求后续帧**（锁外调用，B1b 修复绿证
  TestR22LeftoverRequestsNextFrame：恰 8 帧清空 64）；排空后停止请求；
  **导航不受预算推迟**（R21 已声明候选方案：applyNavigation 每帧照常）；
  逐 fn 取消检查 + 精确丢弃计数（批次剩余 + 留队余量 + pendingNav 经
  dropPendingLocked）；双实例隔离；不新增公开选项（白盒设置）。
- **失效位置与锁重入**：锁内只捕获 inv 引用，回调在 `r.mu.Unlock()` 后调用；
  TestR22InvalidateOutsideLock 用会回查 Current 的失效器 + 10s 探针证明无死锁。

## B3 证据

- **每帧序列（实测日志）**：budget=1/total=8 → 8 帧 [1×8]；8/8 → 1 帧 [8]；
  8/9 → [8,1]；8/17 → [8,8,1]；8/7 → [7]（少于预算单帧排空，**无余量请求**，
  排空后停止 ✓）；路由值/剩余量/请求余量逐帧断言（超预算、残留请求、
  空闲帧执行均即红）。
- **Shutdown 三阶段守恒**：留队（executed 0 + dropped 64）、出队未执行
  （executed 1 + dropped 63）、工厂在途（R18 语义：关闭先获锁 → 在途结果
  一次丢弃、Current 保持、成功页不构造）——executed+dropped=总请求。
- **退化副本双红（可编译变异，已还原）**：A 移除余量请求 → 8/64 红；
  B 重引零预算 break → 1/64 红。均在有界受控事件调度中行为红。
- **race**：MinGW（CC=mingw64/gcc.exe、CGO_ENABLED=1）`-race -count=3`
  readyRef rc=0 + candidateRef rc=0。
- **bench（2000×，边界：仅路由侧 drain+render，不含 Gio 窗口/页面渲染；
  8 个空 fn/帧）**：budget0 341.1 ns/op、budget8 284.9 ns/op，均 120 B/op
  4 allocs/op（含基准自身的闭包分配，非稳帧零分配声明口径）。
- **门禁**：ready 分支 router 95.6% rc0、docgate（测试型门禁）rc0、gofmt 空、
  vet 干净；candidate 全套 rc=0。

## 裁决请求（不自动成为生产接纳）

预算语义（是否引入每帧预算、缺省值、预算耗尽时导航是否照常应用）待负责人
裁决；candidateRef 自测通过仅为候选。readyRef 现状守卫可直接接纳。
