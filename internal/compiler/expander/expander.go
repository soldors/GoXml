package expander

import (
	"strings"

	"github.com/goxml/goxml/contracts"
)

// 诊断码。触发阶段固定，避免 Parser/Expander/Analyzer 生成互相竞争的诊断。
const (
	// CodeUnknownName 名称既不是布局属性，也不在组件 Schema 中。
	// 这是拼写错误的唯一归属，绝不退化为事件。
	CodeUnknownName = contracts.CodePrefixProp + "-004"
	// CodeNotApplicable 名称已知（布局属性），但不适用于当前元素。
	CodeNotApplicable = contracts.CodePrefixProp + "-005"
	// CodeSchemaConflict Schema 定义阶段：属性名与事件名冲突。
	CodeSchemaConflict = contracts.CodePrefixDef + "-003"
	// CodeSchemaDuplicate Schema 定义阶段：重复声明。
	CodeSchemaDuplicate = contracts.CodePrefixDef + "-004"
	// CodeLayoutShadow 组件属性与全局布局属性同名。
	CodeLayoutShadow = contracts.CodePrefixDef + "-005"
	// CodeEventSyntax 事件处理器语法错误（值不是合法的 Method 或 Method(args)）。
	CodeEventSyntax = contracts.CodePrefixEvent + "-002"
	// CodeEventArg 事件实参不是允许的字面量或绑定路径。
	CodeEventArg = contracts.CodePrefixEvent + "-005"
	// CodeNilDocument 内部不变式违反：调用方传入 nil 文档。
	//
	// 这不是用户可触发的错误——正常流程的文档由 Parser 产出，不会为 nil。
	// 出现它说明编译器自身的调用有缺陷，而非用户源码有问题。
	CodeNilDocument = contracts.CodePrefixDef + "-000"
)

// expander 是单次展开的状态。
type expander struct {
	file    string
	doc     *contracts.DocumentIR
	schemas map[string]*ComponentSchema
	// defs 是 <Def> 名到定义节点的索引，供宏展开克隆使用。
	defs map[string]*contracts.NodeIR
	// inExpansion 记录正在展开的组件，用于检测递归引用。
	inExpansion map[string]bool

	diags []contracts.CompilerError
}

// Result 展开结果。
type Result struct {
	Doc *contracts.DocumentIR
	// Schemas 是本次展开得到的组件契约视图，供 Analyzer 查询。
	//
	// 它与 Doc 属于同一编译上下文，不得跨文件或跨轮次混用。
	// 即使 Diags 非空也会填充，便于工具在出错时仍能展示契约。
	Schemas SchemaRegistry
	// Diags 至少有一条时，Doc 不可用于代码生成。
	Diags []contracts.CompilerError
}

// Expand 对 Parser 输出的文档执行组件分类与 <Def> 展开。
//
// 输入文档必须已通过 Parser 的语法检查。
func Expand(file string, doc *contracts.DocumentIR) Result {
	if doc == nil {
		return Result{
			Schemas: newRegistry(nil),
			Diags: []contracts.CompilerError{
				contracts.NewError(file, contracts.SourceSpan{}, CodeNilDocument, "nil document"),
			},
		}
	}

	e := &expander{
		file:        file,
		doc:         doc,
		schemas:     make(map[string]*ComponentSchema),
		defs:        make(map[string]*contracts.NodeIR),
		inExpansion: make(map[string]bool),
	}

	// 先注册本文档的 <Def> Schema，再分类：组件可前向引用文档内定义。
	for _, d := range doc.Defs {
		if cerr := e.registerDef(d); cerr != nil {
			e.diags = append(e.diags, *cerr)
			continue
		}
		if d.Def != nil {
			e.defs[d.Def.Name] = d
		}
	}
	if len(e.diags) > 0 {
		return Result{Schemas: newRegistry(e.schemas), Diags: e.diags}
	}

	// 分类页面与所有 Def 体内的元素。
	if doc.Page != nil {
		e.classifyTree(doc.Page)
	}
	for _, d := range doc.Defs {
		if d.Def != nil && d.Def.Body != nil {
			e.classifyTree(d.Def.Body)
		}
	}

	// 分类完成后执行宏展开：Def 调用点被内联为其渲染子树。
	//
	// 展开必须在分类之后：组件调用的属性需要先按 Schema 分类，
	// 才能确定哪些是 Prop、哪些是事件。
	if len(e.diags) > 0 {
		return Result{Schemas: newRegistry(e.schemas), Diags: e.diags}
	}
	e.expandDefs()

	return Result{Doc: doc, Schemas: newRegistry(e.schemas), Diags: e.diags}
}

