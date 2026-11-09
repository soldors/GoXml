package widgets

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
)

// B2 真实离屏行为矩阵：input.Router 指针/滚轮/按键驱动（无原生窗口），
// 逐帧精确断言访问/绘制 RowID 集合与事件身份（RowID+列），不用计数器
// 以 counter>0、截图或源码 grep 代替行为。

const (
	dgMatrixViewportW = 300
	dgMatrixViewportH = 200
	dgMatrixRowH      = 24
	dgMatrixHeaderH   = 24
)

// dgExpectedAccess 计算期望访问窗口 [first,last]（可见+overscan）与可见行数。
func dgExpectedAccess(scrollOff, rows int) (first, last, drawn int) {
	bodyH := dgMatrixViewportH - dgMatrixHeaderH
	vis0 := 0
	if bodyH > 0 {
		vis0 = scrollOff / dgMatrixRowH
	}
	vis1 := rows
	if end := (scrollOff + bodyH + dgMatrixRowH - 1) / dgMatrixRowH; end < vis1 {
		vis1 = end
	}
	acc0 := vis0 - dgOverscanRows
	if acc0 < 0 {
		acc0 = 0
	}
	acc1 := vis1 + dgOverscanRows
	if acc1 > rows {
		acc1 = rows
	}
	return acc0, acc1 - 1, vis1 - vis0
}

// dgAssertWindow 逐项断言访问/绘制集合的真实 RowID。
func dgAssertWindow(t *testing.T, g *DataGrid, rows int, where string) {
	t.Helper()
	st := g.LastFrame()
	first, last, drawn := dgExpectedAccess(g.ScrollOffset(), rows)
	gotFirst, gotLast := st.First, st.Last
	if rows == 0 {
		first, last = -1, -1
		drawn = 0
		if len(st.Accessed) != 0 || len(st.Drawn) != 0 {
			t.Fatalf("%s：零行数据不应访问/绘制任何行：acc=%d drawn=%d",
				where, len(st.Accessed), len(st.Drawn))
		}
		return
	}
	if gotFirst != first || gotLast != last {
		t.Fatalf("%s：访问窗口应为 [%d,%d]，得到 [%d,%d]（scrollOff=%d）",
			where, first, last, gotFirst, gotLast, g.ScrollOffset())
	}
	if len(st.Accessed) != last-first+1 {
		t.Fatalf("%s：访问集合大小 %d ≠ 窗口 %d", where, len(st.Accessed), last-first+1)
	}
	for i, id := range st.Accessed {
		want := dgRowID(first + i)
		if id != want {
			t.Fatalf("%s：访问[%d]=%q ≠ %q（身份必须逐行精确）", where, i, id, want)
		}
	}
	if len(st.Drawn) != drawn {
		t.Fatalf("%s：绘制行数 %d ≠ 可见 %d", where, len(st.Drawn), drawn)
	}
	for i, id := range st.Drawn {
		vis0 := 0
		if bodyH := dgMatrixViewportH - dgMatrixHeaderH; bodyH > 0 {
			vis0 = g.ScrollOffset() / dgMatrixRowH
		}
		if id != dgRowID(vis0+i) {
			t.Fatalf("%s：绘制[%d]=%q 与可见行不符", where, i, id)
		}
	}
}

func dgNewGrid(t *testing.T, rows int) (*DataGrid, *overlayHarness) {
	t.Helper()
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "名称", WidthDp: unit.Dp(100)},
		{Title: "大小", WidthDp: unit.Dp(100)},
		{Title: "时间", WidthDp: unit.Dp(100)},
	}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(rows)); !rep.OK {
		t.Fatalf("数据初始化失败: %+v", rep)
	}
	h := newOverlayHarness(t, dgMatrixViewportW, dgMatrixViewportH)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	return g, h
}

func dgWheel(h *overlayHarness, delta int) {
	h.frame(pointer.Event{
		Kind:     pointer.Scroll,
		Source:   pointer.Mouse,
		Position: f32Pt(150, 100),
		Scroll:   f32.Point{X: 0, Y: float32(delta)},
	})
}

