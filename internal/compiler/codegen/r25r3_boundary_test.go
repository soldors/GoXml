package codegen

// R25-R3 S2：Exact/无界的真实边界断言（离屏事件区域 + 实际几何）。
//
//   - 自身 Exact：Expanded Width=120 → Rigid + Exact 120 → 按钮区域
//     [0,120]；x=100 命中、x=250 脱靶（区域内/外真实事件判据）。
//   - 子节点 Exact：Expanded[Button Width=60] → cell 内约束注入 →
//     按钮区域 [0,60]；x=30 命中、x=100 脱靶。
//   - 大有限主轴：Max.Y=100000，低于 sizelayout 无界阈值，按 Flexed 分配。
//     真正实现阈值边界见 TestR25R3UnboundedThresholdGeometry。
//
// 全部为离屏真实事件/几何断言；无 Dp 子串或 Generate 成功顶替。

import (
	"testing"
)

func TestR25R3SelfExactBoundary(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <Expanded Width="120"><Button Text="B" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R3SelfExactBoundary(t *testing.T) {
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
	click(100) // Exact 120 内
	if obj.CountB != 1 {
		t.Fatalf("x=100（Exact 120 内）应命中：B=%d", obj.CountB)
	}
	click(250) // Exact 120 外
	if obj.CountB != 1 {
		t.Fatalf("x=250（Exact 120 外）应脱靶：B=%d（自身 Exact 未生效）", obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r3-self-exact-boundary")
}
`)
	assertProbeHealth(t, "r25r3-self-exact-boundary", out)
}

func TestR25R3ChildExactBoundary(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="60">
  <Expanded><Button Text="B" Width="60" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R3ChildExactBoundary(t *testing.T) {
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
	click(30) // 子 Exact 60 内
	if obj.CountB != 1 {
		t.Fatalf("x=30（子 Exact 60 内）应命中：B=%d", obj.CountB)
	}
	click(100) // 子 Exact 60 外（若无子约束注入则按钮拉伸至整 cell → 命中=红）
	if obj.CountB != 1 {
		t.Fatalf("x=100（子 Exact 60 外）应脱靶：B=%d（子节点约束注入缺失）", obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r3-child-exact-boundary")
}
`)
	assertProbeHealth(t, "r25r3-child-exact-boundary", out)
}

func TestR25R3LargeFiniteColumnGeometry(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Column>
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Column>`, `
func TestProbeR25R3LargeFiniteColumn(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
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
	h := lay()
	// Gio Flex 语义（实测锁定，非规范发明）：主轴 Max 有限时，Flexed 子项
	// 获得全部剩余空间——Expanded 随之填满可用高度（h==Max.Y）。真正的
	// 数学无限不是 int 值；本例是大有限 Max，不代替 sizelayout 无界阈值，
	// 实际行为登记为边界记录，不伪造「自然高度」目标值。
	if h != 100000 {
		t.Fatalf("Flexed 语义下 Expanded 应填满可用主轴（h=Max.Y=100000），实际 h=%d", h)
	}
	fmt.Println("SCEN-DONE r25r3-unbounded-column")
}
`)
	assertProbeHealth(t, "r25r3-unbounded-column", out)
}
