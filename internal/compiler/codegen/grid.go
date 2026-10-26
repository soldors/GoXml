package codegen

import (
	"fmt"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/sizelayout"
)

// ============================================================================
// `<Grid>` 的生成（规范 §3.1.6）
//
// 生成形态：**外层纵向 Flex + 每行一个横向 Flex**。
//
// 为什么不自造 Grid 原语：Gio v0.10.3 的 `layout` 包没有 Grid（只有
// Flex/Stack/List/Spacer/Inset）。规范 §3.1.6 的列宽算法（扣 Gap 后分配、
// 取整从左到右结转误差、"合计不超过可用宽度、不保证用满"）与 Gio
// `layout.Flex` 的 `Flexed(weight)` 分配语义逐条一致（规范 :286 原话：
// "与 Gio 的 layout.Flex 行为完全一致"）。重复实现会得到第二份取整逻辑，
// 两处不一致时表现为"同一行单元格错位"。
//
// 三条规范前提（规范 :288-292）在此形态下自动满足：
//  1. 固定列强制精确宽度 → 每行复用同一个 writeExactConstraint；
//  2. 补位列与原列同宽 → 补位复用同一 GridColumn 定义；
//  3. 补位列参与 Gap 计数 → 补位是真实 Flex 子项。
// ============================================================================

// gridExpr 生成 `<Grid>` 的渲染表达式。
//
// Parser 已校验 Columns 非空、子节点非空、直接子项无 Width
// （`parser/element.go:348-364`）。这里做防御性二次校验：IR 也可能被手工构造，
// 静默接受会产生"设了 Width 却不生效"这类难查的视觉错位。
func (g *generator) gridExpr(n *contracts.NodeIR) (string, error) {
	if len(n.Columns) == 0 {
		return "", fmt.Errorf("codegen: <Grid> requires at least one column definition")
	}
	for i, c := range n.Columns {
		if !c.IsValid() {
			return "", fmt.Errorf(
				"codegen: <Grid> column %d is invalid (mode=%d value=%v): "+
					"fixed width must be >= 0 dp, flex weight must be >= 1",
				i, c.Mode, c.Value)
		}
	}
	if len(n.Children) == 0 {
		return "", fmt.Errorf("codegen: <Grid> requires at least one render child")
	}
	for _, c := range n.Children {
		// 规范 §3.1.6：列定义是单元格宽度的唯一来源。
		if c.Width.Mode != contracts.SizeNatural {
			return "", fmt.Errorf(
				"codegen: <Grid> children must not set Width (column definitions own the cell width); " +
					"wrap the content in a <Row> or <Column> if you need independent width")
		}
		// <For>/<If> 在 Grid 内的单元格展开会改变行/列归属：
		// 循环项的个数编译期未知，无法确定它们落在哪一列，补位与 Gap 计数
		// 也随之失去依据。现有 dynamicContainerExpr 是 Flex 专用
		// （它生成 []layout.FlexChild），复用到 Grid 会破坏列宽语义，
		// 因此这里**显式拒绝**，不生成语义错误的代码。
		if isForNode(c) || isIfNode(c) {
			return "", fmt.Errorf(
				"codegen: <%s> inside <Grid> is not supported in this slice: "+
					"the cell it occupies cannot be determined at compile time; "+
					"move it into a <Row> or <Column> cell instead", c.Tag)
		}
	}

	g.imports.add("gioui.org/layout", "layout")

	// 行优先切分（规范 :232-233）。
	perRow := len(n.Columns)
	rows := make([][]*contracts.NodeIR, 0, (len(n.Children)+perRow-1)/perRow)
	for i := 0; i < len(n.Children); i += perRow {
		end := i + perRow
		if end > len(n.Children) {
			end = len(n.Children)
		}
		rows = append(rows, n.Children[i:end])
	}

	// `Grid Align` 映射到**每行**横向 Flex 的 Alignment（规范 :316）。
	// 复用既有 defaultCrossAlign/gioAlignment，不新增对照表。
	align := n.Align
	if align == contracts.AlignUnset {
		align = defaultCrossAlign(n.Tag)
	}
	alignName, err := gioAlignment(align)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("layout.Flex{Axis: layout.Vertical" + gapFieldExpr(n) + "}.Layout(gtx")

	for _, row := range rows {
		b.WriteString(",\n\t\tlayout.Rigid(func(gtx layout.Context) layout.Dimensions {\n")
		b.WriteString("\t\t\treturn layout.Flex{Axis: layout.Horizontal, Alignment: " + alignName +
			gapFieldExpr(n) + "}.Layout(gtx")
		for col := 0; col < perRow; col++ {
			b.WriteString(",\n\t\t\t\t")
			if col < len(row) {
				cell, err := g.gridCellExpr(n.Columns[col], row[col])
				if err != nil {
					return "", err
				}
				b.WriteString(cell)
			} else {
				b.WriteString(g.gridPadExpr(n.Columns[col]))
			}
		}
		b.WriteString(")\n\t\t})")
	}
	b.WriteString(")")
	return b.String(), nil
}

