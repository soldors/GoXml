package expander

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// classifyTree 的分支覆盖：If / For / Def 体内的元素也必须分类
// ---------------------------------------------------------------------------

func TestClassifyInsideIfThen(t *testing.T) {
	// <If> 的 Then 分支内的元素必须被分类。
	r := mustOK(t, `<Goxml Version="1"><Page><If Condition="{X}">
      <Then><Button Text="a" Click="Go"/></Then>
    </If></Page></Goxml>`)

	ifNode := r.Doc.Page.Children[0]
	btn := ifNode.If.Then[0]
	if _, ok := btn.Attr("Text"); !ok {
		t.Error("Then 内的 Text 应被分类为属性")
	}
	if _, ok := btn.Event("Click"); !ok {
		t.Error("Then 内的 Click 应被分类为事件")
	}
	if len(btn.RawAttrs) != 0 {
		t.Error("分类后 RawAttrs 应清空")
	}
}

func TestClassifyInsideIfElse(t *testing.T) {
	r := mustOK(t, `<Goxml Version="1"><Page><If Condition="{X}">
      <Then><Text Value="a"/></Then>
      <Else><Button Text="b" Click="Go"/></Else>
    </If></Page></Goxml>`)

	btn := r.Doc.Page.Children[0].If.Else[0]
	if _, ok := btn.Event("Click"); !ok {
		t.Error("Else 内的 Click 应被分类为事件")
	}
}

func TestUnknownInsideIfIsReported(t *testing.T) {
	// Then 内的未知名称也必须被抓住。
	mustCode(t, `<Goxml Version="1"><Page><If Condition="{X}">
      <Then><Button Text="a" Clik="Go"/></Then>
    </If></Page></Goxml>`, CodeUnknownName)
}

func TestUnknownInsideElseIsReported(t *testing.T) {
	mustCode(t, `<Goxml Version="1"><Page><If Condition="{X}">
      <Then><Text Value="a"/></Then>
      <Else><Text Value="b" Bogus="1"/></Else>
    </If></Page></Goxml>`, CodeUnknownName)
}

func TestClassifyInsideFor(t *testing.T) {
	r := mustOK(t, `<Goxml Version="1"><Page><For Each="item" In="{Items}" Key="{item.ID}">
      <Button Text="{item.Name}" Click="Select(item.ID)"/>
    </For></Page></Goxml>`)

	forNode := r.Doc.Page.Children[0]
	btn := forNode.Children[0]
	if _, ok := btn.Event("Click"); !ok {
		t.Fatal("For 体内的 Click 应被分类为事件")
	}
	ev, _ := btn.Event("Click")
	if len(ev.Args) != 1 || ev.Args[0].Binding.Path() != "item.ID" {
		t.Errorf("参数 = %+v", ev.Args)
	}
}

func TestUnknownInsideForIsReported(t *testing.T) {
	mustCode(t, `<Goxml Version="1"><Page><For Each="i" In="{Items}">
      <Text Value="a" Bogus="1"/>
    </For></Page></Goxml>`, CodeUnknownName)
}

func TestClassifyInsideDefBody(t *testing.T) {
	// Def 体内的元素同样要分类。
	r := mustOK(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string">
    <Column Padding="8"><Text Value="{T}"/></Column>
  </Def></Defs>
  <Page><Card T="x"/></Page>
</Goxml>`)

	def := r.Doc.Defs[0]
	col := def.Def.Body
	if col.Padding.IsZero() {
		t.Error("Def 体内 Column 的 Padding 应已解析")
	}
	text := col.Children[0]
	if _, ok := text.Attr("Value"); !ok {
		t.Error("Def 体内 Text 的 Value 应被分类为属性")
	}
}

func TestNestedIfInsideFor(t *testing.T) {
	// 嵌套结构的递归分类。
	r := mustOK(t, `<Goxml Version="1"><Page><For Each="item" In="{Items}" Key="{item.ID}">
      <If Condition="{item.Visible}">
        <Then><Button Text="{item.Name}" Click="Go(item.ID)"/></Then>
      </If>
    </For></Page></Goxml>`)

	ifNode := r.Doc.Page.Children[0].Children[0]
	btn := ifNode.If.Then[0]
	if _, ok := btn.Event("Click"); !ok {
		t.Error("嵌套 If 内的 Click 应被分类")
	}
}

// ---------------------------------------------------------------------------
// registerDef 未覆盖分支
// ---------------------------------------------------------------------------

func TestSchemaEventLayoutShadow(t *testing.T) {
	// 事件名与全局布局属性冲突。
	src := `<Goxml Version="1">
  <Defs><Def Name="Panel" Props="T:string" Events="Align"><Text Value="{T}"/></Def></Defs>
  <Page><Panel T="x"/></Page>
</Goxml>`
	mustCode(t, src, CodeLayoutShadow)
}

func TestSchemaDuplicateEvent(t *testing.T) {
	src := `<Goxml Version="1">
  <Defs><Def Name="Panel" Props="T:string" Events="Click; Click"><Text Value="{T}"/></Def></Defs>
  <Page><Panel T="x"/></Page>
</Goxml>`
	mustCode(t, src, CodeSchemaDuplicate)
}

// ---------------------------------------------------------------------------
// parseEvent 未覆盖分支
// ---------------------------------------------------------------------------

func TestEventMissingClosingParen(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="Save(item"/>`), CodeEventSyntax)
}

