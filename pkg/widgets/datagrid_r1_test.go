package widgets

import (
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/unit"
)

// R1 持久回归：逐项对应协调者 review-1 F-B1/F-B2 判据（探针源
// coordinator/review-1/probes/formatted/b，历史证据一字不改）。期望值
// 均为手算常量或几何推导，不复用实现公式当唯一 oracle。

// --- F-B1：无可见体时零访问零绘制（规模无关） ---

func TestR1DGHeaderOnlyViewportBounded(t *testing.T) {
	// 视口高度 0/1/header-1/header：无可见体 → 零访问零绘制零回调。
	// height=header+1（25）：体 1px → 恰 1 行可见（手算：vis0=0，
	// end=(0+1+24-1)/24=1 → [0,1)），访问=1+下方 overscan 2=3（顶部钳制）。
	for _, tc := range []struct {
		height    int
		wantAcc   int
		wantDrawn int
	}{
		{0, 0, 0},
		{1, 0, 0},
		{23, 0, 0}, // header-1
		{24, 0, 0}, // header
		{25, 3, 1}, // header+1：1 可见行 + 2 overscan
	} {
		for _, rows := range []int{10000, 100000} {
			g := NewDataGrid(unit.Dp(dgMatrixRowH))
			if err := g.SetColumns(dgTestCols()); err != nil {
				t.Fatal(err)
			}
			if rep := g.SetRows(dgTestRows(rows)); !rep.OK {
				t.Fatal("初始化失败")
			}
			callbacks := 0
			g.Cell = func(layout.Context, DataGridRow, int, image.Rectangle) layout.Dimensions {
				callbacks++
				return layout.Dimensions{}
			}
			h := newOverlayHarness(t, 300, tc.height)
			h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
			h.frame()

			st := g.LastFrame()
			if len(st.Accessed) != tc.wantAcc || len(st.Drawn) != tc.wantDrawn || callbacks != tc.wantDrawn*3 {
				t.Fatalf("height=%d rows=%d：accessed=%d drawn=%d callbacks=%d，期望 %d/%d/%d",
					tc.height, rows, len(st.Accessed), len(st.Drawn), callbacks,
					tc.wantAcc, tc.wantDrawn, tc.wantDrawn*3)
			}
			// 10k 与 100k 的访问规模必须逐项相同（规模无关）。
		}
	}
}

func TestR1DGShrinkToHeaderAfterScroll(t *testing.T) {
	// 滚动到底后缩小视口到仅表头：零访问零绘制；偏移被新几何重钳制。
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(10000)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	dgWheel(h, 10_000_000)
	if g.ScrollOffset() != 10000*dgMatrixRowH-(200-24) {
		t.Fatalf("滚到底偏移不符：%d", g.ScrollOffset())
	}

	h.resize(300, 24) // 只剩表头
	h.frame()
	st := g.LastFrame()
	if len(st.Accessed) != 0 || len(st.Drawn) != 0 {
		t.Fatalf("缩小到仅表头后必须零访问零绘制：acc=%d drawn=%d", len(st.Accessed), len(st.Drawn))
	}
	if g.ScrollOffset() != 0 {
		t.Fatalf("体高 0 时滚动偏移应重钳到 0，得到 %d", g.ScrollOffset())
	}

	// 恢复视口：窗口恢复、规模有界。
	h.resize(300, 200)
	h.frame()
	st = g.LastFrame()
	if len(st.Drawn) != 8 || len(st.Accessed) != 10 {
		t.Fatalf("恢复视口后窗口应为 8/10：drawn=%d acc=%d", len(st.Drawn), len(st.Accessed))
	}
}

// --- F-B2：释放命中按本帧视口/body/列有效范围判断（先浮点后整数） ---

func dgClickPair(h *overlayHarness, x, y float32) {
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(x, y)})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(x, y)})
}

