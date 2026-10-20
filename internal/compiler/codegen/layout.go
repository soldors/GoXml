package codegen

import (
	"fmt"
	"strconv"

	"github.com/goxml/goxml/contracts"
)

// 本文件是布局属性映射的落点（TASK-CODEGEN-001-8 ~ -13、-21 ~ -25）。
//
// 之所以独立于 codegen.go：布局映射会持续增长（对齐、分布、尺寸、Grid），
// 集中在单独文件可以避免每次扩展都改动主生成器的主体逻辑。

// gapFieldExpr 生成 `layout.Flex` 的 Gap 字段片段，未设置时返回空串。
//
// 单位换算（必须显式做，不能省略）：
//
//	GXML 语言规范 §3.1.1 定义 `Gap` 的语义单位是 **dp**；
//	Gio v0.10.3 的 `layout.Flex.Gap` 是 **int**（像素）——见 `go doc gioui.org/layout.Flex`。
//
// 两者不是同一单位，因此必须经 `gtx.Dp` 换算。若图省事直接写 `Gap: 8`，
// 在 DPI 不为 1 的屏幕上间距会整体偏小，且症状随设备变化——这类缺陷
// 在单一开发机上不会暴露。
//
// 作用域前提：容器表达式只出现在 `Layout` 体内（契约 §4.3 的签名保证 `gtx` 在作用域内），
// 静态录制路径用的是 `rec` 作为 Layout 的第一实参，但 `gtx` 本身仍在作用域中，
// 因此换算取的是真实帧上下文的 DPI，与契约 §16.5「布局结果强依赖 gtx」一致。
//
// 安全准入第 1 项（TASK-CODEGEN-001 §3.5）：数值属性**不得以原始文本拼接**，
// 必须写解析后的数值。n.Gap 已由 Parser 的 parseDp 解析为 float32，
// 这里再经 strconv 格式化为 Go 数值字面量，因此不可能是 `"8"` 这类原始文本，
// 也不可能是 `8` + 用户拼写的其余字符。
//
// 校验不在这里重复：非负数、拒绝绑定、拒绝指数与单位后缀均由 Parser 完成
// （language/layout.go 的 parseDp）。Codegen 只消费已验证的值。
func gapFieldExpr(n *contracts.NodeIR) string {
	if n == nil || !n.HasGap {
		return ""
	}
	return ", Gap: gtx.Dp(" + formatDp(n.Gap) + ")"
}

// formatDp 把 dp 数值格式化为 Go 浮点字面量。
//
// 用 'f' 而非 'g'：避免产生 `1e+06` 这类指数形式，保证输出恒为十进制字面量，
// 与规范「只接受十进制」的取向一致，也让生成代码对人可读。
func formatDp(v float32) string {
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

// ============================================================================
// TASK-CODEGEN-001-8：`Align` 交叉轴对齐
// ============================================================================

// defaultCrossAlign 返回标签的默认交叉轴对齐。
//
// 规范 §3.1.2「默认对齐是轴向感知的」与 ADR-0008 §2.3：
//
//	| 标签     | 默认 Align | 依据                       |
//	|----------|-----------|----------------------------|
//	| <Row>    | Center    | 一行元素类比一行文字，垂直居中 |
//	| <Column> | Start     | 一列内容类比一页文档，左对齐   |
//	| <Grid>   | Start     | 单元格内默认左对齐           |
//
// **补默认值是 Codegen 的责任，不是 IR 的责任**：IR 用同一个 `AlignUnset`
// 表示「未显式设置」，而它在不同标签上解析出不同结果；IR 若提前固化默认值，
// 就把「按标签区分」这件事的能力丢掉了（contracts.NodeIR.Align 注释已如此约定）。
//
// 注意 <Row> 的默认值与 Gio 的零值（Start）**不一致**，因此 Codegen 必须显式写出
// `Alignment: layout.Middle`——这是 ADR-0008 §2.3 明确接受的实现代价
// （生成代码的字符成本，无运行时成本）。省略就会静默变成左对齐。
func defaultCrossAlign(tag string) contracts.CrossAlign {
	switch tag {
	case "Row":
		return contracts.AlignCenter
	default: // <Column>、<Grid>
		return contracts.AlignStart
	}
}

// gioAlignment 把 GXML 的交叉轴对齐映射为 Gio `layout.Alignment` 的标识符。
//
// 映射边界（`go doc -all gioui.org/layout` 实测，v0.10.3）：
//
//	const ( Start Alignment = iota; End; Middle; Baseline )
//
// **Gio 没有 `Center`，居中叫 `Middle`**。这是本映射最容易写错的一处：
// 按名字直觉写 `layout.Center` 会直接编译失败（还好，能被发现）；
// 真正的危险是有人为了「先跑起来」改用 `layout.Start` 兜底——
// 那样编译通过、测试若只断言「字段存在」也会通过，但居中静默变成左对齐。
func gioAlignment(a contracts.CrossAlign) (string, error) {
	switch a {
	case contracts.AlignStart:
		return "layout.Start", nil
	case contracts.AlignCenter:
		return "layout.Middle", nil
	case contracts.AlignEnd:
		return "layout.End", nil
	case contracts.AlignBaseline:
		return "layout.Baseline", nil
	default:
		// 非法枚举必须明确失败，不静默退回 Start：
		// 退回会让「未知取值」伪装成合法的左对齐，属于契约禁止的静默降级。
		// Codegen 内部出错返回 error 且不返回部分代码（契约 §2.3 失败语义）。
		return "", fmt.Errorf("codegen: unsupported cross-align value %d on <%s>",
			uint8(a), a)
	}
}

// flexFieldsExpr 拼出 `layout.Flex` 的布局字段片段，供主生成器插入 `Axis: ...` 之后。
//
// 字段顺序按 Gio 结构体声明顺序（Axis → Spacing → Alignment → ... → Gap），
// 生成代码读起来与 `layout.Flex` 的定义对应，后续接入 Justify（Spacing）时插在前面即可。
//
// 本函数的输出恒为「, <字段>: <标识符或数值>」形态，无字段时是空串，
// 因此调用方只需无条件拼接，不需要判断。
func flexFieldsExpr(tag string, n *contracts.NodeIR) (string, error) {
	if n == nil {
		return "", nil
	}
	align := n.Align
	if align == contracts.AlignUnset {
		align = defaultCrossAlign(tag)
	}
	name, err := gioAlignment(align)
	if err != nil {
		return "", err
	}
	// Alignment 在前、Gap 在后：与 Gio 结构体字段顺序一致。
	return ", Alignment: " + name + gapFieldExpr(n), nil
}
