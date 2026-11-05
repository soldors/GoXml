# TASK-ROUTER-001：实现单窗口路由与转场动画

## 元信息

- 状态（**分段，不得合并**，2026-11-05 R23-C 重核）：
  - **子任务 1（路由注册 / 当前路径 / 页面分发）**：已实现、已验收、已合入、已推送。
    R16–R17 候选实现与生命周期守卫；R20 B1 人工桌面视觉与原生 race、B2 受控销毁接线
    独立复核 PASS，发布组合 `4e413f0` 是远端 `a8a40e3` 的祖先。
  - **关闭接线**：`a500a1f` 导出 `Router.Shutdown`，App 提供 `app.WithOnDestroy` View 包装，
    两个事件循环收到销毁事件各调用回调一次；R21-C 的
    [routed-gallery 教程](../../../examples/gxml/testprojects/routed-gallery/README.md) 用真实 Router
    接线，R22-C 把缓存/资产/变异验收变成 [随仓入口](../../../scripts/verify-gallery-accept.py)。
  - **子任务 2（MacroOp 转场快照）/ 子任务 3（独立 OSWindow 逃生接口）**：**仍未实现**。
    因此本卡整体**未闭环**，不得据路由基础已发布把整卡写成 Done。
  - **有界调度预算**：R23-A 的 `9268601` 只接纳设计与记录，生产代码与 `a8a40e3` 相同；
    候选 `35c9ff3` 技术范围通过但**语义未批准、未进主库**。主库沿用同帧排空。
- 优先级：P1 | 预计：6h
- 分支：feature/TASK-ROUTER-001（历史卡）；现行实现在 `pkg/runtime/router/`
- 依赖：TASK-STATE-001
- 现行总览：[R23-C 项目状态总览](../2026-11-05-R23-项目状态总览.md) §4.2

## 1. 目标
在单个 Gio 窗口中按路由状态选择页面；转场时录制并复用旧页面快照，提供独立 OSWindow 逃生接口。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 | 2026-11-05 状态 |
|---|---|---|---|
| TASK-ROUTER-001-1 | 实现路由注册、当前路径与页面分发 | go test -v ./pkg/runtime/router/... | **已实现 / 已验收 / 已合入 / 已推送**（R16–R20） |
| TASK-ROUTER-001-2 | 实现旧页面 MacroOp 快照及转场生命周期 | go test -v -run TestTransition ./pkg/runtime/router/... | **未实现**，验收命令未执行 |
| TASK-ROUTER-001-3 | 实现 OSWindow 逃生接口并覆盖切换、取消场景 | go test -v ./pkg/runtime/router/... | **未实现**，验收命令未执行 |

## 3. 历史状态说明

本卡元信息原写「Todo」，那是 2026-10 的快照；按 R23-B 独立复核确立的「历史证据按日期保留、
不追认、不抹除」纪律，历史原文按日期保留、状态按分段事实更正，不把「基础已发布」外推成
「转场与 OSWindow 已完成」，也不把「预算候选技术通过」写成「语义已批准」。
边界：R20-B2 集成入口 `RunWindow → runWindowEvents` 的真实窗口/DestroyEvent 已有一次原生 race PASS；生产 `App.loop` 由受控事件验证，未做该入口的原生实跑。跨平台与完整 GUI 性能验收仍未完成，不能把两条入口的证据混用。
