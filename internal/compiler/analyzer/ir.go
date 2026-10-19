// Package analyzer 负责校验 GXML 展开后的语义：绑定路径、事件方法签名、
// `<For>` 可迭代性等需要 Go 类型信息才能判断的规则。
//
// 本文件**只定义语义 IR 的类型与不变量**，不含任何校验逻辑。
// 校验实现随 TASK-ANALYZER-001 推进，届时本文件的类型不得先行变动，
// 否则实现细节会反过来绑架契约。
//
// 契约正文见 docs/03-架构/Analyzer输入输出契约.md §6。
package analyzer

import (
	"errors"
	"fmt"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// 分析状态
// ============================================================================

// Status 是一次分析的总体结论。
type Status uint8

const (
	// StatusOK 全部校验通过，可以交给 Codegen。
	StatusOK Status = iota
	// StatusPartial 存在可恢复的局部错误；部分节点未校验。
	//
	// 该状态的结构 IR 仍完整可用（供 IDE 展示），但**不可**用于生成代码。
	StatusPartial
	// StatusBlocked 关键输入不可用（ViewModel 或 go/packages），未做有效分析。
	StatusBlocked
)

// String 返回可读名称，便于日志与测试失败信息。
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusPartial:
		return "Partial"
	case StatusBlocked:
		return "Blocked"
	default:
		return fmt.Sprintf("Status(%d)", uint8(s))
	}
}

// ============================================================================
// 分析结果
// ============================================================================

// Result 是一次分析的完整输出。
//
// 所有权约定：
//   - Doc 是 Expander 产出的结构 IR，Analyzer **不修改**它。
//     调用方同样不得修改——它可能被 IDE 与诊断共享。
//   - Facts 由 Analyzer 独占构建；构造 VerifiedDoc 后**冻结**，不得再改。
type Result struct {
	// Doc 是原样保留的结构 IR，未做任何修改。
	Doc *contracts.DocumentIR
	// Facts 是语义事实旁表。仅当 Status == StatusOK 时完整。
	Facts *Facts
	// vm 是解析出的 ViewModel 类型引用。缺省时为 IsZero。
	vm ViewModelRef
	// Status 是总体结论。
	Status Status
	// Diags 使用项目既有诊断类型，不另造一套。
	Diags []contracts.CompilerError
}

// Verified 返回进入 Codegen 的凭证。
//
// 语义（契约 §6.10）：只有**同时**满足以下条件才发放凭证——
//
//	Status == StatusOK
//	len(Diags) == 0
//	Doc != nil
//	Facts != nil
//
// 不能仅凭 Status 字段就发放：状态与诊断不一致本身就是实现缺陷，
// 此时宁可拒绝，也不要把可疑结果送进 Codegen。
//
// 副作用：成功时冻结 Facts，之后任何构建尝试都会 panic。
func (r *Result) Verified() (*VerifiedDoc, error) {
	if r == nil {
		return nil, errors.New("analyzer: nil Result")
	}
	if r.Status != StatusOK {
		return nil, fmt.Errorf("analyzer: result is %s, only %s may enter codegen", r.Status, StatusOK)
	}
	if len(r.Diags) > 0 {
		return nil, fmt.Errorf("analyzer: status is OK but %d diagnostic(s) present", len(r.Diags))
	}
	if r.Doc == nil {
		return nil, errors.New("analyzer: status is OK but document is nil")
	}
	if r.Facts == nil {
		return nil, errors.New("analyzer: status is OK but facts are nil")
	}
	r.Facts.freeze()
	return &VerifiedDoc{doc: r.Doc, facts: r.Facts, vm: r.vm}, nil
}

// VerifiedDoc 是「已通过全部校验」的凭证。
//
// 防伪：字段全部未导出，且只能由 (*Result).Verified 构造，
// 因此包外无法凭空创建。Codegen 的入口签名接收本类型，
// 从类型上排除「把未验证的 IR 传进去」这条路径。
//
// 这不等于不可变——Go 只能阻止包外构造，不能阻止持有者修改内部指针
// 指向的对象。因此另有所有权约定：
//   - 调用方不得修改 Document() 返回的文档；
//   - 不得通过 Facts() 返回的只读访问器改写内部状态。
type VerifiedDoc struct {
	doc   *contracts.DocumentIR
	facts *Facts
	// vm 是解析出的 ViewModel 类型引用（ViewModelRef 形式，不含 go/types）。
	// 缺省 ViewModel 时为 IsZero。
	vm ViewModelRef
}

