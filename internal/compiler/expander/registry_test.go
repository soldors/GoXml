package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// ---------------------------------------------------------------------------
// SchemaRegistry：Analyzer 的契约查询入口
// ---------------------------------------------------------------------------

// lookup 走完整管线后查询注册表。
func lookup(t *testing.T, src, tag string) (*ComponentSchema, bool) {
	t.Helper()
	r := mustOK(t, src)
	if r.Schemas == nil {
		t.Fatal("Result.Schemas 不应为 nil")
	}
	return r.Schemas.Lookup(tag)
}

func TestRegistryIsNeverNil(t *testing.T) {
	// 即使文档为 nil，也应有可用的注册表，避免调用方判空。
	r := Expand("t.gxml", nil)
	if r.Schemas == nil {
		t.Fatal("nil 文档时 Schemas 仍不应为 nil")
	}
	// 内置契约是静态的，与文档无关，因此仍可查到。
	if _, ok := r.Schemas.Lookup("Text"); !ok {
		t.Error("内置控件应始终可用")
	}
	// 但没有文档就没有自定义组件。
	if _, ok := r.Schemas.Lookup("Card"); ok {
		t.Error("nil 文档时不应有自定义组件")
	}
}

func TestRegistryHasBuiltinControls(t *testing.T) {
	// 内置控件必须合并进注册表，否则 Analyzer 无法校验
	// Input.ValueChanged(value:string) 这类内置事件签名。
	src := wrap(`<Button Text="a"/>`)

	for _, tag := range []string{"Text", "Button", "Input", "Image"} {
		s, ok := lookup(t, src, tag)
		if !ok || s == nil {
			t.Errorf("内置控件 <%s> 应可查到", tag)
			continue
		}
		if s.Tag != tag {
			t.Errorf("<%s>.Tag = %q", tag, s.Tag)
		}
	}
}

func TestRegistryBuiltinEventPayload(t *testing.T) {
	// Analyzer 依赖事件声明参数校验方法签名（契约 §8.4）。
	src := wrap(`<Button Text="a"/>`)

	s, ok := lookup(t, src, "Input")
	if !ok {
		t.Fatal("Input 应可查到")
	}
	ev, ok := s.Event("ValueChanged")
	if !ok {
		t.Fatal("Input.ValueChanged 应存在")
	}
	if len(ev.Params) != 1 {
		t.Fatalf("ValueChanged 参数数 = %d, 期望 1", len(ev.Params))
	}
	if ev.Params[0].Name != "value" || ev.Params[0].Type != contracts.PropString {
		t.Errorf("载荷参数 = %+v, 期望 value:string", ev.Params[0])
	}
}

func TestRegistryLayoutTagsReturnNil(t *testing.T) {
	// 布局标签只有布局属性，且已被 Parser 校验并写入 NodeIR 专用字段，
	// 不应通过组件 Schema 查询。
	src := wrap(`<Column><Text Value="a"/></Column>`)
	for _, tag := range []string{"Row", "Column", "Grid", "Page"} {
		if s, ok := lookup(t, src, tag); ok {
			t.Errorf("布局标签 <%s> 应返回 nil，实际 %+v", tag, s)
		}
	}
}

func TestRegistryHasCustomComponents(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs>
    <Def Name="Card" Props="Title:string" Events="Open(id:string)">
      <Text Value="{Title}"/>
    </Def>
  </Defs>
  <Page><Card Title="x"/></Page>
</Goxml>`

	s, ok := lookup(t, src, "Card")
	if !ok || s == nil {
		t.Fatal("自定义组件 Card 应可查到")
	}
	if len(s.Properties) != 1 || s.Properties[0].Name != "Title" {
		t.Errorf("Properties = %+v", s.Properties)
	}
	if len(s.Events) != 1 || s.Events[0].Name != "Open" {
		t.Fatalf("Events = %+v", s.Events)
	}
	// 自定义事件也必须提供载荷参数，供 Analyzer 拼接方法形参。
	if len(s.Events[0].Params) != 1 || s.Events[0].Params[0].Type != contracts.PropString {
		t.Errorf("Open 载荷 = %+v", s.Events[0].Params)
	}
}

func TestRegistryUnknownTag(t *testing.T) {
	if _, ok := lookup(t, wrap(`<Text Value="a"/>`), "NoSuchComponent"); ok {
		t.Error("未知标签不应命中")
	}
	if _, ok := lookup(t, wrap(`<Text Value="a"/>`), ""); ok {
		t.Error("空标签名不应命中")
	}
}

func TestBuiltinNamesCannotBeOverridden(t *testing.T) {
	// 内置控件名是保留名，Parser 直接拒绝同名 <Def>（GXML-DEF-002）。
	// 因此注册表里「内置优先」这条规则不可能产生歧义。
	src := `<Goxml Version="1">
  <Defs><Def Name="Text" Props="V:string"><Text Value="{V}"/></Def></Defs>
  <Page><Text Value="a"/></Page>
</Goxml>`
	_, ds := parser.Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("保留名冲突应在 Parser 阶段被拒绝")
	}
	if ds[0].Code != "GXML-DEF-002" {
		t.Errorf("诊断码 = %s, 期望 GXML-DEF-002", ds[0].Code)
	}
}

func TestRegistrySharedAcrossQueries(t *testing.T) {
	// 同一 Result 的多次查询应得到相同指针（不重新构建）。
	src := wrap(`<Button Text="a"/>`)
	r := mustOK(t, src)
	a, _ := r.Schemas.Lookup("Button")
	b, _ := r.Schemas.Lookup("Button")
	if a != b {
		t.Error("同一注册表应返回同一契约实例")
	}
}
