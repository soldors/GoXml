package widgets

import (
	"fmt"
	"reflect"
	"testing"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
)

// R29-B S3 多选测试：Ctrl 切换、Shift 范围（锚点）、键盘 Shift 扩展、
// 重排保留（按 RowID）、缩短失效、Ctrl+点击聚焦、默认单选不受影响。
// 行几何同 sortSetup：rowH=24，header=24，视口 300x200。

func msSetup(t *testing.T, n int) (*DataGrid, *overlayHarness) {
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
	g.EnableMultiSelect()
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	return g, h
}

// msModsClick 带修饰键的成对 Press/Release（同帧入队，Gio 窗口同款）。
func msModsClick(h *overlayHarness, x, y int, mods key.Modifiers) {
	h.frame(
		pointer.Event{
			Kind: pointer.Press, Source: pointer.Mouse,
			Buttons: pointer.ButtonPrimary, Modifiers: mods,
			Position: f32Pt(x, y),
		},
		pointer.Event{
			Kind: pointer.Release, Source: pointer.Mouse,
			Modifiers: mods, Position: f32Pt(x, y),
		},
	)
}

func msCtrlClick(h *overlayHarness, x, y int)  { msModsClick(h, x, y, key.ModCtrl) }
func msShiftClick(h *overlayHarness, x, y int) { msModsClick(h, x, y, key.ModShift) }

func msKeyShift(h *overlayHarness, name key.Name) {
	h.frame(key.Event{Name: name, State: key.Press, Modifiers: key.ModShift})
}

func msRowY(i int) int { return 24 + i*24 + 12 }

func msIDs(t *testing.T, g *DataGrid) []string {
	t.Helper()
	ids := g.SelectedRowIDs()
	if ids == nil {
		ids = []string{}
	}
	return ids
}

func TestMSOptInDefaultOff(t *testing.T) {
	// 默认（未开启多选）：单选语义，SelectedRowIDs 恰为 0/1 个。
	g, h := sortSetup(t, 100)
	if g.MultiSelectEnabled() {
		t.Fatal("默认必须为单选")
	}
	h.click(50, msRowY(3))
	if ids := g.SelectedRowIDs(); len(ids) != 1 || ids[0] != sortID(3) {
		t.Fatalf("默认单选 SelectedRowIDs=%v", ids)
	}
	h.click(50, msRowY(5))
	if ids := g.SelectedRowIDs(); len(ids) != 1 || ids[0] != sortID(5) {
		t.Fatalf("默认单选普通点击只选一行：%v", ids)
	}

	// 开启多选：现有单选迁移进选择集。
	g.EnableMultiSelect()
	if !g.MultiSelectEnabled() {
		t.Fatal("EnableMultiSelect 未生效")
	}
	if ids := g.SelectedRowIDs(); len(ids) != 1 || ids[0] != sortID(5) {
		t.Fatalf("开启时单选应迁移进集合：%v", ids)
	}
	// Ctrl+点击在默认单选下只改单选（不产生多集合）。
	g.DisableMultiSelect()
	msCtrlClick(h, 50, msRowY(6))
	if ids := g.SelectedRowIDs(); len(ids) != 1 || ids[0] != sortID(6) {
		t.Fatalf("单选模式忽略修饰键：%v", ids)
	}
}

func TestMSCtrlClickToggle(t *testing.T) {
	g, h := msSetup(t, 100)

	h.click(50, msRowY(3))
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("普通点击：%v", got)
	}
	// Ctrl+点击 row5 → {3,5}。
	msCtrlClick(h, 50, msRowY(5))
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3), sortID(5)}) {
		t.Fatalf("Ctrl+点击后：%v", got)
	}
	// Ctrl+点击 row3 再切回 → {5}。
	msCtrlClick(h, 50, msRowY(3))
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(5)}) {
		t.Fatalf("Ctrl 切换撤销后：%v", got)
	}
	// Ctrl+点击 row5 再切回 → 空集。
	msCtrlClick(h, 50, msRowY(5))
	if got := msIDs(t, g); len(got) != 0 {
		t.Fatalf("全撤销后应为空：%v", got)
	}
	// Clicked() 不被修饰键破坏（Ctrl+点击仍上报点击）。
	msCtrlClick(h, 50, msRowY(5))
	if id, _, ok := g.Clicked(); !ok || id != sortID(5) {
		t.Fatalf("Ctrl+点击 Clicked: %q ok=%v", id, ok)
	}
}

func TestMSShiftClickRange(t *testing.T) {
	g, h := msSetup(t, 100)

	// 锚点 row2；Shift+点击 row6 → {2..6}。
	h.click(50, msRowY(2))
	msShiftClick(h, 50, msRowY(6))
	want := []string{sortID(2), sortID(3), sortID(4), sortID(5), sortID(6)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("Shift 范围选中：%v", got)
	}
	// 反向 Shift+点击 row0：锚点仍是 row2 → {0,1,2}。
	msShiftClick(h, 50, msRowY(0))
	want = []string{sortID(0), sortID(1), sortID(2)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("反向 Shift 范围：%v", got)
	}
	// 普通点击收拢为单行（锚点移到该行）。
	h.click(50, msRowY(6))
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(6)}) {
		t.Fatalf("普通点击收拢：%v", got)
	}
}

