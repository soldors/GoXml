package parser

import (
	"strings"

	"github.com/goxml/goxml/contracts"
)

// parseDefs 解析 <Defs>，返回其中定义的组件。
func (p *parser) parseDefs(el element) ([]*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}
	if len(attrs) > 0 {
		e := p.diag(attrs[0].Span, CodeAttrCall, "<%s> accepts no attributes, got %q", tagDefs, attrs[0].Name)
		return nil, &e
	}

	var defs []*contracts.NodeIR
	for {
		child, err := p.nextElement()
		if err != nil {
			return nil, err
		}
		if child == nil {
			break
		}
		if child.se.Name.Local != tagDef {
			e := p.diag(p.elementSpan(*child), CodeDef,
				"<%s> may only contain <%s>, got <%s>", tagDefs, tagDef, child.se.Name.Local)
			return nil, &e
		}
		def, cerr := p.parseDef(*child)
		if cerr != nil {
			return nil, cerr
		}
		defs = append(defs, def)
	}
	if len(defs) == 0 {
		e := p.diag(p.elementSpan(el), CodeDef, "<%s> must define at least one <%s>", tagDefs, tagDef)
		return nil, &e
	}
	return defs, nil
}

// parseDef 解析单个 <Def Name="..." Props="...">。
func (p *parser) parseDef(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}

	n := &contracts.NodeIR{
		Kind: contracts.KindDef,
		Tag:  tagDef,
		Span: p.elementSpan(el),
		Def:  &contracts.DefClause{},
	}

	for _, a := range attrs {
		switch a.Name {
		case "Name":
			if a.Value.IsBinding || !isIdentifier(a.Value.Raw) {
				e := p.diag(a.Span, CodeDefName, "Def Name must be a valid identifier, got %q", a.Value.Raw)
				return nil, &e
			}
			n.Def.Name = a.Value.Raw
		case "Props":
			if a.Value.IsBinding {
				e := p.diag(a.Span, CodeDef, "Def Props must be a static declaration, not a binding")
				return nil, &e
			}
			props, cerr := p.parseDefProps(a.Value.Raw, a.Span)
			if cerr != nil {
				return nil, cerr
			}
			n.Def.Props = props
		case "Events":
			if a.Value.IsBinding {
				e := p.diag(a.Span, CodeDef, "Def Events must be a static declaration, not a binding")
				return nil, &e
			}
			evs, cerr := p.parseDefEvents(a.Value.Raw, a.Span)
			if cerr != nil {
				return nil, cerr
			}
			n.Def.Events = evs
		default:
			e := p.diag(a.Span, CodeAttrCall, "unknown attribute %q on <%s>", a.Name, tagDef)
			return nil, &e
		}
	}
	if n.Def.Name == "" {
		e := p.diag(n.Span, CodeDefName, "<%s> requires a Name attribute", tagDef)
		return nil, &e
	}
	if isReservedName(n.Def.Name) {
		e := p.diag(n.Span, CodeDefName, "%q is a reserved name and cannot be used as a component name", n.Def.Name)
		return nil, &e
	}

	children, endOff, cerr := p.parseChildren()
	if cerr != nil {
		return nil, cerr
	}
	n.Span = p.smap.span(el.tok.start, endOff)

	if len(children) != 1 {
		e := p.diag(n.Span, CodeDef, "<%s> must have exactly one render root, got %d", tagDef, len(children))
		return nil, &e
	}
	n.Def.Body = children[0]
	n.Def.HasSlot = containsSlot(n.Def.Body)
	if countSlot(n.Def.Body) > 1 {
		e := p.diag(n.Span, CodeDef, "<%s> may contain at most one <Slot/>", tagDef)
		return nil, &e
	}
	return n, nil
}

