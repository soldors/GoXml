package analyzer

import (
	"go/types"
	"strconv"

	"github.com/goxml/goxml/contracts"
)

// 本文件实现契约 §8.4：事件签名校验。
//
// 核心规则（Q7 = 方案 A，已冻结）：
//
//	方法形参 = 事件声明参数（载荷）++ 调用点显式实参
//
// 顺序固定为「载荷在前、实参在后」，**不得**反推。载荷与实参必须分别
// 保存来源，不能只留拼接后的列表——否则无法诊断「第 2 个参数来自哪」。
//
// 诊断：
//   EVENT-003 方法不存在
//   EVENT-004 数量不匹配
//   EVENT-006 某一项类型不兼容
//   EVENT-007 方法有返回值（Q4）

// analyzeEvents 校验节点上的事件绑定。
func (a *analyzer) analyzeEvents(n *contracts.NodeIR, nf *NodeFacts) *NodeFacts {
	for _, ev := range n.Events {
		re, ok := a.resolveEvent(n, ev)
		if !ok {
			continue // 失败只产诊断，不进事实
		}
		if nf == nil {
			nf = &NodeFacts{}
		}
		nf.events = append(nf.events, *re)
	}
	return nf
}

// resolveEvent 校验单个事件绑定并构造语义事实。
func (a *analyzer) resolveEvent(n *contracts.NodeIR, ev contracts.EventBinding) (*ResolvedEvent, bool) {
	// ---- 第 1 步：Schema 载荷 ----
	//
	// 未知事件不在此报错：它属于 Expander 的分类职责（ATTR-004 / 事件
	// 声明校验）。这里只做防御性跳过，避免重复诊断。
	es, ok := a.eventSchema(n.Tag, ev.Name)
	if !ok {
		return nil, false
	}

	payload := make([]ResolvedParam, 0, len(es.Params))
	payloadTypes := make([]types.Type, 0, len(es.Params))
	for _, p := range es.Params {
		payload = append(payload, ResolvedParam{
			Name: p.Name,
			Type: typeInfoForProp(p.Type),
			Span: es.Span,
		})
		payloadTypes = append(payloadTypes, nil) // 由 PropType 判定，见下
	}

	// ---- 第 2 步：调用点实参 ----
	// 每个实参解析为 ResolvedArg：既做类型校验，也保留可生成的类型化取值
	// （事件处理输出契约 §5.4）。
	callArgs := make([]ResolvedArg, 0, len(ev.Args))
	callArgTypes := make([]types.Type, 0, len(ev.Args))
	for _, arg := range ev.Args {
		switch arg.Kind {
		case contracts.ArgLiteral:
			t, lit, ok := parseLiteral(arg.Literal)
			if !ok {
				// 字面量形态异常：Expander 已做过形式校验，此处属内部
				// 不一致，按不兼容处理而不是猜一个类型。
				a.report(ev.Span, CodeMethodArgType,
					"event %q: cannot determine the type of argument %q", ev.Name, arg.Literal)
				return nil, false
			}
			callArgs = append(callArgs, ResolvedArg{
				Kind:    ArgLiteral,
				Literal: lit,
				Type:    typeInfo(t),
				Span:    ev.Span,
			})
			callArgTypes = append(callArgTypes, t)

		case contracts.ArgBinding:
			path, raw, ok := a.resolveBinding(arg.Binding, ev.Span, ev.Name)
			if !ok {
				// 路径自身的诊断已产出。不得再叠加事件类型错误——
				// 否则用户会以为有两个问题（契约 §8.4 R3 的配套约束）。
				return nil, false
			}
			callArgs = append(callArgs, ResolvedArg{
				Kind: ArgPath,
				Path: path,
				Type: path.Type,
				Span: ev.Span,
			})
			callArgTypes = append(callArgTypes, raw)

		default:
			return nil, false
		}
	}

	// ---- 第 3 步：目标方法 ----
	if a.vm == nil {
		a.report(ev.Span, CodeMethodNotFound,
			"event %q: handler %q cannot be resolved because no ViewModel is declared",
			ev.Name, ev.Method)
		return nil, false
	}
	obj, _, _ := types.LookupFieldOrMethod(a.vm.Named, true, nil, ev.Method)
	fn, ok := obj.(*types.Func)
	if !ok {
		a.report(ev.Span, CodeMethodNotFound,
			"event %q: %s has no method %q", ev.Name, typeInfo(a.vm.Named).Display, ev.Method)
		return nil, false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		a.report(ev.Span, CodeMethodNotFound,
			"event %q: %q is not a method", ev.Name, ev.Method)
		return nil, false
	}

	method := ResolvedMethod{
		Name:      ev.Method,
		HasReturn: sig.Results().Len() > 0,
	}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		method.Params = append(method.Params, ResolvedParam{
			Name: p.Name(),
			Type: typeInfo(p.Type()),
		})
	}

	// ---- Q4：方法不得有返回值 ----
	//
	// 先于数量校验：一个有返回值的方法无论参数是否匹配都不可用，
	// 报数量错误会把用户引向错误方向。
	if method.HasReturn {
		a.report(ev.Span, CodeMethodReturns,
			"event %q: method %q must not return values; event handlers have no result",
			ev.Name, ev.Method)
		return nil, false
	}

	// ---- 第 4 步：数量 ----
	expected := len(payload) + len(callArgs)
	if sig.Params().Len() != expected {
		a.report(ev.Span, CodeMethodArity,
			"event %q: method %q expects %d parameter(s) (%d event payload + %d call argument(s)), got %d",
			ev.Name, ev.Method, sig.Params().Len(), len(payload), len(callArgs), expected)
		return nil, false
	}

	// ---- Mapping：与形参一一对应，标注来源 ----
	mapping := make([]ParamOrigin, 0, expected)
	for range payload {
		mapping = append(mapping, FromPayload)
	}
	for range callArgs {
		mapping = append(mapping, FromCallArg)
	}

	// ---- 第 5 步：逐项类型 ----
	for i := 0; i < sig.Params().Len(); i++ {
		want := sig.Params().At(i).Type()
		if i < len(payload) {
			// 载荷由 Schema 的 PropType 描述，不是 Go 类型。
			if !payloadCompatible(es.Params[i].Type, want, a.env) {
				a.report(ev.Span, CodeMethodArgType,
					"event %q: method %q parameter %d expects %s, but the event payload provides %s",
					ev.Name, ev.Method, i+1, typeInfo(want).Display, propTypeName(es.Params[i].Type))
				return nil, false
			}
			continue
		}
		j := i - len(payload)
		if !types.AssignableTo(callArgTypes[j], want) {
			a.report(ev.Span, CodeMethodArgType,
				"event %q: method %q parameter %d expects %s, got %s",
				ev.Name, ev.Method, i+1, typeInfo(want).Display, typeInfo(callArgTypes[j]).Display)
			return nil, false
		}
	}

	return &ResolvedEvent{
		Name:     ev.Name,
		Payload:  payload,
		CallArgs: callArgs,
		Method:   method,
		Mapping:  mapping,
	}, true
}