func TestMSKeyboardShiftExtend(t *testing.T) {
	g, h := msSetup(t, 100)
	// 焦点经表头按压获得（不产生选择）。
	h.frame()
	kbFocus(h)

	// Down（初始无选择）→ 选中可见首行并成为锚点。
	kbKey(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(0)}) {
		t.Fatalf("初始 Down：%v", got)
	}
	// Shift+Down ×3 → {0..3}（锚点保持 row0）。
	for i := 0; i < 3; i++ {
		msKeyShift(h, key.NameDownArrow)
	}
	want := []string{sortID(0), sortID(1), sortID(2), sortID(3)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("Shift+Down 扩展：%v", got)
	}
	// Shift+Up 回撤一步 → {0..2}。
	msKeyShift(h, key.NameUpArrow)
	want = want[:3]
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("Shift+Up 回撤：%v", got)
	}
	// 普通 Down 收拢为单行（锚点移到新行）。
	kbKey(h, key.NameDownArrow)
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("普通 Down 收拢：%v", got)
	}
}

func TestMSCtrlClickFocuses(t *testing.T) {
	// Ctrl+点击自身请求焦点（Press 路径与普通点击一致），随后键盘扩展
	// 无需额外聚焦步骤即可生效。
	g, h := msSetup(t, 100)
	msCtrlClick(h, 50, msRowY(5))
	h.frame() // 焦点命令落地帧
	if !g.Focused() {
		t.Fatal("Ctrl+点击后网格应持焦")
	}
	msKeyShift(h, key.NameDownArrow)
	want := []string{sortID(5), sortID(6)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ctrl+点击后 Shift+Down：%v", got)
	}
}

func TestMSSortPreservesSelection(t *testing.T) {
	g, h := msSetup(t, 100)

	h.click(50, msRowY(3))
	msCtrlClick(h, 50, msRowY(5))
	// 反序排序：RowID 身份不变 → 选择集按 RowID 保留。
	g.SortRows(func(a, b DataGridRow) bool { return a.ID > b.ID })
	h.frame()
	// 新数据顺序：ROW-00005 在 ROW-00003 之前。
	want := []string{sortID(5), sortID(3)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("排序后选择集：%v", got)
	}
	// Shift+点击跟随新顺序：锚点 ROW-00005（视觉 index 94）… 普通点击
	// 视觉 index 2（ROW-00097）建立新锚点，Shift+点击 index 4（ROW-00095）。
	h.click(50, msRowY(2))
	msShiftClick(h, 50, msRowY(4))
	want = []string{sortID(97), sortID(96), sortID(95)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("排序后 Shift 范围：%v", got)
	}
}

func TestMSShrinkInvalidates(t *testing.T) {
	g, h := msSetup(t, 100)

	h.click(50, msRowY(2))
	msCtrlClick(h, 50, msRowY(5))
	msCtrlClick(h, 50, msRowY(6)) // 锚点在此（最后 Ctrl；仅前 7 行可见）
	// 缩短到 6 行：ROW-00006（锚点行）消失 → 幸存者 {2,5}，单选清除。
	if rep := g.SetRows(sortTestRows(6)); !rep.OK {
		t.Fatal("shrink failed")
	}
	h.frame()
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("单选（锚点行被删）应清除")
	}
	want := []string{sortID(2), sortID(5)}
	if got := msIDs(t, g); !reflect.DeepEqual(got, want) {
		t.Fatalf("缩短后幸存者：%v", got)
	}
	// 锚点失效后 Shift+点击退化为单行。
	msShiftClick(h, 50, msRowY(3))
	if got := msIDs(t, g); !reflect.DeepEqual(got, []string{sortID(3)}) {
		t.Fatalf("锚点失效退化：%v", got)
	}
}

func TestMSEmptyGridAndDisable(t *testing.T) {
	g := NewDataGrid(unit.Dp(24))
	if err := g.SetColumns([]DataGridColumn{{Title: "A", WidthDp: unit.Dp(200)}}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(nil); !rep.OK {
		t.Fatal("setup")
	}
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	g.EnableMultiSelect()
	if got := g.SelectedRowIDs(); len(got) != 0 {
		t.Fatalf("空表选择集：%v", got)
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	msCtrlClick(h, 50, msRowY(0)) // 空 body 命中空白带
	if got := g.SelectedRowIDs(); len(got) != 0 {
		t.Fatalf("空表点击不应产生选择：%v", got)
	}
	// 关闭多选：集合清空，回到单选语义。
	g.EnableMultiSelect()
	g.DisableMultiSelect()
	if got := g.SelectedRowIDs(); got != nil {
		t.Fatalf("关闭后不应有集合：%v", got)
	}
	if !reflect.DeepEqual(fmt.Sprint(g.MultiSelectEnabled()), "false") {
		t.Fatal("关闭状态错误")
	}
}
