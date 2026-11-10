package widgets

import (
	"image"
	"testing"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// R28-B 键盘单选/纵向导航候选：真实 input.Router key.Event 矩阵。
// 不直接调用内部处理函数；焦点存在性经 Focused()/行为双重核验；期望
// 按 RowID/几何独立推导。

func kbSetup(t *testing.T, rows int) (*DataGrid, *overlayHarness) {
	t.Helper()
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(rows)); !rep.OK {
		t.Fatal("初始化失败")
	}
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	h := newOverlayHarness(t, dgMatrixViewportW, dgMatrixViewportH)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	return g, h
}

// kbSetupFocused = kbSetup + 表头按压持焦（不产生选择）。
func kbSetupFocused(t *testing.T, rows int) (*DataGrid, *overlayHarness) {
	g, h := kbSetup(t, rows)
	h.frame()
	kbFocus(h)
	return g, h
}

func kbKey(h *overlayHarness, name key.Name) {
	h.frame(key.Event{Name: name, State: key.Press})
}

// kbFocus 用表头按压让网格持焦（候选：网格内任意主键按压聚焦），
// 不产生行选择——「初始无选择」的可达路径。
func kbFocus(h *overlayHarness) {
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32Pt(50, 10)})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32Pt(50, 10)})
	h.frame()
}

func TestKBOptInOutDefaultOff(t *testing.T) {
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
	if g.KeyboardNavEnabled() {
		t.Fatal("默认必须关闭")
	}

	// 默认关闭：Down 键不被网格消费（观察哨收到；选择不变）。
	kbKey(h, key.NameDownArrow)
	found := false
	for _, n := range consumer.got {
		if n == key.NameDownArrow {
			found = true
		}
	}
	if !found {
		t.Fatal("默认关闭路径必须不消费按键（对照观察哨）")
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("默认关闭路径不得产生选择")
	}

	// 显式开启：观察哨停抢焦点；网格经表头按压获得焦点（初始无选择可达）。
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	consumer.requestFocus = false
	consumer.got = nil
	h.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32Pt(50, 10)})
	h.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32Pt(50, 10)})
	h.frame()
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("表头按压只聚焦，不产生选择")
	}
	kbKey(h, key.NameDownArrow)
	for _, n := range consumer.got {
		if n == key.NameDownArrow {
			t.Fatal("开启且持焦后 Down 应被网格消费")
		}
	}
	if sel, ok := g.SelectedRowID(); !ok || sel != dgRowID(0) {
		t.Fatalf("持焦后 Down 应选可见首行：%q ok=%v", sel, ok)
	}

	// 显式关闭：恢复不消费、选择保持（观察哨重新抢焦点接收按键）。
	g.DisableKeyboardNav()
	sel, _ := g.SelectedRowID()
	consumer.requestFocus = true
	consumer.got = nil
	h.frame()
	kbKey(h, key.NameUpArrow)
	found = false
	for _, n := range consumer.got {
		if n == key.NameUpArrow {
			found = true
		}
	}
	if !found {
		t.Fatal("关闭后必须恢复不消费按键")
	}
	if s, _ := g.SelectedRowID(); s != sel {
		t.Fatal("关闭不得清除选择")
	}
	if g.idToIdx != nil {
		t.Fatal("关闭应释放 RowID→索引映射")
	}

	// 重新开启：可复用。
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	if !g.KeyboardNavEnabled() {
		t.Fatal("重新开启失败")
	}
}

