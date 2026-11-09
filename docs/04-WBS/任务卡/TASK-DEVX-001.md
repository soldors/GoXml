# TASK-DEVX-001：实现双进程防抖热重启与状态恢复

## 元信息
- 状态：InProgress（R26 Windows 基础范围已验；整卡剩余范围未闭环） | 优先级：P1 | 预计：6h
- 分支：feature/TASK-DEVX-001
- 依赖：TASK-LIFE-001

## 1. 目标
监听项目文件变更并防抖重建、重启应用；重启前后通过 JSON 暂存恢复可序列化的 ViewModel 状态。

## 1.1 规范依据
- Watch worker、UI dispatcher 和 Hydration 线程边界遵守 `docs/03-架构/运行时线程模型.md`。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-DEVX-001-1 | 实现文件监听、防抖合并与构建失败处理 | go test -v ./internal/devx/watch ./internal/devx/watchadapter |
| TASK-DEVX-001-2 | 实现自有子进程有界停止、超时清理与重启（优雅状态保存未实现） | go test -v -race ./internal/devx/watch ./internal/devx/watchadapter |
| TASK-DEVX-001-3 | R27 基础实现：显式 DTO 状态 JSON/校验/原子暂存/Watch opt-in 恢复；组合验收中，通用应用控制通道及 CLI 自动恢复未交付 | go test -v ./pkg/runtime/hydration ./internal/devx/hydration ./internal/devx/watch |

## 2026-11-09 R27 批准范围

基础协议和程序化 Hook 已批准、作者独立技术通过，现处协调者隔离组合验收；未合入、未发布。捕获/应用在各自所有者串行点，worker 仅搬不可变 DTO；默认关闭，失败保旧，仍按 stop-old→start-new。通用应用端控制通道、CLI 自动恢复参数、通用 Lifecycle 和原生整体验收不在已完成范围，-3 与整卡不因此关闭。见 [R27 范围与组合验收](../../05-开发/2026-11-09-R27-基础范围与组合验收.md)。

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，公共接线和本机原生观察完成；六包覆盖率均达85%，组件Layout稳态零分配。完整Frame、整应用性能与整卡排除项分别登记；准确合入/推送身份由协调者交付与Git引用确认。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

已修正原表中的历史目录，`cmd/goxml/watch` 不是当前实现目录。现行核心与适配器验收为 `go test -v ./internal/devx/watch ./internal/devx/watchadapter`，公共入口为 `go test -v -run ^TestDevWatch ./internal/cli`。采用成功构建后先停旧再起新；未实现优雅状态保存/恢复，不关闭 -3。
