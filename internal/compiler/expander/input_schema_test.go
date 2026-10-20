package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// D3：Input 的 Schema 扩展（TASK-INPUT-001 D3）
//
// 契约依据：《Input 输出契约》§2（属性集）、§4（事件）、§2.3（Submit 只作事件）。
// ============================================================================

// inputSchema 取 Input 的内置 Schema。
func inputSchema(t *testing.T) *ComponentSchema {
	t.Helper()
	s, ok := builtinSchemas["Input"]
	if !ok {
		t.Fatal("Input 应有内置 Schema")
	}
	return s
}

// TestInputSchemaProperties 七个属性齐全且类型正确。
func TestInputSchemaProperties(t *testing.T) {
	s := inputSchema(t)
	want := map[string]contracts.PropType{
		"Value":      contracts.PropString,
		"Hint":       contracts.PropString,
		"ReadOnly":   contracts.PropBool,
		"SingleLine": contracts.PropBool,
		"MaxLen":     contracts.PropInt,
		"Filter":     contracts.PropString,
		"Enabled":    contracts.PropBool,
	}
	for name, typ := range want {
		p, ok := s.Prop(name)
		if !ok {
			t.Errorf("Input 应声明属性 %q", name)
			continue
		}
		if p.Type != typ {
			t.Errorf("属性 %q 类型 = %v, 期望 %v", name, p.Type, typ)
		}
	}
	if len(s.Properties) != len(want) {
		t.Errorf("属性数 = %d, 期望 %d（多声明说明与契约不一致）", len(s.Properties), len(want))
	}
}

// TestInputSchemaNoSubmitProperty Submit 不得作为属性存在（契约 §2.3）。
//
// 这是同名冲突的回归防线：若有人误加 Submit 属性，
// Expander 会先命中 Prop，导致 Submit 事件永远不可达。
func TestInputSchemaNoSubmitProperty(t *testing.T) {
	s := inputSchema(t)
	if _, ok := s.Prop("Submit"); ok {
		t.Fatal("Input 不得声明 Submit 属性——它只作事件（契约 §2.3）")
	}
	if _, ok := s.Event("Submit"); !ok {
		t.Fatal("Input 应声明 Submit 事件")
	}
}

// TestInputSchemaEvents 两个事件及其载荷。
func TestInputSchemaEvents(t *testing.T) {
	s := inputSchema(t)

	vc, ok := s.Event("ValueChanged")
	if !ok {
		t.Fatal("应声明 ValueChanged 事件")
	}
	if len(vc.Params) != 1 || vc.Params[0].Name != "value" || vc.Params[0].Type != contracts.PropString {
		t.Errorf("ValueChanged 载荷 = %+v, 期望 value:string", vc.Params)
	}

	sub, ok := s.Event("Submit")
	if !ok {
		t.Fatal("应声明 Submit 事件")
	}
	if len(sub.Params) != 1 || sub.Params[0].Name != "text" || sub.Params[0].Type != contracts.PropString {
		t.Errorf("Submit 载荷 = %+v, 期望 text:string", sub.Params)
	}
}

// TestInputSchemaUnknownAttrStillRejected 扩属性后，未知属性仍被拒绝。
func TestInputSchemaUnknownAttrStillRejected(t *testing.T) {
	// 用一个未声明的名字，确认分类器第 4 层仍然生效。
	if _, ok := inputSchema(t).Prop("Placeholder"); ok {
		t.Error("Placeholder 不应是 Input 属性（契约用 Hint）")
	}
}

// TestSubmitEventParsesAsEvent Submit="M" 必须走事件路径，而不是被当作属性。
//
// 端到端回归：这条测试锁定「同名冲突已消除」——
// 若 Submit 同时存在于 Properties，本用例会因 bool 类型校验失败而报错。
func TestSubmitEventParsesAsEvent(t *testing.T) {
	r := mustOK(t, wrap(`<Input Value="{Title}" Submit="OnSubmit"/>`))

	input := r.Doc.Page.Children[0]
	if len(input.Events) != 1 {
		t.Fatalf("应解析出 1 个事件, 实际 %d: %+v", len(input.Events), input.Events)
	}
	ev := input.Events[0]
	if ev.Name != "Submit" {
		t.Errorf("事件名 = %q, 期望 Submit", ev.Name)
	}
	if ev.Method != "OnSubmit" {
		t.Errorf("方法名 = %q", ev.Method)
	}
	// 不得同时出现在属性列表里。
	for _, a := range input.Attrs {
		if a.Name == "Submit" {
			t.Error("Submit 不得被归类为属性")
		}
	}
}

// TestInputAllPropsParse 七个属性都能被分类为属性（不是未知名称）。
func TestInputAllPropsParse(t *testing.T) {
	body := `<Input Value="{Title}" Hint="请输入" ReadOnly="true" SingleLine="true" MaxLen="20" Filter="abc"/>`
	r := mustOK(t, wrap(body))

	input := r.Doc.Page.Children[0]
	got := map[string]bool{}
	for _, a := range input.Attrs {
		got[a.Name] = true
	}
	for _, name := range []string{"Value", "Hint", "ReadOnly", "SingleLine", "MaxLen", "Filter"} {
		if !got[name] {
			t.Errorf("属性 %q 未被分类到 Attrs", name)
		}
	}
}