func TestKBClickToFocusAndNavigate(t *testing.T) {
	g, h := kbSetup(t, 100)
	h.frame()
	if g.Focused() {
		t.Fatal("未点击前不应有焦点")
	}

	// 点击聚焦（候选）：主键按压有效单元格。
	h.click(50, dgMatrixHeaderH+3*dgMatrixRowH+12)
	g.Clicked() // 消费鼠标点击
	h.frame()
	if !g.Focused() {
		t.Fatal("点击聚焦后网格应持有焦点")
	}
	if sel, _ := g.SelectedRowID(); sel != dgRowID(3) {
		t.Fatalf("点击选择行3：%q", sel)
	}

	// Down：行4（本就在可见窗口内，偏移不变）。
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(4) {
		t.Fatalf("Down 应到行4：%q", sel)
	}
	if g.ScrollOffset() != 0 {
		t.Fatalf("行4 仍可见，偏移不应变：%d", g.ScrollOffset())
	}

	// 连续 Down 越出可见窗口：行 8（可见 0..7）→ 滚入可见区。
	for i := 0; i < 4; i++ {
		kbKey(h, key.NameDownArrow)
	}
	if sel, _ := g.SelectedRowID(); sel != dgRowID(8) {
		t.Fatalf("连续 Down 应到行8：%q", sel)
	}
	st := g.LastFrame()
	inDrawn := false
	for _, id := range st.Drawn {
		if id == dgRowID(8) {
			inDrawn = true
		}
	}
	if !inDrawn {
		t.Fatalf("滚到目标后选中行必须在可见绘制集合中：drawn=%v", st.Drawn)
	}
	if want := 8*dgMatrixRowH + dgMatrixRowH - (dgMatrixViewportH - dgMatrixHeaderH); g.ScrollOffset() != want {
		t.Fatalf("scroll-into-view 偏移应为 %d，得到 %d", want, g.ScrollOffset())
	}

	// 键盘选中不伪造鼠标点击事件。
	if _, _, ok := g.Clicked(); ok {
		t.Fatal("键盘导航不得产生鼠标点击事件")
	}
}

func TestKBInitialNoSelectionAndPageKeys(t *testing.T) {
	g, h := kbSetupFocused(t, 100)
	// 初始无选择：Down/PageDown 选可见首行；End 选末行。
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(0) {
		t.Fatalf("初始无选择时 Down 应选可见首行：%q", sel)
	}
	g2, h2 := kbSetupFocused(t, 100)
	kbKey(h2, key.NameEnd)
	if sel, _ := g2.SelectedRowID(); sel != dgRowID(99) {
		t.Fatalf("End 应选末行：%q", sel)
	}
	if g2.ScrollOffset() != 99*dgMatrixRowH+dgMatrixRowH-(dgMatrixViewportH-dgMatrixHeaderH) {
		t.Fatalf("End 应滚到底：%d", g2.ScrollOffset())
	}
	st := g2.LastFrame()
	if st.Drawn[len(st.Drawn)-1] != dgRowID(99) {
		t.Fatal("End 后末行应在可见绘制集合")
	}
	g3, h3 := kbSetupFocused(t, 100)
	kbKey(h3, key.NamePageDown) // page=可见 7 行（176/24）
	if sel, _ := g3.SelectedRowID(); sel != dgRowID(7) {
		t.Fatalf("PageDown 应从可见首行下移 7 行：%q", sel)
	}

	// 首尾钳制：边界处按键原地保持、不重滚。
	g4, h4 := kbSetupFocused(t, 100)
	kbKey(h4, key.NameHome)
	if sel, _ := g4.SelectedRowID(); sel != dgRowID(0) {
		t.Fatalf("Home 应选首行：%q", sel)
	}
	kbKey(h4, key.NameUpArrow)
	if sel, _ := g4.SelectedRowID(); sel != dgRowID(0) {
		t.Fatalf("首行 Up 应原地钳制：%q", sel)
	}
	if g4.ScrollOffset() != 0 {
		t.Fatalf("钳制不得重滚：%d", g4.ScrollOffset())
	}
}

func TestKBPageRowsConfigAndRepeat(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(200)); !rep.OK {
		t.Fatal("初始化失败")
	}
	g.EnableKeyboardNav(DataGridKeyboardConfig{PageRows: 3})
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	kbFocus(h)
	kbKey(h, key.NamePageDown)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(3) {
		t.Fatalf("PageRows=3：PageDown 应移动 3 行：%q", sel)
	}
	kbKey(h, key.NamePageDown)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(6) {
		t.Fatalf("第二次 PageDown：%q", sel)
	}

	// 平台连发 = 连续 Press：每 Press 一步。
	h.resize(300, 200)
	h.frame()
	kbKey(h, key.NameDownArrow)
	kbKey(h, key.NameDownArrow)
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(9) {
		t.Fatalf("连续 Press 应各前进一步：%q", sel)
	}
}

