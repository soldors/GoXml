# TASK-DEVX-002：实现独立静态 UI 预览器

## 元信息
- 状态：Todo | 优先级：P1 | 预计：5h
- 分支：feature/TASK-DEVX-002
- 依赖：TASK-STYLE-001

## 1. 目标
提供独立的 GXML 预览命令，用于快速查看布局和主题效果，不启动目标应用的完整开发热重载进程。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-DEVX-002-1 | 实现 `preview <file.gxml>` 参数解析和输入校验 | go test -v ./cmd/goxml/preview/... |
| TASK-DEVX-002-2 | 将解析、宏展开与预览渲染串联 | go test -v -run TestPreview ./cmd/goxml/preview/... |
| TASK-DEVX-002-3 | 覆盖无效文件、诊断展示及预览退出清理 | go test -v ./cmd/goxml/preview/... |
