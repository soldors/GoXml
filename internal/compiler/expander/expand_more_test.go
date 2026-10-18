package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// Slot 出现在 If / For 内：replaceSlotList 与 cloneList 的执行路径
// ---------------------------------------------------------------------------

func TestExpandSlotInsideIf(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Frame">
    <Column><If Condition="{X}"><Then><Slot/></Then></If></Column>
  </Def></Defs>
  <Page><Frame><Text Value="a"/></Frame></Page>
</Goxml>`)

	col := pageChild(t, doc, 0)
	if col.Tag != "Column" {
		t.Fatalf("标签 = %q", col.Tag)
	}
	ifNode := col.Children[0]
	if ifNode.Kind != contracts.KindIf {
		t.Fatalf("子节点 = %v", ifNode.Kind)
	}
	if len(ifNode.If.Then) != 1 || ifNode.If.Then[0].Tag != "Text" {
		t.Fatalf("Then 内插槽未替换: %+v", ifNode.If.Then)
	}
}

func TestExpandSlotInsideIfElse(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Frame">
    <Column><If Condition="{X}"><Then><Text Value="t"/></Then><Else><Slot/></Else></If></Column>
  </Def></Defs>
  <Page><Frame><Text Value="a"/></Frame></Page>
</Goxml>`)

	ifNode := pageChild(t, doc, 0).Children[0]
	if len(ifNode.If.Else) != 1 || ifNode.If.Else[0].Tag != "Text" {
		t.Fatalf("Else 内插槽未替换: %+v", ifNode.If.Else)
	}
}

func TestExpandSlotInsideFor(t *testing.T) {
	// For 内的 <Slot/> 同样要被替换。
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Frame">
    <Column><For Each="i" In="{Items}"><Slot/></For></Column>
  </Def></Defs>
  <Page><Frame><Text Value="a"/></Frame></Page>
</Goxml>`)

	forNode := pageChild(t, doc, 0).Children[0]
	if forNode.Kind != contracts.KindFor {
		t.Fatalf("子节点 = %v", forNode.Kind)
	}
	if len(forNode.Children) != 1 || forNode.Children[0].Tag != "Text" {
		t.Fatalf("For 内插槽未替换: %+v", forNode.Children)
	}
}

// ---------------------------------------------------------------------------
// cloneNode 的深拷贝分支
// ---------------------------------------------------------------------------

func TestExpandClonesNestedStructures(t *testing.T) {
	// Def 体内含 If/For/嵌套子节点，验证克隆不会共享可变状态。
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Complex" Props="T:string">
    <Column Gap="4">
      <If Condition="{Show}"><Then><Text Value="{T}"/></Then></If>
      <For Each="it" In="{Items}" Key="{it.ID}"><Text Value="{it.N}"/></For>
    </Column>
  </Def></Defs>
  <Page>
    <Complex T="first"/>
    <Complex T="second"/>
  </Page>
</Goxml>`)

	if len(doc.Page.Children) != 2 {
		t.Fatalf("子节点数 = %d", len(doc.Page.Children))
	}
	// 两次调用的 If 分支必须各自独立。
	first := doc.Page.Children[0].Children[0]
	second := doc.Page.Children[1].Children[0]
	if first == second {
		t.Fatal("两次展开共享了同一个节点指针")
	}
	v1, _ := first.If.Then[0].Attr("Value")
	v2, _ := second.If.Then[0].Attr("Value")
	if v1.Value.Raw != "first" || v2.Value.Raw != "second" {
		t.Errorf("替换值 = %q / %q", v1.Value.Raw, v2.Value.Raw)
	}
}

