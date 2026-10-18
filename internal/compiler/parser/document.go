package parser

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/goxml/goxml/contracts"
)

// parser 是单次解析的状态。不导出，外部只通过 Parse 使用。
type parser struct {
	file string
	src  string
	dec  *xml.Decoder
	smap *sourceMap

	// prevOffset 是上一个 token 结束处的字节偏移。
	// encoding/xml 的 InputOffset() 在 Token() 之后指向 token 末尾，
	// 因此起始标签的原始区间是 [prevOffset, curOffset-1)。
	prevOffset int64

	// diags 收集诊断。结构错误立即返回，语义错误尽量一次收集。
	diags []contracts.CompilerError
}

// Parse 解析单个 .gxml 源码，返回文档 IR 与诊断列表。
//
// 只要返回至少一条诊断，IR 就不可用于代码生成。
// 结构错误（XML 语法、根节点）会中断解析；属性与语义错误会继续收集。
func Parse(file string, src []byte) (*contracts.DocumentIR, []contracts.CompilerError) {
	p := &parser{
		file: file,
		src:  string(src),
		dec:  xml.NewDecoder(strings.NewReader(string(src))),
	}
	p.smap = newSourceMap(p.src)
	// GXML 不允许处理指令之外的宽松模式；保持严格。
	p.dec.Strict = true

	doc, err := p.parseDocument()
	if err != nil {
		p.diags = append(p.diags, *err)
	}
	if doc != nil && len(p.diags) > 0 {
		// 有诊断时不返回可用 IR，避免下游误用半成品。
		return nil, p.diags
	}
	if doc == nil && len(p.diags) == 0 {
		p.diags = append(p.diags, p.diag(Span{}, CodeSyntax, "empty document"))
		return nil, p.diags
	}
	return doc, p.diags
}

// token 读取下一个 token 并更新偏移。
type token struct {
	xml.Token
	// start、end 是该 token 对应的原始字节区间。
	start, end int64
}

func (p *parser) next() (token, error) {
	start := p.prevOffset
	tok, err := p.dec.Token()
	end := p.dec.InputOffset()
	if err != nil {
		return token{}, err
	}
	p.prevOffset = end
	return token{Token: tok, start: start, end: end}, nil
}

// parseDocument 读取 <Goxml> 根节点及其内容。
func (p *parser) parseDocument() (*contracts.DocumentIR, *contracts.CompilerError) {
	// 跳过空白、注释、处理指令（规范禁止 PI，但先定位根节点）。
	for {
		tok, err := p.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				e := p.diag(Span{}, CodeRoot, "missing root element <%s>", tagGoxml)
				return nil, &e
			}
			e := p.xmlError(err)
			return nil, &e
		}
		switch t := tok.Token.(type) {
		case xml.ProcInst:
			e := p.diag(p.smap.span(tok.start, tok.end), CodeSyntax, "processing instruction is not allowed")
			return nil, &e
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" {
				e := p.diag(p.smap.span(tok.start, tok.end), CodeSyntax, "unexpected text before root element")
				return nil, &e
			}
			continue
		case xml.Comment:
			continue
		case xml.Directive:
			e := p.diag(p.smap.span(tok.start, tok.end), CodeSyntax, "directive is not allowed")
			return nil, &e
		case xml.StartElement:
			if t.Name.Local != tagGoxml {
				e := p.diag(p.smap.span(tok.start, tok.end), CodeRoot,
					"root element must be <%s>, got <%s>", tagGoxml, t.Name.Local)
				return nil, &e
			}
			return p.parseGoxml(tok, t)
		}
	}
}

