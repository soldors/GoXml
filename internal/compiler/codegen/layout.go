package codegen

import (
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
