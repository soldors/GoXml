package codegen

import (
	"testing"
)

// ============================================================================
// 事件切片核心验收：真实指针事件驱动**生成的 View**
//
// 观测手段（比字符串断言强得多）：使用 Gio 的 input.Router 合成
// pointer.Press / pointer.Release，走真实的 Clickable 状态机与命中测试。
// 参照 Gio 自身测试写法（widget/button_test.go、widget/dnd_test.go）。
//
// 这**不是** OS 级注入：真实窗口无法从测试进程外部合成系统指针事件
// （诚实标注，见 button_click_boundary_test.go）。
// 但它是真实事件管线：input.Router → Clickable → 我们的轮询 → 处理器。
// ============================================================================

// TestButtonRealClickInvokesHandler 点击一次 → 处理器执行恰好一次。
func TestButtonRealClickInvokesHandler(t *testing.T) {
	out := rtProbe(t, `<Button Text="+1" Click="Increment"/>`, `
func TestProbeClickOnce(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Count: 0}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var r input.Router
	ops := new(op.Ops)
	gtx := layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}

	// 帧 1：让 Clickable 注册命中区域。
	frame()
	if vm.Count != 0 {
		t.Fatalf("首帧不应触发处理器，Count=%d", vm.Count)
	}

	// 合成一次完整点击。
	r.Queue(
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()

	if vm.Count != 1 {
		t.Fatalf("点击一次后 Count = %d, 期望 1——处理器未被执行或重复执行", vm.Count)
	}

	// 再跑两帧（无新事件）：不得重复触发。
	frame()
	frame()
	if vm.Count != 1 {
		t.Fatalf("无新事件时 Count = %d, 期望仍为 1——存在虚假重复触发", vm.Count)
	}
	t.Log("真实指针点击：处理器恰好执行一次，无事件帧不重复触发")
}
`)

	if !contains(out, "[err=nil]") {
		t.Fatalf("点击验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestButtonTwoClicksTwoCalls 两次点击 → 两次调用（非合并、非丢失）。
func TestButtonTwoClicksTwoCalls(t *testing.T) {
	out := rtProbe(t, `<Button Text="+1" Click="Increment"/>`, `
func TestProbeClickTwice(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var r input.Router
	ops := new(op.Ops)
	gtx := layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(200, 100)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()

	for i := 1; i <= 2; i++ {
		r.Queue(
			pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
			pointer.Event{Position: f32.Pt(10, 10), Kind: pointer.Release, Source: pointer.Mouse},
		)
		frame()
		if vm.Count != i {
			t.Fatalf("第 %d 次点击后 Count = %d", i, vm.Count)
		}
	}
	t.Logf("两次点击累计 Count=%d", vm.Count)
}
`)

	if !contains(out, "[err=nil]") {
		t.Fatalf("两次点击验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}

// TestButtonTwoButtonsIndependent 两个按钮各自接线，互不串扰。
func TestButtonTwoButtonsIndependent(t *testing.T) {
	out := rtProbe(t, `<Column><Button Text="inc" Click="Increment"/><Button Text="reset" Click="Reset"/></Column>`, `
func TestProbeTwoButtons(t *testing.T) {
	th := material.NewTheme()
	vm := &fixture.ViewModel{Count: 5}
	v, err := NewGoxmlDashboardView(vm)
	if err != nil {
		t.Fatal(err)
	}

	var r input.Router
	ops := new(op.Ops)
	gtx := layout.Context{
		Ops:         ops,
		Constraints: layout.Exact(image.Pt(200, 200)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()

	// 只点第一个按钮（在顶部），应执行 Increment 而非 Reset。
	r.Queue(
		pointer.Event{Position: f32.Pt(10, 5), Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary},
		pointer.Event{Position: f32.Pt(10, 5), Kind: pointer.Release, Source: pointer.Mouse},
	)
	frame()

	if vm.Count != 6 {
		t.Fatalf("点第一个按钮后 Count = %d, 期望 6（Increment，而非 Reset 归零）", vm.Count)
	}
	t.Logf("两按钮独立接线：点第一个 → Count=%d", vm.Count)
}
`)

	if !contains(out, "[err=nil]") {
		t.Fatalf("多按钮验收失败:\n%s", out)
	}
	t.Log(rtFilter(out))
}
