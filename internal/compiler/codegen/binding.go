package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// ============================================================================
// 步骤 1：消费 Facts，生成绑定取值表达式
//
// 依据：Analyzer 已验证的事实，Codegen **不得**自行解析绑定路径。
// 绑定路径、目标类型、最终类型都来自 Facts。若 Facts 查不到 → 报错，
// 不得回退到「当作字符串常量」。

// bindingExpr 生成一个绑定属性的取值表达式。
//
// 参数 vm 是 ViewModel 变量名（生成代码里为 "vm"）。
func bindingExpr(ra analyzer.ResolvedAttr, vm string) (string, error) {
	if ra.Path == nil {
		return "", fmt.Errorf("codegen: attr %s has no resolved path", ra.Name)
	}
	return pathExpr(ra.Path, vm, ra.Name)
}

// pathExpr 把一个已解析路径渲染为 Go 表达式。
//
// 属性绑定与事件路径实参共用本函数——两者的路径语义必须一致
// （事件处理输出契约 §5.4.3）。
func pathExpr(path *analyzer.ResolvedPath, vm, what string) (string, error) {
	if path == nil {
		return "", fmt.Errorf("codegen: %s has no resolved path", what)
	}

	// 起点：ViewModel 根 或 <For> 循环变量。
	var base string
	switch path.RootKind {
	case analyzer.RootViewModel:
		if vm == "" {
			return "", fmt.Errorf("codegen: %s binds to ViewModel but no vm parameter available", what)
		}
		base = vm
	case analyzer.RootForVar:
		if path.Root == "" {
			return "", fmt.Errorf("codegen: %s binds to a <For> variable but root is empty", what)
		}
		base = path.Root
	default:
		return "", fmt.Errorf("codegen: %s has unknown root kind %v", what, path.RootKind)
	}

	expr := base
	for _, step := range path.Steps {
		expr += "." + step.Name
		// 方法段必须生成调用：`x.M()`。
		//
		// 若写成 `x.M` 会得到函数值，类型不匹配（实测：编译失败）。
		if step.IsMethod {
			expr += "()"
		}
	}

	if path.Negated {
		expr = "!" + expr
	}

	return expr, nil
}

// renderEventArg 把已校验的事件实参渲染为 Go 表达式（契约 §5.4.3）。
//
// 只使用结构化的 Kind/Literal/Path；`Literal.Raw` 仅用于诊断，禁止据此生成。
func (g *generator) renderEventArg(a analyzer.ResolvedArg) (string, error) {
	switch a.Kind {
	case analyzer.ArgLiteral:
		switch a.Literal.Kind {
		case analyzer.LiteralString:
			// 统一走 quote()：Go 转义规则只存在一处（与属性字面量同套）。
			return quote(a.Literal.Str), nil
		case analyzer.LiteralInt:
			return strconv.FormatInt(a.Literal.Int, 10), nil
		case analyzer.LiteralFloat:
			return strconv.FormatFloat(a.Literal.Float, 'g', -1, 64), nil
		case analyzer.LiteralBool:
			if a.Literal.Bool {
				return "true", nil
			}
			return "false", nil
		default:
			return "", fmt.Errorf("codegen: event argument has unsupported literal kind %v (raw %q)",
				a.Literal.Kind, a.Literal.Raw)
		}

	case analyzer.ArgPath:
		// ForVar 实参在轮询作用域内合法：<For> 列表契约 §6.3。
		// item 在 Layout 顶部的循环里可见，pathExpr 会生成 item.ID。
		return pathExpr(a.Path, "v.vm", "event argument")

	default:
		return "", fmt.Errorf("codegen: event argument has unsupported kind %v", a.Kind)
	}
}

// renderEventArgs 渲染实参列表；空列表返回空串。
func (g *generator) renderEventArgs(args []analyzer.ResolvedArg) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		s, err := g.renderEventArg(a)
		if err != nil {
			return "", err
		}
		out = append(out, s)
	}
	return strings.Join(out, ", "), nil
}

// propFacts 从 Facts 取一个节点的属性事实列表。
func propFacts(n *contracts.NodeIR, facts *analyzer.Facts) ([]analyzer.ResolvedAttr, bool) {
	if facts == nil {
		return nil, false
	}
	nf, ok := facts.Lookup(n)
	if !ok {
		return nil, false
	}
	return nf.Attrs(), true
}

