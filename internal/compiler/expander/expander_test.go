package expander

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// run 走完整的 Parser → Expander 管线。
func run(t *testing.T, src string) Result {
	t.Helper()
	doc, pdiags := parser.Parse("test.gxml", []byte(src))
	if len(pdiags) > 0 {
		t.Fatalf("Parser 诊断:\n%s", join(pdiags))
	}
	return Expand("test.gxml", doc)
}

// mustOK 断言展开无诊断。
func mustOK(t *testing.T, src string) Result {
	t.Helper()
	r := run(t, src)
	if len(r.Diags) > 0 {
		t.Fatalf("意外诊断:\n%s", join(r.Diags))
	}
	return r
}

// mustCode 断言产生指定诊断码。
func mustCode(t *testing.T, src, wantCode string) contracts.CompilerError {
	t.Helper()
	r := run(t, src)
	for _, d := range r.Diags {
		if d.Code == wantCode {
			return d
		}
	}
	t.Fatalf("期望诊断 %s，实际:\n%s", wantCode, join(r.Diags))
	return contracts.CompilerError{}
}

func join(ds []contracts.CompilerError) string {
	var b strings.Builder
	for _, d := range ds {
		b.WriteString("  " + d.Error() + "\n")
	}
	return b.String()
}

// wrap 把片段包成完整文档。
func wrap(body string) string {
	return `<Goxml Version="1"><Page>` + body + `</Page></Goxml>`
}

// ---------------------------------------------------------------------------
// 分层：Parser 不分类，Expander 分类
// ---------------------------------------------------------------------------

func TestParserLeavesAttrsUnclassified(t *testing.T) {
	doc, diags := parser.Parse("t.gxml", []byte(wrap(`<Button Text="保存" Click="Save"/>`)))
	if len(diags) > 0 {
		t.Fatalf("Parser 诊断:\n%s", join(diags))
	}
	btn := doc.Page.Children[0]

	// Parser 阶段：两者都在 RawAttrs，都不在 Attrs/Events。
	if len(btn.RawAttrs) != 2 {
		t.Fatalf("RawAttrs = %d, 期望 2（Text 与 Click 都未分类）", len(btn.RawAttrs))
	}
	if len(btn.Events) != 0 {
		t.Errorf("Parser 不应分类事件，Events = %d", len(btn.Events))
	}
	if len(btn.Attrs) != 0 {
		t.Errorf("Parser 不应分类属性，Attrs = %d", len(btn.Attrs))
	}
}

func TestExpanderClassifiesAttrAndEvent(t *testing.T) {
	r := mustOK(t, wrap(`<Button Text="保存" Click="Save"/>`))
	btn := r.Doc.Page.Children[0]

	if _, ok := btn.Attr("Text"); !ok {
		t.Error("Text 应被分类为属性")
	}
	ev, ok := btn.Event("Click")
	if !ok {
		t.Fatal("Click 应被分类为事件")
	}
	if ev.Method != "Save" {
		t.Errorf("Method = %q", ev.Method)
	}
	if len(btn.RawAttrs) != 0 {
		t.Error("分类后 RawAttrs 应清空")
	}
}

// ---------------------------------------------------------------------------
// 核心：未知名称绝不退化为事件（用户指出的拼写错误漏洞）
// ---------------------------------------------------------------------------

func TestMisspelledEventIsUnknownNotEvent(t *testing.T) {
	// Clik 是 Click 的拼写错误，值是合法方法名。
	// 关键：绝不能因为「不在属性表 + 值是方法名」就当成事件。
	d := mustCode(t, wrap(`<Button Text="保存" Clik="Save"/>`), CodeUnknownName)
	if !strings.Contains(d.Message, "Clik") {
		t.Errorf("诊断应指明名称: %s", d.Message)
	}
}

func TestMisspelledAttrIsUnknown(t *testing.T) {
	mustCode(t, wrap(`<Button TExt="保存"/>`), CodeUnknownName)
}