// ViewModelRef 是 ViewModel 的类型引用。
//
// 刻意**不含** *types.Named：types 属于 Analyzer 内部，不得泄漏给 Codegen。
type ViewModelRef struct {
	// PkgDir 是模块根相对目录，如 "internal/models"。同包（即 .gxml 所在包）时为空串。
	PkgDir string
	// Name 是类型名。
	Name string
}

// IsZero 报告是否未设置（纯静态页面）。
func (r ViewModelRef) IsZero() bool {
	return r.PkgDir == "" && r.Name == ""
}

// ViewModel 返回 ViewModel 类型引用。缺省时返回 false。
func (v *VerifiedDoc) ViewModel() (ViewModelRef, bool) {
	if v == nil {
		return ViewModelRef{}, false
	}
	if v.vm.IsZero() {
		return ViewModelRef{}, false
	}
	return v.vm, true
}

// vmRef 把 *ViewModelType 转换为不含 go/types 的引用。
//
// 为什么需要转换：Codegen 需要知道 vm 参数的类型名，但 types.Named 属于
// Analyzer 内部，不得泄漏。这里只保留 PkgDir 与 Name（来自已解析的 TypeExpr）。
func vmRef(vm *ViewModelType) ViewModelRef {
	if vm == nil {
		return ViewModelRef{}
	}
	return ViewModelRef{PkgDir: vm.Expr.PkgDir, Name: vm.Expr.Name}
}

// Document 返回已验证的结构 IR。
//
// 调用方**只读**：它是 Expander 的产物，被诊断、IDE 与 Codegen 共享，
// 任何修改都会让其他消费者看到不一致的树。
func (v *VerifiedDoc) Document() *contracts.DocumentIR {
	return v.doc
}

// Facts 返回已验证的语义事实。
func (v *VerifiedDoc) Facts() *Facts {
	return v.facts
}

// ============================================================================
// 语义事实旁表
// ============================================================================

// Facts 是按节点索引的语义事实。
//
// 它是**旁表**而非节点字段：Analyzer 不修改结构 IR，
// 失败时结构 IR 仍完整，IDE 可展示未校验的原始结构。
//
// 只读约定：构造 VerifiedDoc 后冻结，此后 Set 会 panic。
type Facts struct {
	byNode map[*contracts.NodeIR]*NodeFacts
	frozen bool
}

// newFacts 创建空旁表。仅供 Analyzer 内部使用。
func newFacts() *Facts {
	return &Facts{byNode: make(map[*contracts.NodeIR]*NodeFacts)}
}

// Lookup 返回某节点的事实；未记录（未校验或校验失败）时返回 false。
//
// 返回的 *NodeFacts 只提供只读访问器，见其注释。
func (f *Facts) Lookup(n *contracts.NodeIR) (*NodeFacts, bool) {
	if f == nil || n == nil {
		return nil, false
	}
	nf, ok := f.byNode[n]
	return nf, ok
}

// Len 返回已记录事实的节点数，供测试与诊断使用。
func (f *Facts) Len() int {
	if f == nil {
		return 0
	}
	return len(f.byNode)
}

// set 记录一个节点的事实。仅供 Analyzer 内部使用。
//
// 冻结后调用会 panic：这是内部不变式违反（Analyzer 在发放凭证后继续写），
// 不是用户错误，因此不做静默忽略。
func (f *Facts) set(n *contracts.NodeIR, nf *NodeFacts) {
	if f.frozen {
		panic("analyzer: Facts modified after Verified()")
	}
	if f.byNode == nil {
		f.byNode = make(map[*contracts.NodeIR]*NodeFacts)
	}
	f.byNode[n] = nf
}

// freeze 之后 Set 拒绝写入。
func (f *Facts) freeze() {
	if f == nil {
		return
	}
	f.frozen = true
}

// Frozen 报告旁表是否已冻结。
func (f *Facts) Frozen() bool {
	if f == nil {
		return false
	}
	return f.frozen
}

// NodeFacts 是单个节点的语义事实。
//
// 字段不导出，只提供返回**副本**的访问器：调用方修改返回的切片
// 不会影响内部状态。
type NodeFacts struct {
	attrs  []ResolvedAttr
	events []ResolvedEvent
	forCl  *ResolvedFor
}