// registerDef 把 <Def> 转换为 ComponentSchema 并检查契约冲突。
func (e *expander) registerDef(d *contracts.NodeIR) *contracts.CompilerError {
	if d == nil || d.Def == nil {
		return nil
	}
	dc := d.Def

	if _, exists := e.schemas[dc.Name]; exists {
		cerr := e.diag(d.Span, CodeSchemaDuplicate, "duplicate component definition %q", dc.Name)
		return &cerr
	}

	schema := &ComponentSchema{Tag: dc.Name}

	for _, p := range dc.Props {
		// 组件属性不得与全局布局属性同名：否则 Width 的语义会随组件变化。
		if _, isLayout := layoutAttr(p.Name); isLayout {
			cerr := e.diag(p.Span, CodeLayoutShadow,
				"component %q declares Prop %q which conflicts with the global layout attribute of the same name",
				dc.Name, p.Name)
			return &cerr
		}
		if _, dup := schema.Prop(p.Name); dup {
			cerr := e.diag(p.Span, CodeSchemaDuplicate,
				"component %q declares duplicate Prop %q", dc.Name, p.Name)
			return &cerr
		}
		schema.Properties = append(schema.Properties, PropertySchema{
			Name:     p.Name,
			Type:     p.Type,
			Required: !p.HasDefault,
		})
	}

	for _, ev := range dc.Events {
		// 属性名与事件名不得冲突。
		if _, clash := schema.Prop(ev.Name); clash {
			cerr := e.diag(ev.Span, CodeSchemaConflict,
				"component %q declares %q as both Prop and Event", dc.Name, ev.Name)
			return &cerr
		}
		if _, dup := schema.Event(ev.Name); dup {
			cerr := e.diag(ev.Span, CodeSchemaDuplicate,
				"component %q declares duplicate Event %q", dc.Name, ev.Name)
			return &cerr
		}
		// 事件名也不得与布局属性冲突。
		if _, isLayout := layoutAttr(ev.Name); isLayout {
			cerr := e.diag(ev.Span, CodeLayoutShadow,
				"component %q declares Event %q which conflicts with the global layout attribute of the same name",
				dc.Name, ev.Name)
			return &cerr
		}
		schema.Events = append(schema.Events, ev)
	}

	e.schemas[dc.Name] = schema
	return nil
}

// classifyTree 递归分类整棵子树。
func (e *expander) classifyTree(n *contracts.NodeIR) {
	if n == nil {
		return
	}

	switch n.Kind {
	case contracts.KindElement:
		e.classifyElement(n)
	case contracts.KindIf:
		if n.If != nil {
			for _, c := range n.If.Then {
				e.classifyTree(c)
			}
			for _, c := range n.If.Else {
				e.classifyTree(c)
			}
		}
	case contracts.KindFor:
		for _, c := range n.Children {
			e.classifyTree(c)
		}
	}

	for _, c := range n.Children {
		e.classifyTree(c)
	}
	if n.Def != nil && n.Def.Body != nil {
		e.classifyTree(n.Def.Body)
	}
}

// classifyElement 按三层规则分类单个元素的 RawAttrs。
//
// 组件调用点（标签是本文档 <Def> 定义的名字）在此**只分类布局属性**：
// 它的 Prop 与事件由 expandCall 按 Props/Events 声明处理，避免同一错误
// 在两处各报一次。分区规则见 classifyCallSite。
func (e *expander) classifyElement(n *contracts.NodeIR) {
	if e.isCallSite(n) {
		e.classifyCallSite(n)
		return
	}

	schema := e.schemaFor(n)

	for _, ra := range n.RawAttrs {
		// 1. 全局布局属性。
		//
		// 注意：名称已知即算布局属性；不适用于当前元素属于「合法性」问题，
		// 报 CodeNotApplicable 而不是 CodeUnknownName。
		if la, ok := layoutAttr(ra.Name); ok {
			if !la.appliesTo(n.Tag) {
				e.diags = append(e.diags, *e.err(ra.Span, CodeNotApplicable,
					"layout attribute %q is not applicable to <%s>", ra.Name, n.Tag))
				continue
			}
			continue
		}

		// 2. 组件属性。
		if schema != nil {
			if _, ok := schema.Prop(ra.Name); ok {
				n.Attrs = append(n.Attrs, contracts.Attr{
					Name:  ra.Name,
					Value: ra.Value,
					Span:  ra.Span,
				})
				continue
			}

			// 3. 组件事件。
			if _, ok := schema.Event(ra.Name); ok {
				ev, cerr := e.parseEvent(ra)
				if cerr != nil {
					e.diags = append(e.diags, *cerr)
					continue
				}
				n.Events = append(n.Events, ev)
				continue
			}
		}

		// 4. 未知名称：绝不退化为事件。
		e.diags = append(e.diags, *e.err(ra.Span, CodeUnknownName,
			"unknown attribute or event %q on <%s>", ra.Name, n.Tag))
	}

	// 清空 RawAttrs，避免下游重复分类。
	//
	// 例外：颜色属性（Background）以绑定形式出现时，Parser 无法解析为
	// 具体颜色（它可能是 <Def> 内对 color 类型 Prop 的引用），必须保留
	// 原始绑定直到宏展开阶段解析。
	if n.Background == nil {
		if _, ok := n.RawAttrs.Lookup("Background"); ok {
			kept := n.RawAttrs[:0:0]
			for _, ra := range n.RawAttrs {
				if ra.Name == "Background" {
					kept = append(kept, ra)
				}
			}
			n.RawAttrs = kept
		} else {
			n.RawAttrs = nil
		}
	} else {
		n.RawAttrs = nil
	}

	// 检查必填属性。
	if schema != nil {
		for _, p := range schema.Properties {
			if !p.Required {
				continue
			}
			if _, ok := n.Attr(p.Name); !ok {
				e.diags = append(e.diags, *e.err(n.Span, CodeUnknownName,
					"<%s> requires attribute %q", n.Tag, p.Name))
			}
		}
	}
}

