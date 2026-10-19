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
			for _, line := range strings.Split(out, "\n") {
				if strings.HasPrefix(line, "Benchmark") {
					t.Logf("[%s] %s", c.label, strings.TrimSpace(line))
				}
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
