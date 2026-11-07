# TASK-WIDGET-001：构建标准自绘组件库

## 元信息
- 状态：InProgress（R26 Windows 基础候选，门禁未齐、未发布） | 优先级：P1 | 预计：8h
- 分支：feature/TASK-WIDGET-001
- 依赖：TASK-STYLE-001

## 1. 目标
基于 Gio 自绘并提供可复用的 ContextMenu、Modal 和支持虚拟滚动的 DataGrid 组件。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-WIDGET-001-1 | 实现 ContextMenu 的定位、打开/关闭与输入事件 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-2 | 实现 Modal 的焦点、遮罩与关闭行为 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-3 | 未实现：DataGrid 虚拟滚动、列布局与大数据集测试 | 待实现后定义专属验收 |

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，隔离分支已实现；正式发布、原生与性能/覆盖率门禁仍待收口。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

现行交互回归 `go test -v ./pkg/widgets`；稳态成本 `go test -run ^$ -bench ^BenchmarkReviewed ./pkg/widgets`。Layout 自身与含 input.Router.Frame 的分配分别报告，后者未达零分配。DataGrid 未实现，不关闭 -3。
