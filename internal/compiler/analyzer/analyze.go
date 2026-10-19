package analyzer

import (
	"errors"
	"fmt"
	"go/types"
	"strconv"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/expander"
)

// Analyze 校验文档语义并产出语义 IR。
//
// 校验 ViewModel、绑定路径与目标属性类型、事件处理方法签名，以及
// <If>/<For> 的类型和作用域约束。StatusOK 表示本阶段全部检查通过；
// VerifiedDoc 是进入 Codegen 的唯一凭证。
func Analyze(
	file string,
	doc *contracts.DocumentIR,
	schemas expander.SchemaRegistry,
	env *TypeEnv,
) *Result {
	if doc == nil {
		// 调用方违反前置条件：程序缺陷，不是用户错误。
		// 返回 Blocked 而不是伪装成功。
		return &Result{Status: StatusBlocked, Facts: newFacts()}
	}

	a := &analyzer{
		env:     env,
		file:    file,
		schemas: schemas,
		facts:   newFacts(),
		seen:    make(map[string]bool),
	}

	// 第 1 步：ViewModel 无法解析时阻断整个分析（契约 §5.2）。
	vm, cerr := env.ResolveViewModel(doc.ViewModel)
	if cerr != nil {
		a.addDiag(*cerr)
		return &Result{Doc: doc, Facts: a.facts, Status: StatusBlocked, Diags: a.diags}
	}
	a.vm = vm

	// 第 2 步：遍历并解析绑定。
	if doc.Page != nil {
		a.walk(doc.Page)
	}

	st := StatusOK
	if len(a.diags) > 0 {
		st = StatusPartial
	}
	return &Result{Doc: doc, Facts: a.facts, Status: st, Diags: a.diags}
}

// analyzer 是单次分析的状态。
type analyzer struct {
	env     *TypeEnv
	file    string
	schemas expander.SchemaRegistry
	vm      *ViewModelType
	facts   *Facts
	diags   []contracts.CompilerError
	// scopes 是 <For> 变量栈，内层在末尾。
	scopes []map[string]types.Type
	// seen 用于诊断去重：同一位置同一码只报一次。
	seen map[string]bool
}

// ============================================================================
// 诊断
// ============================================================================

func (a *analyzer) addDiag(d contracts.CompilerError) {
	key := d.Code + "|" + d.File + "|" + strconv.Itoa(d.Line) + ":" + strconv.Itoa(d.Col)
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	a.diags = append(a.diags, d)
}

func (a *analyzer) report(span contracts.SourceSpan, code, format string, args ...any) {
	a.addDiag(contracts.NewError(a.file, span, code, format, args...))
}

// lookupScope 从内向外查找循环变量。
func (a *analyzer) lookupScope(name string) (types.Type, bool) {
	for i := len(a.scopes) - 1; i >= 0; i-- {
		if t, ok := a.scopes[i][name]; ok {
			return t, true
		}
	}
	return nil, false
}

// ============================================================================
// 遍历
// ============================================================================

func (a *analyzer) walk(n *contracts.NodeIR) {
	if n == nil {
		return
	}

	switch n.Kind {
	case contracts.KindElement:
		a.analyzeElement(n)
	case contracts.KindIf:
		a.analyzeIf(n)
	case contracts.KindFor:
		// For 自行在推入作用域后遍历子节点。
		a.analyzeFor(n)
		return
	}

	for _, c := range n.Children {
		a.walk(c)
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			a.walk(c)
		}
		for _, c := range n.If.Else {
			a.walk(c)
		}
	}
}

func (a *analyzer) analyzeElement(n *contracts.NodeIR) {
	var nf *NodeFacts
	for _, attr := range n.Attrs {
		if !attr.Value.IsBinding {
			continue // 静态字面量无需解析
		}
		path, raw, ok := a.resolveBinding(attr.Value, attr.Span, attr.Name)
		if !ok {
			continue // 失败只产诊断，不进事实
		}

		// 类型兼容（契约 §11）。路径通过但类型不符仍属失败，
		// 不进入事实表。
		target := a.targetType(n.Tag, attr.Name)
		if !a.checkBindingType(path, raw, target, attr.Span, attr.Name) {
			continue
		}

		if nf == nil {
			nf = &NodeFacts{}
		}
		nf.attrs = append(nf.attrs, ResolvedAttr{
			Name:   attr.Name,
			Target: target,
			Path:   path,
			Span:   attr.Span,
		})
	}
	nf = a.analyzeEvents(n, nf)
	if nf != nil {
		a.facts.set(n, nf)
	}
}

