package codegen

// R25-R2 S2：Expanded 实质运行时判别矩阵（真实链路 Analyzer→Codegen→
// go test→离屏 Layout + 真实指针事件）。
//
// 判别力判据：协调者变异（自然子尺寸分支 gtx.Constraints 置零，见
// review-repair-r1/mut-expanded-collapse.go）必须被本文件守卫拒绝——
// 置零后 Expanded 内容零尺寸，所有**内容区坐标**的点击脱靶。原 Row 点击
// 守卫（r25_expanded_matrix_test.go）保留不删除、不放宽。
//
// 每场景断言至少一项：点击命中次数 / 处理器收到的事件实参。

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// r25r2SharedVM 多场景共享探测 ViewModel。
const r25r2SharedVM = `package vm

// VM 是 R25-R2 弹性行为验收用 ViewModel。
type VM struct {
	CountA, CountB int
	LastID         string
	ShowA          bool
	Rows           []Row
}

// Row 是 <For> 行数据。
type Row struct {
	ID   string
	Name string
}

// TapA/B 独立按钮处理器。
func (vm *VM) TapA() { vm.CountA++ }
func (vm *VM) TapB() { vm.CountB++ }

// TapID 记录行事件实参（For 场景）。
func (vm *VM) TapID(id string) { vm.LastID = id; vm.CountA++ }
`