func TestUnknownOnCustomComponent(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Title:string"><Text Value="{Title}"/></Def></Defs>
  <Page><Card Title="x" Click="Open"/></Page>
</Goxml>`
	// Click 未在 Card 的 Events 中声明 → 组件没有该属性/事件。
	// 调用点由 expandCall 按 Props/Events 校验，因此诊断码是 DEF-007；
	// ATTR-004 用于非组件上下文的未知名称。
	mustCode(t, src, CodePropUnknown)
}

// ---------------------------------------------------------------------------
// 布局属性：已知但可能不适用于当前元素
// ---------------------------------------------------------------------------

func TestLayoutAttrAppliesToAll(t *testing.T) {
	mustOK(t, wrap(`<Button Text="a" Padding="8" Width="Fill"/>`))
}

func TestLayoutAttrNotApplicable(t *testing.T) {
	// Columns 是全局已知的布局属性，但只适用于 Grid。
	// 应报「不适用」，而不是「未知」。
	d := mustCode(t, wrap(`<Button Text="a" Columns="72 *"/>`), CodeNotApplicable)
	if !strings.Contains(d.Message, "not applicable") {
		t.Errorf("应报不适用: %s", d.Message)
	}
}

func TestGapNotApplicableToText(t *testing.T) {
	mustCode(t, wrap(`<Text Value="a" Gap="8"/>`), CodeNotApplicable)
}

// ---------------------------------------------------------------------------
// 自定义组件：Props 与 Events 都必须显式声明
// ---------------------------------------------------------------------------

func TestCustomComponentWithDeclaredEvent(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Title:string" Events="Open"><Text Value="{Title}"/></Def></Defs>
  <Page><Card Title="x" Open="OpenDetail"/></Page>
</Goxml>`
	r := mustOK(t, src)
	// 组件调用被内联展开为 <Text>，调用点本身不再存在。
	text := r.Doc.Page.Children[0]
	if text.Tag != "Text" {
		t.Fatalf("展开后标签 = %q, 期望 Text", text.Tag)
	}
	// v1 的事件仅作契约校验，不传播到展开结果（详见 §7 遗留说明）。
	if len(text.Events) != 0 {
		t.Errorf("v1 不应传播组件事件，实际 %d", len(text.Events))
	}
}

func TestCustomComponentEventWithParams(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Title:string" Events="Open(id:string)"><Text Value="{Title}"/></Def></Defs>
  <Page><Card Title="x" Open="OpenDetail(item.ID)"/></Page>
</Goxml>`
	r := mustOK(t, src)
	// 带参数的事件处理器同样能通过契约与语法校验，但不传播。
	text := r.Doc.Page.Children[0]
	if text.Tag != "Text" {
		t.Fatalf("展开后标签 = %q", text.Tag)
	}
}

// ---------------------------------------------------------------------------
// Schema 定义阶段冲突
// ---------------------------------------------------------------------------

func TestSchemaPropEventConflict(t *testing.T) {
	// 同一名称既是 Prop 又是 Event。
	src := `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Open:string" Events="Open"><Text Value="{Open}"/></Def></Defs>
  <Page><Card Open="x"/></Page>
</Goxml>`
	mustCode(t, src, CodeSchemaConflict)
}

func TestSchemaLayoutShadow(t *testing.T) {
	// 组件属性不得与全局布局属性同名，否则 Width 语义会随组件变化。
	src := `<Goxml Version="1">
  <Defs><Def Name="Panel" Props="Width:string"><Text Value="{Width}"/></Def></Defs>
  <Page><Panel/></Page>
</Goxml>`
	mustCode(t, src, CodeLayoutShadow)
}

func TestSchemaDuplicateProp(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs><Def Name="Card" Props="A:string; A:int"><Text Value="x"/></Def></Defs>
  <Page><Card A="x"/></Page>
</Goxml>`
	// 重复 Prop 由 Parser 的 parseDefProps 拦截（GXML-DEF-001），
	// 两层都检查是正确的防御；此处断言 Parser 阶段报错即可。
	_, pdiags := parser.Parse("t.gxml", []byte(src))
	if len(pdiags) == 0 {
		t.Fatal("期望 Parser 报重复 Prop")
	}
}

func TestSchemaDuplicateComponent(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs>
    <Def Name="Card" Props="A:string"><Text Value="{A}"/></Def>
    <Def Name="Card" Props="B:string"><Text Value="{B}"/></Def>
  </Defs>
  <Page><Card A="x"/></Page>
</Goxml>`
	mustCode(t, src, CodeSchemaDuplicate)
}

// ---------------------------------------------------------------------------
// 事件处理器语法
// ---------------------------------------------------------------------------

func TestEventRejectsExpression(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="Save(User.Name + '!')"/>`), CodeEventArg)
}

func TestEventRejectsBinding(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="{Save}"/>`), CodeEventSyntax)
}

func TestEventAcceptsLiterals(t *testing.T) {
	r := mustOK(t, wrap(`<Button Text="a" Click="Delete(1, 'confirm', true)"/>`))
	ev, _ := r.Doc.Page.Children[0].Event("Click")
	if len(ev.Args) != 3 {
		t.Fatalf("参数数 = %d", len(ev.Args))
	}
	for _, a := range ev.Args {
		if a.Kind != contracts.ArgLiteral {
			t.Errorf("应为字面量: %+v", a)
		}
	}
}

// ---------------------------------------------------------------------------
// 必填属性
// ---------------------------------------------------------------------------

func TestRequiredPropMissing(t *testing.T) {
	// Text 的 Value 是必填。
	mustCode(t, wrap(`<Text/>`), CodeUnknownName)
}

// ---------------------------------------------------------------------------
// 布局标签不参与组件 Schema
// ---------------------------------------------------------------------------

func TestLayoutTagAcceptsOnlyLayoutAttrs(t *testing.T) {
	mustOK(t, wrap(`<Column Padding="8" Gap="4"><Text Value="a"/></Column>`))
	// Row 没有 Text 属性。
	mustCode(t, wrap(`<Row Text="x"><Text Value="a"/></Row>`), CodeUnknownName)
}
