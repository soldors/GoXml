package widgets

import (
	"testing"

	"gioui.org/layout"
	"gioui.org/unit"
)

// B1 单元/状态测试：显式更新契约（列配置校验、RowID 校验 fail-closed、
// 选择清理）、单次读取、空数据/零视口有界退路。真实指针/滚轮行为在
// datagrid_matrix_test.go（B2）。

func dgTestCols() []DataGridColumn {
	return []DataGridColumn{
		{Title: "名称", WidthDp: unit.Dp(100)},
		{Title: "大小", WidthDp: unit.Dp(100)},
		{Title: "修改时间", WidthDp: unit.Dp(100)},
	}
}

func dgTestRows(n int) []DataGridRow {
	rows := make([]DataGridRow, n)
	for i := range rows {
		rows[i] = DataGridRow{
			ID:    dgRowID(i),
			Cells: []string{dgRowID(i), "24 KB", "2026-11-09"},
		}
	}
	return rows
}

func dgRowID(i int) string {
	return "row-" + string(rune('a'+i%26)) + string(rune('0'+i/26%10)) + string(rune('0'+i/260%10)) + string(rune('0'+i/2600%10)) + string(rune('0'+i/26000%10))
}

func TestDGSetColumnsValidation(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatalf("合法列配置被拒绝: %v", err)
	}
	bad := []DataGridColumn{{Title: "a", WidthDp: unit.Dp(50)}, {Title: "b", WidthDp: 0}}
	if err := g.SetColumns(bad); err == nil {
		t.Fatal("零宽度列必须被拒绝")
	}
	if len(g.cols) != 3 {
		t.Fatalf("拒绝后必须保持旧配置，得到 %d 列", len(g.cols))
	}
	if err := g.SetColumns(nil); err != nil {
		t.Fatalf("空列集合应有界允许: %v", err)
	}
	if len(g.cols) != 0 {
		t.Fatal("空列集合应生效")
	}
	// 恢复
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
}

func TestDGSetRowsRejectsDuplicateAndEmptyIDs(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	rep := g.SetRows(dgTestRows(5))
	if !rep.OK || rep.ScannedIDs != 5 || g.Rows() != 5 {
		t.Fatalf("合法更新应成功：rep=%+v rows=%d", rep, g.Rows())
	}

	// 重复 ID：整体拒绝（fail-closed），数据保持原状。
	dup := []DataGridRow{
		{ID: "a", Cells: []string{"1", "2", "3"}},
		{ID: "b", Cells: []string{"1", "2", "3"}},
		{ID: "a", Cells: []string{"x", "2", "3"}},
	}
	rep = g.SetRows(dup)
	if rep.OK {
		t.Fatal("重复 ID 必须被整体拒绝")
	}
	if rep.ScannedIDs != 3 {
		t.Fatalf("校验扫描数应披露：得到 %d", rep.ScannedIDs)
	}
	if len(rep.Duplicates) == 0 || rep.Duplicates[0] != "a" {
		t.Fatalf("报告必须列明重复 ID：%v", rep.Duplicates)
	}
	if g.Rows() != 5 {
		t.Fatalf("拒绝后数据必须保持原状，得到 %d 行", g.Rows())
	}

	// 空 ID：与重复同样 fail-closed。
	empty := []DataGridRow{{ID: "", Cells: []string{"1", "2", "3"}}}
	rep = g.SetRows(empty)
	if rep.OK {
		t.Fatal("空 RowID 必须被拒绝")
	}
	if g.Rows() != 5 {
		t.Fatalf("拒绝后数据必须保持原状，得到 %d 行", g.Rows())
	}
}

func TestDGSetRowsClearsLostSelection(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(10)); !rep.OK {
		t.Fatal("初始化失败")
	}
	// 模拟选中 row-3（直接状态推进在 B2 由真实点击完成；这里用更新语义）。
	g.selID, g.selOK = dgRowID(3), true

	// 新数据不含该 ID：选择显式清除并声明。
	short := dgTestRows(2)
	rep := g.SetRows(short)
	if !rep.OK {
		t.Fatal("合法更新被拒绝")
	}
	if !rep.SelectionCleared {
		t.Fatal("选中行被删除必须显式声明选择清除")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("选择应已清除（不静默误选）")
	}

	// 重排（包含目标 ID）：重新选中后重排，选择保持（按 RowID，不按索引）。
	g.selID, g.selOK = dgRowID(3), true
	reordered := []DataGridRow{dgTestRows(10)[7], dgTestRows(10)[3]}
	if rep := g.SetRows(reordered); !rep.OK {
		t.Fatal("重排更新被拒绝")
	}
	id, ok := g.SelectedRowID()
	if !ok || id != dgRowID(3) {
		t.Fatalf("重排后选择必须仍按 RowID 保持，得到 %q ok=%v", id, ok)
	}
}

func TestDGClickedSingleRead(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("无点击时 Clicked 不应报告")
	}
	g.clickedID, g.clickedCol, g.clickedOK = "row-a", 2, true
	id, col, ok := g.Clicked()
	if !ok || id != "row-a" || col != 2 {
		t.Fatalf("单次读取应返回点击内容：id=%q col=%d ok=%v", id, col, ok)
	}
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("Clicked 必须读取即清空")
	}
}

func TestDGEmptyDataAndTinyViewportBounded(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(nil); !rep.OK {
		t.Fatal("空数据更新应成功")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	if st := g.LastFrame(); st.First != -1 || len(st.Accessed) != 0 || len(st.Drawn) != 0 {
		t.Fatalf("零行数据不应访问/绘制任何行：%+v", st)
	}

	// 视口小于一行 + 零行：有界（无 panic、零访问）。
	h.resize(300, 10)
	h.frame()
	if st := g.LastFrame(); len(st.Accessed) != 0 {
		t.Fatalf("零行+小视口不应访问行：%+v", st)
	}

	// 零视口。
	h.resize(0, 0)
	h.frame()
}

func TestDGFirstFrameAccessWindow(t *testing.T) {
	// B1 冒烟：首帧访问窗口 = 可见行 + 有界 overscan。
	const ( // 视口 300×200；行高 24；表头 24 → 体 176 → 可见 8 行
		rows = 100
	)
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(rows)); !rep.OK {
		t.Fatal("数据初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()

	st := g.LastFrame()
	if st.First != 0 || st.Last != min(8+dgOverscanRows, rows)-1 {
		t.Fatalf("首帧访问窗口应为 [0,%d)，得到 [%d,%d]", 8+dgOverscanRows-1, st.First, st.Last)
	}
	if len(st.Accessed) != st.Last-st.First+1 {
		t.Fatalf("访问披露应逐行：len=%d 窗口=%d", len(st.Accessed), st.Last-st.First+1)
	}
	if len(st.Drawn) != 8 {
		t.Fatalf("首帧应绘制 8 个可见行，得到 %d", len(st.Drawn))
	}
	for i, id := range st.Accessed {
		if id != dgRowID(i) {
			t.Fatalf("访问披露应按真实 RowID：[%d]=%q 期望 %q", i, id, dgRowID(i))
		}
	}
}