// 首部/中部/尾部三个位置：访问窗口只与可见区+overscan 相关。
func TestDGMatrixAccessWindowsFirstMiddleLast(t *testing.T) {
	for _, n := range []int{10000, 100000} {
		t.Run(dgScaleName(n), func(t *testing.T) {
			g, h := dgNewGrid(t, n)

			dgAssertWindow(t, g, n, "首部")

			// 滚到中部：wheel 正向若干次（每帧真实步进，无假活性）。
			for i := 0; i < 10; i++ {
				dgWheel(h, 500)
				dgAssertWindow(t, g, n, "中部步进")
			}
			if g.ScrollOffset() != 5000 {
				t.Fatalf("10×500px 滚动后偏移应为 5000，得到 %d", g.ScrollOffset())
			}

			// 一步到尾：大 delta 被 ScrollRange 钳制为 maxOff。
			dgWheel(h, 10_000_000)
			if g.ScrollOffset() != n*dgMatrixRowH-(dgMatrixViewportH-dgMatrixHeaderH) {
				t.Fatalf("滚到底偏移不符：%d", g.ScrollOffset())
			}
			dgAssertWindow(t, g, n, "尾部")

			// 尾部身份：最后访问的是最后一行。
			st := g.LastFrame()
			if st.Accessed[len(st.Accessed)-1] != dgRowID(n-1) {
				t.Fatalf("尾部访问应含最后一行：%q", st.Accessed[len(st.Accessed)-1])
			}

			// 回到顶：负向大 delta 钳制为 0。
			dgWheel(h, -10_000_000)
			if g.ScrollOffset() != 0 {
				t.Fatalf("滚到顶偏移应为 0，得到 %d", g.ScrollOffset())
			}
			dgAssertWindow(t, g, n, "回顶")

			// 10k 与 100k 同视口的单帧访问集合大小必须一致（INV-PERF-3 结构守卫）：
			// 顶部 acc0 在 0 钳制 → 8 可见 + 2 下方 overscan = 10。
			if n == 100000 {
				st := g.LastFrame()
				if len(st.Accessed) != 8+dgOverscanRows || len(st.Drawn) != 8 {
					t.Fatalf("十万行访问/绘制规模应与万行一致：acc=%d drawn=%d", len(st.Accessed), len(st.Drawn))
				}
			}
		})
	}
}

func dgScaleName(n int) string {
	if n >= 100000 {
		return "100k"
	}
	return "10k"
}

// 点击身份：同帧点击给期望 RowID+列；滚动后同位置点击给新行身份。
func TestDGMatrixClickIdentityFollowsGeometry(t *testing.T) {
	g, h := dgNewGrid(t, 10000)

	// 首次点击：可见第 2 行（y=24+24+12=60）、第 1 列（x=50）。
	h.click(50, 60)
	id, col, ok := g.Clicked()
	if !ok || id != dgRowID(1) || col != 0 {
		t.Fatalf("首次点击身份不符：id=%q col=%d ok=%v", id, col, ok)
	}
	if sel, ok := g.SelectedRowID(); !ok || sel != dgRowID(1) {
		t.Fatalf("点击应选中该行：sel=%q ok=%v", sel, ok)
	}

	// 向下一行（wheel +24）后同位置点击：身份必须是新行。
	dgWheel(h, dgMatrixRowH)
	h.click(50, 60)
	id, col, ok = g.Clicked()
	if !ok || id != dgRowID(2) || col != 0 {
		t.Fatalf("滚动后同位置点击应给新行身份：id=%q col=%d ok=%v", id, col, ok)
	}

	// 第 2 列（x=150）。
	h.click(150, 60)
	id, col, _ = g.Clicked()
	if id != dgRowID(2) || col != 1 {
		t.Fatalf("列身份不符：id=%q col=%d", id, col)
	}
}

