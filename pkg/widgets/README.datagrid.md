# pkg/widgets DataGrid（TASK-WIDGET-001-3 候选组件说明）

只读、固定行高、垂直虚拟滚动的表格组件候选。**候选语义未经协调者裁决
不视为已批准**；策略与代价见 R27-B 交付 scope-matrix.md。

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
- 键盘：首版不支持键盘选择/滚动（不注册按键过滤器）。
- 本组件不读 ThemeRef、不新增 GXML 标签、不接线 CLI——公共接线无需改动。