func (a *analyzer) targetType(tag, name string) contracts.PropType {
	if a.schemas == nil {
		return contracts.PropUnknown
	}
	s, ok := a.schemas.Lookup(tag)
	if !ok || s == nil {
		return contracts.PropUnknown
	}
	p, ok := s.Prop(name)
	if !ok {
		return contracts.PropUnknown
	}
	return p.Type
}

// analyzeIf 解析 <If Condition>。布尔类型校验属于 BIND-004（第 3 步）。
func (a *analyzer) analyzeIf(n *contracts.NodeIR) {
	if n.If == nil || !n.If.Condition.IsBinding {
		return
	}
	path, raw, ok := a.resolveBinding(n.If.Condition, n.Span, "Condition")
	if !ok {
		return
	}
	if !a.checkBindingType(path, raw, contracts.PropBool, n.Span, "Condition") {
		return
	}
	nf, _ := a.facts.Lookup(n)
	if nf == nil {
		nf = &NodeFacts{}
	}
	nf.attrs = append(nf.attrs, ResolvedAttr{
		Name:   "Condition",
		Target: contracts.PropBool,
		Path:   path,
		Span:   n.Span,
	})
	a.facts.set(n, nf)
}

// analyzeFor 解析 <For In>/<For Key> 并推入循环作用域。
func (a *analyzer) analyzeFor(n *contracts.NodeIR) {
	f := n.For
	if f == nil {
		return
	}

	// In 在**外层**作用域中解析（不能引用自身变量）。
	if !f.In.IsBinding {
		a.report(n.Span, CodeNotIterable,
			"<For In> must be a binding like {Items}; got %q", f.In.Raw)
		return
	}
	_, raw, ok := a.resolveBinding(f.In, n.Span, "In")
	if !ok {
		// In 自身解析失败，诊断已产出。**跳过整个子树**：
		// 继续遍历会让循环体里的 {item.X} 被误报成「首段无法解析」，
		// 把「In 写错了」掩盖成「item 写错了」。
		return
	}
	elem, index, err := iterableElemType(raw)
	if err != nil {
		// 同上：无法建立作用域时只报这一条，不继续分析循环体。
		a.report(n.Span, CodeNotIterable, "<For In> %s", err.Error())
		return
	}

	scope := make(map[string]types.Type, 2)
	if f.Each != "" {
		scope[f.Each] = elem
	}
	if f.Index != "" {
		scope[f.Index] = index
	}
	a.scopes = append(a.scopes, scope)

	// Key 在循环作用域内解析（可引用 Each/Index）。
	var keyPath *ResolvedPath
	if f.HasKey && f.Key.IsBinding {
		if p, _, ok := a.resolveBinding(f.Key, n.Span, "Key"); ok {
			keyPath = p
		}
	}

	a.facts.set(n, &NodeFacts{forCl: &ResolvedFor{
		Each:      f.Each,
		Elem:      typeInfo(elem),
		Index:     f.Index,
		IndexType: typeInfo(index),
		Key:       keyPath,
	}})

	for _, c := range n.Children {
		a.walk(c)
	}

	a.scopes = a.scopes[:len(a.scopes)-1]
}

// ============================================================================
// 绑定路径解析
// ============================================================================

