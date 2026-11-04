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
//
// R14-B：限定符经 g.q 解析（目标包已有同名包级符号时随 import 一起改别名）。
func paddingInsetExpr(g *generator, n *contracts.NodeIR, inner string) string {
	if n == nil || n.Padding.IsZero() {
		return inner
	}
	ql := g.q("gioui.org/layout", "layout")
	qu := g.q("gioui.org/unit", "unit")
	e := n.Padding
	var b strings.Builder
	b.WriteString(ql + ".Inset{")
	b.WriteString("Top: " + qu + ".Dp(" + formatDp(e.Top) + "), ")
	b.WriteString("Bottom: " + qu + ".Dp(" + formatDp(e.Bottom) + "), ")
	b.WriteString("Left: " + qu + ".Dp(" + formatDp(e.Left) + "), ")
	b.WriteString("Right: " + qu + ".Dp(" + formatDp(e.Right) + ")")
	b.WriteString("}.Layout(gtx, func(gtx " + ql + ".Context) " + ql + ".Dimensions {\n")
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
//
// R14-B：限定符经 g.q 解析，与 import 块同步。
func leafDecorationExpr(g *generator, n *contracts.NodeIR, inner string) string {
	if n == nil {
		return inner
	}
	bg := n.Background
	if bg == nil || bg.IsThemeRef {
		return paddingInsetExpr(g, n, inner)
	}
	// 先构造 Padding 包裹（外框布局），Background 覆盖其返回的外框尺寸。
	padded := paddingInsetExpr(g, n, inner)
	qi := g.q("image", "image")
	qc := g.q("image/color", "color")
	ql := g.q("gioui.org/layout", "layout")
	qop := g.q("gioui.org/op", "op")
	qclip := g.q("gioui.org/op/clip", "clip")
	qpaint := g.q("gioui.org/op/paint", "paint")
	col := qc + ".NRGBA{R: " + u8(bg.R) + ", G: " + u8(bg.G) + ", B: " + u8(bg.B) + ", A: " + u8(bg.A) + "}"
	var b strings.Builder
	b.WriteString("func(gtx " + ql + ".Context) " + ql + ".Dimensions {\n")
	b.WriteString("\t\t\t\tm := " + qop + ".Record(gtx.Ops)\n")
	b.WriteString("\t\t\t\tdims := " + padded + "\n")
	b.WriteString("\t\t\t\tcall := m.Stop()\n")
	b.WriteString("\t\t\t\tif dims.Size.X > 0 && dims.Size.Y > 0 {\n")
	b.WriteString("\t\t\t\t\tr := " + qclip + ".Rect{Max: " + qi + ".Pt(dims.Size.X, dims.Size.Y)}.Push(gtx.Ops)\n")
	b.WriteString("\t\t\t\t\t" + qpaint + ".Fill(gtx.Ops, " + col + ")\n")
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
