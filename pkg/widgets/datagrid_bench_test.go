package widgets

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/font"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// B3 基准。范围声明（对齐性能预算 §6.1/BUG-002 口径）：
//   - layout-only：组件 Layout 自身，不含 input.Router.Frame；
//   - frame：完整离屏帧（Layout + Router.Frame 命中区收集），单列；
//   - first-frame-cold：绘制宏缓存冷启动（每迭代清缓存）；
//   - set-rows-update：显式数据更新（含 O(N) RowID 校验）。
// 全部使用真实公开组件 Layout 与预生成显示字符串；报告 ns/op、B/op、
// allocs/op 与实际访问行数。数值为本机微基准，不外推 GPU/整应用。

type dgBenchHarness struct {
	b       *testing.B
	r       input.Router
	th      *material.Theme
	ops     op.Ops
	gtx     layout.Context
	overlay func(gtx layout.Context)
}

func (hb *dgBenchHarness) frame() {
	hb.ops.Reset()
	hb.gtx.Ops = &hb.ops
	if hb.overlay != nil {
		hb.overlay(hb.gtx)
	}
	hb.r.Frame(&hb.ops)
}

func dgBenchGrid(b *testing.B, rows int) (*DataGrid, *dgBenchHarness) {
	b.Helper()
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns([]DataGridColumn{
		{Title: "名称", WidthDp: unit.Dp(100)},
		{Title: "大小", WidthDp: unit.Dp(100)},
		{Title: "时间", WidthDp: unit.Dp(100)},
	}); err != nil {
		b.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(rows)); !rep.OK {
		b.Fatalf("数据初始化失败: %+v", rep)
	}
	hb := &dgBenchHarness{b: b, th: material.NewTheme()}
	hb.gtx = layout.Context{Ops: &hb.ops, Source: hb.r.Source(), Constraints: layout.Exact(image.Pt(300, 200))}
	hb.overlay = func(gtx layout.Context) { g.Layout(gtx, hb.th) }
	return g, hb
}

func BenchmarkDataGrid(b *testing.B) {
	for _, rows := range []int{10000, 100000} {
		b.Run(dgScaleName(rows)+"/layout-only", func(b *testing.B) {
			g, hb := dgBenchGrid(b, rows)
			for i := 0; i < 50; i++ {
				hb.frame() // 预热：文本宏缓存建立
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				hb.ops.Reset()
				g.Layout(hb.gtx, hb.th)
			}
			b.StopTimer()
			st := g.LastFrame()
			b.Logf("accessed rows/frame=%d drawn=%d", len(st.Accessed), len(st.Drawn))
		})
		b.Run(dgScaleName(rows)+"/frame", func(b *testing.B) {
			_, hb := dgBenchGrid(b, rows)
			for i := 0; i < 50; i++ {
				hb.frame()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				hb.frame()
			}
		})
		b.Run(dgScaleName(rows)+"/first-frame-cold", func(b *testing.B) {
			g, hb := dgBenchGrid(b, rows)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				g.clearTextCache()
				b.StartTimer()
				hb.frame()
			}
		})
		b.Run(dgScaleName(rows)+"/set-rows-update", func(b *testing.B) {
			g, _ := dgBenchGrid(b, rows)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				fresh := dgTestRows(rows)
				b.StartTimer()
				rep := g.SetRows(fresh)
				if !rep.OK {
					b.Fatal("更新失败")
				}
			}
		})
	}
}

// TestDGStableLayoutZeroAllocs 是 INV-PERF-2 口径的机器可判定断言：
// 稳定帧（缓存热、无输入事件）组件 Layout 热路径 0 allocs/op，且万行
// 可见布局 ≤1ms（性能预算 §4；本机微基准口径）。
func TestDGStableLayoutZeroAllocs(t *testing.T) {
	g := NewDataGrid(unit.Dp(dgMatrixRowH))
	if err := g.SetColumns(dgTestCols()); err != nil {
		t.Fatal(err)
	}
	if rep := g.SetRows(dgTestRows(10000)); !rep.OK {
		t.Fatal("初始化失败")
	}
	var ops op.Ops
	var router input.Router
	th := material.NewTheme()
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(300, 200))}
	frame := func() {
		ops.Reset()
		gtx.Ops = &ops
		g.Layout(gtx, th)
		router.Frame(&ops)
	}
	for i := 0; i < 100; i++ {
		frame() // 预热：宏缓存、访问缓冲、列宽缓冲达到稳态
	}
	res := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			ops.Reset()
			gtx.Ops = &ops
			g.Layout(gtx, th)
		}
	})
	if allocs := res.AllocsPerOp(); allocs != 0 {
		t.Fatalf("稳定帧 Layout 热路径必须 0 allocs/op，得到 %d（耗时 %v/%d 次迭代）", allocs, res.T, res.N)
	}
	if ns := res.T / time.Duration(res.N); ns > 1_000_000 {
		t.Fatalf("万行可见布局应 ≤1ms（性能预算 §4），得到 %v/op", ns)
	}
}

// BenchmarkListBaseline 是「等价手写 Gio/已有 layout.List」对照：同文字/
// 数据/约束、固定可见窗口，用 layout.List 渲染同一批预生成字符串
// （无组件侧宏缓存，逐帧 shaping——这是手写基线的常见形态）。
func BenchmarkListBaseline(b *testing.B) {
	const rows = 10000
	data := dgTestRows(rows)
	lst := &layout.List{Axis: layout.Vertical}
	th := material.NewTheme()
	shaper := th.Shaper
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(300, 200))}
	labels := make([]widget.Label, 64) // 复用（与组件侧一致：不重建对象）
	textColor := color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xFF}
	frame := func() {
		ops.Reset()
		gtx.Ops = &ops
		lst.Layout(gtx, rows, func(gtx layout.Context, i int) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				col := op.Record(gtx.Ops)
				paint.ColorOp{Color: textColor}.Add(gtx.Ops)
				textCall := col.Stop()
				return labels[i%len(labels)].Layout(gtx, shaper, font.Font{}, unit.Sp(13), data[i].Cells[0], textCall)
			})
		})
		router.Frame(&ops)
	}
	for i := 0; i < 50; i++ {
		frame()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		frame()
	}
}
