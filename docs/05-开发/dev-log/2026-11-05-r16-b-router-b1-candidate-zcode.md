# 2026-11-05 R16-B ROUTER-B1-RELEASE-CANDIDATE（ZCode）dev-log

基线 `b26155115ee76b1415ae36870fa18b252cb7a5d5`；树 `C:/Users/admin/.codex/worktrees/product-r16-zcode/Goxml`，
分支 `codex/product-r16-zcode-20261105`。
改动范围：pkg/runtime/router（B1 候选三文件字节一致带入 + F-02 七行修复 + 本线验收测试）+ 本线 dev-log。
主仓与其他作者树全程只读（Router 三未跟踪文件经只读核对后复制，主仓零改动）。

## B1 审计登记（一致与漂移）

**证据链（主仓未跟踪，只读）**：开工核查与接口设计 → B1 路由核心报告（实现者）→
B1 独立验收报告（有条件通过，16 项契约矩阵 + 13/13 测试矩阵，F-01..F-08 全 P3）→
B1 验收收口（技术 PM，F-06 源码级核查 + B2 门槛 G1-G4）→ B2 任务卡草案（未批准）。

**一致项**：
- 文件身份零漂移：三文件 sha256（router.go 898672de… / router_test.go cc690c80… /
  integration_test.go b0bf9acc…）与字节数（11598/14025/3620）同独立验收报告 §1.1 登记一致。
- 报告行号引用与当前源码吻合：F-01 死字段（router.go:78 hasPending）、F-02 drain 口径
  （:261-270）、F-05 自指断言、F-08 drain fn 无界等逐项复核成立。
- 16 项契约矩阵复验：显式注册/精确匹配/单窗口边界/UI 线程归属/Context 派生与切换取消/
  renderCurrent 唯一渲染入口（B2 接缝）/OSWindow 未实现/不导入 app（包级）/无半提交/
  同帧合并/关闭计数——与源码一致。
- 13/13 既有测试在本候选上原样通过（count=1）。

**漂移项**：无。报告的基线（9f3ab8e）与远端（e92e696）早于本候选基线 b261551，
属环境时序而非交付物漂移；报告列的字体线在制文件与本候选无关（未触碰）。

**B1 边界（B2 能力另列）**：转场快照/OSWindow/codegen 路由表/多窗口/OnMount-OnUnmount
未实现；DestroyEvent→rootCancel 生产接线未做（B2 任务卡草案 B2-2/B2-3，待负责人批准）；
F-06（integration_test.go 导入 app 的测试目录豁免）维持「待负责人追认」状态，本候选
如实带入并在审计中登记。

## 候选提交（精确文件集）

`d387b02`：pkg/runtime/router/{router.go, router_test.go, integration_test.go} 三文件
字节一致复制（sha256 复核）；验收命令 go test -v ./pkg/runtime/router/... 全绿。

## B2/B3 验收（2d3cf73 测试 + 4b581d6 修复）

受控反例 + 同步点（显式帧驱动），全部围绕既有接口：

| 片 | 测试 | 断言要点 |
|---|---|---|
| B2 | LegalSwitchLifecycle | pending 帧前不生效；**构造新页时旧 ctx 仍存活 → 切换后 Canceled**（无半提交顺序）；旧页停止渲染 |
| B2 | FactoryErrorThenRetry | 失败整体放弃；失败帧旧页照常渲染；NavError 记录/成功清空 |
| B2 | DuplicateNavigationCancelsPending | 同帧合并最后一个（中间页不构造）；「导航到当前路径」撤销 pending |
| B3 | DrainOrderQueueThenNavigation | 帧先排空队列后应用路由（线程模型 §3，队列函数看到旧路径） |
| B3 | CallbackTriggersNextNavigation | 回调内 Navigate 同帧生效 |
| B3 | ClosedDropCountsPendingNav | **F-02 复现并修复**（见下） |
| B3 | InvalidationSequenceAndIsolation | 合法切换恰 1 次失效、no-op 不追加、Dispatch 各 1 次、双实例互不串写 |
| B4 | HostCompositionWindowless | app.NewApp 接受 Router 根 View（窗口首次 w.Event() 才创建，零 OS 资源）、*App 接线 + 二次拒绝、手动帧驱动下导航/Layout/被换页 ctx 取消 |

**F-02 修复（4b581d6，7 行）**：drain 侧丢弃统一计入 DroppedCount（1 队列 fn +
1 pendingNav）。契约依据：DroppedCount 文档语义 + 线程模型 §4 全量丢弃记录；
B1 独立验收报告 F-02 处置建议第一选项。**变异红**：禁用该计数（可编译变异）→
测试红（实际 1，want 2），已还原。公开接口与路由语义不变。

## B2 候选清单（另列，待负责人批准——本线未实施）

沿用主仓 B2 任务卡草案（未批准）：B2-1 Shutdown+F-01/05/07/08 收口、
B2-2 app.go OnDestroy 最小集成（G3 前置）、B2-3 生产形态真实窗口集成测试、
B2-4 报告登记。F-03（drain 侧关闭测试）已由本轮 TestR16ClosedDropCountsPendingNav
覆盖，可从 B2-1 划除。

## 未证路径（原生时序，明确标注）

- 真实窗口**视觉**验收（人工）；-race（CGO_ENABLED=0 环境阻塞，D-006）；
- DestroyEvent → Router 关闭的生产接线（B2 授权范围）；
- 集成测试 TestRouterInRealEventLoop（候选自带）覆盖进程内真实事件循环，
  其窗口创建属 B1 既有验收证据，本轮按既有形态复跑通过。

## 门禁

router 包 `go test -count=1` 全绿（13+8=21 测试）；gofmt 空；vet 干净；
`go build ./...` ok。受影响包门禁数字见 handoff（锁内、台账）。
