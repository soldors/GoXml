package expander

import (
	"github.com/goxml/goxml/contracts"
)

// 宏展开相关的诊断码。
const (
	// CodeComponentUnknown 使用了未定义的组件标签。
	CodeComponentUnknown = contracts.CodePrefixDef + "-006"
	// CodePropUnknown 调用了组件未声明的属性。
	CodePropUnknown = contracts.CodePrefixDef + "-007"
	// CodePropMissing 缺少必填属性且无默认值。
	CodePropMissing = contracts.CodePrefixDef + "-008"
	// CodeSlotNotAllowed 组件未声明 <Slot/> 却传入了子节点。
	CodeSlotNotAllowed = contracts.CodePrefixDef + "-009"
	// CodeRecursiveDef <Def> 直接或间接递归引用自身。
	CodeRecursiveDef = contracts.CodePrefixDef + "-010"
)

// expandDefs 把文档中所有 <Def> 调用点内联展开为组件的渲染子树。
//
// 展开语义（规范 §7）：
//
//   - 调用点的每个属性按 Props 声明做「值替换」：Def 体内任何以该 Prop 名
//     开头的绑定路径，其首段被替换为调用方传入的 BindingVal。
//     替换的是值而非文本，因此调用方传静态字面量时，展开结果也是静态字面量。
//   - 调用点子节点替换 <Slot/>；组件无 <Slot/> 时不得传子节点。
//   - 未提供的属性使用默认值；无默认值且必填时报告缺失。
//
// 展开是纯结构变换：不生成新标签、不引入运行时注册表。
func (e *expander) expandDefs() {
	if e.doc == nil || e.doc.Page == nil {
		return
	}
	// 页面是展开入口；Def 体本身不展开（它只在被调用时实例化）。
	e.doc.Page, _ = e.expandNode(e.doc.Page, nil)

	// 只检查页面：Def 体是模板，其中的 Prop 引用在未实例化时是合法的，
	// 不能按「未解析的颜色绑定」报错。
	e.checkUnresolvedColors(e.doc.Page)
}

// checkUnresolvedColors 报告展开后仍无法解析的颜色属性。
//
// 两种情况都必须报错：
//
//  1. 展开后仍是绑定形式：说明它不是对 color 类型 Prop 的引用，
//     而颜色属性不接受任意绑定。
//  2. 展开后是静态值但不是合法颜色形式：说明调用方给 color 类型 Prop
//     传了非法值（如 "red"）。
//
// Background 为 nil 表示 Parser 未能解析出颜色，需在这里兜底检查。
func (e *expander) checkUnresolvedColors(n *contracts.NodeIR) {
	if n == nil {
		return
	}
	if n.Background == nil {
		if ra, ok := n.RawAttrs.Lookup("Background"); ok {
			if ra.Value.IsBinding {
				e.diags = append(e.diags, *e.err(ra.Span, CodeNotApplicable,
					"Background expects #RRGGBB, #RRGGBBAA or $Theme.Field; "+
						"a binding is only allowed when referencing a color-typed <Def> Prop"))
			} else {
				e.diags = append(e.diags, *e.err(ra.Span, CodeNotApplicable,
					"Background expects #RRGGBB, #RRGGBBAA or $Theme.Field, got %q", ra.Value.Raw))
			}
		}
	}
	for _, c := range n.Children {
		e.checkUnresolvedColors(c)
	}
	if n.If != nil {
		for _, c := range n.If.Then {
			e.checkUnresolvedColors(c)
		}
		for _, c := range n.If.Else {
			e.checkUnresolvedColors(c)
		}
	}
	if n.Def != nil && n.Def.Body != nil {
		e.checkUnresolvedColors(n.Def.Body)
	}
}

// expandEnv 是展开时的替换环境：Prop 名 -> 调用方传入的值。
type expandEnv map[string]contracts.BindingVal

// lookup 按首段查找 Prop 替换值。
func (env expandEnv) lookup(seg string) (contracts.BindingVal, bool) {
	if env == nil {
		return contracts.BindingVal{}, false
	}
	v, ok := env[seg]
	return v, ok
}

// expandNode 递归展开节点。
//
// 若节点标签是已注册组件，则替换为该组件的展开副本；
// 否则原地展开子节点并返回自身。
func (e *expander) expandNode(n *contracts.NodeIR, env expandEnv) (*contracts.NodeIR, bool) {
	if n == nil {
		return nil, false
	}

	// 未注册标签：按普通节点处理。
	if _, isDef := e.schemas[n.Tag]; !isDef || n.Kind != contracts.KindElement {
		e.expandChildren(n, env)
		return n, true
	}

	// 组件调用点。
	expanded, cerr := e.expandCall(n, env)
	if cerr != nil {
		e.diags = append(e.diags, *cerr)
		return n, false
	}
	return expanded, true
}

