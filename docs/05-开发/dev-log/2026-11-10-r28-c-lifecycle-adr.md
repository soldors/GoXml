# R28-C Lifecycle ADR 文档化收口

## 记录

- 日期：2026-11-10
- 分支：`codex/r28-c-lifecycle-adr`
- 父基线：`474dfb2a047ab19bc56a6093b13c32f7a0bbd204`
- 变更性质：文档与拓扑同步；生产代码零改动；未触碰共享文档索引和 `CHANGELOG.md`

## 已完成

- 新增已批准 ADR：`docs/03-架构/ADR/ADR-0018-Lifecycle-Owner与Router页面生命周期.md`。
- 更新 Lifecycle 任务卡：`docs/04-WBS/任务卡/TASK-LIFE-001.md`，记录 R28-C 已批准边界、已验证时序和未完成静态资源/原生验收。
- 更新唯一所有权拓扑：`AGENT.md`，将 `pkg/runtime/lifecycle` 标为已有 opt-in 核心，并注明 Router 接线与 native 未验收边界。

## 固定合同

- 成功 publish 后先取消旧页，再对旧页执行一次 `OnUnmount`，新页在首次业务 `Layout` 前 `OnMount`。
- 失败、合并、close-winning 路径不挂载；后台 `Shutdown` 只走 `CancelOnly`。
- `Owner.CancelOnly()` 只取消 owner 根；independent-parent 页面由 Router `Shutdown`/`Finalize` 负责；`Finalize` 不隐式调用 `Shutdown`。
- App `DestroyEvent -> LifecycleFinalize` 是最小一次性接线；native DestroyEvent/native race 仍未验收，发布级状态等待原生接受。

## 基线门禁

组合基线已记录 lifecycle/router/app 套件 `230 PASS / 0 FAIL / 1 existing SKIP`、MinGW race、CGO=0 build、vet、gofmt、docgate、diagdoc 与覆盖率结果。本次提交只改变文档，实际门禁命令在公共 `heavy.lock` 下重跑并保留原始记录；未启动窗口、未安装依赖、未修改主库、未 push 或 merge。
