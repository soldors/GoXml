# TASK-WIDGET-001：构建标准自绘组件库

## 元信息
- 状态：InProgress（R26 Windows 基础范围已验；整卡剩余范围未闭环） | 优先级：P1 | 预计：8h
- 分支：feature/TASK-WIDGET-001
- 依赖：TASK-STYLE-001

## 1. 目标
基于 Gio 自绘并提供可复用的 ContextMenu、Modal 和支持虚拟滚动的 DataGrid 组件。

## 2. Subtask 清单与验收命令
| 子任务ID | 内容 | 验收命令 |
|---|---|---|
| TASK-WIDGET-001-1 | 实现 ContextMenu 的定位、打开/关闭与输入事件 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-2 | 实现 Modal 的焦点、遮罩与关闭行为 | go test -v ./pkg/widgets/... |
| TASK-WIDGET-001-3 | R27 基础实现：只读固定行高/显式列宽/RowID/虚拟滚动/单选，组合验收中；原生及扩展交互未闭环 | go test -v ./pkg/widgets |

## 2026-11-09 R27 批准范围

DataGrid 基础范围已批准、独立离屏/规模技术复核通过，现处隔离组合验收；未合入、未发布。原生滚轮/显示器/DPI/IME 待验；键盘、横向滚动、编辑、多选、排序不支持；主题/Shaper/字号改变需重建组件。微基准不代替整应用性能验收，-3 与整卡不自动关闭。见 [R27 范围与组合验收](../../05-开发/2026-11-09-R27-基础范围与组合验收.md)。

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，公共接线和本机原生观察完成；六包覆盖率均达85%，组件Layout稳态零分配。完整Frame、整应用性能与整卡排除项分别登记；准确合入/推送身份由协调者交付与Git引用确认。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

现行交互回归 `go test -v ./pkg/widgets`；稳态成本 `go test -run ^$ -bench ^BenchmarkReviewed ./pkg/widgets`。Layout 自身与含 input.Router.Frame 的分配分别报告，后者未达零分配。DataGrid 未实现，不关闭 -3。