// expandChildren 原地展开子节点，跳过被删除的节点。
func (e *expander) expandChildren(n *contracts.NodeIR, env expandEnv) {
	if len(n.Children) > 0 {
		out := make([]*contracts.NodeIR, 0, len(n.Children))
		for _, c := range n.Children {
			if r, ok := e.expandNode(c, env); ok && r != nil {
				out = append(out, r)
			}
		}
		n.Children = out
	}
	if n.If != nil {
		n.If.Then = e.expandList(n.If.Then, env)
		n.If.Else = e.expandList(n.If.Else, env)
	}
	if n.Def != nil && n.Def.Body != nil {
		if r, ok := e.expandNode(n.Def.Body, env); ok && r != nil {
			n.Def.Body = r
		}
	}
}

// expandList 展开列表，跳过被删除的节点。
func (e *expander) expandList(list []*contracts.NodeIR, env expandEnv) []*contracts.NodeIR {
	out := make([]*contracts.NodeIR, 0, len(list))
	for _, c := range list {
		if r, ok := e.expandNode(c, env); ok && r != nil {
			out = append(out, r)
		}
	}
	return out
}

// expandCall 把组件调用点替换为展开后的子树。
func (e *expander) expandCall(call *contracts.NodeIR, outer expandEnv) (*contracts.NodeIR, *contracts.CompilerError) {
	def, ok := e.defs[call.Tag]
	if !ok || def.Def == nil {
		cerr := e.err(call.Span, CodeComponentUnknown, "unknown component <%s>", call.Tag)
		return nil, cerr
	}

	if e.inExpansion[call.Tag] {
		cerr := e.err(call.Span, CodeRecursiveDef,
			"component <%s> refers to itself, directly or indirectly", call.Tag)
		return nil, cerr
	}

	// 收集调用实参：按 Props 声明顺序解析，未提供的用默认值。
	args, cerr := e.collectArgs(call, def)
	if cerr != nil {
		return nil, cerr
	}

	// 插槽内容：调用点的子节点（在调用方作用域内展开）。
	slotted := e.expandList(call.Children, outer)
	if !def.Def.HasSlot && len(slotted) > 0 {
		cerr := e.err(call.Span, CodeSlotNotAllowed,
			"component <%s> does not declare <Slot/> and cannot accept children", call.Tag)
		return nil, cerr
	}

	// 克隆 Def 体并做值替换。
	// 递归检测必须覆盖「克隆 → 替换 → 再次展开」的全程。
	// 若只包住克隆，嵌套展开时标志已被清除，A→B→A 会无限递归。
	e.inExpansion[call.Tag] = true
	defer delete(e.inExpansion, call.Tag)

	body := cloneNode(def.Def.Body)
	substituted := e.substitute(body, args)

	// 用插槽内容替换 <Slot/> 节点。
	result := e.replaceSlot(substituted, slotted)
	if result == nil {
		// Def 体整个就是 <Slot/>：直接返回插槽内容。
		// 多个子节点时用一个隐式容器不可行（GXML 无此节点），
		// 因此取第一个并对其余报告诊断。
		if len(slotted) == 0 {
			cerr := e.err(call.Span, CodeSlotNotAllowed,
				"component <%s> requires slot content but the call has no children", call.Tag)
			return nil, cerr
		}
		if len(slotted) > 1 {
			cerr := e.err(call.Span, CodeSlotNotAllowed,
				"component <%s> whose body is <Slot/> accepts exactly one child, got %d",
				call.Tag, len(slotted))
			return nil, cerr
		}
		return slotted[0], nil
	}

	// 展开结果可能仍含组件调用（嵌套），继续展开。
	// 展开后的节点处于调用方作用域，因此传 outer 环境。
	expanded, _ := e.expandNode(result, outer)
	return expanded, nil
}