// parseDefProps 解析 "Name:Type; Other:Type=Default"。
func (p *parser) parseDefProps(raw string, span Span) ([]contracts.DefProp, *contracts.CompilerError) {
	var out []contracts.DefProp
	seen := map[string]bool{}

	for _, decl := range strings.Split(raw, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		name, rest, ok := strings.Cut(decl, ":")
		if !ok {
			e := p.diag(span, CodeDef, "invalid Prop declaration %q; expected Name:Type", decl)
			return nil, &e
		}
		name = strings.TrimSpace(name)
		if !isIdentifier(name) {
			e := p.diag(span, CodeDef, "invalid Prop name %q", name)
			return nil, &e
		}
		if seen[name] {
			e := p.diag(span, CodeDef, "duplicate Prop %q", name)
			return nil, &e
		}
		seen[name] = true

		typeName, defVal, hasDef := strings.Cut(rest, "=")
		pt := contracts.ParsePropType(typeName)
		if pt == contracts.PropUnknown {
			e := p.diag(span, CodeDef,
				"unknown Prop type %q for %q; expected string|bool|int|float|color|insets",
				strings.TrimSpace(typeName), name)
			return nil, &e
		}

		prop := contracts.DefProp{Name: name, Type: pt, Span: span}
		if hasDef {
			prop.HasDefault = true
			prop.Default = contracts.BindingVal{Raw: strings.TrimSpace(defVal)}
		}
		out = append(out, prop)
	}
	if len(out) == 0 {
		e := p.diag(span, CodeDef, "Props must declare at least one property")
		return nil, &e
	}
	return out, nil
}

// parsePage 解析 <Page Route="...">。
func (p *parser) parsePage(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}
	n := &contracts.NodeIR{
		Kind: contracts.KindPage,
		Tag:  tagPage,
		Span: p.elementSpan(el),
	}
	for _, a := range attrs {
		switch a.Name {
		case "Route":
			if a.Value.IsBinding {
				e := p.diag(a.Span, CodeAttrCall, "Route must be a static path, not a binding")
				return nil, &e
			}
			n.RoutePath = a.Value.Raw
		default:
			e := p.diag(a.Span, CodeAttrCall, "unknown attribute %q on <%s>", a.Name, tagPage)
			return nil, &e
		}
	}
	children, endOff, cerr := p.parseChildren()
	if cerr != nil {
		return nil, cerr
	}
	n.Span = p.smap.span(el.tok.start, endOff)
	n.Children = children
	if len(children) == 0 {
		e := p.diag(n.Span, CodeRoot, "<%s> must contain at least one render node", tagPage)
		return nil, &e
	}
	return n, nil
}

// parseChildren 读取直到当前元素的结束标签，返回子节点与结束标签末尾偏移。
func (p *parser) parseChildren() ([]*contracts.NodeIR, int64, *contracts.CompilerError) {
	var out []*contracts.NodeIR
	for {
		child, err := p.nextElement()
		if err != nil {
			return nil, 0, err
		}
		if child == nil {
			return out, p.prevOffset, nil
		}
		if child.se.Name.Local == tagSlot {
			n, cerr := p.parseSlot(*child)
			if cerr != nil {
				return nil, 0, cerr
			}
			out = append(out, n)
			continue
		}
		n, cerr := p.parseNode(*child)
		if cerr != nil {
			return nil, 0, cerr
		}
		out = append(out, n)
	}
}

// parseSlot 解析 <Slot/>；必须自闭合。
func (p *parser) parseSlot(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}
	if len(attrs) > 0 {
		e := p.diag(attrs[0].Span, CodeAttrCall, "<%s/> accepts no attributes", tagSlot)
		return nil, &e
	}
	n := &contracts.NodeIR{Kind: contracts.KindSlot, Tag: tagSlot, Span: p.elementSpan(el)}

	// 立即读取结束标签，确保没有子节点。
	children, endOff, cerr := p.parseChildren()
	if cerr != nil {
		return nil, cerr
	}
	n.Span = p.smap.span(el.tok.start, endOff)
	if len(children) > 0 {
		e := p.diag(n.Span, CodeDef, "<%s/> cannot contain children", tagSlot)
		return nil, &e
	}
	return n, nil
}

// parseNode 按标签分派到具体解析逻辑。
func (p *parser) parseNode(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	switch el.se.Name.Local {
	case tagIf:
		return p.parseIf(el)
	case tagFor:
		return p.parseFor(el)
	case tagThen, tagElse:
		e := p.diag(p.elementSpan(el), CodeControl,
			"<%s> is only allowed directly inside <%s>", el.se.Name.Local, tagIf)
		return nil, &e
	case tagDef:
		e := p.diag(p.elementSpan(el), CodeDef, "<%s> is only allowed inside <%s>", tagDef, tagDefs)
		return nil, &e
	case tagDefs, tagPage, tagGoxml:
		e := p.diag(p.elementSpan(el), CodeRoot, "<%s> is not allowed here", el.se.Name.Local)
		return nil, &e
	}
	return p.parseElement(el)
}