// Attrs 返回已解析的属性绑定副本。
func (n *NodeFacts) Attrs() []ResolvedAttr {
	if n == nil {
		return nil
	}
	return append([]ResolvedAttr(nil), n.attrs...)
}

// Events 返回已验证的事件绑定副本。
func (n *NodeFacts) Events() []ResolvedEvent {
	if n == nil {
		return nil
	}
	return append([]ResolvedEvent(nil), n.events...)
}

// For 返回 `<For>` 作用域信息。非循环节点返回 (零值, false)。
func (n *NodeFacts) For() (ResolvedFor, bool) {
	if n == nil || n.forCl == nil {
		return ResolvedFor{}, false
	}
	return *n.forCl, true
}

// ============================================================================
// 属性绑定
// ============================================================================

// ResolvedAttr 是一条已通过校验的属性绑定。
//
// 只有校验通过的属性才会出现在这里；失败的属性只有诊断，没有事实——
// 避免「有事实但已被判定失败」的矛盾状态。
//
// 静态字面量属性（如 `Text="保存"`）不在此列：它们无需类型解析。
type ResolvedAttr struct {
	// Name 是属性名。
	Name string
	// Target 是该属性声明的目标类型（来自 ComponentSchema）。
	Target contracts.PropType
	// Path 是解析后的绑定路径。
	Path *ResolvedPath
	// Span 覆盖属性值，用于诊断定位。
	Span contracts.SourceSpan
}

// ============================================================================
// 绑定路径
// ============================================================================

// RootKind 标识绑定路径的起点类型。
type RootKind uint8

const (
	// RootViewModel 起点是 ViewModel 根对象。
	RootViewModel RootKind = iota + 1
	// RootForVar 起点是 `<For>` 引入的循环变量。
	RootForVar
)

// String 返回可读名称。
func (k RootKind) String() string {
	switch k {
	case RootViewModel:
		return "ViewModel"
	case RootForVar:
		return "ForVar"
	default:
		return fmt.Sprintf("RootKind(%d)", uint8(k))
	}
}

// ResolvedPath 是一条已通过类型检查的点分字段路径。
//
// 只覆盖已冻结的语法：`{Path}` 与 `{!Path}`，路径由 `.` 分隔的字段名组成。
// **不含**索引访问、Map 取值、方法调用或任意表达式——这些语法尚未冻结，
// 对应 IR 待语法扩展时再设计。
type ResolvedPath struct {
	// Root 是起点名称：RootViewModel 时为空串；RootForVar 时为变量名。
	Root string
	// RootKind 标识起点类型，决定 Root 的解释方式。
	RootKind RootKind
	// Steps 按访问顺序记录字段。
	Steps []PathStep
	// Type 是整条路径解析后的类型。
	Type TypeInfo
	// Negated 对应 `{!Path}`。
	//
	// 仅当 Type.Kind == ValueBool 时允许为 true（契约 §8.3）；
	// 违反该约束的绑定不会产出 ResolvedPath，只会产出诊断。
	Negated bool
}

// PathStep 是路径上的一步字段访问。
type PathStep struct {
	// Name 是字段名。
	Name string
	// Type 是访问该字段后的类型。
	Type TypeInfo
}

// ============================================================================
// 类型信息
// ============================================================================

// ValueKind 是类型的**底层**类别（剥离命名类型的外壳）。
//
// 例如 `type Status string` 的 Kind 为 ValueString、Named 为 true。
type ValueKind uint8

const (
	ValueUnknown ValueKind = iota
	ValueString
	ValueBool
	// ValueInt 有符号整数：int、int8~int64。
	ValueInt
	// ValueUint 无符号整数：uint、uint8~uint64、uintptr。
	//
	// 与 ValueInt 分开是必要的：`int` 属性只接受有符号整数
	// （契约 §11.2）——无符号需要范围检查，属于值转换而非类型兼容。
	ValueUint
	// ValueFloat float32 / float64。
	ValueFloat
	ValueStruct
	ValueSlice
	ValueMap
	ValuePointer
	ValueInterface
	ValueOther
)