// collectArgs 解析调用点实参为替换环境。
//
// 调用点的属性在分类阶段只处理了布局属性，其余仍留在 RawAttrs。
// 这里区分两类：
//
//   - 命中 Props 声明 → 作为替换值进入 env；
//   - 命中 Events 声明 → 记为调用方的事件处理器（见 callEvents）；
//   - 都不是           → GXML-DEF-007 组件没有该属性。
func (e *expander) collectArgs(call *contracts.NodeIR, def *contracts.NodeIR) (expandEnv, *contracts.CompilerError) {
	env := make(expandEnv, len(def.Def.Props))

	for _, ra := range call.RawAttrs {
		if _, ok := def.Def.Prop(ra.Name); ok {
			continue
		}
		if _, ok := def.Def.Event(ra.Name); ok {
			// 事件处理器：v1 只做契约校验，不参与展开替换。
			// 事件如何在 Def 体内触发属于 v2（需要事件冒泡或回调插槽）。
			if _, cerr := e.parseEvent(ra); cerr != nil {
				return nil, cerr
			}
			continue
		}
		cerr := e.err(ra.Span, CodePropUnknown,
			"component <%s> has no property or event %q", call.Tag, ra.Name)
		return nil, cerr
	}

	for _, p := range def.Def.Props {
		ra, ok := call.RawAttrs.Lookup(p.Name)
		if ok {
			env[p.Name] = ra.Value
			continue
		}
		// 未传入：使用默认值。
		if p.HasDefault {
			env[p.Name] = p.Default
			continue
		}
		cerr := e.err(call.Span, CodePropMissing,
			"component <%s> requires property %q", call.Tag, p.Name)
		return nil, cerr
	}
	return env, nil
}

// substitute 对子树做 Prop 值替换。
//
// 替换规则：绑定路径的首段若命中某个 Prop 名，则整条绑定被替换为
// 调用方传入的值。例如 Def 体内 `{Name}` 在调用方传 `{User.Name}` 时
// 变为 `{User.Name}`；调用方传静态 `张三` 时变为静态 `张三`。
//
// 只替换首段命中者，避免误伤 `{UserName}` 这类更长的标识符。
func (e *expander) substitute(n *contracts.NodeIR, env expandEnv) *contracts.NodeIR {
	if n == nil || len(env) == 0 {
		return n
	}

	// 颜色属性（Background）在 Parser 阶段遇到绑定形式时留空，
	// 这里先替换再解析为实际颜色或主题引用。
	e.resolveColorAttr(n, "Background", env)

	n.RawAttrs = substituteRawAttrs(n.RawAttrs, env)
	n.Attrs = substituteAttrs(n.Attrs, env)
	n.Events = substituteEvents(n.Events, env)
	if n.For != nil {
		n.For.In = substituteVal(n.For.In, env)
		n.For.Key = substituteVal(n.For.Key, env)
	}
	if n.If != nil {
		n.If.Condition = substituteVal(n.If.Condition, env)
	}

	for i, c := range n.Children {
		n.Children[i] = e.substitute(c, env)
	}
	if n.If != nil {
		for i, c := range n.If.Then {
			n.If.Then[i] = e.substitute(c, env)
		}
		for i, c := range n.If.Else {
			n.If.Else[i] = e.substitute(c, env)
		}
	}
	if n.Def != nil && n.Def.Body != nil {
		n.Def.Body = e.substitute(n.Def.Body, env)
	}
	return n
}

// substituteVal 替换单个 BindingVal。
func substituteVal(v contracts.BindingVal, env expandEnv) contracts.BindingVal {
	if !v.IsBinding || len(v.Segments) == 0 {
		return v
	}
	repl, ok := env.lookup(v.Segments[0])
	if !ok {
		return v
	}
	// 调用方传入静态值时，Prop 引用同样变为静态值。
	if !repl.IsBinding {
		return repl
	}
	// 调用方传入绑定时，拼接剩余路径：{Name.X} + {User.Name} -> {User.Name.X}。
	if len(v.Segments) == 1 {
		return repl
	}
	segs := make([]string, 0, len(repl.Segments)+len(v.Segments)-1)
	segs = append(segs, repl.Segments...)
	segs = append(segs, v.Segments[1:]...)
	out := repl
	out.Segments = segs
	return out
}

func substituteRawAttrs(list contracts.RawAttrList, env expandEnv) contracts.RawAttrList {
	if len(list) == 0 {
		return list
	}
	out := make(contracts.RawAttrList, len(list))
	for i, a := range list {
		out[i] = contracts.RawAttr{Name: a.Name, Value: substituteVal(a.Value, env), Span: a.Span}
	}
	return out
}

func substituteAttrs(list contracts.AttrList, env expandEnv) contracts.AttrList {
	if len(list) == 0 {
		return list
	}
	out := make(contracts.AttrList, len(list))
	for i, a := range list {
		out[i] = contracts.Attr{Name: a.Name, Value: substituteVal(a.Value, env), Span: a.Span}
	}
	return out
}