// eventSchema 查询组件事件的契约。
func (a *analyzer) eventSchema(tag, name string) (contracts.EventSchema, bool) {
	if a.schemas == nil {
		return contracts.EventSchema{}, false
	}
	s, ok := a.schemas.Lookup(tag)
	if !ok || s == nil {
		return contracts.EventSchema{}, false
	}
	return s.Event(name)
}

// literalType 推断事件实参字面量的 Go 类型。
//
// v1 只允许四种字面量（契约 §8.4）：布尔、整数、浮点、字符串。
//
// 保留为薄包装：单元测试与旧调用点按类型取用即可。
func literalType(lit string) (types.Type, bool) {
	t, _, ok := parseLiteral(lit)
	return t, ok
}

// parseLiteral 解析字面量：返回类型与**类型化的值**（事件处理输出契约 §5.4）。
//
// 文法由 Expander 保证（expander.parseEventArg）：
//   - 布尔：true / false
//   - 字符串：引号包裹，**保留引号**，内容为引号之间的原始字节（无转义处理）
//   - 数字：整数或浮点，允许负号（isNumberLiteral）
//
// 本函数只做类型化，**不决定 Go 语法**（不加引号、不定数字格式）——
// 渲染归 Codegen（契约 §5.4.2）。
func parseLiteral(lit string) (types.Type, LiteralValue, bool) {
	if lit == "" {
		return nil, LiteralValue{}, false
	}
	// 字符串字面量保留引号（见 contracts.EventArg.Literal 的契约）。
	if lit[0] == '\'' || lit[0] == '"' {
		q := lit[0]
		if len(lit) < 2 || lit[len(lit)-1] != q {
			return nil, LiteralValue{}, false
		}
		return types.Typ[types.String],
			LiteralValue{Kind: LiteralString, Str: lit[1 : len(lit)-1], Raw: lit}, true
	}
	switch lit {
	case "true":
		return types.Typ[types.Bool],
			LiteralValue{Kind: LiteralBool, Bool: true, Raw: lit}, true
	case "false":
		return types.Typ[types.Bool],
			LiteralValue{Kind: LiteralBool, Bool: false, Raw: lit}, true
	}
	// 整数
	if n, err := strconv.ParseInt(lit, 10, 64); err == nil {
		return types.Typ[types.Int],
			LiteralValue{Kind: LiteralInt, Int: n, Raw: lit}, true
	}
	// 浮点
	if f, err := strconv.ParseFloat(lit, 64); err == nil {
		return types.Typ[types.Float64],
			LiteralValue{Kind: LiteralFloat, Float: f, Raw: lit}, true
	}
	// Expander 已保证字面量只有这四种形态；走到这里说明 IR 不一致。
	return nil, LiteralValue{}, false
}