// ============================================================================
// 绑定节点的生成

// lookupAttr 在 Facts 中查找属性。
func lookupAttr(n *contracts.NodeIR, name string, facts *analyzer.Facts) (analyzer.ResolvedAttr, bool) {
	attrs, ok := propFacts(n, facts)
	if !ok {
		return analyzer.ResolvedAttr{}, false
	}
	for _, ra := range attrs {
		if ra.Name == name {
			return ra, true
		}
	}
	return analyzer.ResolvedAttr{}, false
}

// hasDynamic 递归检查子树是否为**动态**（需要 vm、且禁止整页静态固化）。
//
// 动态的判定（事件处理输出契约 §4.1）：
//   - 交互控件（Button / Input）——即使没有事件
//   - 携带事件（len(Events) > 0）
//   - 存在属性绑定（BindingVal.IsBinding，契约 §16.7）
//   - 任一子节点为动态
//
// ⚠️ 事件必须计入。若只看绑定，含 Click 的页面会被判为静态并整页录进
// CallOp——录制帧轮询一次 Clicked(gtx) 后再无人点击，后续帧只回放宏，
// 按钮表现为「编译通过、界面正常、点了没反应」。
//
// ⚠️ 交互控件也必须计入：Clickable 的悬停/按下状态随帧变化，
// 固化会把交互动画一起冻住（ADR-0006 §2.1「不含交互控件」）。
func hasDynamic(n *contracts.NodeIR) bool {
	if n == nil {
		return false
	}
	if isInteractiveTag(n.Tag) {
		return true
	}
	// <For> 的内容依赖运行时数据（行数、元素值），必然是动态的。
	// 若漏判，循环会被冻进整页宏，列表永远显示录制帧的内容。
	if n.For != nil {
		return true
	}
	if len(n.Events) > 0 {
		return true
	}
	for _, a := range n.Attrs {
		if a.Value.IsBinding {
			return true
		}
	}
	for _, c := range n.Children {
		if hasDynamic(c) {
			return true
		}
	}
	return false
}

// isInteractiveTag 报告标签是否为交互控件。
//
// 名单来自 ADR-0006 §2.1 的静态子树判定标准（「不含交互控件」）。
func isInteractiveTag(tag string) bool {
	switch tag {
	case "Button", "Input":
		return true
	}
	return false
}

// bindCacheDecl 生成绑定文本的显示缓存类型与回放方法（契约 §05.3.1）。
//
// 三层中的前两层：
//   - 值读取：每次都读（否则无法感知变化）；
//   - 显示转换：值 → 字符串。键 = renderKey + 显示串；
//     显示串不变时**回放**已录制的绘制（不再调用文本布局入口）。
//
// shaping 层：由 Gio 内部缓存承担，项目层不维护第二份
// （TASK-BINDING-IMPLEMENT-003 §3：先测量，Gio 内部已有 layoutCache）。
func bindCacheDecl(view string, slots int) string {
	if slots == 0 {
		return ""
	}
	return `
// textCache 是单个绑定 <Text> 的显示缓存槽位（契约 §05.2/§05.3.1）。
//
// ⚠️ 生成产物，不要手改。语义见 绑定系统契约 §05。
type textCache struct {
	// key 是录制时的上下文指纹（约束/DPI/主题/Locale）。
	key renderKey
	// text 是录制时的显示字符串。
	text string
	// ops 是**本槽位私有**的持久 Ops。
	//
	// 不能多槽共享：Reset 会截断数据，使所有指向它的 CallOp 失效
	// （ADR-0009 §A1 实测：panic: slice bounds out of range）。
	ops op.Ops
	// call 指向本槽 ops 中的录制区间。
	call op.CallOp
	// dims 是录制时的布局结果（CallOp.Add 不返回尺寸）。
	dims layout.Dimensions
	// valid 表示 call 是否已有效录制。
	valid bool
}

// bindText 渲染一个绑定 <Text>：
//
//   - 值读取：每次调用取值表达式（必须，否则无法感知变化）；
//   - 显示串不变且上下文未变 → 回放已录制的绘制；
//   - 否则 → 在本槽位私有的 Ops 上重录本节点。
//
// 每个绑定节点有独立槽位，互不干扰。
//
// ⚠️ 本方法由测试显式驱动时才能观察到更新；
// 生产环境的自动刷新（ViewModel 变化 → UI 重绘）归 TASK-STATE-001。
func (v *` + view + `) bindText(gtx layout.Context, th *material.Theme, slot int, s string) layout.Dimensions {
	// 惰性初始化槽位数组。
	if len(v.slots) != ` + strconv.Itoa(slots) + ` {
		v.slots = make([]textCache, ` + strconv.Itoa(slots) + `)
	}
	k := v.currentKey(gtx, th)
	c := &v.slots[slot]
	if c.valid && c.text == s && c.key.equal(k) {
		c.call.Add(gtx.Ops)
		return c.dims
	}
	c.ops.Reset()
	m := op.Record(&c.ops)
	rec := gtx
	rec.Ops = &c.ops
	c.dims = material.Body1(th, s).Layout(rec)
	c.call = m.Stop()
	c.key = k
	c.text = s
	c.valid = true
	c.call.Add(gtx.Ops)
	return c.dims
}
`
}