func substituteEvents(list []contracts.EventBinding, env expandEnv) []contracts.EventBinding {
	if len(list) == 0 {
		return list
	}
	out := make([]contracts.EventBinding, len(list))
	for i, ev := range list {
		cp := ev
		if len(ev.Args) > 0 {
			cp.Args = make([]contracts.EventArg, len(ev.Args))
			for j, a := range ev.Args {
				cp.Args[j] = a
				cp.Args[j].Binding = substituteVal(a.Binding, env)
			}
		}
		out[i] = cp
	}
	return out
}

// replaceSlot 把子树中的 <Slot/> 节点替换为插槽内容。
//
// 返回 nil 表示整个子树就是一个 <Slot/>，由调用方直接返回插槽内容。
// 若 <Slot/> 出现在非根位置且插槽内容多于一个节点，报告诊断。
func (e *expander) replaceSlot(n *contracts.NodeIR, slotted []*contracts.NodeIR) *contracts.NodeIR {
	if n == nil {
		return nil
	}
	if n.Kind == contracts.KindSlot {
		if len(slotted) == 1 {
			return slotted[0]
		}
		if len(slotted) == 0 {
			return nil
		}
		// 多个子节点插入单个 <Slot/> 位置：GXML 无隐式容器节点，
		// 无法表达，明确报错而不是静默丢弃。
		e.diags = append(e.diags, *e.err(n.Span, CodeSlotNotAllowed,
			"<Slot/> is replaced by %d children; v1 supports exactly one child per slot",
			len(slotted)))
		return slotted[0]
	}

	out := make([]*contracts.NodeIR, 0, len(n.Children))
	for _, c := range n.Children {
		if r := e.replaceSlot(c, slotted); r != nil {
			out = append(out, r)
		}
	}
	n.Children = out

	if n.If != nil {
		n.If.Then = e.replaceSlotList(n.If.Then, slotted)
		n.If.Else = e.replaceSlotList(n.If.Else, slotted)
	}
	if n.Def != nil && n.Def.Body != nil {
		n.Def.Body = e.replaceSlot(n.Def.Body, slotted)
	}
	return n
}

func (e *expander) replaceSlotList(list []*contracts.NodeIR, slotted []*contracts.NodeIR) []*contracts.NodeIR {
	out := make([]*contracts.NodeIR, 0, len(list))
	for _, c := range list {
		if r := e.replaceSlot(c, slotted); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// cloneNode 深拷贝节点，避免展开时修改 Def 定义本身。
//
// 同一个 Def 可能被调用多次，若不克隆，第二次展开会看到已被替换的子树。
func cloneNode(n *contracts.NodeIR) *contracts.NodeIR {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Span = n.Span

	if len(n.Columns) > 0 {
		cp.Columns = append([]contracts.GridColumn(nil), n.Columns...)
	}
	if len(n.RawAttrs) > 0 {
		cp.RawAttrs = append(contracts.RawAttrList(nil), n.RawAttrs...)
	}
	if len(n.Attrs) > 0 {
		cp.Attrs = append(contracts.AttrList(nil), n.Attrs...)
	}
	if len(n.Events) > 0 {
		cp.Events = append([]contracts.EventBinding(nil), n.Events...)
	}
	if len(n.Children) > 0 {
		cp.Children = make([]*contracts.NodeIR, len(n.Children))
		for i, c := range n.Children {
			cp.Children[i] = cloneNode(c)
		}
	}
	if n.For != nil {
		f := *n.For
		cp.For = &f
	}
	if n.If != nil {
		ic := *n.If
		ic.Then = cloneList(n.If.Then)
		ic.Else = cloneList(n.If.Else)
		cp.If = &ic
	}
	if n.Def != nil {
		dc := *n.Def
		if len(n.Def.Props) > 0 {
			dc.Props = append([]contracts.DefProp(nil), n.Def.Props...)
		}
		if len(n.Def.Events) > 0 {
			dc.Events = append([]contracts.EventSchema(nil), n.Def.Events...)
		}
		dc.Body = cloneNode(n.Def.Body)
		cp.Def = &dc
	}
	return &cp
}

func cloneList(list []*contracts.NodeIR) []*contracts.NodeIR {
	if len(list) == 0 {
		return list
	}
	out := make([]*contracts.NodeIR, len(list))
	for i, c := range list {
		out[i] = cloneNode(c)
	}
	return out
}
