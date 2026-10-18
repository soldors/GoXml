# TASK-STATE-001：实现编译期 AOT 浅对比 Diff 代码生成与 MacroOp 局部失效

## 元信息
- 状态：Todo | 优先级：P0 | 预计：6h
- 分支：feature/TASK-STATE-001
- 依赖：TASK-CODEGEN-001

## 1. 目标
生成零反射的 checkStateDiff() 硬编码字段对比方法，仅重绘受影响子树，未变动子树直接复用 op.CallOp。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-STATE-001-1 | 自动生成 oldVM 状态快照与组件 op.CallOp 字段 | go test -v -run TestGenerateShadowStruct ./internal/compiler/codegen |
| TASK-STATE-001-2 | 生成硬编码字段对比逻辑与局部重绘触发分支 | go test -v -run TestAOTStateDiff ./internal/compiler/codegen |
| TASK-STATE-001-3 | 源码 AST 断言：严禁包含 import "reflect" | go test -v -run TestZeroReflectionInGeneratedCode ./internal/compiler/codegen |