func TestKBEmptyTableAndNoBody(t *testing.T) {
	// 空表：按键明确无操作。
	g, h := kbSetup(t, 0)
	h.frame()
	for _, n := range dgNavKeys {
		kbKey(h, n)
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("空表不得产生选择")
	}
	if len(g.LastFrame().Accessed) != 0 {
		t.Fatal("空表不得访问任何行")
	}

	// 空 body（仅表头）：选择按 RowID 更新、不滚动、无全表访问。
	g2, h2 := kbSetup(t, 10000)
	h2.resize(300, 24) // 仅表头
	h2.frame()
	kbFocus(h2)
	kbKey(h2, key.NameDownArrow)
	if sel, ok := g2.SelectedRowID(); !ok || sel != dgRowID(0) {
		t.Fatalf("空 body 下 Down 应记选择（明示语义）：%q ok=%v", sel, ok)
	}
	st := g2.LastFrame()
	if len(st.Accessed) != 0 || len(st.Drawn) != 0 {
		t.Fatalf("空 body 不得扩大为隐藏全表访问：acc=%d drawn=%d", len(st.Accessed), len(st.Drawn))
	}
	if g2.ScrollOffset() != 0 {
		t.Fatalf("空 body 不应滚动：%d", g2.ScrollOffset())
	}
	// End：选择到末行，仍零访问。
	kbKey(h2, key.NameEnd)
	if sel, _ := g2.SelectedRowID(); sel != dgRowID(9999) {
		t.Fatalf("空 body End：%q", sel)
	}
	if len(g2.LastFrame().Accessed) != 0 {
		t.Fatal("空 body 下 End 不得触发全表访问")
	}
}

func TestKBScaleIdentity10k100k(t *testing.T) {
	for _, rows := range []int{10000, 100000} {
		g, h := kbSetupFocused(t, rows)
		kbKey(h, key.NameEnd)
		wantIdx := rows - 1
		if sel, _ := g.SelectedRowID(); sel != dgRowID(wantIdx) {
			t.Fatalf("rows=%d：End 身份不符：%q", rows, sel)
		}
		st := g.LastFrame()
		inDrawn := false
		for _, id := range st.Drawn {
			if id == dgRowID(wantIdx) {
				inDrawn = true
			}
		}
		if !inDrawn {
			t.Fatalf("rows=%d：末行应在可见绘制集合", rows)
		}
		if len(st.Accessed) != 10 || len(st.Drawn) != 8 {
			t.Fatalf("rows=%d：访问/绘制规模应有界：acc=%d drawn=%d", rows, len(st.Accessed), len(st.Drawn))
		}
		// Home 回顶。
		kbKey(h, key.NameHome)
		if sel, _ := g.SelectedRowID(); sel != dgRowID(0) {
			t.Fatalf("rows=%d：Home 身份不符：%q", rows, sel)
		}
	}
}

func TestKBReorderShrinkClear(t *testing.T) {
	g, h := kbSetup(t, 100)
	h.frame()
	h.click(50, dgMatrixHeaderH+5*dgMatrixRowH+12) // 行 5
	g.Clicked()
	h.frame()
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(6) {
		t.Fatalf("键盘接续鼠标应到行6：%q", sel)
	}

	// 重排：row6 移到 index 0（与 row0 互换）。
	rows := dgTestRows(100)
	rows[0], rows[6] = rows[6], rows[0]
	if rep := g.SetRows(rows); !rep.OK {
		t.Fatal("重排更新被拒绝")
	}
	if sel, _ := g.SelectedRowID(); sel != dgRowID(6) {
		t.Fatalf("重排后选择按 RowID 保持：%q", sel)
	}
	// 键盘 Up：当前顺序中 dgRowID(6) 在 index 0 → 钳制在首行原地。
	kbKey(h, key.NameUpArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(6) {
		t.Fatalf("index0 处 Up 应原地钳制（RowID 不退化旧索引）：%q", sel)
	}
	// 键盘 Down：index 1 = dgRowID(1)（当前顺序邻接）。
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(1) {
		t.Fatalf("重排后 Down 应按当前顺序邻接移动：%q", sel)
	}

	// 缩短到不含选中行的数据：选择显式清除；Down 重新选可见首行。
	short := []DataGridRow{
		{ID: "other-1", Cells: []string{"甲", "1", "x"}},
		{ID: "other-2", Cells: []string{"乙", "2", "y"}},
	}
	if rep := g.SetRows(short); !rep.OK || !rep.KeyboardIndexBuilt || !rep.SelectionCleared {
		t.Fatalf("缩短更新应声明选择清除并重建索引：%+v", rep)
	}
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("选中行被删除应清除选择")
	}
	h.frame()
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != "other-1" {
		t.Fatalf("清除后 Down 应重新选可见首行：%q", sel)
	}

	// 清空：按键无操作。
	if rep := g.SetRows(nil); !rep.OK {
		t.Fatal("清空失败")
	}
	h.frame()
	kbKey(h, key.NameDownArrow)
	if _, ok := g.SelectedRowID(); ok {
		t.Fatal("清空后按键不得产生选择")
	}
}

