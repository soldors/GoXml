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
- 本组件不读 ThemeRef、不新增 GXML 标签、不接线 CLI——公共接线无需改动。
