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
| TASK-WIDGET-001-3 | R27 基础范围通过：只读固定行高/显式列宽/RowID/虚拟滚动/单选，原生滚轮/选择/缩放已验；扩展交互未闭环 | go test -v ./pkg/widgets |

## 2026-11-10 R28-B 键盘导航语义裁决

R28-B 的 C-DG10..C-DG16 基础语义已批准：键盘功能保持显式 opt-in、默认关闭；支持 Up/Down/Home/End/PageUp/PageDown 的稳定 RowID 单选与纵向滚入可见区；`PageRows<=0` 采用当前可见行数；空 body 可更新选择但不滚动；Editor 获得焦点后网格停止消费按键；键盘不伪造 `Clicked()`。

TASK-WIDGET-001-3 仍为部分完成。DataGrid 键盘导航已在候选 `35b61b5` 完成 Windows/amd64 限定原生验收。运行 `candidate-native-grid-20261113-021654` 覆盖物理键连发、切换布局后的导航、IME 组合提交、125% 与 150% 间的切换及标题栏关闭。固定表头已通过像素回归和桌面复验。编辑、多选、排序/过滤、横向滚动、Tab 焦点环和键盘激活事件在本次批准范围之外。正式语义见 [ADR-0017](../../03-架构/adr/ADR-0017-DataGrid键盘导航语义.md) 与 [DataGrid 键盘语义表](../../../pkg/widgets/testdata/datagrid-keyboard/keyboard-semantics.md)。

## 2026-11-09 R27 批准范围

DataGrid 基础范围已批准、独立离屏/规模、组合/race和本机原生滚轮/选择/最大化还原/标题栏关闭通过，准确发布身份见 Git 与协调者凭据。真实 DPI 百分比、IME和跨平台仍未验；键盘、横向滚动、编辑、多选、排序不支持；主题/Shaper/字号改变需重建组件。微基准不代替整应用性能验收，-3 与整卡不自动关闭。见 [R27 范围与组合验收](../../05-开发/2026-11-09-R27-基础范围与组合验收.md)。

## 2026-11-08 R26 接线复核

基础范围已由负责人批准，公共接线和本机原生观察完成；六包覆盖率均达85%，组件Layout稳态零分配。完整Frame、整应用性能与整卡排除项分别登记；准确合入/推送身份由协调者交付与Git引用确认。
现行范围和复跑命令见 [R26 接线与验收](../../05-开发/2026-11-08-R26-Windows接线与验收.md)。

现行交互回归 `go test -v ./pkg/widgets`；稳态成本 `go test -run ^$ -bench ^BenchmarkReviewed ./pkg/widgets`。Layout 自身与含 input.Router.Frame 的分配分别报告，后者未达零分配。DataGrid 未实现，不关闭 -3。
