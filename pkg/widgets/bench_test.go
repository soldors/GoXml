package widgets

import (
	"image"
	"testing"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget/material"
)

// 开/关布局成本基准。
//
// 范围声明（重要）：本基准度量的是「组件 Layout + 事件消费 + Router 帧
// 登记」的纯 CPU 成本——ops 被录制但不渲染（无 GPU、无绘制、无窗口）。
// Router.Frame 的命中区收集计入打开态成本；关闭态退路不含 Router 帧
// 以外的组件工作。数值随机器而异，只用于回归对比，不做绝对承诺。
func BenchmarkContextMenuLayout(b *testing.B) {
	menu := NewContextMenu(
		MenuItem{Title: "剪切", Action: "cut"},
		MenuItem{Title: "粘贴", Action: "paste"},
		MenuItem{Title: "复制", Action: "copy"},
	)
	th := material.NewTheme()

	b.Run("closed", func(b *testing.B) {
		ops := new(op.Ops)
		gtx := layout.Context{
			Ops:         ops,
			Constraints: layout.Exact(image.Pt(300, 200)),
		}
		for b.Loop() {
			ops = new(op.Ops)
			gtx.Ops = ops
			menu.Layout(gtx, th)
		}
	})

	b.Run("open", func(b *testing.B) {
		var r input.Router
		ops := new(op.Ops)
		gtx := layout.Context{
			Ops:         ops,
			Source:      r.Source(),
			Constraints: layout.Exact(image.Pt(300, 200)),
		}
		menu.OpenAt(image.Pt(20, 20))
		// 预热一帧建立命中区，避免首帧分支污染。
		menu.Layout(gtx, th)
		r.Frame(ops)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			ops = new(op.Ops)
			gtx.Ops = ops
			menu.Layout(gtx, th)
			r.Frame(ops)
		}
	})
}

func BenchmarkModalLayout(b *testing.B) {
	modal := NewModal(func(gtx layout.Context, th *material.Theme) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(120, 80)}
	})
	th := material.NewTheme()

	b.Run("closed", func(b *testing.B) {
		ops := new(op.Ops)
		gtx := layout.Context{
			Ops:         ops,
			Constraints: layout.Exact(image.Pt(300, 200)),
		}
		for b.Loop() {
			ops = new(op.Ops)
			gtx.Ops = ops
			modal.Layout(gtx, th)
		}
	})

	b.Run("open", func(b *testing.B) {
		var r input.Router
		ops := new(op.Ops)
		gtx := layout.Context{
			Ops:         ops,
			Source:      r.Source(),
			Constraints: layout.Exact(image.Pt(300, 200)),
		}
		modal.Open()
		modal.Layout(gtx, th)
		r.Frame(ops)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			ops = new(op.Ops)
			gtx.Ops = ops
			modal.Layout(gtx, th)
			r.Frame(ops)
		}
	})
}