// parseElement 解析布局标签与基础控件。
func (p *parser) parseElement(el element) (*contracts.NodeIR, *contracts.CompilerError) {
	attrs, cerr := p.elementAttrs(el.tok, el.se)
	if cerr != nil {
		return nil, cerr
	}

	n := &contracts.NodeIR{
		Kind: contracts.KindElement,
		Tag:  el.se.Name.Local,
		Span: p.elementSpan(el),
	}

	// 布局属性的「值形态」在 Parser 解析（它只依赖属性自身的语法，
	// 不需要组件 Schema）；但「是否适用于当前元素」属于语境相关的合法性
	// 检查，归 Expander。
	//
	// 因此布局属性既被解析进专用字段，也保留在 RawAttrs 中，
	// 使 Expander 能发现 Columns 出现在 <Button> 上这类不适用错误。
	// Expander 完成分类后会清空 RawAttrs。
	if _, cerr := p.applyLayoutAttrs(n, attrs); cerr != nil {
		return nil, cerr
	}
	for _, a := range attrs {
		n.RawAttrs = append(n.RawAttrs, contracts.RawAttr{
			Name:  a.Name,
			Value: a.Value,
			Span:  a.Span,
		})
	}

	// 布局标签的语法约束。
	switch n.Tag {
	case tagGrid:
		if len(n.Columns) == 0 {
			e := p.diag(n.Span, CodeAttrCall, "<%s> requires a Columns attribute", tagGrid)
			return nil, &e
		}
	case tagSpacer:
		if len(attrs) > 0 {
			for _, a := range attrs {
				if a.Name != attrWidth && a.Name != attrHeight {
					e := p.diag(a.Span, CodeAttrCall, "<%s> only accepts Width and Height", tagSpacer)
					return nil, &e
				}
			}
		}
	}

	children, endOff, cerr := p.parseChildren()
	if cerr != nil {
		return nil, cerr
	}
	n.Span = p.smap.span(el.tok.start, endOff)

	switch n.Tag {
	case tagSpacer:
		if len(children) > 0 {
			e := p.diag(n.Span, CodeSyntax, "<%s> cannot contain children", tagSpacer)
			return nil, &e
		}
	case tagExpand:
		if len(children) != 1 {
			e := p.diag(n.Span, CodeSyntax,
				"<%s> must contain exactly one render node, got %d", tagExpand, len(children))
			return nil, &e
		}
	case tagGrid:
		if len(children) == 0 {
			e := p.diag(n.Span, CodeAttrCall,
				"<%s> requires at least one render child", tagGrid)
			return nil, &e
		}
		// 规范 §3.1.6：列定义是单元格宽度的唯一来源。
		// 子项设置 Width 会让同一行后续单元格分到本不属于它的空间，
		// 导致整列错位——必须在编译期拒绝，而不是运行时容忍。
		for _, c := range children {
			if _, ok := c.RawAttrs.Lookup(attrWidth); ok {
				e := p.diag(c.Span, CodeAttrCall,
					"<%s> children must not set Width: the column definitions own the cell width; "+
						"wrap the content in a <Row> or <Column> if you need independent width", tagGrid)
				return nil, &e
			}
		}
	}
	n.Children = children
	return n, nil
}

// ---- 辅助 ----

// isReservedName 报告名称是否为保留名（规范 §8）。
//
// 内置控件名（Text/Button/Input/Image）同样是保留名：否则用户
// <Def Name="Text"> 会与内置标签冲突，宏展开无法判定该用哪一个。
func isReservedName(name string) bool {
	switch name {
	case tagGoxml, tagPage, tagDefs, tagDef, tagSlot, tagIf, tagThen, tagElse, tagFor,
		tagColumn, tagRow, tagGrid, tagSpacer, tagExpand:
		return true
	}
	for _, c := range builtinControls {
		if name == c {
			return true
		}
	}
	return false
}

// containsSlot 报告子树中是否存在 <Slot/>。
func containsSlot(n *contracts.NodeIR) bool { return countSlot(n) > 0 }

// countSlot 统计子树中的 <Slot/> 数量。
func countSlot(n *contracts.NodeIR) int {
	if n == nil {
		return 0
	}
	c := 0
	if n.Kind == contracts.KindSlot {
		c++
	}
	for _, ch := range n.Children {
		c += countSlot(ch)
	}
	if n.If != nil {
		for _, ch := range n.If.Then {
			c += countSlot(ch)
		}
		for _, ch := range n.If.Else {
			c += countSlot(ch)
		}
	}
	if n.Def != nil {
		c += countSlot(n.Def.Body)
	}
	return c
}
