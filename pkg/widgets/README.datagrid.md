# pkg/widgets DataGrid（TASK-WIDGET-001-3 基础组件说明）

只读、固定行高、垂直虚拟滚动的表格基础范围已批准并通过 Windows 限定验收。
策略、限制和未证边界见 [R27 验收](../../docs/05-开发/2026-11-09-R27-基础范围与组合验收.md)，不据此关闭整卡或扩展交互。

## 最小用法（只构建不开窗；真实窗口验收由协调者组织）

```go
g := widgets.NewDataGrid(unit.Dp(24)) // 固定行高 24dp（表头默认同行高）
err := g.SetColumns([]widgets.DataGridColumn{
	{Title: "名称", WidthDp: unit.Dp(100)},
	{Title: "大小", WidthDp: unit.Dp(100)},
})
rep := g.SetRows([]widgets.DataGridRow{ // 显示字符串由调用方预生成
	{ID: "file-1", Cells: []string{"main.go", "12 KB"}},
	{ID: "file-2", Cells: []string{"go.mod", "1 KB"}},
})
// rep.OK=false 时更新整体被拒绝（重复/空 RowID），数据保持原状。

// 每帧：事件消费与绘制都在 Layout 内推进
g.Layout(gtx, th)

// 单次读取行点击（稳定 RowID + 列号）
if id, col, ok := g.Clicked(); ok { ... }
// 当前选中行（按 RowID；选中行被删除时显式清除并经 UpdateReport 声明）
if id, ok := g.SelectedRowID(); ok { ... }
```

- 数据所有权：`SetRows` 持有切片引用，返回后调用方不得再修改底层数据。
- 每帧只访问可见行+有界 overscan（2）并预热绘制宏缓存；`LastFrame()`
  白盒披露本帧访问/绘制 RowID 集合（底层数组复用，只读到下一帧）。
- 稳定帧 Layout 热路径 0 allocs/op（INV-PERF-2 口径，见
  `TestDGStableLayoutZeroAllocs`）；文本宏按（RowID, 列, 像素尺寸）LRU
  缓存于条目私有持久 Ops（ADR-0009 形态），上限 4096 条。
- 键盘：R28-B 基础语义已批准为**默认关闭、显式 opt-in** 的只读单选/纵向
  导航。调用 `EnableKeyboardNav(DataGridKeyboardConfig{})` 后，
  Up/Down/Home/End/PageUp/PageDown 按稳定 RowID 选择并将目标行滚入可见区；
  主键按压网格内任意位置可聚焦；焦点转给 Editor 等消费者后网格自动停用。
  详细语义、边界、成本见 `testdata/datagrid-keyboard/keyboard-semantics.md`
  与 [ADR-0017](../../docs/03-架构/adr/ADR-0017-DataGrid键盘导航语义.md)。
  原生键盘连发、IME、DPI 与发布级窗口验收仍待协调者完成。
- 横向滚动（R29-B S1 候选，默认关闭）：`EnableHorizontalScroll()` 显式开启
  后按轴独立滚动（滚轮 X=横向、Y=纵向），表头与表体共享同一列偏移，
  越界按列宽总和与视口宽钳制；`HScrollOffset()` 披露当前偏移。
  自定义 `Cell` 回调收到的矩形已含列偏移。关闭路径零新增监听。
- 排序（R29-B S2 候选）：`SortRows(less)` 在显式更新阶段用稳定排序重排
  当前数据——等值行保持原相对顺序，RowID 身份不变；排序后键盘邻接、
  点击命中与滚动钳制跟随新顺序。调用方也可在 `SetRows` 前预排序，效果
  等同。组件不自行发起排序，也不缓存比较规则。
- 多选（R29-B S3 候选，默认关闭）：`EnableMultiSelect()` 显式开启后
  Ctrl+点击切换选中、Shift+点击按锚点范围选中（替换式，锚点=最后一次
  非 Shift 选择的行）、普通点击收拢为单行。与键盘导航组合时
  Shift+方向键从锚点扩展、普通方向键收拢。选择集以稳定 RowID 表示：
  `SortRows` 重排天然保留；`SetRows` 缩短/清空后自动剔除失效 RowID 并
  在锚点失效时退化为单行。`SelectedRowIDs()` 按当前数据顺序返回全部
  选中行；多选关闭时返回单选结果（0 或 1 个）。多选模式下可视高亮由
  选择集驱动；单选模式行为与原实现完全一致。
- 本组件不读 ThemeRef、不新增 GXML 标签、不接线 CLI——公共接线无需改动。
