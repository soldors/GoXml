package contracts

import (
	"fmt"
	"strings"
)

// ============================================================================
// 源码定位
// ============================================================================

// Position 源码位置，Line 和 Col 均从 1 开始。
type Position struct {
	Line int
	Col  int
}

// SourceSpan 源码范围。End 为闭区间终点，零值表示未知。
type SourceSpan struct {
	Start Position
	End   Position
}

// IsZero 报告是否有可用的起始位置。
func (s SourceSpan) IsZero() bool { return s.Start.Line == 0 && s.Start.Col == 0 }

// String 返回 "line:col" 或 "line:col-line:col" 形式。
func (s SourceSpan) String() string {
	if s.IsZero() {
		return "?"
	}
	if s.End.Line == 0 && s.End.Col == 0 {
		return fmt.Sprintf("%d:%d", s.Start.Line, s.Start.Col)
	}
	return fmt.Sprintf("%d:%d-%d:%d", s.Start.Line, s.Start.Col, s.End.Line, s.End.Col)
}

// ============================================================================
// 诊断
// ============================================================================

// 诊断错误码前缀。完整错误码形如 GXML-BIND-001，见 GXML 语言规范 §8。
const (
	CodePrefixSyntax = "GXML-SYNTAX" // XML 结构、标签、属性名错误
	CodePrefixProp   = "GXML-ATTR"   // 属性值类型或格式错误
	CodePrefixBind   = "GXML-BIND"   // 绑定路径错误
	CodePrefixEvent  = "GXML-EVENT"  // 事件方法或参数错误
	CodePrefixDef    = "GXML-DEF"    // Def/Slot 定义与展开错误
	CodePrefixScope  = "GXML-SCOPE"  // If/For 作用域与变量错误
	CodePrefixRoot   = "GXML-ROOT"   // 根节点与文档结构错误
)

// CompilerError 编译器统一诊断错误。
//
// 格式固定为 file:line:col: CODE: message，满足 GXML 语言规范 §8。
// EndLine/EndCol 为可选结束位置，供编辑器标记源码范围；零值表示未知。
type CompilerError struct {
	File    string
	Line    int
	Col     int
	EndLine int
	EndCol  int
	Code    string
	Message string
}