func TestEventInvalidMethod(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="1Bad"/>`), CodeEventSyntax)
}

func TestEventEmptyArgument(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="Save(a, )"/>`), CodeEventArg)
}

// ---------------------------------------------------------------------------
// isNumberLiteral 边界
// ---------------------------------------------------------------------------

func TestEventNumericArgVariants(t *testing.T) {
	r := mustOK(t, wrap(`<Button Text="a" Click="Go(1, 2.5, -3)"/>`))
	ev, _ := r.Doc.Page.Children[0].Event("Click")
	if len(ev.Args) != 3 {
		t.Fatalf("参数数 = %d", len(ev.Args))
	}
	for i, want := range []string{"1", "2.5", "-3"} {
		if ev.Args[i].Kind != contracts.ArgLiteral || ev.Args[i].Literal != want {
			t.Errorf("arg%d = %+v, want literal %q", i, ev.Args[i], want)
		}
	}
}

func TestEventRejectsBadNumber(t *testing.T) {
	// 两个小数点不是合法数字，也不是合法标识符。
	mustCode(t, wrap(`<Button Text="a" Click="Go(1.2.3)"/>`), CodeEventArg)
}

// ---------------------------------------------------------------------------
// schemaFor 未覆盖分支：未知标签不报错（可能是 Go 自定义组件）
// ---------------------------------------------------------------------------

func TestUnknownTagIsNotReportedByExpander(t *testing.T) {
	// 未在 Defs 中声明、也不是内置控件的标签，
	// 可能是 //goxml:component 定义的 Go 组件，由 Analyzer 解析。
	// Expander 不应报错，但其属性无法分类 → 未知属性。
	r := run(t, wrap(`<MyWidget Foo="bar"/>`))
	if len(r.Diags) == 0 {
		t.Fatal("未知标签的属性应报未知")
	}
	if r.Diags[0].Code != CodeUnknownName {
		t.Errorf("诊断码 = %s", r.Diags[0].Code)
	}
}

func TestCustomComponentWithNoAttrs(t *testing.T) {
	// 无属性的未知标签不产生诊断。
	mustOK(t, wrap(`<MyWidget/>`))
}

// ---------------------------------------------------------------------------
// splitPath / isIdentifier 边界
// ---------------------------------------------------------------------------

func TestEventRejectsTrailingDot(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="Go(user.)"/>`), CodeEventArg)
}

func TestEventRejectsLeadingDigitSegment(t *testing.T) {
	mustCode(t, wrap(`<Button Text="a" Click="Go(1user)"/>`), CodeEventArg)
}

// ---------------------------------------------------------------------------
// Expand 的 nil 与空输入
// ---------------------------------------------------------------------------

func TestExpandNilDocument(t *testing.T) {
	r := Expand("t.gxml", nil)
	if len(r.Diags) == 0 {
		t.Fatal("nil 文档应报错")
	}
	if !strings.Contains(r.Diags[0].Message, "nil") {
		t.Errorf("诊断 = %q", r.Diags[0].Message)
	}
}

func TestExpandDocumentWithoutPage(t *testing.T) {
	// Parser 保证有 Page；直接构造一个没有 Page 的文档验证健壮性。
	doc := &contracts.DocumentIR{FilePath: "t.gxml"}
	r := Expand("t.gxml", doc)
	if len(r.Diags) != 0 {
		t.Errorf("无 Page 不应崩溃: %v", join(r.Diags))
	}
}
