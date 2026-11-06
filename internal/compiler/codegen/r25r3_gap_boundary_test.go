package codegen

// R25-R3 S1：Gap/Padding 边界特异性（真实指针事件，非子串断言）。
//
// 布局几何（W=400, H=100, Padding=6, Gap=4, 两个 Flexed(1) 弹性项）：
//   A cell = [6, 198]，Gap = [198, 202]，B cell = [202, 394]，
//   右 Padding = [394, 400]，左 Padding = [0, 6]。
//
// 判别力：协调者变异（含 Expanded 的 Row 生成时 Padding/Gap 置零，
// mut-row-expanded-no-gap-padding.go 同款）下 A cell 变为 [0, 200]——
// 边界不命中点 x=2 / x=199 变为命中（红）；内容命中点 x=100 / x=300
// 保持命中（对照）。恢复后边界不命中恢复。

import (
	"testing"
)

func TestR25R3GapPaddingBoundaries(t *testing.T) {
	out := rtProbeVM(t, r25r2SharedVM, `<Row Height="100" Padding="6" Gap="4">
  <Expanded><Button Text="A" Click="TapA"/></Expanded>
  <Expanded><Button Text="B" Click="TapB"/></Expanded>
</Row>`, `
func TestProbeR25R3GapPaddingBoundaries(t *testing.T) {
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
			pointer.Event{Position: f32.Pt(x, 50), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(x, 50), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
	}
	// 内容命中对照：A cell 内 x=100、B cell 内 x=300。
	click(100)
	if obj.CountA != 1 {
		t.Fatalf("x=100 应命中 A：CountA=%d", obj.CountA)
	}
	click(300)
	if obj.CountB != 1 {
		t.Fatalf("x=300 应命中 B：CountB=%d", obj.CountB)
	}
	// 边界不命中：左 Padding 区 x=2、Gap 区 x=200。
	click(2)
	if obj.CountA != 1 || obj.CountB != 1 {
		t.Fatalf("x=2（左 Padding 区）不得命中任何按钮：A=%d B=%d", obj.CountA, obj.CountB)
	}
	click(200)
	if obj.CountA != 1 || obj.CountB != 1 {
		t.Fatalf("x=200（Gap 区）不得命中任何按钮：A=%d B=%d", obj.CountA, obj.CountB)
	}
	fmt.Println("SCEN-DONE r25r3-gap-padding-boundaries")
}
`)
	assertProbeHealth(t, "r25r3-gap-padding-boundaries", out)
}