func TestKBDualInstanceFocusIsolation(t *testing.T) {
	a, _ := kbSetup(t, 100)
	b, _ := kbSetup(t, 100)
	// 重建 B 的 harness：两个网格上/下半布局（B 带父级 offset）。
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
	// 各自 opt-in 已在上面的 kbSetup 中完成。

	h.click(50, 60) // A 行 1
	a.Clicked()
	h.frame()
	if !a.Focused() {
		t.Fatal("A 应持有焦点")
	}
	kbKey(h, key.NameDownArrow) // y 位置无关：键按焦点路由
	if sel, _ := a.SelectedRowID(); sel != dgRowID(2) {
		t.Fatalf("A 应收到 Down：%q", sel)
	}
	if _, ok := b.SelectedRowID(); ok {
		t.Fatal("B 不应被 A 的按键影响")
	}

	h.click(50, 260) // B 行 1（父级偏移 200）
	b.Clicked()
	h.frame()
	if !b.Focused() {
		t.Fatal("B 应持有焦点")
	}
	kbKey(h, key.NameDownArrow)
	if sel, _ := b.SelectedRowID(); sel != dgRowID(2) {
		t.Fatalf("B 应收到 Down（父级偏移下）：%q", sel)
	}
	if sel, _ := a.SelectedRowID(); sel != dgRowID(2) {
		t.Fatalf("B 的按键不得影响 A：%q", sel)
	}
}

func TestKBEditorFocusIsolation(t *testing.T) {
	g, h := kbSetup(t, 100)
	h.frame()
	h.click(50, dgMatrixHeaderH+12) // 行 0，网格持焦
	g.Clicked()
	h.frame()
	if !g.Focused() {
		t.Fatal("前置：网格应持焦")
	}

	// 真实可聚焦消费者：widget.Editor（自身即焦点标签）。
	var ed widget.Editor
	th := h.th
	h.overlay = func(gtx layout.Context) {
		// material.Editor 同款调用形态：处理事件 + 渲染文本（focusable
		// 资格由 Editor 自身以 *widget.Editor 为标签注册）。
		ed.Update(gtx)
		m := material.Editor(th, &ed, "hint")
		m.Layout(gtx)
		g.Layout(gtx, th)
	}
	h.frame()
	h.gtx.Execute(key.FocusCmd{Tag: &ed})
	h.frame()

	// 焦点转给 Editor：输入归 Editor，网格不吞按键。
	kbKey(h, key.NameDownArrow) // 网格未持焦，不得消费
	// 可打印文本经系统 EditEvent 通道投递给焦点可聚焦消费者。
	h.frame(key.EditEvent{Range: key.Range{}, Text: "a"})
	h.frame()
	if got := ed.Text(); got != "a" {
		t.Fatalf("Editor 应收到文本输入：got=%q", got)
	}
	if sel, ok := g.SelectedRowID(); !ok || sel != dgRowID(0) {
		t.Fatalf("Editor 持焦期间网格选择不得变化：sel=%q ok=%v", sel, ok)
	}
	if g.Focused() {
		t.Fatal("网格应已失去焦点")
	}

	// 点击网格：焦点回网格，Down 再生效。
	h.click(50, dgMatrixHeaderH+12)
	g.Clicked()
	h.frame()
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(1) {
		t.Fatalf("焦点回网格后 Down 应生效：%q", sel)
	}
}

