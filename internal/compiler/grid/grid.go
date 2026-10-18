// Package grid 提供 Grid 列宽计算的参考实现（规范 §3.1.6）。
//
// 这不是运行时或 Codegen 的生产路径：Codegen 会把列定义直接翻译为
// layout.Rigid/layout.Flexed，由 Gio 完成实际分配。
//
// 本包存在的目的是把规范里写死的取整规则变成可执行、可测试的代码：
//
//   - 规范里的算法必须有唯一的可执行解释，否则文档、Codegen 与测试
//     会各自形成一套理解；
//   - 测试用本实现预测列宽，再与 Gio 实测输出对照（见 grid_test.go
//     中标注「Gio 实测」的用例），确保规范没有与实际行为脱节。
//
// 因此本包是**规范的 oracle**，改动它等同于改规范。
package grid

import "github.com/goxml/goxml/contracts"

// Widths 计算一行中每个单元格的整数像素宽度。
//
// 参数：
//   - cols     列定义（固定宽或权重）
//   - available 容器在该方向上的可用宽度
//   - gap      列间距（相邻列之间，共 cols-1 个）
//
// 返回的切片与 cols 等长且顺序一致。
//
// 算法（规范 §3.1.6，与 Gio 的 layout.Flex 一致）：
//
//	弹性空间 = available − Σ固定列 − gap×(列数−1)      若 < 0 则取 0
//	remaining = 弹性空间；flexTotal = 弹性空间
//	对每个弹性列（按列序）：
//	    若 remaining <= 0：取 0
//	    理想 = flexTotal × 权重 / Σ权重
//	    实际 = floor(理想 + 结转 + 0.5)
//	    结转 = 理想 − 实际
//	    若 实际 > remaining：实际 = remaining
//	    remaining -= 实际
//
// 结转必须按列序传递，这就是 100px 三等分得到 [33 34 33] 而非
// [33 33 34] 的原因。
//
// remaining 的追踪不可省略：它保证总分配量不超过弹性空间。
// 例如可用 1px、4 个等权列时，若只按「理想 + 结转」取整会得到
// [0 1 0 1]（合计 2，超出 1px），加上 remaining 追踪才得到 [0 1 0 0]。
func Widths(cols []contracts.GridColumn, available, gap int) []int {
	out := make([]int, len(cols))
	if len(cols) == 0 {
		return out
	}

	// 1. 扣除固定列与列间距，得到弹性空间。
	flexSpace := available
	if len(cols) > 1 && gap > 0 {
		flexSpace -= gap * (len(cols) - 1)
	}
	var totalWeight float32
	for _, c := range cols {
		switch c.Mode {
		case contracts.GridColumnFixed:
			flexSpace -= int(c.Value)
		case contracts.GridColumnFlex:
			totalWeight += c.Value
		}
	}
	if flexSpace < 0 {
		flexSpace = 0
	}

	// 2. 固定列直接取定义值。
	for i, c := range cols {
		if c.Mode == contracts.GridColumnFixed {
			out[i] = int(c.Value)
		}
	}

	// 3. 弹性列按列序分配，结转舍入误差并追踪剩余量。
	if totalWeight <= 0 {
		return out
	}
	remaining := flexSpace
	flexTotal := flexSpace
	var carry float32
	for i, c := range cols {
		if c.Mode != contracts.GridColumnFlex {
			continue
		}
		var actual int
		if remaining > 0 {
			ideal := float32(flexTotal) * c.Value / totalWeight
			actual = int(ideal + carry + 0.5)
			carry = ideal - float32(actual)
			if actual > remaining {
				actual = remaining
			}
		}
		out[i] = actual
		remaining -= actual
	}
	return out
}
