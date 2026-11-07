# TASK-DEVX-001：实现双进程防抖热重启与状态恢复

## 元信息
- 状态：InProgress（R26 Windows 基础候选，门禁未齐、未发布） | 优先级：P1 | 预计：6h
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
| TASK-DEVX-001-2 | 实现子进程优雅退出、超时清理与重启 | go test -v -race ./internal/devx/watch ./internal/devx/watchadapter |
| TASK-DEVX-001-3 | 未实现：状态 JSON 暂存、恢复及损坏文件处理 | 待实现后定义专属验收 |

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，隔离分支已实现；正式发布、原生与性能/覆盖率门禁仍待收口。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

已修正原表中的历史目录，`cmd/goxml/watch` 不是当前实现目录。现行核心与适配器验收为 `go test -v ./internal/devx/watch ./internal/devx/watchadapter`，公共入口为 `go test -v -run ^TestDevWatch ./internal/cli`。采用成功构建后先停旧再起新；未实现优雅状态保存/恢复，不关闭 -3。