// parseGoxml 解析根节点：属性 + 可选的 <Defs> + 必选的 <Page>。
func (p *parser) parseGoxml(tok token, se xml.StartElement) (*contracts.DocumentIR, *contracts.CompilerError) {
	rootSpan := p.elementSpan(element{tok: tok, se: se})

	doc := &contracts.DocumentIR{
		FilePath: p.file,
		Span:     rootSpan,
	}

	attrs, cerr := p.elementAttrs(tok, se)
	if cerr != nil {
		return nil, cerr
	}

	// Version 必填且必须为 "1"。
	ver, ok := attrs.Lookup("Version")
	if !ok {
		e := p.diag(rootSpan, CodeRoot, "<%s> requires attribute Version", tagGoxml)
		return nil, &e
	}
	if !ver.Value.IsBinding && ver.Value.Raw != "1" {
		e := p.diag(ver.Span, CodeRoot, "unsupported GXML version %q, expected \"1\"", ver.Value.Raw)
		return nil, &e
	}
	doc.Version = ver.Value.Raw

	// ViewModel 可选，纯静态页面可省略。
	//
	// 这里只保留原始文本与位置：把 "demo.ViewModel" 解析为包与类型
	// 属于 Analyzer 的职责（Analyzer输入输出契约 §4.2），Parser 不引入
	// Go 类型系统依赖。
	if vm, ok := attrs.Lookup("ViewModel"); ok {
		if vm.Value.IsBinding {
			e := p.diag(vm.Span, CodeAttrCall, "ViewModel must be a static Go type path, not a binding")
			return nil, &e
		}
		doc.ViewModel = contracts.ViewModelDecl{Value: vm.Value.Raw, Span: vm.Span}
	}

	// 校验未声明的根属性。
	for _, a := range attrs {
		switch a.Name {
		case "Version", "ViewModel":
		default:
			e := p.diag(a.Span, CodeAttrCall, "unknown attribute %q on <%s>", a.Name, tagGoxml)
			return nil, &e
		}
	}

	// 子节点：可选 <Defs>，必选 <Page>，顺序固定。
	var seenDefs, seenPage bool
	for {
		child, err := p.nextElement()
		if err != nil {
			return nil, err
		}
		if child == nil {
			break // 结束标签
		}
		switch child.se.Name.Local {
		case tagDefs:
			if seenDefs {
				e := p.diag(p.elementSpan(*child), CodeRoot, "duplicate <%s>", tagDefs)
				return nil, &e
			}
			if seenPage {
				e := p.diag(p.elementSpan(*child), CodeRoot, "<%s> must appear before <%s>", tagDefs, tagPage)
				return nil, &e
			}
			seenDefs = true
			defs, cerr := p.parseDefs(*child)
			if cerr != nil {
				return nil, cerr
			}
			doc.Defs = defs
		case tagPage:
			if seenPage {
				e := p.diag(p.elementSpan(*child), CodeRoot, "duplicate <%s>", tagPage)
				return nil, &e
			}
			seenPage = true
			page, cerr := p.parsePage(*child)
			if cerr != nil {
				return nil, cerr
			}
			doc.Page = page
		default:
			e := p.diag(p.elementSpan(*child), CodeRoot,
				"unexpected element <%s> in <%s>; only <%s> and <%s> are allowed",
				child.se.Name.Local, tagGoxml, tagDefs, tagPage)
			return nil, &e
		}
	}

	if !seenPage {
		e := p.diag(rootSpan, CodeRoot, "<%s> requires exactly one <%s>", tagGoxml, tagPage)
		return nil, &e
	}
	return doc, nil
}

// element 是尚未处理的起始元素。
type element struct {
	tok token
	se  xml.StartElement
}

// nextElement 跳过空白与注释，返回下一个起始元素；
// 若遇到结束标签（即当前元素结束）返回 nil, nil。
func (p *parser) nextElement() (*element, *contracts.CompilerError) {
	for {
		tok, err := p.next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				e := p.diag(Span{}, CodeSyntax, "unexpected end of file")
				return nil, &e
			}
			e := p.xmlError(err)
			return nil, &e
		}
		switch t := tok.Token.(type) {
		case xml.CharData:
			if s := strings.TrimSpace(string(t)); s != "" {
				e := p.diag(p.smap.span(tok.start, tok.end), CodeTextNode,
					"text node %q is not allowed; use <Text Value=\"...\"/>", s)
				return nil, &e
			}
			continue
		case xml.Comment:
			continue
		case xml.ProcInst:
			e := p.diag(p.smap.span(tok.start, tok.end), CodeSyntax, "processing instruction is not allowed")
			return nil, &e
		case xml.EndElement:
			return nil, nil
		case xml.StartElement:
			return &element{tok: tok, se: t}, nil
		}
	}
}

// xmlError 把 encoding/xml 的错误转换为带位置的诊断。
func (p *parser) xmlError(err error) contracts.CompilerError {
	var se *xml.SyntaxError
	if errors.As(err, &se) {
		sp := Span{Start: Position{Line: se.Line, Col: 1}}
		return p.diag(sp, CodeXML, "XML syntax error: %s", se.Msg)
	}
	return p.diag(Span{}, CodeXML, "XML parse error: %v", err)
}

// elementSpan 计算元素起始标签的范围。
//
// 由于此时尚未读到结束标签，先只给出起始标签范围；
// 完整范围在元素结束后由 p.smap.span(el.tok.start, p.prevOffset) 补齐。
func (p *parser) elementSpan(el element) Span {
	return p.smap.span(el.tok.start, el.tok.end)
}
