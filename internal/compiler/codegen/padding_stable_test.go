package codegen

// R5-A 稳态守卫：真实生成模块的无Padding/有Padding两态均要求稳定帧零分配。
// 不重定义性能预算，也不把该固定夹具外推成所有页面的性能验收。

import (
	"strings"
	"testing"
)

// TestDecorationStableFrameAllocsRuntime —— 装饰页稳定帧 + 零分配断言。
func TestDecorationStableFrameAllocsRuntime(t *testing.T) {
	cases := []struct{ name, gxml string }{
		{"Plain", `<Column><Text Value="A"/><Text Value="B"/></Column>`},
		{"Padded", `<Column Padding="8"><Text Value="A" Padding="4"/><Text Value="B"/></Column>`},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			src := mustGen(t, c.gxml, defaultOpts())
			probe := `
func TestProbeDecorationStable(t *testing.T) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var ops1, ops2 op.Ops
	d1 := v.Layout(sexact(&ops1, 200, 200), th)
	d2 := v.Layout(sexact(&ops2, 200, 200), th)
	if d1.Size != d2.Size {
		t.Fatalf("稳定帧尺寸不一致: %v vs %v", d1.Size, d2.Size)
	}
	// AllocsPerRun（同一视图、ops 每次 Reset 复用，模拟稳定帧）
	var ops op.Ops
	gtx := sexact(&ops, 200, 200)
	_ = v.Layout(gtx, th) // 预热
	allocs := testing.AllocsPerRun(50, func() {
		ops.Reset()
		v.Layout(gtx, th)
	})
	if allocs != 0 {
		t.Fatalf("stable frame allocated: %v", allocs)
	}
	t.Logf("ALLOCS=%v dims=%v", allocs, d1.Size)
}
`
			out, err := genModuleWithProbe(t, src, sizeProbeHeader+probe)
			if strings.Contains(out, "NO_HEADLESS") {
				t.Skipf("本机无 headless:%s", out)
			}
			if err != nil {
				t.Fatalf("探针失败: %v\n%s", err, out)
			}
			if strings.Contains(out, "--- FAIL") {
				t.Fatalf("探针断言失败:\n%s", out)
			}
			t.Logf("%s", lastLogLines(out, 4))
		})
	}
}
