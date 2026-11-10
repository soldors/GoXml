# 2026-11-10 R28 批准范围与文档同步

本轮在组合基线 `474dfb2` 上完成负责人已批准范围的正式文档化。Hydration、DataGrid 键盘导航和 Lifecycle Owner/Router 三条线均为文档与任务卡收口，不修改生产代码。

## 已批准范围

- Hydration：开发态 Windows 首发使用 `127.0.0.1:0` 回环 TCP、显式 DTO、显式 opt-in/默认关闭；状态目录与大小、超时、并发限制为可配置的开发态推荐默认；代次不符返回 `ErrMismatch` 且不返回 payload。
- DataGrid：批准 C-DG10..16 当前键盘语义，保持功能默认关闭；原生键盘连发、IME、DPI 和发布级别仍需后续验收。
- Lifecycle：批准 opt-in Owner/Router 页面生命周期合同；成功发布后旧页取消并只卸载一次，首个业务 Layout 前挂载新页；`CancelOnly` 只取消 owner 根，页面 Context 由 Router `Shutdown`/`Finalize` 负责，Finalize 不隐式 Shutdown。

## 组合与边界

三项文档提交已合入 R28 组合树。组合树此前已通过全量测试、受影响包 race、CGO=0 build、vet、gofmt、docgate 和 diagdoc；本轮文档合入后仍需由协调者复跑最终门禁。原生窗口、真实 DestroyEvent/native race、跨平台运行、IME/DPI、发布级性能与视觉验收仍不视为完成。

本轮不修改共享索引和 CHANGELOG 之外的历史证据，不把负责人批准范围扩大为整张任务卡完成或正式跨平台发布。

## 本轮提交登记

为满足 dev-log 提交覆盖门禁，本轮文档链登记以下真实提交：`8a78ed8`、`e96734a`、`edd7cc0`、`b46168f`、`26cc576`、`f05de25`、`9d4904b`。当前文档同步提交和后续合并提交由本文件所在提交链覆盖；组合树此前的代码与测试提交仍由既有 R28 协调者日志覆盖。
