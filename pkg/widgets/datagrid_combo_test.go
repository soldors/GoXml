package widgets

import (
	"image"
	"reflect"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// R29-B S4 组合验证：横向滚动 + 排序 + 多选叠加后的语义一致性、双实例
// 多选焦点隔离、Editor 焦点交接下多选集保持、组合 opt-in 稳定帧零分配
// （见 datagrid_bench_test.go）。行几何：rowH=24、header=24、视口 300x200。

func comboSetup(t *testing.T, n int) (*DataGrid, *overlayHarness) {
	t.Helper()
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "ID", WidthDp: unit.Dp(200)},
		{Title: "Data", WidthDp: unit.Dp(200)},
		{Title: "Time", WidthDp: unit.Dp(200)},
	}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(sortTestRows(n)); !rep.OK {
		t.Fatal("setup failed")
	}
	g.EnableHorizontalScroll()
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	g.EnableMultiSelect()
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	return g, h
}

func TestComboAllOptInsCoherence(t *testing.T) {
	g, h := comboSetup(t, 100)

	// 多选：锚点 row2 + Ctrl row5。
	h.click(50, msRowY(2))
	msCtrlClick(h, 50, msRowY(5))
	want := []string{sortID(2), sortID(5)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("组合前置多选：%v", got)
	}

	// 横向滚动 100 后点击列 1（偏移下命中）：行/列解析仍正确。
	hsScrollX(h, 100)
	h.click(200, msRowY(3))
	if id, col, ok := g.Clicked(); !ok || id != sortID(3) || col != 1 {
		t.Fatalf("滚动后命中：id=%q col=%d ok=%v", id, col, ok)
	}
	// 普通点击已收拢多选（替换语义）。
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("滚动后普通点击收拢：%v", got)
	}

	// 排序（反序）：RowID 保留，键盘邻接跟新顺序，横向偏移不受影响。
	g.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
	h.frame()
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("排序后选择保留：%v", got)
	}
	if off := g.HScrollOffset(); off != 100 {
		t.Fatalf("排序不得影响横向偏移：%d", off)
	}
	// 新顺序下：普通点击 index 2（ROW-00097）重建锚点，Shift+点击
	// index 4（ROW-00095）→ {97,96,95}（排序前旧锚点 ROW-00003 仍有效，
	// 不重建会拉出跨全表的区间——正是锚点语义）。
	h.click(50, msRowY(2))
	msShiftClick(h, 50, msRowY(4))
	want = []string{sortID(97), sortID(96), sortID(95)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("排序后 Shift 范围：%v", got)
	}

	// 缩短到 50 行：{99,98,97} 全部越界 → 选择集清空，锚点清空。
	if rep := g.SetRows(sortTestRows(50)); !rep.OK {
		t.Fatal("shrink failed")
	}
	h.frame()
	if got := msIDs(t, g); len(got) != 0 {
		t.Fatalf("缩短后应清空：%v", got)
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("单选应同步清除")
	}
}

func TestComboDualInstanceMultiSelect(t *testing.T) {
	a, _ := comboSetup(t, 100)
	b, _ := comboSetup(t, 100)
	// 重建 harness：上/下半布局（B 带父级 offset 200）。
	h := newOverlayHarness(t, 300, 400)
	h.overlay = func(gtx layout.Context) {
		agtx := gtx
		agtx.Constraints = layout.Exact(image.Pt(300, 200))
		a.Layout(agtx, h.th)
		trans := op.Offset(image.Pt(0, 200)).Push(gtx.Ops)
		bgtx := gtx
		bgtx.Constraints = layout.Exact(image.Pt(300, 200))
		b.Layout(bgtx, h.th)
		trans.Pop()
	}
	h.frame()

	// A：Ctrl 多选 {1,4}，A 持焦。
	h.click(50, msRowY(1))
	msCtrlClick(h, 50, msRowY(4))
	h.frame()
	if !a.Focused() {
		t.Fatal("A 应持焦")
	}
	wantA := []string{sortID(1), sortID(4)}
	if got := msIDs(t, a); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("A 多选：%v", got)
	}

	// B：普通点击行 1 持焦；A 集合不受点击影响。
	h.click(50, 200+msRowY(1))
	b.Clicked()
	h.frame()
	if !b.Focused() {
		t.Fatal("B 应持焦")
	}
	if got := msIDs(t, a); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("B 点击不得影响 A：%v", got)
	}

	// B：Shift+Down 扩展 B 的集合；A 集合与焦点无关地保持。
	msKeyShift(h, key.NameDownArrow)
	if got := msIDs(t, b); !reflect.DeepEqual(got, []string{sortID(1), sortID(2)}) {
		t.Fatalf("B Shift 扩展：%v", got)
	}
	if got := msIDs(t, a); !reflect.DeepEqual(got, wantA) {
		t.Fatalf("B 键盘不得影响 A：%v", got)
	}

	// A 再次持焦后 Down：A 收拢为单行，B 集合保持。
	h.click(50, msRowY(1))
	a.Clicked()
	h.frame()
	kbKey(h, key.NameDownArrow)
	if got := msIDs(t, a); !reflect.DeepEqual(got, []string{sortID(2)}) {
		t.Fatalf("A 收拢：%v", got)
	}
	if got := msIDs(t, b); !reflect.DeepEqual(got, []string{sortID(1), sortID(2)}) {
		t.Fatalf("A 键盘不得影响 B：%v", got)
	}
}

func TestComboEditorHandoffMultiSelect(t *testing.T) {
	g, h := comboSetup(t, 100)

	// 网格持焦并建立多选 {0,1}（Down 后 Shift+Down）。
	h.click(50, msRowY(0))
	h.frame()
	kbKey(h, key.NameDownArrow) // Down：收拢为 row1，锚点移到 row1
	msKeyShift(h, key.NameDownArrow)
	want := []string{sortID(1), sortID(2)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("前置多选：%v", got)
	}

	// 真实可聚焦消费者：widget.Editor。焦点转移后网格停止消费按键，
	// 多选集保持不变。
	var ed widget.Editor
	th := h.th
	h.overlay = func(gtx layout.Context) {
		ed.Update(gtx)
		m := material.Editor(th, &ed, "hint")
		m.Layout(gtx)
		g.Layout(gtx, th)
	}
	h.frame()
	h.gtx.Execute(key.FocusCmd{Tag: &ed})
	h.frame()

	kbKey(h, key.NameDownArrow) // 网格未持焦，不得消费
	h.frame(key.EditEvent{Range: key.Range{}, Text: "a"})
	h.frame()
	if got := ed.Text(); got != "a" {
		t.Fatalf("Editor 应收到文本输入：%q", got)
	}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("Editor 持焦期间多选集不得变化：%v", got)
	}
	if g.Focused() {
		t.Fatal("网格应已失去焦点")
	}

	// 点击网格：焦点回网格，多选语义恢复（普通点击收拢）。
	h.click(50, msRowY(3))
	g.Clicked()
	h.frame()
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("焦点回网格后收拢：%v", got)
	}
	kbKey(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(4)}) {
		t.Fatalf("焦点回网格后 Down：%v", got)
	}
}