// payloadCompatible 判定事件载荷的 PropType 能否传给方法形参。
//
// **与调用点实参的判定不同，这里按「类别」而非「Go 赋值」比较。**
//
// 原因：载荷在 Schema 中用 PropType 描述，**没有具体 Go 类型**——
// `float` 到底是 float32 还是 float64 由方法签名决定。若用
// types.AssignableTo 拿 float64 去比，会错误拒绝 `OnX(v float32)`。
//
// 调用点实参则相反：它有确定的 Go 类型，必须严格按赋值规则判断
// （契约 §8.4 R3：不做隐式转换）。
//
// 类别匹配与契约 §11.3「命名类型按底层类别兼容」一致。
func payloadCompatible(prop contracts.PropType, param types.Type, env *TypeEnv) bool {
	if param == nil {
		return false
	}
	switch prop {
	case contracts.PropString:
		return valueKind(param) == ValueString
	case contracts.PropBool:
		return valueKind(param) == ValueBool
	case contracts.PropInt:
		// 与 §11.4 一致：接受全部有符号宽度。
		return valueKind(param) == ValueInt
	case contracts.PropFloat:
		return valueKind(param) == ValueFloat
	case contracts.PropColor:
		// 颜色载荷在 v1 极少见；接受契约规定的两种标准库颜色。
		return matchesAnyAssignable(param, stdColorPkg, colorTypeNames, env)
	case contracts.PropInsets:
		return matchesAnyAssignable(param, contractsPkg, []string{insetsTypeName}, env)
	}
	return false
}

// matchesAnyAssignable 检查 param 能否接收指定包中的任一类型。
func matchesAnyAssignable(param types.Type, pkgPath string, names []string, env *TypeEnv) bool {
	if env == nil {
		return false
	}
	ts, err := env.LookupNamed(pkgPath, names)
	if err != nil {
		return false
	}
	for _, t := range ts {
		if types.AssignableTo(t, param) {
			return true
		}
	}
	return false
}

// typeInfoForProp 把 PropType 转换为 TypeInfo（用于载荷参数的事实）。
//
// 载荷的「类型」在 Schema 中用 PropType 表示，没有精确 Go 类型。
// 这里给出等价的 TypeInfo，供 Codegen 判断如何取值，但 Display 只作
// 提示用途——载荷参数的精确类型来自方法签名本身。
func typeInfoForProp(p contracts.PropType) TypeInfo {
	switch p {
	case contracts.PropString:
		return TypeInfo{Display: "string", Kind: ValueString}
	case contracts.PropBool:
		return TypeInfo{Display: "bool", Kind: ValueBool}
	case contracts.PropInt:
		return TypeInfo{Display: "int", Kind: ValueInt}
	case contracts.PropFloat:
		return TypeInfo{Display: "float64", Kind: ValueFloat}
	case contracts.PropColor:
		return TypeInfo{Display: "color", Kind: ValueStruct, Named: true}
	case contracts.PropInsets:
		return TypeInfo{Display: "insets", Kind: ValueStruct, Named: true}
	}
	return TypeInfo{}
}