// 跨帧 Press/Release 与 Cancel：真实分帧事件。
func TestDGMatrixCrossFramePressReleaseAndCancel(t *testing.T) {
	g, h := dgNewGrid(t, 10000)

	// 帧内按压不产生点击。
	h.press(50, 60)
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("仅 Press 不应产生点击")
	}
	// 下一帧释放（Buttons=0 真实形态）：点击成立。
	h.release(50, 60)
	id, _, ok := g.Clicked()
	if !ok || id != dgRowID(1) {
		t.Fatalf("跨帧 Press/Release 应给 RowID：%q ok=%v", id, ok)
	}

	// press→cancel：不产生点击，不改选择。
	selBefore, _ := g.SelectedRowID()
	h.press(50, 84)
	h.frame(pointer.Event{Kind: pointer.Cancel, Source: pointer.Mouse})
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("被取消的按压不应产生点击")
	}
	if sel, _ := g.SelectedRowID(); sel != selBefore {
		t.Fatalf("取消不应改变选择：%q → %q", selBefore, sel)
	}
	// cancel 后新点击仍可用。
	h.click(50, 84)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(2) {
		t.Fatalf("取消后新点击应可用：%q ok=%v", id, ok)
	}
}

// 重排：点击身份跟随本帧几何；选择按 RowID 保持。
func TestDGMatrixReorderIdentity(t *testing.T) {
	g, h := dgNewGrid(t, 10000)

	// 选中 index 5 的行。
	h.click(50, dgMatrixHeaderH+5*24+12)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(5) {
		t.Fatalf("选中 index5 失败：%q", id)
	}

	// 重排：把 dgRowID(5) 移到 index 0，原 index0 移到 index 5。
	rows := dgTestRows(10000)
	rows[0], rows[5] = rows[5], rows[0]
	if rep := g.SetRows(rows); !rep.OK {
		t.Fatal("重排更新被拒绝")
	}

	// 选择按 RowID 保持。
	if sel, ok := g.SelectedRowID(); !ok || sel != dgRowID(5) {
		t.Fatalf("重排后选择应按 RowID 保持：%q ok=%v", sel, ok)
	}

	// 重排后 index 0 处点击：身份是 dgRowID(5)（本帧几何解析）。
	h.frame()
	h.click(50, dgMatrixHeaderH+12)
	id, _, ok := g.Clicked()
	if !ok || id != dgRowID(5) {
		t.Fatalf("重排后首行点击身份应为新驻留行：%q ok=%v", id, ok)
	}
}

// 数据缩短/清空：选择显式清除、偏移重钳制、有界访问。
func TestDGMatrixShortenAndClear(t *testing.T) {
	g, h := dgNewGrid(t, 10000)

	// 滚到底并点击：身份按本帧几何解析（y=表头+12 → 行=(12+偏移)/行高）。
	dgWheel(h, 10_000_000)
	h.click(50, dgMatrixHeaderH+12)
	id, _, _ := g.Clicked()
	wantRow := (12 + g.ScrollOffset()) / dgMatrixRowH
	if id != dgRowID(wantRow) {
		t.Fatalf("底部点击身份不符：got=%q want=%q（行 %d）", id, dgRowID(wantRow), wantRow)
	}

	// 缩短到 50 行：选中行丢失 → 显式清除；偏移重钳制。
	rep := g.SetRows(dgTestRows(50))
	if !rep.OK || !rep.SelectionCleared {
		t.Fatalf("缩短更新应声明选择清除：%+v", rep)
	}
	h.frame()
	if g.ScrollOffset() > 50*24-(dgMatrixViewportH-dgMatrixHeaderH) {
		t.Fatalf("缩短后偏移必须重钳制：%d", g.ScrollOffset())
	}
	dgAssertWindow(t, g, 50, "缩短后")
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("选择应已清除")
	}

	// 清空：零访问/零绘制，点击无结果。
	if rep := g.SetRows(nil); !rep.OK {
		t.Fatal("清空更新应成功")
	}
	h.frame()
	dgAssertWindow(t, g, 0, "清空后")
	h.click(150, 100)
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("空表格不应产生点击")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("空表格不应有选择")
	}
}