// gridCellExpr 生成一个单元格：固定列用精确约束，弹性列用 Flexed(权重)。
//
// 两层结构的理由（`ZCode` 复审发现的类型错误）：
//
//	`sizeWrap` 的产物是 **`layout.FlexChild`**（`layout.Rigid(...)` / `layout.Flexed(...)`），
//	只能作为 `Flex.Layout(...)` 的**实参**；而单元格闭包必须返回 `layout.Dimensions`。
//	因此外层用列宽 Rigid/Flexed 决定宽度，内层再用一个**纵向 Flex** 把
//	`sizeWrap` 作为子项消费——这样 Height 的 Exact/Natural 语义仍由
//	sizelayout 决定，且返回类型正确。
//
//	layout.Rigid(func(gtx layout.Context) layout.Dimensions {      // ← 外层：列宽
//	    gtx.Constraints.Min.X = gtx.Dp(72)                        //   固定列精确 X
//	    gtx.Constraints.Max.X = gtx.Constraints.Min.X
//	    return layout.Flex{Axis: layout.Vertical}.Layout(gtx,     // ← 内层：垂直 Flex
//	        <sizeWrap(gridCellSize(child), inner, false)>,        //   Height 语义在这里生效
//	    )
//	})
//
// 纵向 Flex 不带 Gap/Alignment：单元格内只有一个子项，加 Gap 不会生效，
// 加 Alignment 会与 Grid 的行对齐（已作用于横向 Flex）语义重复。
func (g *generator) gridCellExpr(col contracts.GridColumn, child *contracts.NodeIR) (string, error) {
	inner, err := g.nodeExpr(child)
	if err != nil {
		return "", err
	}
	g.imports.add("gioui.org/layout", "layout")

	// 已拒绝 Width，故只有 Height 可能生效（mainIsHorizontal=false 时主轴=Height）。
	// sizeWrap 返回 FlexChild ⇒ 必须作为 Flex.Layout 的实参，不能出现在 return 后。
	//
	// 传入的是**归一后的副本**（gridCellSize），不是原始节点：Grid 单元格的
	// Height="Fill" 必须退化为自然高度（规范 §3.1.7 :454、TASK-CODEGEN-001 :94）。
	wrapped := sizeWrap(gridCellSize(child), inner, false)
	body := "layout.Flex{Axis: layout.Vertical}.Layout(gtx,\n" +
		"\t\t\t\t" + wrapped + ",\n" +
		"\t\t\t)"

	switch col.Mode {
	case contracts.GridColumnFixed:
		var c strings.Builder
		c.WriteString("layout.Rigid(func(gtx layout.Context) layout.Dimensions {\n")
		writeExactConstraint(&c, sizelayout.AxisX, col.Value)
		c.WriteString("\t\t\t\treturn " + body + "\n\t\t\t})")
		return c.String(), nil
	default: // GridColumnFlex
		return "layout.Flexed(" + formatDp(col.Value) + ", func(gtx layout.Context) layout.Dimensions {\n" +
			"\t\t\t\treturn " + body + "\n\t\t\t})", nil
	}
}