func TestExpandClonesGridColumns(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="G"><Grid Columns="72 *"><Text Value="a"/></Grid></Def></Defs>
  <Page><G/><G/></Page>
</Goxml>`)

	first := pageChild(t, doc, 0)
	second := pageChild(t, doc, 1)
	if len(first.Columns) != 2 || len(second.Columns) != 2 {
		t.Fatalf("列数 = %d / %d", len(first.Columns), len(second.Columns))
	}
	// 修改一个不应影响另一个。
	first.Columns[0].Value = 999
	if second.Columns[0].Value == 999 {
		t.Error("Columns 切片被共享")
	}
}

// ---------------------------------------------------------------------------
// 事件参数中的 Prop 替换（substituteEvents）
// ---------------------------------------------------------------------------

func TestExpandSubstitutesEventArgs(t *testing.T) {
	// 规范 §5.5：事件实参只能是字面量或绑定路径（不带花括号）。
	// 因此 Prop 引用写作 Del(Id)，展开时由 substituteEvents 替换为调用方值。
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Btn" Props="Id:string"><Button Text="x" Click="Del(Id)"/></Def></Defs>
  <Page><Btn Id="{item.ID}"/></Page>
</Goxml>`)

	btn := pageChild(t, doc, 0)
	if btn.Tag != "Button" {
		t.Fatalf("标签 = %q", btn.Tag)
	}
	ev, ok := btn.Event("Click")
	if !ok {
		t.Fatal("Click 事件丢失")
	}
	if len(ev.Args) != 1 {
		t.Fatalf("参数数 = %d", len(ev.Args))
	}
	if got := ev.Args[0].Binding.Path(); got != "item.ID" {
		t.Errorf("参数 = %q, 期望 item.ID（Prop 替换）", got)
	}
}

// ---------------------------------------------------------------------------
// 颜色：各种静态形式与未解析绑定
// ---------------------------------------------------------------------------

func TestExpandColorHex8(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="C" Props="Bg:color"><Column Background="{Bg}"><Text Value="x"/></Column></Def></Defs>
  <Page><C Bg="#11223344"/></Page>
</Goxml>`)

	bg := pageChild(t, doc, 0).Background
	if bg == nil || bg.R != 0x11 || bg.G != 0x22 || bg.B != 0x33 || bg.A != 0x44 {
		t.Fatalf("Background = %+v", bg)
	}
}

func TestExpandColorInvalidFormRetained(t *testing.T) {
	// 调用方传入非法颜色形式：解析失败，保留原样并在展开后报错。
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="C" Props="Bg:color"><Column Background="{Bg}"><Text Value="x"/></Column></Def></Defs>
  <Page><C Bg="red"/></Page>
</Goxml>`, CodeNotApplicable)
}

func TestUnresolvedColorBindingRejected(t *testing.T) {
	// 页面直接写 Background="{User.Color}"：不是 Prop 引用，展开后仍为绑定。
	mustCode(t, wrap(`<Column Background="{User.Color}"><Text Value="a"/></Column>`), CodeNotApplicable)
}

func TestColorFromValueRejects(t *testing.T) {
	cases := []contracts.BindingVal{
		{Raw: "red"},
		{Raw: "#FFF"},
		{Raw: "#GGGGGG"},
		{Raw: "$Theme."},
		{IsBinding: true, Segments: []string{"X"}},
	}
	for _, v := range cases {
		if c, ok := colorFromValue(v); ok {
			t.Errorf("%+v 不应解析成功，得到 %+v", v, c)
		}
	}
}

// ---------------------------------------------------------------------------
// 调用点的布局属性被剔除，其余留给 expandCall
// ---------------------------------------------------------------------------

func TestCallSiteLayoutAttrNotApplicable(t *testing.T) {
	// 组件调用点上写 Columns（只适用于 Grid）应报不适用。
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Columns="72"/></Page>
</Goxml>`, CodeNotApplicable)
}

func TestCallSiteAcceptsLayoutAttrs(t *testing.T) {
	// 调用点可以带通用布局属性（如 Width），它们作用于展开后的根节点容器。
	mustOK(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Width="Fill"/></Page>
</Goxml>`)
}

// ---------------------------------------------------------------------------
// 事件处理器在调用点的语法校验
// ---------------------------------------------------------------------------

func TestCallSiteEventSyntaxValidated(t *testing.T) {
	// 调用点事件的值语法非法（表达式）应被拒绝。
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string" Events="Open"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Open="Go(a + b)"/></Page>
</Goxml>`, CodeEventArg)
}

// ---------------------------------------------------------------------------
// 组件调用点上的必填 Prop 检查发生在 expandCall
// ---------------------------------------------------------------------------

func TestCallSiteMissingPropReportsDefCode(t *testing.T) {
	r := run(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="A:string; B:string"><Text Value="{A}"/></Def></Defs>
  <Page><Card A="1"/></Page>
</Goxml>`)
	found := false
	for _, d := range r.Diags {
		if d.Code == CodePropMissing {
			found = true
		}
	}
	if !found {
		t.Errorf("应报缺少必填属性: %s", join(r.Diags))
	}
}
