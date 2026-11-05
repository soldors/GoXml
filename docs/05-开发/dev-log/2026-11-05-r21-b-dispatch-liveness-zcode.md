# 2026-11-05 R21-B ROUTER-DISPATCH-LIVENESS（ZCode）dev-log

基线 `a9bd8857f18c9eba6fecc076c85cc03d0000cf77`（A 线合入后的主库 SHA）；
树 `C:/Users/admin/.codex/worktrees/r21-zcode-liveness/Goxml`。
readyRef 分支 `codex/r21-zcode-router-liveness`；candidateRef 分支
`codex/r21-zcode-liveness-candidate`。改动范围：pkg/runtime/router 测试
（readyRef）+ drain 预算实验（candidateRef）+ 本线 dev-log。
主库、用户 WIP、R16-R20 冻结树与证据只读。

## F-08 实测（任务 1；七项矩阵，探针带墙钟超时）

基线状态（源码 + 实测）：`Shutdown()` 已导出（R20 发布）、drain 逐 fn 根取消
检查 + 剩余批次计数、F-08 注释在位。实测七项（r21_liveness_test.go，全部绿）：

| 实测项 | 结果（有限实测） |
|---|---|
| 64 节回调链 + pending 导航 | 链全部**同帧**执行（fixpoint）；链内每节观察 Current=/a（导航被推迟到排空后）；帧末 /b；耗时仅为该轮观测值 |
| FIFO / 同帧再入队 | **批内快照 FIFO**——实测序 [f1, f2, f1-requeued]：再入队任务加入下一批（不插队当前批），同帧内继续执行 |
| 链 + 多 pending | 只构造最终目标（/c），中间不构造 |
| 链中途后台 Dispatch | 同帧执行（fixpoint 到空为止） |
| Shutdown 与链竞争 | 闸门同步下确定性竞争：剩余批次+pendingNav 全部计账（实测 DroppedCount=2）、/b 不构造、Current 保持 |
| 双实例隔离 | r1 有待处理回调时 r2 导航/计数正常 |
| 关闭后入口计数 | 逐请求 3+3+1 |

**区分**：64 节完整链为有限实测；「无上限增长/可无限拖延同帧导航与布局」为
源码推断（drain 无预算字段、外层循环到空为止）——以 candidateRef 原型对照。

## 方案对比（任务 2）

| 维度 | 现状（无界 fixpoint，readyRef） | 方案 A 帧首快照（只处理已入队） | 方案 B 每帧有界批次（candidateRef 实现，预算 8） |
|---|---|---|---|
| 延后任务下一帧 | 不适用（无延后） | 再入队任务延后；下一帧请求需单独验证 | 余量留队；失效请求合并时能否推进仍待验证 |
| 回调顺序 | 批内快照 FIFO + 同帧 fixpoint | 跨帧 FIFO（链跨多帧） | 跨帧 FIFO + 每帧恰 budget 节 |
| 空闲帧成本 | 0（队列空即一检） | 0 | 0（预算只在有任务时消耗） |
| 关闭时未执行任务记账 | dropPendingLocked（队列+pendingNav）✓ | 同左（余量留队即被同口径计账） | 同左 + 逐 fn 检查的剩余批次 |
| 导航活性 | 链会**推迟**同帧导航到排空后（实测） | 同现状（排空后应用） | **导航不受预算推迟**——每帧仍应用（候选改进点） |

## 判定（任务 2）

- **基线符合现有契约**：F-08 独立验收「语义可辩护」、线程模型 §3「帧首排空」
  即现状 fixpoint、§4 合并语义——readyRef **交守卫无生产改动**（七项守卫锁定
  现状，防未来漂移）。
- **判别变异（已录已还原）**：drain 改单批快照（`for i:=0;i<1;i++`）→
  ChainRunsSameFrame 与 BackgroundDispatch 双红（executed=1≠64）。
- **candidateRef**：每帧预算原型（budget=8）——执行时机改变 + 新增预算语义，
  按任务书单独冻结等裁决。分支上三个链测试翻转断言候选语义（64 节跨 8 帧、
  每帧恰 8 节、**导航不受预算推迟**即第 1 帧末切换 /b——导航活性优于基线）。

## race 与门禁（任务 3）

- MinGW race（`CC=…/mingw64/bin/gcc.exe`、CGO_ENABLED=1、GORACE=halt_on_error=1、
  `-race -count=5`）：readyRef rc=0（3.3s）+ candidateRef rc=0（2.8s）——回调
  顺序、导航、重绘、关闭计数在竞争下无数据竞争。
- readyRef router 全套 rc=0；gofmt 空、vet 干净。
- 原生窗口与采样：明确排除（默认选择器本就不含原生用例）。

## 未验证边界

- 无预算下「无限链」的极端墙钟（只实测 64 节完整链）；
- 原生窗口形态下的链行为（默认选择器排除，未授权不跑）；
- 预算值 8 的取舍（原型值，裁决时定）。

## Coordinator review

Author test commit 415540f adds the initial matrix. Commit 84f5505 restores single-frame assertions, checks each callback observation, and requires an exact dropped count on shutdown.