func TestKBMouseKeyboardInterleave(t *testing.T) {
	g, h := kbSetup(t, 100)
	h.frame()
	h.click(50, dgMatrixHeaderH+5*dgMatrixRowH+12) // 行 5（视口内）
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(5) {
		t.Fatalf("鼠标选择行5：%v", ok)
	}
	kbKey(h, key.NameDownArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(6) {
		t.Fatalf("键盘接续鼠标：%q", sel)
	}
	h.click(50, dgMatrixHeaderH+2*dgMatrixRowH+12) // 行 2
	if id, _, ok := g.Clicked(); !ok || id != dgRowID(2) {
		t.Fatal("鼠标改选行2")
	}
	kbKey(h, key.NameUpArrow)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(1) {
		t.Fatalf("键盘接续鼠标（Up）：%q", sel)
	}
}

func TestKBIndexReport(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	rep := g.SetRows(dgTestRows(100))
	if rep.KeyboardIndexBuilt {
		t.Fatal("未启用键盘时不得构建索引")
	}
	g.EnableKeyboardNav(DataGridKeyboardConfig{})
	if rep := g.SetRows(dgTestRows(50)); !rep.OK || !rep.KeyboardIndexBuilt || rep.KeyboardIndexRows != 50 {
		t.Fatalf("启用后 SetRows 应在同一次遍历重建索引：%+v", rep)
	}
	// 索引正确性抽查：选中末行（键盘 End）身份必须与新数据一致。
	h := newOverlayHarness(t, 300, 200)
	h.overlay = func(gtx layout.Context) { g.Layout(gtx, h.th) }
	h.frame()
	kbFocus(h)
	kbKey(h, key.NameEnd)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(49) {
		t.Fatalf("索引重建后 End 身份不符：%q", sel)
	}
}

// TestKBPageClampEdges 补全「首尾钳制」的边界矩阵（R28-B 收口：明确遗漏
// 的行为守卫，非扩功能）：PageUp 初始无选择、翻页超表长、PageRows 大于
// 数据行数——期望均为手算的钳制结果。
func TestKBPageClampEdges(t *testing.T) {
	// 1) 初始无选择时 PageUp：firstVis(0) - page(7) 钳到 0 → 选可见首行。
	g, h := kbSetupFocused(t, 100)
	kbKey(h, key.NamePageUp)
	if sel, _ := g.SelectedRowID(); sel != dgRowID(0) {
		t.Fatalf("无选择 PageUp 应钳到可见首行：%q", sel)
	}

	// 2) PageRows 大于数据行数：PageDown 一次即钳到末行（End 等价）。
	g2 := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g2.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g2.SetRows(dgTestRows(5)); !rep.OK {
		t.Fatal("初始化失败")
	}
	g2.EnableKeyboardNav(DataGridKeyboardConfig{PageRows: 50})
	h2 := newOverlayHarness(t, 300, 200)
	h2.overlay = func(gtx layout.Context) { g2.Layout(gtx, h2.th) }
	h2.frame()
	kbFocus(h2)
	kbKey(h2, key.NamePageDown)
	if sel, _ := g2.SelectedRowID(); sel != dgRowID(4) {
		t.Fatalf("PageRows 超表长应钳到末行：%q", sel)
	}
	// 末行 PageDown 原地钳制（不重滚）。
	off := g2.ScrollOffset()
	kbKey(h2, key.NamePageDown)
	if sel, _ := g2.SelectedRowID(); sel != dgRowID(4) {
		t.Fatalf("末行 PageDown 应原地钳制：%q", sel)
	}
	if g2.ScrollOffset() != off {
		t.Fatalf("钳制不得重滚：%d → %d", off, g2.ScrollOffset())
	}

	// 3) PageRows + PageUp 组合：翻回并钳到首行。
	kbKey(h2, key.NamePageUp)
	if sel, _ := g2.SelectedRowID(); sel != dgRowID(0) {
		t.Fatalf("PageUp 应钳到首行（50>5 行数据）：%q", sel)
	}
	if g2.ScrollOffset() != 0 {
		t.Fatalf("首行应滚到顶：%d", g2.ScrollOffset())
	}
}
