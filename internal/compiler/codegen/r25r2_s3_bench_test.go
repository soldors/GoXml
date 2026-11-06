package codegen

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"gioui.org/io/input"
)

// handwrittenExpandedView 等价手写形态：Row[Text, Flexed[Button]]。
type handwrittenExpandedView struct {
	btn widget.Clickable
}

func (h *handwrittenExpandedView) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, "左").Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.Button(th, &h.btn, "点我").Layout(gtx)
		}),
	)
}

// BenchmarkR25R2HandwrittenExpandedStableLayout 手写对照：同控件树/主题/
// 约束；预热 3 帧在计时外。与 generated 侧（临时模块 bench）口径一致。
func BenchmarkR25R2HandwrittenExpandedStableLayout(b *testing.B) {
	h := &handwrittenExpandedView{}
	th := material.NewTheme()
	var r input.Router
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Constraints: layout.Exact(image.Pt(300, 60)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Source:      r.Source(),
	}
	frame := func() {
		gtx.Reset()
		h.Layout(gtx, th)
	}
	frame()
	frame()
	frame()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gtx.Reset()
		h.Layout(gtx, th)
	}
}
