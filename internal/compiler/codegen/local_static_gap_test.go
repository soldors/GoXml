package codegen

import (
	"strings"
	"testing"
)

// R10-B B3 stable-frame Layout-only allocation guard.
// Independent review replayed this exact final probe on baseline29a0e3b and
// candidatef1ac272: both observed zero allocations. The earlier value2 included
// Router.Frame bookkeeping and is not evidence of a Layout allocation reduction.
// This guard asserts the candidate remains zero, not a baseline-to-candidate gain.
func TestLocalStaticStableFrameAllocs(t *testing.T) {
	vmSrc := probeVMSrc + "\nfunc (vm *VM) Increment() { vm.ChangeCount++ }\n"
	out := rtProbeVM(t, vmSrc, `<Column>
  <Text Value="{Title}"/>
  <Button Text="提交" Click="Increment"/>
  <Column>
    <Text Value="静态标题A"/>
    <Text Value="静态标题B"/>
  </Column>
</Column>`, `
func TestProbeLocalStaticAllocs(t *testing.T) {
	th := material.NewTheme()
	obj := &gvm.VM{}
	v, err := NewGoxmlDashboardView(obj)
	if err != nil {
		t.Fatal(err)
	}
	var r input.Router
	gtx := layout.Context{
		Ops:        new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 200)),
		Metric:     unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:     r.Source(),
	}
	frame := func() {
		gtx.Reset()
		v.Layout(gtx, th)
		r.Frame(gtx.Ops)
	}
	frame()
	frame()
	gtx.Reset()
	// 只测渲染路径 v.Layout；r.Frame 的路由簿记与缓存无关（A5 口径）。
	allocs := testing.AllocsPerRun(20, func() {
		g := gtx
		g.Reset()
		v.Layout(g, th)
	})
	fmt.Printf("LOCALSTATIC-ALLOC stable=%v\n", allocs)
	if allocs != 0 {
		t.Fatalf("稳定帧分配 %v, want 0（静态子树应回放）", allocs)
	}
	fmt.Println("SCEN-DONE localstatic-alloc")
}
`)
	assertProbeHealth(t, "localstatic-alloc", out)
	if !strings.Contains(out, "LOCALSTATIC-ALLOC stable=0") {
		t.Fatalf("稳定帧零分配未实证:\n%s", out)
	}
	t.Log(out)
}