// resolveBinding 解析一条绑定路径。
//
// 返回公开结果（只含字符串与枚举）、原始 Go 类型（供作用域建立，不进入
// contracts）以及是否成功。失败时已产出对应诊断。
func (a *analyzer) resolveBinding(
	b contracts.BindingVal,
	span contracts.SourceSpan,
	what string,
) (*ResolvedPath, types.Type, bool) {
	if !b.IsBinding || len(b.Segments) == 0 {
		a.report(span, CodeBindUnsupported,
			"%s: binding %q is not a dotted identifier path", what, b.Raw)
		return nil, nil, false
	}

	segs := b.Segments
	path := &ResolvedPath{Negated: b.IsNegated}

	// ---- 首段：循环变量优先，其次是 ViewModel 字段 ----
	var cur types.Type
	if t, ok := a.lookupScope(segs[0]); ok {
		cur = t
		path.Root = segs[0]
		path.RootKind = RootForVar
	} else {
		if a.vm == nil {
			a.report(span, CodePathRoot,
				"%s: %q cannot be resolved: no ViewModel is declared", what, segs[0])
			return nil, nil, false
		}
		st, found := stepType(a.vm.Named, segs[0])
		switch {
		case !found:
			a.report(span, CodePathRoot,
				"%s: %q is neither a loop variable nor a field of %s",
				what, segs[0], typeInfo(a.vm.Named).Display)
			return nil, nil, false
		case st.unexported:
			a.report(span, CodePathUnexported,
				"%s: field %q of %s exists but is not exported",
				what, segs[0], typeInfo(a.vm.Named).Display)
			return nil, nil, false
		case st.bad != "":
			a.report(span, CodeBindUnsupported, "%s: %s", what, st.bad)
			return nil, nil, false
		}
		cur = st.typ
		path.RootKind = RootViewModel
		path.Steps = append(path.Steps, PathStep{Name: segs[0], Type: typeInfo(cur)})
	}

	// ---- 中间段：逐段基于**上一段解析出的实际类型**查找 ----
	for _, seg := range segs[1:] {
		st, found := stepType(cur, seg)
		switch {
		case !found:
			a.report(span, CodePathField,
				"%s: type %s has no field %q", what, typeInfo(cur).Display, seg)
			return nil, nil, false
		case st.unexported:
			a.report(span, CodePathUnexported,
				"%s: field %q of %s exists but is not exported",
				what, seg, typeInfo(cur).Display)
			return nil, nil, false
		case st.bad != "":
			a.report(span, CodeBindUnsupported, "%s: %s", what, st.bad)
			return nil, nil, false
		}
		cur = st.typ
		path.Steps = append(path.Steps, PathStep{Name: seg, Type: typeInfo(cur)})
	}

	path.Type = typeInfo(cur)
	return path, cur, true
}

// stepResult 是一次字段/方法访问的结果。
type stepResult struct {
	typ types.Type
	// unexported 表示成员存在但未导出。
	unexported bool
	// bad 非空表示该形式不受支持（BIND-005）。
	bad string
}

// stepType 在 recv 上查找成员 name 的类型。
//
// 支持两类成员（规范 §4 v1 允许）：
//   - 字段；
//   - 零参数、单返回值的**方法**（隐式取值，不允许写成 Method()）。
func stepType(recv types.Type, name string) (stepResult, bool) {
	if recv == nil {
		return stepResult{}, false
	}

	// 先手动扫描**直接字段**。
	//
	// 原因（实测）：types.LookupFieldOrMethod 在 pkg == nil 时**不会返回
	// 未导出成员**。若直接用它会得到「字段不存在」，从而把 BIND-003
	// （存在但未导出）误报成 BIND-002（不存在）——而这两者的修复动作
	// 完全不同。手动遍历可以看到未导出字段，据此给出正确诊断。
	base := derefPtr(recv)
	if st, ok := base.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			f := st.Field(i)
			if f.Name() == name {
				return stepResult{typ: f.Type(), unexported: !f.Exported()}, true
			}
		}
	}

	// 再查提升字段（嵌入类型）与方法。
	// addressable=true：允许指针接收者方法。
	obj, _, _ := types.LookupFieldOrMethod(recv, true, nil, name)
	if obj == nil {
		// 接收者根本无法承载成员（如 int 上取字段）→ 形式不受支持。
		if !hasMembers(base) {
			return stepResult{bad: fmt.Sprintf(
				"cannot access %q on %s; only struct fields and zero-argument methods are allowed",
				name, typeInfo(recv).Display)}, true
		}
		return stepResult{}, false
	}

	switch o := obj.(type) {
	case *types.Var:
		return stepResult{typ: o.Type(), unexported: !o.Exported()}, true
	case *types.Func:
		sig, ok := o.Type().(*types.Signature)
		if !ok {
			return stepResult{bad: fmt.Sprintf("%q is not usable in a binding path", name)}, true
		}
		if sig.Params().Len() != 0 {
			return stepResult{bad: fmt.Sprintf(
				"method %q takes parameters and cannot be used as a path segment", name)}, true
		}
		if sig.Results().Len() != 1 {
			return stepResult{bad: fmt.Sprintf(
				"method %q must return exactly one value to be used as a path segment", name)}, true
		}
		if !o.Exported() {
			return stepResult{unexported: true}, true
		}
		return stepResult{typ: sig.Results().At(0).Type()}, true
	default:
		return stepResult{bad: fmt.Sprintf("%q is not a field", name)}, true
	}
}

// ============================================================================
// 可迭代类型
// ============================================================================

