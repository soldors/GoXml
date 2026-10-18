package parser

import (
	"github.com/goxml/goxml/contracts"
)

// parseIf 解析 <If Condition="..."> <Then>…</Then> [<Else>…</Else>] </If>。
func (p *parser) parseIf(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}

	n := &contracts.NodeIR{
		Kind: contracts.KindIf,
		Tag:  tagIf,
		Span: p.elementSpan(el),
		If:   &contracts.IfClause{},
	}

	for _, a := range attrs {
		switch a.Name {
		case "Condition":
			if !a.Value.IsBinding {
				e := p.diag(a.Span, CodeControl, "Condition must be a binding like {User.LoggedIn}")
				return nil, &e
			}
			n.If.Condition = a.Value
		default:
			e := p.diag(a.Span, CodeAttrCall, "unknown attribute %q on <%s>", a.Name, tagIf)
			return nil, &e
		}
	}
	if n.If.Condition.IsZero() {
		e := p.diag(n.Span, CodeControl, "<%s> requires a Condition attribute", tagIf)
		return nil, &e
	}

	var thenSeen, elseSeen bool
	for {
		child, err := p.nextElement()
		if err != nil {
			return nil, err
		}
		if child == nil {
			break
		}
		switch child.se.Name.Local {
		case tagThen:
			if thenSeen {
				e := p.diag(p.elementSpan(*child), CodeControl, "<%s> accepts at most one <%s>", tagIf, tagThen)
				return nil, &e
			}
			thenSeen = true
			body, endOff, cerr := p.parseClauseBody(*child, tagThen)
			if cerr != nil {
				return nil, cerr
			}
			_ = endOff
			n.If.Then = body
		case tagElse:
			if !thenSeen {
				e := p.diag(p.elementSpan(*child), CodeControl, "<%s> must appear after <%s>", tagElse, tagThen)
				return nil, &e
			}
			if elseSeen {
				e := p.diag(p.elementSpan(*child), CodeControl, "<%s> accepts at most one <%s>", tagIf, tagElse)
				return nil, &e
			}
			elseSeen = true
			body, endOff, cerr := p.parseClauseBody(*child, tagElse)
			if cerr != nil {
				return nil, cerr
			}
			_ = endOff
			n.If.Else = body
		default:
			e := p.diag(p.elementSpan(*child), CodeControl,
				"<%s> may only contain <%s> and <%s>, got <%s>",
				tagIf, tagThen, tagElse, child.se.Name.Local)
			return nil, &e
		}
	}

	n.Span = p.smap.span(el.tok.start, p.prevOffset)
	if !thenSeen {
		e := p.diag(n.Span, CodeControl, "<%s> requires exactly one <%s>", tagIf, tagThen)
		return nil, &e
	}
	if len(n.If.Then) == 0 {
		e := p.diag(n.Span, CodeControl, "<%s> must contain at least one render node", tagThen)
		return nil, &e
	}
	return n, nil
}

// parseClauseBody 解析 <Then>/<Else> 的内容。
func (p *parser) parseClauseBody(el element, name string) ([]*contracts.NodeIR, int64, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, 0, cerr
	}
	if len(attrs) > 0 {
		e := p.diag(attrs[0].Span, CodeAttrCall, "<%s> accepts no attributes", name)
		return nil, 0, &e
	}
	return p.parseChildren()
}

// parseFor 解析 <For Each="item" In="{Items}" [Index="i"] [Key="{item.ID}"]>。
func (p *parser) parseFor(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}

	n := &contracts.NodeIR{
		Kind: contracts.KindFor,
		Tag:  tagFor,
		Span: p.elementSpan(el),
		For:  &contracts.ForClause{},
	}

	for _, a := range attrs {
		span := a.Span
		switch a.Name {
		case "Each":
			if a.Value.IsBinding || !isIdentifier(a.Value.Raw) {
				e := p.diag(span, CodeControl, "Each must be a valid identifier, got %q", a.Value.Raw)
				return nil, &e
			}
			n.For.Each = a.Value.Raw
		case "In":
			if !a.Value.IsBinding {
				e := p.diag(span, CodeControl, "In must be a binding like {Items}")
				return nil, &e
			}
			n.For.In = a.Value
		case "Index":
			if a.Value.IsBinding || !isIdentifier(a.Value.Raw) {
				e := p.diag(span, CodeControl, "Index must be a valid identifier, got %q", a.Value.Raw)
				return nil, &e
			}
			n.For.Index = a.Value.Raw
		case "Key":
			if !a.Value.IsBinding {
				e := p.diag(span, CodeControl, "Key must be a binding like {item.ID}")
				return nil, &e
			}
			n.For.Key = a.Value
			n.For.HasKey = true
		case "Virtualize":
			if a.Value.IsBinding || (a.Value.Raw != "true" && a.Value.Raw != "false") {
				e := p.diag(span, CodeControl, "Virtualize must be true or false, got %q", a.Value.Raw)
				return nil, &e
			}
			n.For.Virtualize = a.Value.Raw == "true"
		case "ItemExtent":
			v, cerr := p.parseDp(a, "ItemExtent")
			if cerr != nil {
				return nil, cerr
			}
			if v <= 0 {
				e := p.diag(span, CodeControl, "ItemExtent must be greater than 0")
				return nil, &e
			}
			n.For.ItemExtent = v
			n.For.HasItemExtent = true
		default:
			e := p.diag(span, CodeAttrCall, "unknown attribute %q on <%s>", a.Name, tagFor)
			return nil, &e
		}
	}

	if n.For.Each == "" {
		e := p.diag(n.Span, CodeControl, "<%s> requires an Each attribute", tagFor)
		return nil, &e
	}
	if n.For.In.IsZero() {
		e := p.diag(n.Span, CodeControl, "<%s> requires an In attribute", tagFor)
		return nil, &e
	}
	if n.For.Virtualize && !n.For.HasItemExtent {
		e := p.diag(n.Span, CodeControl,
			"<%s Virtualize=\"true\"> requires ItemExtent until variable-height virtualization is supported", tagFor)
		return nil, &e
	}

	children, endOff, cerr := p.parseChildren()
	if cerr != nil {
		return nil, cerr
	}
	n.Span = p.smap.span(el.tok.start, endOff)
	n.Children = children
	if len(children) == 0 {
		e := p.diag(n.Span, CodeControl, "<%s> must contain at least one render node", tagFor)
		return nil, &e
	}

	// 有状态循环必须提供 Key（AOT Diff §5.1）。
	if !n.For.HasKey && containsStatefulNode(children) {
		e := p.diag(n.Span, CodeKey,
			"<%s> contains stateful controls and requires a Key attribute to keep per-item state stable", tagFor)
		return nil, &e
	}
	return n, nil
}

// containsStatefulNode 报告子树中是否含有需要身份保持的交互控件。
func containsStatefulNode(nodes []*contracts.NodeIR) bool {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.Kind == contracts.KindElement {
			switch n.Tag {
			case "Button", "Input":
				return true
			}
		}
		if containsStatefulNode(n.Children) ||
			(n.If != nil && (containsStatefulNode(n.If.Then) || containsStatefulNode(n.If.Else))) ||
			(n.Def != nil && containsStatefulNode([]*contracts.NodeIR{n.Def.Body})) {
			return true
		}
	}
	return false
}
