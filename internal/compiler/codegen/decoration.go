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

// leafDecorationExpr 为叶子（Text/Button/Input）叠加静态 Padding。
//
// PRODUCT-R5-FOLLOWUP：**仅 Padding 子集**——Background（op.Record/clip/paint）
// 留在原作者候选 97a99c9 中，本子集不包含。批准后由协调者组合。
func leafDecorationExpr(n *contracts.NodeIR, importSet *importSet, inner string) string {
	if n == nil {
		return inner
	}
	return paddingInsetExpr(n, importSet, inner)
}