// iterableElemType 返回可迭代体的元素与索引类型。
func iterableElemType(t types.Type) (elem, index types.Type, err error) {
	if t == nil {
		return nil, nil, errors.New("has no type")
	}
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return u.Elem(), types.Typ[types.Int], nil
	case *types.Array:
		return u.Elem(), types.Typ[types.Int], nil
	case *types.Map:
		return u.Elem(), u.Key(), nil
	case *types.Basic:
		if u.Kind() == types.String {
			return nil, nil, errors.New(
				"expects a slice, array or map, got string; iterating runes is not supported in v1")
		}
	}
	return nil, nil, fmt.Errorf("expects a slice, array or map, got %s", typeInfo(t).Display)
}

// ============================================================================
// 类型信息转换
// ============================================================================

// typeInfo 把 Go 类型转换为语义 IR 的类型事实。
func typeInfo(t types.Type) TypeInfo {
	if t == nil {
		return TypeInfo{}
	}
	_, named := t.(*types.Named)
	return TypeInfo{
		Display: types.TypeString(t, func(p *types.Package) string {
			if p == nil {
				return ""
			}
			return p.Name()
		}),
		Kind:     valueKind(t),
		Named:    named,
		Stringer: implementsStringer(t),
	}
}

// valueKind 返回底层类别（剥离命名类型外壳）。
func valueKind(t types.Type) ValueKind {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.String:
			return ValueString
		case types.Bool:
			return ValueBool
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
			return ValueInt
		case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
			return ValueUint
		case types.Float32, types.Float64:
			return ValueFloat
		}
		return ValueOther
	case *types.Struct:
		return ValueStruct
	case *types.Slice:
		return ValueSlice
	case *types.Map:
		return ValueMap
	case *types.Pointer:
		return ValuePointer
	case *types.Interface:
		return ValueInterface
	}
	return ValueOther
}

// implementsStringer 报告类型是否实现 fmt.Stringer。
func implementsStringer(t types.Type) bool {
	for _, typ := range []types.Type{t, types.NewPointer(t)} {
		ms := types.NewMethodSet(typ)
		sel := ms.Lookup(nil, "String")
		if sel == nil {
			continue
		}
		fn, ok := sel.Obj().(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		if b, ok := sig.Results().At(0).Type().(*types.Basic); ok && b.Kind() == types.String {
			return true
		}
	}
	return false
}

// derefPtr 逐层剥离指针。
func derefPtr(t types.Type) types.Type {
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			return t
		}
		t = p.Elem()
	}
}

// hasMembers 报告类型是否可能承载字段或方法。
func hasMembers(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Struct, *types.Interface:
		return true
	}
	return types.NewMethodSet(t).Len() > 0
}

// checkBindingType 判定路径结果类型能否赋给目标属性类型。
//
// 两层检查（契约 §11）：
//  1. `{!Path}` 的操作数必须是布尔（§11.6），不复用兼容矩阵；
//  2. 普通绑定按兼容矩阵判定（§11.2）。
//
// 返回 false 表示不通过，此时不产生语义事实。
func (a *analyzer) checkBindingType(
	path *ResolvedPath,
	raw types.Type,
	target contracts.PropType,
	span contracts.SourceSpan,
	what string,
) bool {
	// 取反是独立的表达式约束，先判。
	if path.Negated {
		if !negatable(raw) {
			a.report(span, CodeBindTypeMismatch,
				"%s: cannot negate %q: type %s is not bool",
				what, pathString(path), typeInfo(raw).Display)
			return false
		}
		// 取反结果恒为 bool，因此要求目标属性是 bool。
		if target == contracts.PropUnknown {
			return false // Schema 缺类型，不产生事实（§11.7）
		}
		if target != contracts.PropBool {
			a.report(span, CodeBindTypeMismatch,
				"%s: negated expression is bool, but the attribute expects %s",
				what, propTypeName(target))
			return false
		}
		return true
	}

	switch checkCompat(raw, target, a.env) {
	case compatUnknown:
		return false
	case compatMismatch:
		a.report(span, CodeBindTypeMismatch,
			"%s: binding %q has type %s, but the attribute expects %s",
			what, pathString(path), typeInfo(raw).Display, propTypeName(target))
		return false
	}
	return true
}

// pathString 还原文本形式，用于诊断。
func pathString(p *ResolvedPath) string {
	if p == nil {
		return ""
	}
	s := ""
	if p.Root != "" {
		s = p.Root
	}
	for _, st := range p.Steps {
		if s != "" {
			s += "."
		}
		s += st.Name
	}
	if p.RootKind == RootViewModel && p.Root == "" {
		return "{" + s + "}"
	}
	return "{" + s + "}"
}