// isCallSite 报告该节点是否为本文档 <Def> 定义的组件调用点。
func (e *expander) isCallSite(n *contracts.NodeIR) bool {
	if n.Kind != contracts.KindElement {
		return false
	}
	_, ok := e.defs[n.Tag]
	return ok
}

// classifyCallSite 只分类组件调用点的布局属性。
//
// 调用点的 Prop 与事件由 expandCall 依据 Props/Events 声明处理：
// 例如未声明的属性在那里报 GXML-DEF-007（组件没有该属性），
// 而不是这里的 GXML-ATTR-004（未知名称）。两处各报一次会造成诊断重复。
func (e *expander) classifyCallSite(n *contracts.NodeIR) {
	kept := make(contracts.RawAttrList, 0, len(n.RawAttrs))
	for _, ra := range n.RawAttrs {
		if la, ok := layoutAttr(ra.Name); ok {
			if !la.appliesTo(n.Tag) {
				e.diags = append(e.diags, *e.err(ra.Span, CodeNotApplicable,
					"layout attribute %q is not applicable to <%s>", ra.Name, n.Tag))
			}
			continue
		}
		// 非布局属性：留给 expandCall 处理。
		kept = append(kept, ra)
	}
	n.RawAttrs = kept
}

// schemaFor 返回元素对应的组件 Schema。
//
// 布局标签没有组件 Schema（它们只有布局属性）；内置控件在 builtinSchemas；
// 其余标签在本文档的 <Def> 注册表中查找。
func (e *expander) schemaFor(n *contracts.NodeIR) *ComponentSchema {
	if isLayoutTag(n.Tag) {
		return nil
	}
	if s, ok := builtinSchema(n.Tag); ok {
		return s
	}
	if s, ok := e.schemas[n.Tag]; ok {
		return s
	}
	// 未知标签：Parser 不校验标签存在性，这里也不报错——
	// 它可能是 Go 自定义组件（//goxml:component），由 Analyzer 解析。
	return nil
}

// isLayoutTag 报告是否为内置布局标签。
func isLayoutTag(tag string) bool {
	switch tag {
	case "Column", "Row", "Grid", "Spacer", "Expanded":
		return true
	}
	return false
}

// parseEvent 把已确认为事件的属性值解析为 EventBinding。
//
// 语法：Method 或 Method(Arg, Arg)。实参只能是字面量或绑定路径。
func (e *expander) parseEvent(ra contracts.RawAttr) (contracts.EventBinding, *contracts.CompilerError) {
	if ra.Value.IsBinding {
		cerr := e.err(ra.Span, CodeEventSyntax,
			"event %q must be a method reference like Save or Save(item.ID), not a binding", ra.Name)
		return contracts.EventBinding{}, cerr
	}

	raw := strings.TrimSpace(ra.Value.Raw)
	method := raw
	var argSrc string

	if open := strings.IndexByte(raw, '('); open >= 0 {
		if !strings.HasSuffix(raw, ")") {
			cerr := e.err(ra.Span, CodeEventSyntax,
				"event handler %q is missing a closing parenthesis", raw)
			return contracts.EventBinding{}, cerr
		}
		method = strings.TrimSpace(raw[:open])
		argSrc = raw[open+1 : len(raw)-1]
	}
	if !isIdentifier(method) {
		cerr := e.err(ra.Span, CodeEventSyntax, "invalid event method %q", method)
		return contracts.EventBinding{}, cerr
	}

	ev := contracts.EventBinding{Name: ra.Name, Method: method, Span: ra.Span}
	if strings.TrimSpace(argSrc) == "" {
		return ev, nil
	}

	parts, ok := splitEventArgs(argSrc)
	if !ok {
		cerr := e.err(ra.Span, CodeEventArg, "event %q has an unterminated string argument", ra.Name)
		return contracts.EventBinding{}, cerr
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			cerr := e.err(ra.Span, CodeEventArg, "event %q has an empty argument", ra.Name)
			return contracts.EventBinding{}, cerr
		}
		arg, cerr := e.parseEventArg(part, ra)
		if cerr != nil {
			return contracts.EventBinding{}, cerr
		}
		ev.Args = append(ev.Args, arg)
	}
	return ev, nil
}

