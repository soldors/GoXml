package analyzer

import "github.com/goxml/goxml/contracts"

// Analyzer 阶段的诊断码。
//
// 这些码的语义已在 docs/03-架构/诊断码目录.md 中定稿（状态为「定稿」而非
// 「实现」）：常量在此定义，供夹具与后续实现类型安全地引用，
// 但当前**尚未被任何检测发射**。
//
// 实现 TASK-ANALYZER-001 时，把这些码从诊断码目录的 frozen 列表迁到
// 实现列表，并补齐发射点与「该输入产生该码」的测试。
const (
	// CodeViewModelType ViewModel 声明无法解析为 Go 类型。
	//
	// 触发：文法非法、包不存在、类型不存在、类型不可作为 ViewModel。
	CodeViewModelType = contracts.CodePrefixRoot + "-002"

	// CodePathRoot 绑定路径首段无法解析。
	//
	// 首段既不是 ViewModel 的字段/方法，也不是 `<For>` 作用域变量。
	CodePathRoot = contracts.CodePrefixBind + "-001"

	// CodePathField 绑定路径中间段字段不存在。
	CodePathField = contracts.CodePrefixBind + "-002"

	// CodePathUnexported 字段存在但未导出，生成代码无法访问。
	//
	// 与 CodePathField 分开：前者要改拼写，后者要把字段名改为大写。
	CodePathUnexported = contracts.CodePrefixBind + "-003"

	// CodeBindTypeMismatch 绑定值类型与属性目标类型不匹配。
	//
	// 也用于 `{!Path}` 作用于非布尔类型（契约 §8.3）。
	CodeBindTypeMismatch = contracts.CodePrefixBind + "-004"

	// CodeBindUnsupported 绑定的表达式形式不受支持。
	//
	// 规范 §9 明确要求这类错误**不得**误报成「路径不存在」，
	// 否则用户会去检查拼写，而真正的问题是表达式形式。
	CodeBindUnsupported = contracts.CodePrefixBind + "-005"

	// CodeMethodNotFound 事件处理方法在 ViewModel 上不存在。
	CodeMethodNotFound = contracts.CodePrefixEvent + "-003"

	// CodeMethodArity 事件载荷数与调用点实参数之和 ≠ 方法形参数量。
	//
	// 计数规则见契约 §8.4：方法形参 = 事件声明参数 ++ 调用点实参。
	CodeMethodArity = contracts.CodePrefixEvent + "-004"

	// CodeMethodArgType 按「载荷在前、实参在后」的顺序，某项类型不兼容。
	CodeMethodArgType = contracts.CodePrefixEvent + "-006"

	// CodeNotIterable `<For In>` 的值不是可迭代类型。
	CodeNotIterable = contracts.CodePrefixScope + "-004"
)
