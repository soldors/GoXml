# TASK-PARSER-002：实现强类型属性解析器

## 元信息
- 状态：Todo | 优先级：P0 | 预计：3h
- 分支：feature/TASK-PARSER-002
- 依赖：TASK-PARSER-001

## 1. 目标
将 GXML 属性解析为契约定义的强类型值，支持 EdgeInsets、颜色、绑定和事件属性，并为非法输入返回带源码位置的诊断。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-PARSER-002-1 | 实现 EdgeInsets 的 1/2/4 值解析及非法值诊断 | go test -v -run TestEdgeInsets ./internal/compiler/parser/... |
| TASK-PARSER-002-2 | 实现十六进制颜色与主题引用解析 | go test -v -run TestColor ./internal/compiler/parser/... |
| TASK-PARSER-002-3 | 实现 Binding/Event 属性解析与错误位置测试 | go test -v -run 'Test(Binding|Event)' ./internal/compiler/parser/... |
