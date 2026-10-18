package contracts

import "testing"

// ---------------------------------------------------------------------------
// RawAttrList.Lookup：Parser 输出的未分类属性查找
// ---------------------------------------------------------------------------

func TestRawAttrListLookup(t *testing.T) {
	list := RawAttrList{
		{Name: "Click", Value: BindingVal{Raw: "Save"}},
		{Name: "Text", Value: BindingVal{Raw: "确定"}},
	}

	a, ok := list.Lookup("Click")
	if !ok || a.Value.Raw != "Save" {
		t.Fatalf("Lookup(Click) = %+v, %v", a, ok)
	}
	if _, ok := list.Lookup("Missing"); ok {
		t.Error("不存在的名称不应命中")
	}
	// 名称区分大小写。
	if _, ok := list.Lookup("click"); ok {
		t.Error("名称应区分大小写")
	}
}

func TestRawAttrListLookupEmpty(t *testing.T) {
	var list RawAttrList
	if _, ok := list.Lookup("X"); ok {
		t.Error("空列表不应命中")
	}
}

// ---------------------------------------------------------------------------
// DefClause.Prop / Event：自定义组件契约查找
// ---------------------------------------------------------------------------

func newDefClause() *DefClause {
	return &DefClause{
		Name: "Card",
		Props: []DefProp{
			{Name: "Title", Type: PropString},
			{Name: "Accent", Type: PropColor, HasDefault: true},
		},
		Events: []EventSchema{
			{Name: "Click"},
			{Name: "Open", Params: []EventParam{{Name: "id", Type: PropString}}},
		},
	}
}

func TestDefClauseProp(t *testing.T) {
	d := newDefClause()

	p, ok := d.Prop("Title")
	if !ok || p.Type != PropString {
		t.Fatalf("Prop(Title) = %+v, %v", p, ok)
	}
	if _, ok := d.Prop("Missing"); ok {
		t.Error("未声明的 Prop 不应命中")
	}
	// 事件名不应被当作属性。
	if _, ok := d.Prop("Click"); ok {
		t.Error("事件名不应通过 Prop 查到")
	}
}

func TestDefClauseEvent(t *testing.T) {
	d := newDefClause()

	e, ok := d.Event("Open")
	if !ok || len(e.Params) != 1 || e.Params[0].Name != "id" {
		t.Fatalf("Event(Open) = %+v, %v", e, ok)
	}
	if _, ok := d.Event("Missing"); ok {
		t.Error("未声明的事件不应命中")
	}
	// 属性名不应被当作事件。
	if _, ok := d.Event("Title"); ok {
		t.Error("属性名不应通过 Event 查到")
	}
}

func TestDefClauseNilSafety(t *testing.T) {
	var d *DefClause
	if _, ok := d.Prop("X"); ok {
		t.Error("nil DefClause 的 Prop 应返回 false")
	}
	if _, ok := d.Event("X"); ok {
		t.Error("nil DefClause 的 Event 应返回 false")
	}
}

// ---------------------------------------------------------------------------
// EventSchema 与 EventBinding 的语义区分
// ---------------------------------------------------------------------------

func TestEventSchemaVsBindingAreSeparateTypes(t *testing.T) {
	// EventSchema 描述「组件发出什么」；
	// EventBinding 描述「父级调用哪个处理方法」。
	// 二者不得共用同一份声明，因此是不同类型。
	schema := EventSchema{Name: "Open", Params: []EventParam{{Name: "id", Type: PropInt}}}
	binding := EventBinding{Name: "Open", Method: "OpenDetail", Args: []EventArg{
		{Kind: ArgBinding, Binding: BindingVal{IsBinding: true, Segments: []string{"item", "ID"}}},
	}}

	if schema.Name != binding.Name {
		t.Fatal("两者的事件名应一致，但语义不同")
	}
	if len(schema.Params) != 1 || schema.Params[0].Type != PropInt {
		t.Errorf("事件契约参数 = %+v", schema.Params)
	}
	if len(binding.Args) != 1 || binding.Args[0].Kind != ArgBinding {
		t.Errorf("处理器调用参数 = %+v", binding.Args)
	}
}
