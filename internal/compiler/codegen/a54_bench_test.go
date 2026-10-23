package codegen

import (
	"strings"
	"testing"
)

// A5.4：嵌套场景的缓存收益测量（与 A6a 同环境可比）。
func TestA54NestedBench(t *testing.T) {
	cases := []struct {
		name  string
		gxml  string
		label string
	}{
		{"Single", `<Text Value="Hello"/>`, "单节点"},
		{"Column3", `<Column><Text Value="a"/><Text Value="b"/><Text Value="c"/></Column>`, "Column 3 子节点"},
		{"Nested", `<Column><Text Value="a"/><Row><Text Value="b"/><Text Value="c"/></Row></Column>`, "Column 套 Row"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := mustGen(t, c.gxml, defaultOpts())

			probe := benchCommon + `
func BenchmarkNoCache(b *testing.B) {
	th := material.NewTheme()
	var ops op.Ops
	gtx := benchGtx(&ops)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()` + renderDirect(c.gxml) + `
	}
}

func BenchmarkFirstRecord(b *testing.B) {
	th := material.NewTheme()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := &GoxmlDashboardView{}
		var ops op.Ops
		v.Layout(benchGtx(&ops), th)
	}
}

func BenchmarkCacheHit(b *testing.B) {
	th := material.NewTheme()
	v := &GoxmlDashboardView{}
	var warm op.Ops
	v.Layout(benchGtx(&warm), th)

	var ops op.Ops
	gtx := benchGtx(&ops)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.Reset()
		v.Layout(gtx, th)
	}
}
`
			out := benchModule(t, src, probe)
			// 最小健全性断言（BUG-008）：这条测试此前**没有任何失败路径**，
			// 探针空转、基准一条都没跑出来时它照样 PASS——"通过"因此证明不了
			// 任何事。这里先钉住「确实产出了基准结果」这一点。
			//
			// 暂不设性能阈值：本机墙上时间的数值不能直接写成跨平台阈值
			// （见 PIT-013），阈值要等各平台基线都测过再定。
			benchLines := 0
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "Benchmark") {
					t.Logf("[%s] %s", c.label, strings.TrimSpace(line))
					benchLines++
				}
			}
			if benchLines == 0 {
				t.Fatalf("[%s] 基准未产出任何 Benchmark 行——探针可能根本没跑起来"+
					"（空转会假绿，这是本条测试曾经的实际状态）:\n%s", c.label, out)
			}
		})
	}
}

// renderDirect 生成与 gxml 等价的直接布局语句（用于 NoCache 对照）。
func renderDirect(gxml string) string {
	switch {
	case strings.Contains(gxml, "<Row>"):
		return `
		layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "a").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Body1(th, "b").Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return material.Body1(th, "c").Layout(gtx)
					}))
			}))`
	case strings.Contains(gxml, "<Column>"):
		return `
		layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "a").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "b").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "c").Layout(gtx)
			}))`
	default:
		return `
		material.Body1(th, "Hello").Layout(gtx)`
	}
}
