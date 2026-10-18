// Package sizelayout 把规范 §3.1.7 的尺寸决策规则实现为可测试的纯函数。
//
// 它回答的是「这个方向应该怎么布局」，与具体渲染无关：
//
//   - 主轴：参与弹性分配（权重 1）、固定精确值，或按内容自然尺寸；
//   - 交叉轴：填充可用上限、固定精确值，或按内容自然尺寸；
//   - 无界时 `Fill` 退化为自然尺寸。
//
// Codegen 应以此为准生成约束代码，避免各处自行解释规范。
package sizelayout

import "github.com/goxml/goxml/contracts"

// UnboundedThreshold 判定「无界」的阈值（规范 §3.1.7）。
//
// 任何现实显示设备的单边像素数都远低于此（8K 为 7680），
// 而 Gio 用 math.MaxInt 表示无界，必然触发。
const UnboundedThreshold = 1 << 20

// Axis 一个方向相对父容器的角色。
type Axis uint8

const (
	// AxisMain 主轴。
	AxisMain Axis = iota
	// AxisCross 交叉轴。
	AxisCross
)

// Directive 某个方向的布局指令。
type Directive uint8

const (
	// DirectiveNatural 按内容自然尺寸，不施加额外约束。
	DirectiveNatural Directive = iota
	// DirectiveFlex 参与主轴弹性分配，权重 1。
	DirectiveFlex
	// DirectiveFillCross 填充交叉轴可用上限。
	DirectiveFillCross
	// DirectiveExact 精确到 Value。
	DirectiveExact
)

// String 便于测试与诊断输出。
func (d Directive) String() string {
	switch d {
	case DirectiveNatural:
		return "Natural"
	case DirectiveFlex:
		return "Flex"
	case DirectiveFillCross:
		return "FillCross"
	case DirectiveExact:
		return "Exact"
	default:
		return "Unknown"
	}
}

// IsUnbounded 报告某个方向的可用上限是否视为无界。
func IsUnbounded(max int) bool {
	return max >= UnboundedThreshold
}

// Resolve 解析单个方向的布局指令。
//
// 参数：
//   - spec    该方向的尺寸规格（自然 / Fill / 固定）
//   - axis    该方向相对父容器的角色
//   - max     父容器传入的该方向可用上限
//
// 返回的 DirectiveExact 需要读取 spec.Value 得到具体像素值。
func Resolve(spec contracts.SizeSpec, axis Axis, max int) Directive {
	switch spec.Mode {
	case contracts.SizeFixed:
		// 固定值不受父容器上限影响：允许溢出（规范 §3.1.5）。
		return DirectiveExact

	case contracts.SizeFill:
		if axis == AxisMain {
			// 主轴 Fill 参与弹性分配，与是否有界无关：
			// 无界主轴意味着父容器本身可滚动，弹性分配仍有意义。
			return DirectiveFlex
		}
		// 交叉轴 Fill 需要一个填充基准；无界时没有基准，退化为自然尺寸。
		if IsUnbounded(max) {
			return DirectiveNatural
		}
		return DirectiveFillCross

	default:
		return DirectiveNatural
	}
}

// ResolveMainCross 一次性解析两个方向。
func ResolveMainCross(
	width, height contracts.SizeSpec,
	mainIsHorizontal bool,
	availWidth, availHeight int,
) (main, cross Directive) {
	if mainIsHorizontal {
		return Resolve(width, AxisMain, availWidth),
			Resolve(height, AxisCross, availHeight)
	}
	return Resolve(height, AxisMain, availHeight),
		Resolve(width, AxisCross, availWidth)
}