// 视口尺寸与列宽变化：窗口随之变化、几何来自本帧约束。
func TestDGMatrixViewportAndColumnChange(t *testing.T) {
	g, h := dgNewGrid(t, 10000)

	// 视口变小：窗口收紧（复用 harness resize）。
	h.resize(300, 72) // 体 48px → 可见 2 行
	h.frame()
	st := g.LastFrame()
	if len(st.Drawn) != 2+dgOverscanRows && len(st.Drawn) != 2 {
		t.Fatalf("小视口可见行数不符：drawn=%d", len(st.Drawn))
	}
	if st.Last-st.First+1 > 2+2*dgOverscanRows {
		t.Fatalf("小视口访问窗口必须收紧：[%d,%d]", st.First, st.Last)
	}
	h.resize(dgMatrixViewportW, dgMatrixViewportH)
	h.frame()

	// 列宽变化：列身份随本帧列宽解析。
	if err := g.SetColumns([]DataGridColumn{
		{Title: "A", WidthDp: unit.Dp(150)},
		{Title: "B", WidthDp: unit.Dp(100)},
		{Title: "C", WidthDp: unit.Dp(50)},
	}); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.click(120, 60) // x=120 落在新列宽 150 内 → col 0
	if id, col, ok := g.Clicked(); !ok || id != dgRowID(1) || col != 0 {
		t.Fatalf("列宽变化后列身份应按新几何：id=%q col=%d ok=%v", id, col, ok)
	}
}

// 视口外/超总列宽不命中。
func TestDGMatrixNoHitOutsideContent(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "A", WidthDp: unit.Dp(100)},
		{Title: "B", WidthDp: unit.Dp(100)},
		{Title: "C", WidthDp: unit.Dp(50)},
	}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(3)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()

	h.click(270, 60) // 超出总列宽 250 → 不命中
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("超出总列宽不应命中")
	}
	h.click(50, 150) // 行 (150-24)/24=5 ≥ 3 行 → 不命中
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("数据区外（视口内）不应命中")
	}
	h.click(50, 10) // 表头带 → 不命中
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("表头点击不应产生行事件")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("未命中不应改变选择")
	}
	// 有效命中仍工作。
	h.click(120, 60) // 行 1、列 (120>=100 → col1)
	if id, col, ok := g.Clicked(); !ok || id != dgRowID(1) || col != 1 {
		t.Fatalf("有效命中应工作：id=%q col=%d ok=%v", id, col, ok)
	}
}

// 双实例隔离：A 点击/滚动不影响 B；父级偏移下各自几何正确。
func TestDGMatrixDualInstanceIndependent(t *testing.T) {
	a := NewDataGrid(unit.Dp(dgMatrixRowH))
	b := NewDataGrid(unit.Dp(dgMatrixRowH))
	cols := []DataGridColumn{
		{Title: "名称", WidthDp: unit.Dp(100)},
		{Title: "大小", WidthDp: unit.Dp(100)},
		{Title: "时间", WidthDp: unit.Dp(100)},
	}
	if err := a.SetColumns(cols); err != nil {
		t.Fatal(err)
	}
	if err := b.SetColumns(cols); err != nil {
		t.Fatal(err)
	}
	if rep := a.SetRows(dgTestRows(10000)); !rep.OK {
		t.Fatal("A 初始化失败")
	}
	if rep := b.SetRows(dgTestRows(20)); !rep.OK {
		t.Fatal("B 初始化失败")
	}
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

	// A 内点击（上半 y=60）与 B 内点击（下半 y=200+60）。
	h.click(50, 60)
	if id, _, ok := a.Clicked(); !ok || id != dgRowID(1) {
		t.Fatalf("A 点击身份不符：%q ok=%v", id, ok)
	}
	if _, _, ok := b.Clicked(); ok {
		t.Fatal("A 的点击不得串给 B")
	}
	h.click(50, 260)
	if id, _, ok := b.Clicked(); !ok || id != dgRowID(1) {
		t.Fatalf("B（父级偏移 200）点击身份不符：%q ok=%v", id, ok)
	}
	if _, ok := a.SelectedRowID(); !ok || func() string { s, _ := a.SelectedRowID(); return s }() != dgRowID(1) {
		t.Fatal("B 的点击不得影响 A 的选择")
	}

	// 滚动 B 不动 A（滚轮事件落在 B 的区域：下半 y=300）。
	aOff := a.ScrollOffset()
	h.frame(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32Pt(150, 300), Scroll: f32.Point{X: 0, Y: 240}})
	if a.ScrollOffset() != aOff {
		t.Fatalf("B 的滚动不得影响 A：A 偏移 %d → %d", aOff, a.ScrollOffset())
	}
	if b.ScrollOffset() != 240 {
		t.Fatalf("B 应滚动：%d", b.ScrollOffset())
	}
}

