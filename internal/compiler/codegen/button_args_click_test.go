package codegen

import (
	"testing"
)

// ============================================================================
// 带参事件的运行时证据：实参真的流进了处理器
//
// 只断言生成源码里出现 `v.vm.RecordText("draft")` 是不够的——
// 那证明的是文本，不是行为。这里用真实指针事件驱动生成代码，
// 再检查 ViewModel 上被处理器记录下来的**实参值**。
// ============================================================================

const clickProbePrelude = `
func clickProbeFrame(v *GoxmlDashboardView, th *material.Theme, r *input.Router, gtx *layout.Context) {
	gtx.Reset()
	v.Layout(*gtx, th)
	r.Frame(gtx.Ops)
}
`

// TestArgStringLiteralReachesHandler 字符串字面量实参到达处理器。
func TestArgStringLiteralReachesHandler(t *testing.T) {
	out := rtProbe(t, `<Button Text="t" Click="RecordText('draft')"/>`, clickProbePrelude+`
func TestProbeArgString(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	clickProbeFrame(v, th, &r, &gtx)

	r.Queue(
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse},
	)
	clickProbeFrame(v, th, &r, &gtx)

	if vm.LastText != "draft" {
		t.Fatalf("处理器收到的字符串实参 = %q, 期望 \"draft\"——实参未正确传入", vm.LastText)
	}
	t.Log("字符串字面量实参到达处理器")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("字符串实参点击失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestArgIntLiteralReachesHandler 整数字面量实参到达处理器。
func TestArgIntLiteralReachesHandler(t *testing.T) {
	out := rtProbe(t, `<Button Text="t" Click="RecordNum(42)"/>`, clickProbePrelude+`
func TestProbeArgInt(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	clickProbeFrame(v, th, &r, &gtx)
	r.Queue(
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse},
	)
	clickProbeFrame(v, th, &r, &gtx)

	if vm.LastNum != 42 {
		t.Fatalf("处理器收到的整数实参 = %d, 期望 42", vm.LastNum)
	}
	t.Log("整数字面量实参到达处理器")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("整数实参点击失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestArgPathReachesHandler 路径实参取的是**当前值**（点击时求值）。
func TestArgPathReachesHandler(t *testing.T) {
	out := rtProbe(t, `<Button Text="t" Click="RecordNum(User.Age)"/>`, clickProbePrelude+`
func TestProbeArgPath(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	vm.User.Age = 7
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	clickProbeFrame(v, th, &r, &gtx)

	// 改变路径值：处理器应收到**点击时**的值，而不是录制时的。
	vm.User.Age = 99
	r.Queue(
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse},
	)
	clickProbeFrame(v, th, &r, &gtx)

	if vm.LastNum != 99 {
		t.Fatalf("路径实参应取当前值 99, 实际 %d", vm.LastNum)
	}
	t.Log("路径实参在点击时求值（取当前 99，而非首帧的 7）")
}
`)
	if !contains(out, "[err=nil]") {
		t.Fatalf("路径实参点击失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