func (e CompilerError) Error() string {
	loc := fmt.Sprintf("%s:%d:%d", e.File, e.Line, e.Col)
	if e.Code != "" {
		return fmt.Sprintf("%s: %s: %s", loc, e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s", loc, e.Message)
}

// Span 返回该错误的源码范围。
func (e CompilerError) Span() SourceSpan {
	return SourceSpan{
		Start: Position{Line: e.Line, Col: e.Col},
		End:   Position{Line: e.EndLine, Col: e.EndCol},
	}
}

// NewError 构造带错误码的诊断。
func NewError(file string, span SourceSpan, code, format string, args ...any) CompilerError {
	return CompilerError{
		File:    file,
		Line:    span.Start.Line,
		Col:     span.Start.Col,
		EndLine: span.End.Line,
		EndCol:  span.End.Col,
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// ============================================================================
// 强类型属性值
// ============================================================================

// EdgeInsets 对应 CSS 风格边距，单位统一为 dp。
//
// 语法：1 个值（四边）／2 个值（上下、左右）／4 个值（上右下左）。
// 见 GXML 语言规范 §4.3。
type EdgeInsets struct {
	Top    float32
	Right  float32
	Bottom float32
	Left   float32
}

// IsZero 报告四边是否全为 0。
func (e EdgeInsets) IsZero() bool {
	return e.Top == 0 && e.Right == 0 && e.Bottom == 0 && e.Left == 0
}

// NRGBAColor 静态颜色或主题引用。
//
// 语法：#RRGGBB、#RRGGBBAA、$Theme.Field。见 GXML 语言规范 §4.4。
// 主题引用只允许出现在颜色属性中。
type NRGBAColor struct {
	IsThemeRef bool
	ThemeField string
	R, G, B, A uint8
}

// ============================================================================
// 绑定与事件
// ============================================================================

// BindingVal 属性值的两种情况：静态字面量或完整绑定。
//
// 绑定语法为 {Path} 或 {!Path}，不允许与静态文本混合。
// 见 GXML 语言规范 §4.1、§4.2。
type BindingVal struct {
	// IsBinding 为 true 时 Raw/Segments 表示绑定路径，否则表示静态字面量。
	IsBinding bool
	// IsNegated 表示路径带 ! 前缀（仅布尔绑定允许）。
	IsNegated bool
	// Raw 保留原始文本，用于诊断回显。
	Raw string
	// Segments 是按 "." 切分后的路径分段，供 Analyzer 逐段解析类型。
	// 不允许保存整串路径后再在运行时切分。
	Segments []string
}

// Path 返回以 "." 连接的分段路径；静态值返回 Raw。
func (b BindingVal) Path() string {
	if !b.IsBinding || len(b.Segments) == 0 {
		return b.Raw
	}
	return strings.Join(b.Segments, ".")
}

// IsZero 报告该值是否未设置。
func (b BindingVal) IsZero() bool {
	return !b.IsBinding && b.Raw == "" && len(b.Segments) == 0
}

// EventArgKind 事件实参种类。
type EventArgKind uint8

const (
	// ArgLiteral 字符串、数字或布尔字面量。
	ArgLiteral EventArgKind = iota + 1
	// ArgBinding ViewModel 或 For 作用域内的绑定路径。
	ArgBinding
)

// EventArg 单个事件实参。GXML v1 不允许表达式，只允许字面量与绑定路径。
type EventArg struct {
	Kind EventArgKind
	// Literal 在 Kind == ArgLiteral 时有效，保留**原始**字面量文本。
	//
	// 字符串字面量保留单引号（如 `'ok'`），否则无法与数字区分：
	// '42' 是字符串，42 是整数。消费者据此判断字面量类型。
	Literal string
	Binding BindingVal // Kind == ArgBinding 时有效
}

// EventBinding 对应 Click 等事件绑定。
//
// 语义分两层，不可混淆：
//
//  1. 事件契约（组件对外发出什么事件、携带什么数据）：由组件 Schema 的
//     Events 声明，见 EventSchema。
//  2. 处理器调用（事件触发时调用哪个方法、传入哪些字面量或绑定路径）：
//     即本结构体的 Method 与 Args。
//
// 两者独立：即使组件事件不携带参数，处理器仍可读取当前作用域并传参。
//
// 本结构体由 Expander 在完成属性/事件分类后填充；
// 事件方法是否存在、参数签名是否匹配由 Analyzer 校验。
type EventBinding struct {
	// Name 是事件名，如 "Click"。不再带 @ 前缀。
	Name string
	// Method 是 ViewModel 上的方法名。
	Method string
	// Args 是实参列表，可为空。v1 只允许字面量与绑定路径。
	Args []EventArg
	// Span 覆盖整个属性值，供编辑器定位。
	Span SourceSpan
}

// AttrKind 属性的语义类别，由 Expander 分类后确定。
type AttrKind uint8

const (
	// AttrUnclassified 尚未分类：Parser 阶段的所有属性都是这个状态。
	AttrUnclassified AttrKind = iota
	// AttrLayout 全局布局属性，由编译器内置表管理，不进入组件 Schema。
	AttrLayout
	// AttrComponent 组件专有属性，由组件 Schema.Properties 声明。
	AttrComponent
)

// RawAttr Parser 输出的未分类属性。
//
// Parser 只负责语法，不判断该名称是属性还是事件：这项判断需要组件
// Schema 知识，属于 Expander 的职责。Parser 依赖组件白名单会让语法层
// 逐渐渗入类型知识，导致新增控件或第三方组件时产生耦合。
type RawAttr struct {
	Name  string
	Value BindingVal
	Span  SourceSpan
}

// RawAttrList 未分类属性列表。
type RawAttrList []RawAttr

// Lookup 按名称查找。
func (l RawAttrList) Lookup(name string) (RawAttr, bool) {
	for _, a := range l {
		if a.Name == name {
			return a, true
		}
	}
	return RawAttr{}, false
}

// EventSchema 组件事件的契约声明：组件能发出什么事件、携带什么数据。
//
// 注意与 EventBinding 的区别：EventSchema 描述「组件发出什么」，
// EventBinding 描述「父级调用哪个处理方法」。二者不得共用同一份声明。
type EventSchema struct {
	Name string
	// Params 是事件携带的参数类型，用于校验处理器签名。
	Params []EventParam
	Span   SourceSpan
}

// EventParam 事件参数声明。
type EventParam struct {
	Name string
	Type PropType
}

// ============================================================================
// 属性
// ============================================================================

// Attr 单个属性及其源码范围。
//
// 使用切片而非 map，是为了保留声明顺序与逐属性 SourceSpan
// （GXML 语言规范 §10 要求每个属性都能定位）。
type Attr struct {
	Name  string
	Value BindingVal
	Span  SourceSpan
}

// AttrList 属性列表，提供按名查找的辅助方法。
type AttrList []Attr

// Lookup 按名称查找属性，名称区分大小写。
func (l AttrList) Lookup(name string) (Attr, bool) {
	for _, a := range l {
		if a.Name == name {
			return a, true
		}
	}
	return Attr{}, false
}

// Has 报告是否存在指定名称的属性。
func (l AttrList) Has(name string) bool {
	_, ok := l.Lookup(name)
	return ok
}

// ============================================================================
// 布局枚举（见 ADR-0008）
// ============================================================================

// CrossAlign 交叉轴对齐，映射到 Gio 的 layout.Alignment。
type CrossAlign uint8

const (
	// AlignUnset 表示未显式设置，由 Codegen 按标签补默认值：
	// <Row> 默认 AlignCenter，<Column>/<Grid> 默认 AlignStart。
	AlignUnset CrossAlign = iota
	// AlignStart 对应 layout.Start。
	AlignStart
	// AlignCenter 对应 layout.Middle（Gio 的 Center 名为 Middle）。
	AlignCenter
	// AlignEnd 对应 layout.End。
	AlignEnd
	// AlignBaseline 对应 layout.Baseline。
	//
	// 适用范围严格限定（规范 §3.1.3）：
	//   - 仅 <Row Align="Baseline"> 合法；
	//   - <Column>、<Grid> 上使用是编译错误（GXML-ATTR-003）；
	AlignBaseline
)

// String 返回 GXML 中的取值名。
func (a CrossAlign) String() string {
	switch a {
	case AlignStart:
		return "Start"
	case AlignCenter:
		return "Center"
	case AlignEnd:
		return "End"
	case AlignBaseline:
		return "Baseline"
	default:
		return "Unset"
	}
}

// ParseCrossAlign 解析 GXML 的 Align 取值，大小写敏感。
func ParseCrossAlign(s string) (CrossAlign, bool) {
	switch s {
	case "Start":
		return AlignStart, true
	case "Center":
		return AlignCenter, true
	case "End":
		return AlignEnd, true
	case "Baseline":
		return AlignBaseline, true
	default:
		return AlignUnset, false
	}
}

// MainJustify 主轴剩余空间分布，映射到 Gio 的 layout.Spacing。
type MainJustify uint8

const (
	// JustifyUnset 等价于 JustifyStart。
	JustifyUnset MainJustify = iota
	// JustifyStart 剩余空间留在末尾，对应 layout.SpaceEnd。
	JustifyStart
	// JustifyCenter 剩余空间平分到首尾，对应 layout.SpaceSides。
	JustifyCenter
	// JustifyEnd 剩余空间留在开头，对应 layout.SpaceStart。
	JustifyEnd
	// JustifySpaceBetween 子项间均分，首尾不留，对应 layout.SpaceBetween。
	JustifySpaceBetween
	// JustifySpaceAround 子项间均分，首尾各留一半，对应 layout.SpaceAround。
	JustifySpaceAround
	// JustifySpaceEvenly 含首尾完全均分，对应 layout.SpaceEvenly。
	JustifySpaceEvenly
)

// String 返回 GXML 中的取值名。
func (j MainJustify) String() string {
	switch j {
	case JustifyStart:
		return "Start"
	case JustifyCenter:
		return "Center"
	case JustifyEnd:
		return "End"
	case JustifySpaceBetween:
		return "SpaceBetween"
	case JustifySpaceAround:
		return "SpaceAround"
	case JustifySpaceEvenly:
		return "SpaceEvenly"
	default:
		return "Unset"
	}
}

// ParseMainJustify 解析 GXML 的 Justify 取值，大小写敏感。
func ParseMainJustify(s string) (MainJustify, bool) {
	switch s {
	case "Start":
		return JustifyStart, true
	case "Center":
		return JustifyCenter, true
	case "End":
		return JustifyEnd, true
	case "SpaceBetween":
		return JustifySpaceBetween, true
	case "SpaceAround":
		return JustifySpaceAround, true
	case "SpaceEvenly":
		return JustifySpaceEvenly, true
	default:
		return JustifyUnset, false
	}
}

// SizeMode 尺寸模式。
type SizeMode uint8

const (
	// SizeNatural 缺省：由内容决定，不施加额外约束。
	SizeNatural SizeMode = iota
	// SizeFill 占满父容器在对应方向上的可用空间。
	//
	// 这是语言层语义，不是某个具体布局调用的别名（规范 §3.1.5）：
	// 方向相对父容器的角色决定实现——主轴参与弹性分配，交叉轴填充可用约束。
	// 因此 Codegen 不可把它一律翻译为 layout.Flexed(1)。
	SizeFill
	// SizeFixed 固定 dp 尺寸，对应 layout.Constraints.Exact。
	SizeFixed
)

// SizeSpec 单个方向的尺寸规格。
type SizeSpec struct {
	Mode SizeMode
	// Value 仅当 Mode == SizeFixed 时有效，单位为 dp，必须非负。
	Value float32
}

// IsSet 报告是否显式设置了尺寸。
func (s SizeSpec) IsSet() bool { return s.Mode != SizeNatural }

// NaturalSize 返回表示内容自然尺寸的规格。
func NaturalSize() SizeSpec { return SizeSpec{Mode: SizeNatural} }

// FillSize 返回表示占满可用空间的规格。
func FillSize() SizeSpec { return SizeSpec{Mode: SizeFill} }

// FixedSize 返回固定 dp 尺寸；负值视为非法，由调用方校验。
func FixedSize(dp float32) SizeSpec { return SizeSpec{Mode: SizeFixed, Value: dp} }

// GridColumnMode 列宽模式。
type GridColumnMode uint8

const (
	// GridColumnFixed 固定列宽（dp）。
	GridColumnFixed GridColumnMode = iota + 1
	// GridColumnFlex 按权重分配剩余宽度（对应 * 与 N*）。
	GridColumnFlex
)

// GridColumn <Grid> 的单列定义。
type GridColumn struct {
	Mode GridColumnMode
	// Value 在 GridColumnFixed 时是 dp 宽度，在 GridColumnFlex 时是权重（>= 1）。
	Value float32
}

// IsValid 报告列定义是否合法。
func (c GridColumn) IsValid() bool {
	switch c.Mode {
	case GridColumnFixed:
		return c.Value >= 0
	case GridColumnFlex:
		return c.Value > 0
	default:
		return false
	}
}

// ============================================================================
// 节点种类
// ============================================================================

// NodeKind 节点种类。
//
// 禁止只用 Tag 字符串判别节点类型：<Text> 与用户自定义的 <Text> 组件
// 必须能区分，且 If/For/Def 等结构节点没有对应的 Go 渲染标签。
type NodeKind uint8

const (
	// KindUnknown 未初始化或解析失败的占位。
	KindUnknown NodeKind = iota
	// KindGoxml 文档根节点。
	KindGoxml
	// KindDefs <Defs> 容器。
	KindDefs
	// KindDef 编译期组件定义。
	KindDef
	// KindSlot <Slot/> 插槽占位。
	KindSlot
	// KindPage 页面。
	KindPage
	// KindElement 内置布局标签或基础控件，如 Column/Row/Text/Button。
	KindElement
	// KindIf 条件分支结构节点。
	KindIf
	// KindFor 循环结构节点。
	KindFor
)

// String 返回可读的节点种类名，用于诊断。
func (k NodeKind) String() string {
	switch k {
	case KindGoxml:
		return "Goxml"
	case KindDefs:
		return "Defs"
	case KindDef:
		return "Def"
	case KindSlot:
		return "Slot"
	case KindPage:
		return "Page"
	case KindElement:
		return "Element"
	case KindIf:
		return "If"
	case KindFor:
		return "For"
	default:
		return "Unknown"
	}
}

// IsStructural 报告该节点是否只在编译期存在、不产生直接渲染。
func (k NodeKind) IsStructural() bool {
	switch k {
	case KindDefs, KindDef, KindIf, KindFor, KindSlot:
		return true
	default:
		return false
	}
}

// ============================================================================
// 结构与子句（按 Kind 生效）
// ============================================================================

// ForClause <For> 的循环子句。仅 Kind == KindFor 时有效。
//
// 见 GXML 语言规范 §6.2 与 AOT 状态 Diff 语义 §5。
type ForClause struct {
	// Each 是循环变量名，必填。
	Each string
	// In 是可迭代绑定，必填，必须是集合或数组。
	In BindingVal
	// Index 是可选索引变量名。
	Index string
	// Key 是稳定身份绑定；循环体含交互控件时必填。
	// 缺失是 <For> 状态串位的根因，见 BUG-001。
	Key BindingVal
	// HasKey 表示 Key 是否被显式提供（Key 自身零值无法区分）。
	HasKey bool
	// Virtualize 启用虚拟化，缺省 false 保持 v1 基线行为。
	Virtualize bool
	// ItemExtent 是虚拟化的固定行高（dp）。启用 Virtualize 时使用。
	ItemExtent    float32
	HasItemExtent bool
}

// IfClause <If> 的条件子句。仅 Kind == KindIf 时有效。
//
// Then 必填且只有一个；Else 最多一个。见 GXML 语言规范 §6.1。
type IfClause struct {
	// Condition 必须是布尔绑定。
	Condition BindingVal
	// Then 是 <Then> 块的内容。
	Then []*NodeIR
	// Else 是 <Else> 块的内容，可为 nil。
	Else []*NodeIR
}

// PropType Def 属性的声明类型。见 GXML 语言规范 §7.1。
type PropType uint8

const (
	PropUnknown PropType = iota
	PropString
	PropBool
	PropInt
	PropFloat
	PropColor
	PropInsets
)

// String 返回 GXML 中的类型名。
func (p PropType) String() string {
	switch p {
	case PropString:
		return "string"
	case PropBool:
		return "bool"
	case PropInt:
		return "int"
	case PropFloat:
		return "float"
	case PropColor:
		return "color"
	case PropInsets:
		return "insets"
	default:
		return "unknown"
	}
}

// ParsePropType 解析 Props 声明中的类型名，未知类型返回 PropUnknown。
func ParsePropType(s string) PropType {
	switch strings.TrimSpace(s) {
	case "string":
		return PropString
	case "bool":
		return PropBool
	case "int":
		return PropInt
	case "float":
		return PropFloat
	case "color":
		return PropColor
	case "insets":
		return PropInsets
	default:
		return PropUnknown
	}
}

// DefProp <Def> 的单个属性声明。语法 Name:Type[=Default]。
type DefProp struct {
	Name string
	Type PropType
	// Default 是默认值，只能是静态字面量或主题颜色引用；无默认值时 IsZero 为真。
	Default    BindingVal
	HasDefault bool
	Span       SourceSpan
}

// DefClause <Def> 的定义子句。仅 Kind == KindDef 时有效。
type DefClause struct {
	// Name 是组件名，必须唯一且不得覆盖保留名称。
	Name string
	// Props 是声明的属性表，保持声明顺序。
	Props []DefProp
	// Events 是声明的事件契约。自定义组件必须显式声明事件，
	// 不能通过「未在 Props 中出现」推断它是事件。
	Events []EventSchema
	// Body 是 Def 的渲染根节点（规范要求有且只有一个）。
	Body *NodeIR
	// HasSlot 表示体内是否出现 <Slot/>；v1 最多一个默认插槽。
	HasSlot bool
}

// Prop 按名称查找属性声明。
func (d *DefClause) Prop(name string) (DefProp, bool) {
	if d == nil {
		return DefProp{}, false
	}
	for _, p := range d.Props {
		if p.Name == name {
			return p, true
		}
	}
	return DefProp{}, false
}

// Event 按名称查找事件声明。
func (d *DefClause) Event(name string) (EventSchema, bool) {
	if d == nil {
		return EventSchema{}, false
	}
	for _, e := range d.Events {
		if e.Name == name {
			return e, true
		}
	}
	return EventSchema{}, false
}

// ============================================================================
// 节点
// ============================================================================

// NodeIR 语法树节点。
//
// 字段按 NodeKind 分组：通用字段对所有节点有效，子句字段只在其对应
// Kind 下有意义。Codegen 必须先按 Kind 分派，再读取对应子句。
type NodeIR struct {
	// ---- 通用字段 ----

	// Kind 决定该节点如何被解析、分析与生成。
	Kind NodeKind
	// Tag 保留原始标签名（区分大小写）；结构节点也保留，便于诊断。
	Tag string
	// Span 覆盖整个元素（起始标签到结束标签）。
	Span SourceSpan

	// ---- 布局通用属性（KindElement 适用） ----

	// Padding 对应 Padding 属性；缺省为零值。
	Padding EdgeInsets
	// Gap 对应 Gap 属性，是相邻子节点之间的 dp 间距。
	// 直接映射到 Gio 的 Flex.Gap，不需要逐项生成 Inset。
	// 注意：该字段在 v1 早期草案中名为 Spacing，已重命名（ADR-0008 §4）。
	Gap float32
	// HasGap 表示 Gap 是否被显式设置（零值无法区分「未设置」与「设为 0」）。
	HasGap bool
	// Background 是静态颜色或主题引用，nil 表示未设置。
	Background *NRGBAColor

	// ---- 布局对齐与尺寸（KindElement 适用，见 ADR-0008） ----

	// Align 是交叉轴对齐；缺省值取决于 Tag（Row=Center，Column/Grid=Start）。
	// 使用 AlignUnset 表示未显式设置，由 Codegen 按标签补默认值。
	Align CrossAlign
	// Justify 是主轴剩余空间分布，仅对 Column/Row 有效。
	// JustifyUnset 等价于 JustifyStart；由 Codegen 显式写出默认值
	// （不依赖 Gio 零值，见 codegen/layout.go 的 flexFieldsExpr）。
	Justify MainJustify
	// Width 是横向尺寸；缺省为内容自然尺寸。
	Width SizeSpec
	// Height 是纵向尺寸；缺省为内容自然尺寸。
	Height SizeSpec
	// Columns 仅对 <Grid> 有效，是容器声明的列定义。
	Columns []GridColumn

	// ---- 普通属性与事件（KindElement / KindPage 适用） ----

	// RawAttrs 是 Parser 输出的未分类属性，保留声明顺序与源码范围。
	//
	// Parser 不做属性/事件分类（那需要组件 Schema 知识）。
	// Expander 消费 RawAttrs，按「布局属性表 → Schema.Properties →
	// Schema.Events」三类分派，填充 Attrs 与 Events，并清空 RawAttrs。
	RawAttrs RawAttrList
	// Attrs 是 Expander 分类后的组件属性，保留声明顺序与源码范围。
	Attrs AttrList
	// Events 是 Expander 分类后的事件绑定。
	Events []EventBinding
	// Children 是子节点；结构节点使用各自的子句字段而非此字段。
	Children []*NodeIR

	// ---- 子句（按 Kind 生效） ----

	// For 仅在 Kind == KindFor 时非 nil。
	For *ForClause
	// If 仅在 Kind == KindIf 时非 nil。
	If *IfClause
	// Def 仅在 Kind == KindDef 时非 nil。
	Def *DefClause

	// ---- 页面专有（KindPage 适用） ----

	// RoutePath 对应 <Page Route="...">。
	RoutePath string
}

// Attr 按名称查找节点属性（不含布局通用属性）。
func (n *NodeIR) Attr(name string) (Attr, bool) {
	if n == nil {
		return Attr{}, false
	}
	return n.Attrs.Lookup(name)
}

// Event 按事件名（不含 @）查找事件绑定。
func (n *NodeIR) Event(name string) (EventBinding, bool) {
	if n == nil {
		return EventBinding{}, false
	}
	for _, e := range n.Events {
		if e.Name == name {
			return e, true
		}
	}
	return EventBinding{}, false
}

// ============================================================================
// 文档
// ============================================================================

// DocumentIR 单个 .gxml 文件的抽象语法树。
//
// 文档结构固定为：<Goxml Version="1" [ViewModel="..."]> → [<Defs>] → <Page>。
// 见 GXML 语言规范 §2.2。
// ViewModelDecl 是根节点 ViewModel 属性的原始声明。
//
// 它只承载「用户写了什么、写在哪」，不承载「它解析成哪个 Go 类型」。
// 这个分界是刻意的：
//
//   - Parser 不引入 Go 类型系统依赖，只做语法；
//   - 包定位与类型查找由 Analyzer 完成（Analyzer输入输出契约 §4.2）；
//   - Span 让类型解析错误能定位到该属性，而不是笼统地指向文档根。
type ViewModelDecl struct {
	// Value 是属性的原始文本，如 "demo.ViewModel"。
	// 其合法文法由 Analyzer 校验，Parser 只保证它是静态字面量。
	Value string
	// Span 覆盖整个属性（名称与值），用于诊断定位。
	Span SourceSpan
}

// IsZero 报告该声明是否缺省。
//
// 缺省表示纯静态页面：允许存在绑定，但绑定只能是循环变量等非 ViewModel 路径；
// 具体语义由 Analyzer 判定。
func (d ViewModelDecl) IsZero() bool {
	return d.Value == "" && d.Span.IsZero()
}

// String 返回原始文本，便于日志与测试输出。
func (d ViewModelDecl) String() string {
	return d.Value
}

type DocumentIR struct {
	// FilePath 是项目相对路径，统一使用 "/"。
	FilePath string
	// Package 只是生成上下文，不是 GXML 语义字段。
	// Go 包名由目标目录或 CLI 参数确定，不得从 GXML 推导。
	Package string
	// ViewModel 是 <Goxml ViewModel="..."> 的原始声明；纯静态页面为零值。
	//
	// Parser 只保留原始文本与位置，**不解析为 Go 类型**：
	// 包定位与类型查找属于 Analyzer 的职责（Analyzer输入输出契约 §4.2）。
	ViewModel ViewModelDecl
	// Version 对应根节点 Version 属性，v1 必须为 "1"。
	Version string
	// Defs 是 <Defs> 下定义的组件，保持声明顺序。
	Defs []*NodeIR
	// Page 是文档唯一页面。
	Page *NodeIR
	// Assets 是 //go:embed 注入的静态资源相对路径。
	Assets []string
	// Span 覆盖整个文档。
	Span SourceSpan
}