// String 返回可读名称，便于测试失败信息与诊断。
func (k ValueKind) String() string {
	switch k {
	case ValueString:
		return "string"
	case ValueBool:
		return "bool"
	case ValueInt:
		return "int"
	case ValueUint:
		return "uint"
	case ValueFloat:
		return "float"
	case ValueStruct:
		return "struct"
	case ValueSlice:
		return "slice"
	case ValueMap:
		return "map"
	case ValuePointer:
		return "pointer"
	case ValueInterface:
		return "interface"
	case ValueOther:
		return "other"
	default:
		return "unknown"
	}
}

// TypeInfo 描述一个已通过类型检查的类型。
//
// 表示方式约束（契约 §6.7）：contracts 不依赖 go/types，但 Codegen
// 也不能拿到字符串再自己解析。因此语义由**结构化字段**承载。
type TypeInfo struct {
	// Display 是可打印形式（如 "demo.User"）。
	//
	// **仅用于诊断与日志。** 消费者不得解析它来推断语义——那等于让
	// Codegen 重做类型检查。判断类型请用 Kind / Named / Stringer。
	Display string
	// Kind 是底层类别。
	Kind ValueKind
	// Named 表示是否为命名类型（有包限定）。
	Named bool
	// Stringer 表示是否实现 fmt.Stringer。
	Stringer bool
}

// ============================================================================
// 事件绑定
// ============================================================================

// ParamOrigin 标识一个方法形参的数据来源。
type ParamOrigin uint8

const (
	// FromPayload 来自组件 Schema 的事件声明（契约 §8.4 的「事件载荷」）。
	FromPayload ParamOrigin = iota + 1
	// FromCallArg 来自 XML 中显式写出的调用点实参。
	FromCallArg
)

// String 返回可读名称。
func (o ParamOrigin) String() string {
	switch o {
	case FromPayload:
		return "payload"
	case FromCallArg:
		return "callarg"
	default:
		return fmt.Sprintf("ParamOrigin(%d)", uint8(o))
	}
}

// ResolvedParam 是一个已解析的参数（形参或实参）。
type ResolvedParam struct {
	// Name 是参数名（载荷来自 Schema 声明；实参可能为空）。
	Name string
	// Type 是参数类型。
	Type TypeInfo
	// Span 指向该参数的声明位置。
	//
	// 载荷参数只能指向事件声明整体（contracts.EventParam 没有独立 Span），
	// 不得假装定位到某个独立参数（契约 §8.4.2 R4）。
	Span contracts.SourceSpan
}

// ResolvedMethod 是目标 Go 方法的真实签名。
type ResolvedMethod struct {
	// Name 是方法名。
	Name string
	// Params 是形参列表，按声明顺序。
	Params []ResolvedParam
	// HasReturn 记录方法是否有返回值。
	//
	// 契约 §8.2（Q4）规定事件方法**不得**有返回值；此事实用于产出诊断。
	HasReturn bool
}

// ResolvedEvent 是一条已验证的事件绑定。
//
// 三类信息必须**分开保留**（契约 §6.8）：只存拼接后的参数列表，
// 将无法区分某个参数来自事件载荷还是 XML 实参，也难以生成准确诊断。
type ResolvedEvent struct {
	// Name 是事件名，如 "ValueChanged"。
	Name string
	// Payload 来自 ComponentSchema.Events[i].Params。
	Payload []ResolvedParam
	// CallArgs 来自 XML 的 EventBinding.Args。
	CallArgs []ResolvedParam
	// Method 来自 go/types 的真实签名。
	Method ResolvedMethod
	// Mapping 与 Method.Params 一一对应，标注每个形参的来源。
	//
	// 长度必然等于 len(Method.Params)——契约 §8.4 的 R2 已校验数量。
	// 若不等，是 Analyzer 实现缺陷，不是用户错误。
	Mapping []ParamOrigin
}

// ============================================================================
// <For> 作用域
// ============================================================================

// ResolvedFor 是 `<For>` 引入的作用域信息。
type ResolvedFor struct {
	// Each 是循环变量名（如 "item"）。
	Each string
	// Elem 是元素类型；可迭代性已校验。
	Elem TypeInfo
	// Index 是索引变量名，可为空。
	Index string
	// IndexType 仅在 Index 非空时有效。
	IndexType TypeInfo
	// Key 是已解析的 Key 路径，可为 nil（无状态循环可不提供）。
	Key *ResolvedPath
}
