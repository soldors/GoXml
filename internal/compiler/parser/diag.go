package parser

import (
	"github.com/goxml/goxml/contracts"
)

// Span、Position 直接复用契约中的类型，避免在 parser 里定义平行结构。
type (
	Span     = contracts.SourceSpan
	Position = contracts.Position
)

// 语法层诊断码。与 contracts 中的前缀常量保持一致。
//
// 归集说明：
//   - GXML-ATTR-003（CodeAttrCall）是「属性值或用法不合法」的归集码，
//     涵盖取值格式错误、布局属性用在不允许的位置、Grid 结构约束等。
//   - 事件语法错误统一归 Expander 的 CodeEventSyntax（GXML-EVENT-002）。
//     Parser 不定义事件码，也不得重复占用该编号。
const (
	CodeSyntax   = contracts.CodePrefixSyntax + "-001"
	CodeXML      = contracts.CodePrefixSyntax + "-002"
	CodeRoot     = contracts.CodePrefixRoot + "-001"
	CodeAttrDup  = contracts.CodePrefixProp + "-001"
	CodeAttrCall = contracts.CodePrefixProp + "-003"
	CodeControl  = contracts.CodePrefixScope + "-002"
	CodeKey      = contracts.CodePrefixScope + "-003"
	// CodeInputInFor 是「<For> 内含 <Input>」的拒绝码（v1 静态 Input only）。
	//
	// 语义见《Input 输出契约》§7；诊断码目录中登记为**定稿态**，
	// 本常量的存在即表示已进入实现态（目录同步由 diagdoc 门禁保证）。
	CodeInputInFor = contracts.CodePrefixScope + "-005"
	CodeDef        = contracts.CodePrefixDef + "-001"
	CodeDefName    = contracts.CodePrefixDef + "-002"
	CodeTextNode   = contracts.CodePrefixSyntax + "-004"
)

// diag 构造带源码范围的诊断。
func (p *parser) diag(s Span, code, format string, args ...any) contracts.CompilerError {
	return contracts.NewError(p.file, s, code, format, args...)
}
