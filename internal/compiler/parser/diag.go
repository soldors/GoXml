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
const (
	CodeSyntax    = contracts.CodePrefixSyntax + "-001"
	CodeXML       = contracts.CodePrefixSyntax + "-002"
	CodeRoot      = contracts.CodePrefixRoot + "-001"
	CodeTag       = contracts.CodePrefixSyntax + "-003"
	CodeAttrDup   = contracts.CodePrefixProp + "-001"
	CodeAttrCall  = contracts.CodePrefixProp + "-003"
	CodeScope     = contracts.CodePrefixScope + "-001"
	CodeControl   = contracts.CodePrefixScope + "-002"
	CodeKey       = contracts.CodePrefixScope + "-003"
	CodeDef       = contracts.CodePrefixDef + "-001"
	CodeDefName   = contracts.CodePrefixDef + "-002"
	CodeTextNode  = contracts.CodePrefixSyntax + "-004"
	CodeEventName = contracts.CodePrefixEvent + "-001"
	CodeEvent     = contracts.CodePrefixEvent + "-002"
)

// diag 构造带源码范围的诊断。
func (p *parser) diag(s Span, code, format string, args ...any) contracts.CompilerError {
	return contracts.NewError(p.file, s, code, format, args...)
}
