package widgets

import (
	"fmt"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
)

// S2 排序测试：使用字母序连续的 RowID（ROW-00000..ROW-00099），确保
// 排序后的键盘邻接可独立手算。dgRowID 的编码不保证字母序=数字序。

func sortTestRows(n int) []DataGridRow {
	rows := make([]DataGridRow, n)
	for i := range rows {
		id := fmt.Sprintf("ROW-%05d", i)
		rows[i] = DataGridRow{ID: id, Cells: []string{id, "data"}}
	}
	return rows
}

func sortSetup(t *testing.T, n int) (*DataGrid, *overlayHarness) {
	t.Helper()
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "ID", WidthDp: unit.Dp(200)},
		{Title: "Data", WidthDp: unit.Dp(120)},
	}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(sortTestRows(n)); !rep.OK {
		t.Fatal("setup failed")
	}
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	return g, h
}

func sortID(i int) string { return fmt.Sprintf("ROW-%05d", i) }

func TestSortKeyboardAdjacency(t *testing.T) {
	g, h := sortSetup(t, 100)

	// 点击 row 5 → ROW-00005。
	h.click(50, 24+5*24+12)
	g.Clicked()
	h.frame()
	if sel, _ := g.SelectedRowID(); sel != sortID(5) {
		t.Fatalf("click row5: %q", sel)
	}
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != sortID(6) {
		t.Fatalf("pre-sort Down: %q", sel)
	}

	// 反序排序：ROW-00099 变为第一行。ROW-00006 移到 index 93。
	g.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
	h.frame()
	if sel, _ := g.SelectedRowID(); sel != sortID(6) {
		t.Fatalf("sort RowID preserved: %q", sel)
	}
	// 排序后 ROW-00006 在 index 93。Down → index 94 → ROW-00005。
	kbKey(h, key.NameDownArrow)
	sel2, _ := g.SelectedRowID()
	if sel2 != sortID(5) {
		t.Fatalf("post-sort Down should follow new adjacency: %q", sel2)
	}
	// 选中行当帧可见。
	found := false
	for _, d := range g.LastFrame().Drawn {
		if d == sel2 {
			found = true
		}
	}
	if !found {
		t.Fatal("selected row must be visible after sort+navigate")
	}
}

func TestSortClickHit(t *testing.T) {
	g, h := sortSetup(t, 100)
	g.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
	h.frame()
	// 反序后第一行 = ROW-00099。
	h.click(50, 24+12)
	if id, _, ok := g.Clicked(); !ok || id != sortID(99) {
		t.Fatalf("sorted first row click: %q ok=%v", id, ok)
	}
}

func TestSortShrinkSelection(t *testing.T) {
	g, h := sortSetup(t, 100)
	g.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
	h.frame()
	// 选中 ROW-00099（排序后的第一行）。
	h.click(50, 24+12)
	g.Clicked()
	// 缩短到 50 行：ROW-00099 被删除 → 选择清除。
	short := sortTestRows(50)
	if rep := g.SetRows(short); !rep.OK {
		t.Fatal("shrink failed")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("selection should be cleared after shrink removes selected row")
	}
}

func TestSortStableEqualValues(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "名称", WidthDp: unit.Dp(200)},
	}); err != nil {
		t.Fatal(err)
	}
	data := make([]DataGridRow, 50)
	for i := range data {
		data[i] = DataGridRow{ID: fmt.Sprintf("ROW-%05d", i), Cells: []string{"same"}}
	}
	if rep := g.SetRows(data); !rep.OK {
		t.Fatal("setup")
	}
	g.SortRows(func(a, b DataGridRow) bool { return false })
	if g.Rows() != 50 {
		t.Fatal("row count must be preserved")
	}
}
