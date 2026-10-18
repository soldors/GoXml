# TASK-ANALYZER-001：实现 ViewModel AST 静态扫描与校验

## 元信息
- 状态：Todo | 优先级：P0 | 预计：5h
- 分支：feature/TASK-ANALYZER-001
- 依赖：TASK-PARSER-002

## 1. 目标
在编译期检查 GXML 中的绑定路径、事件处理器和 ViewModel 声明，发现的问题以统一的源码位置诊断返回。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-ANALYZER-001-1 | 使用 Go AST/类型信息加载 ViewModel 包及声明 | go test -v ./internal/compiler/analyzer/... |
| TASK-ANALYZER-001-2 | 校验 Binding 路径存在且可访问 | go test -v -run TestBinding ./internal/compiler/analyzer/... |
| TASK-ANALYZER-001-3 | 校验事件方法签名并验证诊断源码位置 | go test -v -run TestEvent ./internal/compiler/analyzer/... |
