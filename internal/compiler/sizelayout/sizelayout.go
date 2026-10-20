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

// UnboundedThreshold 是 Gio 适配层判定「某方向可用空间无界」的阈值。
//
// # 这不是语言规范的一部分
//
// 规范（§3.1.7）只要求「无界时交叉轴 Fill 退化为自然尺寸」，并要求识别
// 由本层统一负责、Parser 与 Codegen 不得各自判断。规范不规定具体阈值。
//
// 本常量是**可调整的实现参数**，用于把 Gio 的约束转换为规范中的「无界」概念。
//
// # 取值依据
//
//   - Gio 的窗口始终给出有界约束，内置滚动容器（widget.List）也不产生无界
//     约束。因此「无界」是自定义容器或测试把某方向放开时的情形，属防御性回退。
//   - 现实显示设备单边像素数远低于此（8K 宽 7680），正常布局不会误判。
//   - 明显的「无穷」标记（如 math.MaxInt）必然触发。
//
// # 何时应该改这个值
//
//   - 支持的目标设备出现单边像素数接近该量级的情况；
//   - 尺寸单位从 dp 改为更细的粒度，使同样的像素数不再意味着「无穷」；
//   - 后端更改了表示无界的约定。
//
// 改动时必须同步更新 ADR-0008 §2.8.2 与本文件的边界测试。
const UnboundedThreshold = 1 << 20 // 1,048,576 个布局单位

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

// ============================================================================
// 编译期计划（供 Codegen 使用）
//
// 上面两个函数是**运行时语义**的 oracle：输入包含可用空间，输出该方向的指令，
// 用于回答「在给定的可用空间下应该怎么布局」。
//
// 但 Codegen 是编译期的，运行时的可用空间尚不存在。它需要的不是这个问题的答案，
// 而是「运行时需要检查什么」。下面的 PlanFor 就是这个问题的答案——
// 它不复用运行时的判断结果，只复用**与可用空间无关的那部分语义**。
// ============================================================================

// Axis2D 是 Gio 约束中的某一维。
//
// 与 Axis 的区别：Axis 描述「相对父容器的主轴/交叉轴」这一**角色**，
// 而 Axis2D 描述**具体的维**（X 或 Y）。生成代码里要做的是后者：
// 交叉轴可能是 X 也可能是 Y，取决于容器方向。
type Axis2D uint8

const (
	// AxisX 水平维（gtx.Constraints.*.X）。
	AxisX Axis2D = iota
	// AxisY 垂直维（gtx.Constraints.*.Y）。
	AxisY
)

// String 便于测试与诊断输出。
func (a Axis2D) String() string {
	switch a {
	case AxisX:
		return "X"
	case AxisY:
		return "Y"
	default:
		return "Unknown"
	}
}

// CrossFillPlan 是交叉轴 Fill 在运行时执行所需的参数。
//
// 它存在的前提是「需要做一次有界性检查」——这一点由 Plan.Cross 是否为
// DirectiveFillCross 决定，因此本结构**只在那种情况下非 nil**，
// 不用零值字段去表示「不适用」，避免调用方误读。
//
// 比较方向的约定（契约 §15.6.5）：
//
//	IsUnbounded(max) 等价于 max >= Threshold；
//	因此「有界」的判据是 max < Threshold。
//
// 生成器按此约定输出 `if gtx.Constraints.Max.<A> < Threshold { ... }`。
// 两者的互补性由 TestBoundedCheckComplementsIsUnbounded 锁住。
type CrossFillPlan struct {
	// Threshold 是无界判定的阈值，唯一来源是 UnboundedThreshold。
	//
	// 生成器必须原样使用它，不得另写常量：无界这个概念在仓库里只有这一处定义
	// （规范 §3.1.7「识别无界……只有一处」）。
	Threshold int
}

// Plan 是 Codegen 使用的编译期尺寸计划。
//
// 只描述决策，**不执行任何运行时判断**：构造 Plan 不需要、也不接受运行时约束。
type Plan struct {
	// Main 是主轴方向的布局指令，Cross 是交叉轴方向的布局指令。
	Main, Cross Directive

	// MainAxis / CrossAxis 是这两个方向对应的**具体维**（X 或 Y）。
	//
	// 单独给出而不是让生成器从容器方向推导：规范 §3.1.7 要求无界识别「只有一处」，
	// 而轴向是这项识别的一部分（哪一维需要检查）。生成器去推导就等于第二处定义。
	MainAxis, CrossAxis Axis2D

	// Fill 仅在 Cross == DirectiveFillCross 时非 nil，携带运行时检查的阈值。
	//
	// 用指针而非「零值字段」表达「不适用」：Threshold 为 0 是合法数值，
	// 若用零值表示不适用，调用方无法区分「不需要检查」和「检查阈值为 0」。
	Fill *CrossFillPlan
}

// PlanFor 计算编译期尺寸计划。
//
// # 前提：假定两个方向都**有界**
//
// 编译期不存在运行时约束，这是唯一能做的假设。因此交叉轴 Fill 得到
// DirectiveFillCross（「需要填充，但要先确认有界」），其无界退化由生成代码
// 依据 Plan.Fill 的参数在运行时完成。
//
// # 为什么可以传 0 给 Resolve
//
// Resolve 的 max 只影响 Fill+Cross 一个分支（Fixed 与 Natural 不读 max，
// Fill+Main 也不读）。传 0 的意思是「有界且无可用空间」，而
// IsUnbounded(0) == false 是由 0 >= UnboundedThreshold 直接成立的，
// **不是碰巧落进某个分支**。
//
// 这一点由两条测试锁住，防止将来 Resolve 或阈值变化后 PlanFor 静默改变决策：
//   - TestZeroIsBounded：IsUnbounded(0) 必须为 false；
//   - TestPlanForMatchesResolveWhenBounded：Plan 的 Main/Cross 必须与
//     Resolve 在「有界」输入下逐项相等。
//
// 换句话说，PlanFor 不复制任何判断分支——它调用 Resolve 拿到语义，
// 再补上 Resolve 不处理的编译期元信息（两个方向分别是哪一维、阈值是多少）。
func PlanFor(width, height contracts.SizeSpec, mainIsHorizontal bool) Plan {
	main, cross := ResolveMainCross(width, height, mainIsHorizontal, 0, 0)

	mainAxis, crossAxis := AxisY, AxisX
	if mainIsHorizontal {
		mainAxis, crossAxis = AxisX, AxisY
	}

	p := Plan{Main: main, Cross: cross, MainAxis: mainAxis, CrossAxis: crossAxis}
	if cross == DirectiveFillCross {
		p.Fill = &CrossFillPlan{Threshold: UnboundedThreshold}
	}
	return p
}