// splitEventArgs separates arguments without treating commas inside a string
// literal as separators. The literal text is retained verbatim, including its
// quotes and backslashes; interpreting it remains parseEventArg's responsibility.
func splitEventArgs(src string) ([]string, bool) {
	var parts []string
	start := 0
	quoted := false
	atStart := true
	for i := 0; i < len(src); i++ {
		c := src[i]
		if atStart && (c == ' ' || c == '\t' || c == '\r' || c == '\n') {
			continue
		}
		if atStart && c == '\'' {
			quoted = true
			atStart = false
			continue
		}
		atStart = false
		if quoted {
			if c == '\'' {
				// Existing string literals retain their interior apostrophes.
				// A closing quote must end the argument, before a comma or EOF.
				rest := strings.TrimLeft(src[i+1:], " \t\r\n")
				if rest == "" || rest[0] == ',' {
					quoted = false
				}
			}
			continue
		}
		if c == ',' {
			parts = append(parts, src[start:i])
			start = i + 1
			atStart = true
		}
	}
	if quoted {
		return nil, false
	}
	return append(parts, src[start:]), true
}

// parseEventArg 解析单个实参。
//
// 判定顺序很重要：字面量必须先于绑定路径判断。true/false 既是关键字
// 又是合法标识符，若先走 splitPath 会被误判为绑定路径。
func (e *expander) parseEventArg(s string, ra contracts.RawAttr) (contracts.EventArg, *contracts.CompilerError) {
	// 布尔字面量优先。
	if s == "true" || s == "false" {
		return contracts.EventArg{Kind: contracts.ArgLiteral, Literal: s}, nil
	}
	// 带引号的字符串字面量。
	//
	// **保留引号**：EventArg.Literal 的契约是「保留原始字面量文本」。
	// 早期实现剥掉了引号，导致 Analyzer 无法区分 '42'（字符串）与
	// 42（整数）——两者剥引号后都是 "42"。
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return contracts.EventArg{Kind: contracts.ArgLiteral, Literal: s}, nil
	}
	// 数字字面量。
	if isNumberLiteral(s) {
		return contracts.EventArg{Kind: contracts.ArgLiteral, Literal: s}, nil
	}
	// 绑定路径。
	if segs, ok := splitPath(s); ok {
		return contracts.EventArg{
			Kind:    contracts.ArgBinding,
			Binding: contracts.BindingVal{IsBinding: true, Raw: s, Segments: segs},
		}, nil
	}
	cerr := e.err(ra.Span, CodeEventArg,
		"event argument %q is not allowed; expected a literal, an identifier or a dotted path", s)
	return contracts.EventArg{}, cerr
}

// splitPath 拆分并校验 "A.B.C"。
func splitPath(s string) ([]string, bool) {
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if !isIdentifier(p) {
			return nil, false
		}
	}
	return parts, true
}

// isIdentifier 校验 [_A-Za-z][_A-Za-z0-9]*。
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isAlpha := c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isAlpha {
				return false
			}
			continue
		}
		if !isAlpha && !isDigit {
			return false
		}
	}
	return true
}

// isNumberLiteral 报告 s 是否为十进制数字面量。
func isNumberLiteral(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && !dot:
			dot = true
		case c == '-' && i == 0:
		default:
			return false
		}
	}
	return true
}

// diag 构造诊断。
func (e *expander) diag(s contracts.SourceSpan, code, format string, args ...any) contracts.CompilerError {
	return contracts.NewError(e.file, s, code, format, args...)
}

// err 返回诊断指针，便于在 append 时解引用。
func (e *expander) err(s contracts.SourceSpan, code, format string, args ...any) *contracts.CompilerError {
	d := e.diag(s, code, format, args...)
	return &d
}
