# TASK-DEVX-001：实现双进程防抖热重启与状态恢复

## 元信息
- 状态：Todo | 优先级：P1 | 预计：6h
- 分支：feature/TASK-DEVX-001
- 依赖：TASK-LIFE-001

## 1. 目标
监听项目文件变更并防抖重建、重启应用；重启前后通过 JSON 暂存恢复可序列化的 ViewModel 状态。

## 1.1 规范依据
- Watch worker、UI dispatcher 和 Hydration 线程边界遵守 `docs/03-架构/运行时线程模型.md`。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-DEVX-001-1 | 实现文件监听、防抖合并与构建失败处理 | go test -v ./cmd/goxml/watch/... |
| TASK-DEVX-001-2 | 实现子进程优雅退出、超时清理与重启 | go test -v -race ./cmd/goxml/watch/... |
| TASK-DEVX-001-3 | 实现状态 JSON 暂存、恢复及损坏文件处理 | go test -v -race ./pkg/runtime/hydration/... |