func TestR1DGReleaseBoundsMatrix(t *testing.T) {
	// 视口 300×200，表头 24，列宽 100/100/100（总 300），100 行。
	// 行 y 区间：[24+24k, 24+24(k+1))；正控制 y=195 → 行 7。
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(100)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()

	// 正控制：界内边缘命中必须真实成立。
	// (50,195)→行 7；(299.5,60)→列 2（299 截断 <300 总宽）；(50,24.5)→行 0 上缘。
	dgClickPair(h, 50, 195)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(7) {
		t.Fatalf("正控制(50,195)应命中行7：%q ok=%v", id, ok)
	}
	dgClickPair(h, 299, 60)
	if id, col, ok := g.Clicked(); !ok || id != dgRowID(1) || col != 2 {
		t.Fatalf("右缘正控制(299,60)应命中列2：id=%q col=%d ok=%v", id, col, ok)
	}
	dgClickPair(h, 50, 24)
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(0) {
		t.Fatalf("body 上缘正控制(50,24)应命中行0：%q ok=%v", id, ok)
	}

	// 四边释放越界：按压在界内，释放在视口外（浮点小数含 -0.1/+0.1）。
	outside := []struct {
		name string
		x, y float32
	}{
		{"上", 50, -0.1},
		{"下", 50, 200.1},
		{"左", -0.1, 60},
		{"右", 300.1, 60},
	}
	for _, tc := range outside {
		// 按压在界内（frame 分离，不与正控制共用），释放在越界点。
		h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(50, 195)})
		h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(tc.x, tc.y)})
		if _, _, ok := g.Clicked(); ok {
			t.Fatalf("%s 边释放越界不得提交点击", tc.name)
		}
	}

	// 释放落在表头带（y=23.9）：不产生行事件。
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(50, 195)})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(50, 23.9)})
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("表头带释放不得产生行点击")
	}

	// 释放超出总列宽（x=300.1 已覆盖）；列内小数右缘 (299.9,60) 合法。
	dgClickPair(h, 299.9, 60)
	if id, col, ok := g.Clicked(); !ok || id != dgRowID(1) || col != 2 {
		t.Fatalf("小数右缘(299.9,60)应合法命中列2：id=%q col=%d ok=%v", id, col, ok)
	}

	// 跨帧取消：按压后 Cancel，无论后续释放位置都不提交。
	dgClickPair(h, 50, 195)
	g.Clicked() // 消费正控制点击
	sel, _ := g.SelectedRowID()
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(50, 60)})
	h.frame(pointer.Event{Kind: pointer.Cancel, Source: pointer.Mouse})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(50, 60)})
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("取消后的释放不得提交点击")
	}
	if s, _ := g.SelectedRowID(); s != sel {
		t.Fatalf("取消不得改变选择：%q → %q", sel, s)
	}
}

func TestR1DGShrunkViewportRejectsStaleRelease(t *testing.T) {
	// 按压时视口 200，缩到 100 后释放仍在旧 y=195：新帧视口外 → 拒绝。
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(100)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	// 建立一个已知选择作为基线（消费掉点击事件）。
	dgClickPair(h, 50, 60) // 行 1
	g.Clicked()
	wantSel, _ := g.SelectedRowID()

	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(50, 195)})
	h.resize(300, 100) // 视口缩小：y=195 已在新视口外
	h.frame()
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(50, 195)})
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("缩小视口后的越界释放不得提交点击")
	}
	if s, _ := g.SelectedRowID(); s != wantSel {
		t.Fatalf("越界释放不得改变选择：%q → %q", wantSel, s)
	}
}

func TestR1DGPressOutsideViewportIsInert(t *testing.T) {
	// 按压本身越界（浮点边界）：不得设置按压状态，后续释放也不提交。
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(100)); !rep.OK {
		t.Fatal("初始化失败")
	}
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, PointerID: 1, Position: f32.Pt(50, 200.5)})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(50, 200.5)})
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("按压越界后的释放不得提交点击")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("越界按压不得产生选择")
	}
}