// lookupEvent 在 Facts 中查找某个节点的已解析事件。
//
// 实参必须走它：原始 contracts.EventBinding.Args 是**未类型化**的文本，
// Codegen 不得据其生成代码（事件处理输出契约 §5.4.2）。
func lookupEvent(n *contracts.NodeIR, name string, facts *analyzer.Facts) (analyzer.ResolvedEvent, bool) {
	if facts == nil {
		return analyzer.ResolvedEvent{}, false
	}
	nf, ok := facts.Lookup(n)
	if !ok {
		return analyzer.ResolvedEvent{}, false
	}
	for _, ev := range nf.Events() {
		if ev.Name == name {
			return ev, true
		}
	}
	return analyzer.ResolvedEvent{}, false
}

// lookupFor 在 Facts 中取某个节点的 <For> 作用域信息。
func lookupFor(n *contracts.NodeIR, facts *analyzer.Facts) (analyzer.ResolvedFor, bool) {
	if facts == nil {
		return analyzer.ResolvedFor{}, false
	}
	nf, ok := facts.Lookup(n)
	if !ok {
		return analyzer.ResolvedFor{}, false
	}
	return nf.For()
}

// forVarUsed 报告循环体是否引用了某个 <For> 变量（Each 或 Index）。
//
// 用途：只在被引用时生成索引变量——Go 对未使用的 range 变量会报
// "declared and not used"。
func (g *generator) forVarUsed(nodes []*contracts.NodeIR, name string) bool {
	if name == "" {
		return false
	}
	for _, n := range nodes {
		if n == nil {
			continue
		}
		// 属性绑定
		for _, ra := range g.attrFactsOf(n) {
			if pathRootsAt(ra.Path, name) {
				return true
			}
		}
		// 事件实参
		for _, ev := range g.eventFactsOf(n) {
			for _, a := range ev.CallArgs {
				if a.Kind == analyzer.ArgPath && pathRootsAt(a.Path, name) {
					return true
				}
			}
		}
		if g.forVarUsed(n.Children, name) {
			return true
		}
	}
	return false
}

// attrFactsOf 取节点的属性事实（事实缺失时返回 nil）。
func (g *generator) attrFactsOf(n *contracts.NodeIR) []analyzer.ResolvedAttr {
	attrs, _ := propFacts(n, g.facts)
	return attrs
}

// eventFactsOf 取节点的已解析事件（事实缺失时返回 nil）。
func (g *generator) eventFactsOf(n *contracts.NodeIR) []analyzer.ResolvedEvent {
	if g.facts == nil {
		return nil
	}
	nf, ok := g.facts.Lookup(n)
	if !ok {
		return nil
	}
	return nf.Events()
}

// pathRootsAt 报告路径是否以指定循环变量为根。
func pathRootsAt(p *analyzer.ResolvedPath, name string) bool {
	if p == nil {
		return false
	}
	return p.RootKind == analyzer.RootForVar && p.Root == name
}
