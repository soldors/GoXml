# TASK-WIDGET-001：构建标准自绘组件库

## 元信息
- 状态：Todo | 优先级：P1 | 预计：8h
- 分支：feature/TASK-WIDGET-001
- 依赖：TASK-STYLE-001

## 1. 目标
基于 Gio 自绘并提供可复用的 ContextMenu、Modal 和支持虚拟滚动的 DataGrid 组件。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-WIDGET-001-1 | 实现 ContextMenu 的定位、打开/关闭与输入事件 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-2 | 实现 Modal 的焦点、遮罩与关闭行为 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-3 | 实现 DataGrid 虚拟滚动、列布局与大数据集测试 | go test -v ./pkg/widgets/... |