// gridCellSize 返回用于尺寸规划的**副本**节点，把 Grid 单元格的纵向语义归一。
//
// 规范 §3.1.7（`:454`）：「子项 `Height="Fill"` **退化为自然高度**：行高本身由
// 该行内容决定，用它作为填充基准会形成循环」。`TASK-CODEGEN-001.md:94` 同样要求。
//
// 为什么在这里做、而不改 `sizelayout`：
//
//	`sizelayout.PlanFor` 是通用层，`DirectiveFlex` 对 Column/Row 是正确语义；
//	Grid 单元格是**唯一**要求 Fill 退化为主轴自然高度的上下文（行高由内容决定）。
//	放进通用层会连带改变 Column/Row 的行为。
//
// 两处归一（只作用在副本上，不改 IR）：
//
//  1. `Height == SizeFill` → `NaturalSize()`。
//     否则 `sizeWrap` 会生成纵向 `layout.Flexed(1, ...)`，Gio 把该单元格的
//     Min.Y/Max.Y 撑到剩余高度 ⇒ 第一行吃掉整个 Grid 高度、后续行布局错位。
//
//  2. `<Spacer>` 的隐式主轴 Fill 必须被抑制。
//     `sizePlanFor`（layout.go）对 `Tag=="Spacer"` 且 Height 为 Natural 时
//     **隐式提升为 Fill**——这是为 `<Column><Spacer/></Column>`「占满剩余空间」
//     设计的。在 Grid 单元格里它同样会产生纵向 `Flexed(1)`，把行高撑满。
//     抑制手段：把副本的 Tag 改成非 "Spacer"（`sizePlanFor` 的唯一判据就是该
//     Tag 字面量），同时保留其 Width/Height 规格 ⇒ 规划结果等价于
//     "普通节点 + 自然高度"。Spacer 的**渲染表达式**已在调用方由
//     `nodeExpr` 生成完毕，与这里的尺寸规划解耦，故改 Tag 不影响产物内容。
//
// 不改 Width：Grid 的宽度由列定义拥有（子项 Width 已在 gridExpr 中被拒绝）。
func gridCellSize(n *contracts.NodeIR) *contracts.NodeIR {
	if n == nil {
		return nil
	}
	needFillFix := n.Height.Mode == contracts.SizeFill
	needSpacerFix := n.Tag == "Spacer"
	if !needFillFix && !needSpacerFix {
		return n // 无需归一，避免无谓拷贝
	}
	cp := *n
	if needFillFix {
		cp.Height = contracts.NaturalSize()
	}
	if needSpacerFix {
		// 换成中性 Tag：sizePlanFor 的特例只认 "Spacer" 字面量。
		// 该副本仅用于尺寸规划，不参与渲染表达式生成。
		cp.Tag = "gridCell"
	}
	return &cp
}

// gridPadExpr 生成补位单元格：与对应列同宽，不渲染内容。
//
// 补位必须是**真实 Flex 子项**，否则该行少扣一个 Gap、弹性列会变宽
// （规范 :307 举了这个反例）。
func (g *generator) gridPadExpr(col contracts.GridColumn) string {
	switch col.Mode {
	case contracts.GridColumnFixed:
		var c strings.Builder
		c.WriteString("layout.Rigid(func(gtx layout.Context) layout.Dimensions {\n")
		writeExactConstraint(&c, sizelayout.AxisX, col.Value)
		c.WriteString("\t\t\t\treturn layout.Spacer{}.Layout(gtx)\n\t\t\t})")
		return c.String()
	default: // GridColumnFlex
		return "layout.Flexed(" + formatDp(col.Value) + ", func(gtx layout.Context) layout.Dimensions {\n" +
			"\t\t\t\treturn layout.Spacer{}.Layout(gtx)\n\t\t\t})"
	}
}
