package codegen

import (
	"strings"

	"github.com/goxml/goxml/contracts"
)

// paddingInsetExpr 把节点 Padding 包成 `layout.Inset{...}.Layout(gtx, <widget>)`。
//
// 约定：
//   - Padding 为零值（四边全 0）→ 原样返回 inner（零装饰无副作用，也不引入分配）。
//   - 否则生成 `layout.Inset{Top: unit.Dp(v), ...}.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return <inner> })`。
//     Gio 的 `layout.Inset` 字段是 `unit.Dp`，其 `Layout` 内部再调 `gtx.Dp` 换算，
//     因此这里直接给 dp 值（`unit.Dp(v)`），与 gapFieldExpr 的做法一致。
func paddingInsetExpr(n *contracts.NodeIR, importSet *importSet, inner string) string {
	if n == nil || n.Padding.IsZero() {
		return inner
	}
	importSet.add("gioui.org/layout", "layout")
	importSet.add("gioui.org/unit", "unit")
	e := n.Padding
	var b strings.Builder
	b.WriteString("layout.Inset{")
	b.WriteString("Top: unit.Dp(" + formatDp(e.Top) + "), ")
	b.WriteString("Bottom: unit.Dp(" + formatDp(e.Bottom) + "), ")
	b.WriteString("Left: unit.Dp(" + formatDp(e.Left) + "), ")
	b.WriteString("Right: unit.Dp(" + formatDp(e.Right) + ")")
	b.WriteString("}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {\n")
	b.WriteString("\t\t\t\treturn " + inner + "\n")
	b.WriteString("\t\t\t})")
	return b.String()
}

// leafDecorationExpr 为叶子（Text/Button/Input）叠加静态 Padding 与静态 Background。
//
// R6 用户裁决（2026-11-02）：采用 B（OuterBox），背景包含 Padding。
// Background 覆盖**含 Padding 的外框**（内容尺寸 + 四边 Padding）。实现：把
// `layout.Inset{...}.Layout(gtx, 内容)` 整个 record，取其返回尺寸（Inset.Layout
// 返回 Size=内容 + 四边内边距，即外框尺寸），先按外框 dims 画背景、再 call.Add。
func leafDecorationExpr(n *contracts.NodeIR, importSet *importSet, inner string) string {
	if n == nil {
		return inner
	}
	bg := n.Background
	if bg == nil || bg.IsThemeRef {
		return paddingInsetExpr(n, importSet, inner)
	}
	// 先构造 Padding 包裹（外框布局），Background 覆盖其返回的外框尺寸。
	padded := paddingInsetExpr(n, importSet, inner)
	importSet.add("image", "image")
	importSet.add("image/color", "color")
	importSet.add("gioui.org/layout", "layout")
	importSet.add("gioui.org/op", "op")
	importSet.add("gioui.org/op/clip", "clip")
	importSet.add("gioui.org/op/paint", "paint")
	col := "color.NRGBA{R: " + u8(bg.R) + ", G: " + u8(bg.G) + ", B: " + u8(bg.B) + ", A: " + u8(bg.A) + "}"
	var b strings.Builder
	b.WriteString("func(gtx layout.Context) layout.Dimensions {\n")
	b.WriteString("\t\t\t\tm := op.Record(gtx.Ops)\n")
	b.WriteString("\t\t\t\tdims := " + padded + "\n")
	b.WriteString("\t\t\t\tcall := m.Stop()\n")
	b.WriteString("\t\t\t\tif dims.Size.X > 0 && dims.Size.Y > 0 {\n")
	b.WriteString("\t\t\t\t\tr := clip.Rect{Max: image.Pt(dims.Size.X, dims.Size.Y)}.Push(gtx.Ops)\n")
	b.WriteString("\t\t\t\t\tpaint.Fill(gtx.Ops, " + col + ")\n")
	b.WriteString("\t\t\t\t\tr.Pop()\n")
	b.WriteString("\t\t\t\t}\n")
	b.WriteString("\t\t\t\tcall.Add(gtx.Ops)\n")
	b.WriteString("\t\t\t\treturn dims\n")
	b.WriteString("\t\t\t}(gtx)")
	return b.String()
}

// u8 把 uint8 格式化为十进制字面量。
func u8(v uint8) string {
	if v == 0 {
		return "0"
	}
	var d []byte
	for v > 0 {
		d = append([]byte{byte('0' + v%10)}, d...)
		v /= 10
	}
	return string(d)
}
