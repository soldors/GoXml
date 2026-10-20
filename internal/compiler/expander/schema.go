// Package expander 负责 GXML 的语义分类与组件展开。
//
// 阶段边界（不可逾越）：
//
//	Parser   —— 只做语法：解析 XML、收集属性名/值/源码位置。
//	Expander —— 本包：读取组件 Schema、分类属性与事件、展开 <Def>/<Slot>。
//	            不做 Go 类型加载，不做方法签名校验。
//	Analyzer —— 校验绑定路径、事件处理方法与参数签名（需 go/packages）。
//	Codegen  —— 消费已完成分类的 IR 生成代码，不重新解释原始属性。
//
// 分类规则（三层，无歧义）：
//
//  1. 全局布局属性表   → 属性（不算未知，即使不适用于当前元素）
//  2. Schema.Properties → 属性
//  3. Schema.Events     → 事件
//  4. 都不匹配          → GXML-ATTR-004 未知名称
//
// 第 4 条绝不退化为事件：否则 Clik="Save" 这类拼写错误会被静默接受。
//
// 分类与合法性是两项独立检查：Columns 是全局已知的布局属性，
// 出现在 <Button> 上应报「不适用于该元素」，而不是「未知属性」。
package expander

import (
	"github.com/goxml/goxml/contracts"
)

// LayoutAttr 描述一个全局布局属性及其适用范围。
type LayoutAttr struct {
	Name string
	// AppliesTo 为空表示适用于所有元素；否则只适用于列出的标签。
	AppliesTo []string
}

// layoutAttrs 是全局布局属性表。
//
// 这些属性对所有元素通用，因此不放进任何组件的 Schema：
// 否则每个控件与每个 <Def> 都要重复声明，且新增布局属性需改所有 Schema。
var layoutAttrs = []LayoutAttr{
	{Name: "Padding"},
	{Name: "Background"},
	{Name: "Width"},
	{Name: "Height"},
	{Name: "Gap", AppliesTo: []string{"Column", "Row", "Grid"}},
	{Name: "Align", AppliesTo: []string{"Column", "Row", "Grid"}},
	{Name: "Justify", AppliesTo: []string{"Column", "Row"}},
	{Name: "Columns", AppliesTo: []string{"Grid"}},
}

// layoutAttr 按名称查找布局属性。
func layoutAttr(name string) (LayoutAttr, bool) {
	for _, a := range layoutAttrs {
		if a.Name == name {
			return a, true
		}
	}
	return LayoutAttr{}, false
}

// appliesTo 报告该布局属性是否适用于指定标签。
func (a LayoutAttr) appliesTo(tag string) bool {
	if len(a.AppliesTo) == 0 {
		return true
	}
	for _, t := range a.AppliesTo {
		if t == tag {
			return true
		}
	}
	return false
}

// PropertySchema 组件属性声明。
type PropertySchema struct {
	Name string
	// Type 是该属性的值类型。
	//
	// 必须显式声明：Analyzer 的 BIND-004 依赖它判定类型兼容
	// （契约 §11）。留空会得到 PropUnknown，该属性的绑定将无法被
	// 验证——这是 Schema 完整性缺陷，不是用户错误。
	Type contracts.PropType
	// Required 表示该属性必须出现。
	Required bool
}

// ComponentSchema 组件的完整契约：专有属性 + 事件。
//
// 注意：布局属性不在此处，见 layoutAttrs。
type ComponentSchema struct {
	// Tag 是标签名。
	Tag string
	// Properties 是组件专有属性。
	Properties []PropertySchema
	// Events 是组件可发出的事件契约。
	//
	// 自定义组件必须显式声明事件，不能通过「未在 Properties 中出现」推断，
	// 否则拼写错误会被当成未知事件而难以定位。
	Events []contracts.EventSchema
}

// Prop 按名称查找属性声明。
func (s *ComponentSchema) Prop(name string) (PropertySchema, bool) {
	for _, p := range s.Properties {
		if p.Name == name {
			return p, true
		}
	}
	return PropertySchema{}, false
}

// Event 按名称查找事件声明。
func (s *ComponentSchema) Event(name string) (contracts.EventSchema, bool) {
	for _, e := range s.Events {
		if e.Name == name {
			return e, true
		}
	}
	return contracts.EventSchema{}, false
}

// builtinSchemas 是内置控件的 Schema。
//
// 只声明组件专有属性与事件；布局属性（Width/Height/Padding 等）不在此。
// 新增内置属性或事件不得改变既有名称的语义（兼容性约束）。
var builtinSchemas = map[string]*ComponentSchema{
	"Text": {
		Tag: "Text",
		Properties: []PropertySchema{
			// 规范 §3.2：Text 的 Value 为必选属性。
			{Name: "Value", Type: contracts.PropString, Required: true},
			{Name: "Color", Type: contracts.PropColor},
		},
	},
	"Button": {
		Tag: "Button",
		Properties: []PropertySchema{
			{Name: "Text", Type: contracts.PropString},
			{Name: "Enabled", Type: contracts.PropBool},
		},
		Events: []contracts.EventSchema{
			{Name: "Click"},
		},
	},
	"Input": {
		Tag: "Input",
		// 属性集见《Input 输出契约》§2 与语言规范 §3.2.1。
		//
		// 注意：**没有 `Submit` 属性**——它只作为事件存在（契约 §2.3：
		// Expander 先查 Prop 再查 Event，同名会让事件永远不可达）。
		Properties: []PropertySchema{
			{Name: "Value", Type: contracts.PropString},
			{Name: "Hint", Type: contracts.PropString},
			{Name: "ReadOnly", Type: contracts.PropBool},
			{Name: "SingleLine", Type: contracts.PropBool},
			{Name: "MaxLen", Type: contracts.PropInt},
			{Name: "Filter", Type: contracts.PropString},
			{Name: "Enabled", Type: contracts.PropBool},
		},
		// Input 是受控或非受控控件，回写由事件完成；v1 不引入双向绑定。
		Events: []contracts.EventSchema{
			{Name: "ValueChanged", Params: []contracts.EventParam{{Name: "value", Type: contracts.PropString}}},
			// Submit：回车提交（契约 §4.3）。声明即启用 Editor.Submit。
			{Name: "Submit", Params: []contracts.EventParam{{Name: "text", Type: contracts.PropString}}},
		},
	},
	"Image": {
		Tag: "Image",
		Properties: []PropertySchema{
			{Name: "Src", Type: contracts.PropString},
		},
	},
}

// builtinSchema 返回内置控件的 Schema。
func builtinSchema(tag string) (*ComponentSchema, bool) {
	s, ok := builtinSchemas[tag]
	return s, ok
}