// 父容器偏移不泄漏：网格后的原点兄弟独立命中，网格自身在偏移下几何正确。
func TestDGMatrixParentOffsetNoLeak(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "A", WidthDp: unit.Dp(100)},
		{Title: "B", WidthDp: unit.Dp(100)},
	}); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(1000)); !rep.OK {
		t.Fatal("初始化失败")
	}
	sentinel := &overlayTag{}
	sentinel.owner = sentinel
	presses := 0
	h := newOverlayHarness(t, 300, 300)
	h.overlay = func(gtx layout.Context) {
		trans := op.Offset(image.Pt(30, 40)).Push(gtx.Ops)
		ggtx := gtx
		ggtx.Constraints = layout.Exact(image.Pt(300, 200))
		g.Layout(ggtx, h.th)
		trans.Pop()
		func() {
			defer clip.Rect(image.Rectangle{Max: image.Pt(10, 10)}).Push(gtx.Ops).Pop()
			for {
				_, ok := gtx.Source.Event(pointer.Filter{Target: sentinel, Kinds: pointer.Press})
				if !ok {
					break
				}
				presses++
			}
			event.Op(gtx.Ops, sentinel)
		}()
	}
	h.frame()
	h.click(2, 2) // 哨兵完整点击（按压中指针再按下非真实硬件形态）
	if presses != 1 {
		t.Fatalf("网格后的原点兄弟未独立命中：got=%d want=1（变换泄漏）", presses)
	}
	// 网格自身在父偏移 (30,40) 下：屏幕 (80,88) = 局部 (50,48) → 行 (48-24)/24=1。
	h.click(80, 88)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(1) {
		t.Fatalf("父偏移下网格命中几何不符：%q ok=%v", id, ok)
	}
}

// 键盘：首版不支持键盘选择/滚动——不注册按键过滤器，不改变选择。
func TestDGMatrixKeyboardNotClaimed(t *testing.T) {
	consumer := &keyConsumer{requestFocus: true}
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(100)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) {
		consumer.layout(gtx)
		g.Layout(gtx, h.th)
	}
	h.frame()
	// 选中一行作为状态标记。
	h.click(50, 60)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(1) {
		t.Fatal("前置点击失败")
	}
	// 按键由观察哨收到；网格选择/滚动不变。
	h.frame(key.Event{Name: "x", State: key.Press})
	h.frame(key.Event{Name: key.NameEscape, State: key.Press})
	found := false
	for _, n := range consumer.got {
		if n == "x" {
			found = true
		}
	}
	if !found {
		t.Fatal("对照：观察哨应收到按键（网格未拦截键盘）")
	}
	if sel, _ := g.SelectedRowID(); sel != dgRowID(1) {
		t.Fatalf("键盘不得改变选择：%q", sel)
	}
}

// 绘制回调：逐可见单元格计数（记录次数证明，不用截图/grep）。
func TestDGMatrixDrawCallbackCounting(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(1000)); !rep.OK {
		t.Fatal("初始化失败")
	}
	var calls map[string]int
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	g.Cell = func(gtx layout.Context, row DataGridRow, col int, rect image.Rectangle) layout.Dimensions {
		calls[row.ID+","+string(rune('0'+col))]++
		return layout.Dimensions{Size: rect.Size()}
	}
	calls = map[string]int{}
	h.frame()
	// 首帧：8 可见行 × 3 列。
	if len(calls) != 8*3 {
		t.Fatalf("首帧回调单元格数应为 24，得到 %d", len(calls))
	}
	// 滚动一行：新窗口每行每列恰一次（精确计数，非 counter>0）。
	calls = map[string]int{}
	dgWheel(h, dgMatrixRowH)
	if len(calls) != 8*3 {
		t.Fatalf("滚动帧回调单元格数应为 24，得到 %d", len(calls))
	}
	for _, v := range calls {
		if v != 1 {
			t.Fatalf("回调应每单元格恰一次：%v", v)
		}
	}
	// 稳定帧（无输入）：回调仍按可见窗口调用（立即模式语义），但集合不变。
	calls = map[string]int{}
	h.frame()
	if len(calls) != 8*3 {
		t.Fatalf("稳定帧回调单元格数应为 24，得到 %d", len(calls))
	}
}