// TestR25R2RowBothEnds 横轴：两个弹性项的**起点与终点附近**均可命中——
// 零约束变异使内容零尺寸，全部脱靶。多弹性不得同串查两遍：A/B 各自计数。
func TestR25R2RowBothEnds(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <Expanded><Button Text="A" Click="TapA"/></Expanded>
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R2RowBothEnds(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	click := func(x float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	click(30)
	click(270)
	if obj.CountA != 1 || obj.CountB != 1 {
		t.Fatalf("两个弹性项均应可命中：A=%d B=%d（零尺寸/顺序错误）", obj.CountA, obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r2-row-both-ends")
}
`)
	assertProbeHealth(t, "r25r2-row-both-ends", out)
}

// TestR25R2ColumnFlex 纵轴：rigid Height=150 之上，Expanded cell 从
// y=150 起——健康时 y=175 命中；零约束变异使内容零尺寸 → 脱靶。
func TestR25R2ColumnFlex(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Column Height="200">
  <Text Value="顶部" Height="150"/>
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Column>`, `
func TestProbeR25R2ColumnFlex(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	click := func(y float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	click(175)
	if obj.CountB != 1 {
		t.Fatalf("纵轴弹性 cell 应可命中（y=175 在 Expanded cell 内）：B=%d", obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r2-column-flex")
}
`)
	assertProbeHealth(t, "r25r2-column-flex", out)
}

// TestR25R2GapPaddingOffsets Gap=4/Padding=30 下命中坐标整体偏移：
// Padding 把内容起点推到 x=30；Gap 在两个弹性项之间加 4px。健康时
// x=40 命中 A、x=250 命中 B（坐标含偏移；零约束变异则全脱靶）。
func TestR25R2GapPaddingOffsets(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="100" Padding="6" Gap="4">
  <Expanded><Button Text="A" Click="TapA"/></Expanded>
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R2GapPaddingOffsets(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(400, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	click := func(x float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	click(40)  // Padding 6 之后：A cell 内
	click(250) // Gap 4 之后：B cell 内
	if obj.CountA != 1 || obj.CountB != 1 {
		t.Fatalf("Padding/Gap 偏移后两弹性项均应可命中：A=%d B=%d", obj.CountA, obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r2-gap-padding-offsets")
}
`)
	assertProbeHealth(t, "r25r2-gap-padding-offsets", out)
}

// TestR25R2PerFrameConstraintChange 约束逐帧变化：同一 View 先 300 宽、
// 后 500 宽——健康实现中 Expanded 内容跟随新约束（x=450 命中）；零约束
// 变异下内容恒零 → 脱靶。
func TestR25R2PerFrameConstraintChange(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R2PerFrameConstraintChange(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:    new(op.Ops),
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source: r.Source(),
	}
	// R25-R3：当前约束宽度显式记录，click 后保持**当前阶段宽度**——
	// 第二阶段持续 500 宽（不再每帧切回 300），逐阶段断言约束/结果。
	curW := 0
	layoutAt := func(w, h int) {
		gtx.Reset()
		gtx.Constraints = layout.Exact(image.Pt(w, h))
		curW = w
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	click := func(x, y float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(x, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		layoutAt(curW, 60)
	}
	layoutAt(300, 60)
	click(250, 30)
	if obj.CountB != 1 || curW != 300 {
		t.Fatalf("300 宽阶段: B=%d curW=%d", obj.CountB, curW)
	}
	layoutAt(500, 60)
	click(450, 30)
	if obj.CountB != 2 || curW != 500 {
		t.Fatalf("500 宽阶段: B=%d curW=%d（第二阶段应持续 500）", obj.CountB, curW)
	}
	click(250, 30)
	if obj.CountB != 3 || curW != 500 {
		t.Fatalf("500 宽阶段再点击 x=250 仍应命中（内容跟随当前约束）：B=%d", obj.CountB)
	}
	layoutAt(300, 60)
	click(450, 30)
	if obj.CountB != 3 {
		t.Fatalf("回到 300 宽后 x=450 应脱靶（约束回退生效）：B=%d", obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r2-constraint-change")
}
`)
	assertProbeHealth(t, "r25r2-constraint-change", out)
}

// TestR25R2IfFlip If 分支翻转：ShowA 切换两个 Expanded 按钮，同一坐标
// 命中不同处理器（分支翻转证明 If 内容轴/尺寸透传）。
func TestR25R2IfFlip(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <If Condition="{ShowA}">
    <Then><Expanded><Button Text="A" Click="TapA"/></Expanded></Then>
    <Else><Expanded><Button Text="B" Click="TapB"/></Expanded></Else>
  </If>
</Row>`, `
func TestProbeR25R2IfFlip(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{ShowA: true}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	click := func(x float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	// 双帧稳定：If 分支内容在第 1 帧注册区域，指针事件在下一帧投递
	// （与 r25_expanded_matrix 的健康模式一致——先稳帧再点击）。
	frame()
	frame()
	click(100)
	if obj.CountA != 1 {
		t.Fatalf("ShowA=true 时 x=100 应命中 A：A=%d B=%d", obj.CountA, obj.CountB)
	}
	obj.ShowA = false
	frame()
	frame()
	click(100)
	if obj.CountB != 1 {
		t.Fatalf("分支翻转后 x=100 应命中 B：A=%d B=%d", obj.CountA, obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r2-if-flip")
}
`)
	assertProbeHealth(t, "r25r2-if-flip", out)
}

// TestR25R2ForRows For 包装：三行各自 Expanded[Button Tap(item.ID)]——
// 点击第 N 行收到第 N 行 ID（轴/尺寸透传 + 事件实参）。
func TestR25R2ForRows(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Column>
  <For Each="item" In="{Rows}" Key="{item.ID}">
    <Row Height="40"><Expanded><Button Text="{item.Name}" Click="TapID(item.ID)"/></Expanded></Row>
  </For>
</Column>`, `
func TestProbeR25R2ForRows(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{Rows: []gvm.Row{
		{ID: "A", Name: "行A"},
		{ID: "B", Name: "行B"},
		{ID: "C", Name: "行C"},
	}}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Constraints{Max: image.Pt(300, 100000)},
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	lay := func() int {
		gtx.Reset()
		d := v.Layout(gtx, th)
		r.Frame(gtx.Ops)
		return d.Size.Y
	}
	h3 := lay()
	obj.Rows = obj.Rows[:2]
	h2 := lay()
	rowH := h3 - h2
	if rowH <= 0 {
		t.Fatalf("行高推导失败: h3=%d h2=%d", h3, h2)
	}
	obj.Rows = []gvm.Row{{ID: "A", Name: "行A"}, {ID: "B", Name: "行B"}, {ID: "C", Name: "行C"}}
	y0 := h3 - 3*rowH
	lay()
	click := func(y float32) {
		r.Queue(
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(30, y), Kind: pointer.Release, Source: pointer.Mouse},
		)
		lay()
	}
	yOf := func(i int) float32 { return float32(y0 + i*rowH + rowH/2) }
	click(yOf(1))
	if obj.LastID != "B" {
		t.Fatalf("点第2行: LastID=%q, want B", obj.LastID)
	}
	click(yOf(2))
	if obj.LastID != "C" {
		t.Fatalf("点第3行: LastID=%q, want C", obj.LastID)
	}
	fmt.Println("SCEN-DONE r25r2-for-rows")
}
`)
	assertProbeHealth(t, "r25r2-for-rows", out)
}

// TestR25R2DualInstance 双实例交错布局与事件隔离：两个 View 各自 VM，
// 同一坐标点击只影响对应实例的计数器；交错后 A 再次命中。
func TestR25R2DualInstance(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <Expanded><Button Text="点我" Click="TapA"/></Expanded>
</Row>`, `
func TestProbeR25R2DualInstance(t *testing.T) {
	th := material.NewTheme()
	objA := &gvm.VM{}
	objB := &gvm.VM{}
	vA, err := NewGoxmlDashboardView(objA)
	if err != nil {
		t.Fatal(err)
	}
	vB, err := NewGoxmlDashboardView(objB)
	if err != nil {
		t.Fatal(err)
	}
	var rA, rB input.Router
	gtxA := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 60)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rA.Source()}
	gtxB := layout.Context{Ops: new(op.Ops), Constraints: layout.Exact(image.Pt(300, 60)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Source: rB.Source()}
	frameA := func() {
		gtxA.Reset()
		vA.Layout(gtxA, th)
		rA.Frame(gtxA.Ops)
	}
	frameB := func() {
		gtxB.Reset()
		vB.Layout(gtxB, th)
		rB.Frame(gtxB.Ops)
	}
	for i := 0; i < 3; i++ {
		frameA()
		frameB()
	}
	clickA := func(x float32) {
		rA.Queue(
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 30), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frameA()
	}
	clickA(250)
	if objA.CountA != 1 {
		t.Fatalf("A 实例应命中：TapA=%d", objA.CountA)
	}
	if objB.CountA != 0 {
		t.Fatalf("B 实例不得被串写：TapA=%d", objB.CountA)
	}
	frameB()
	clickA(250)
	if objA.CountA != 2 {
		t.Fatalf("A 第二次命中失败：CountA=%d（缓存隔离破坏）", objA.CountA)
	}
	if objB.CountA != 0 {
		t.Fatalf("B 仍不得被串写：%d", objB.CountA)
	}
	fmt.Println("SCEN-DONE r25r2-dual-instance")
}
`)
	assertProbeHealth(t, "r25r2-dual-instance", out)
}

// TestR25R2BareSlotNegative 裸 Slot 负例（R25-R3 准确命名）：Page 中未经
// Def 实例化的 <Slot/> 到达 Codegen 被明确拒绝。**范围仅限裸 Slot 负例**——
// 合法的 Def 内 Expanded + Page 实例化调用由 TestR25R2RealDefSlotRuntime
// 正例覆盖（协调者复核确认可生成/编译/真实点击，"Def/Slot 不支持"的旧
// 结论已撤销）。
func TestR25R2BareSlotNegative(t *testing.T) {
	src := `<Goxml Version="1" ViewModel="internal/compiler/codegen/testdata/r25vm.VM">
  <Page Route="/"><Column><Slot/></Column></Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer: %v", r.Diags)
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Generate(v, bindingOpts())
	if err == nil || !strings.Contains(err.Error(), "Slot") {
		t.Fatalf("裸 Slot 应被 Codegen 明确拒绝，实际 err=%v", err)
	}
}
