package expander

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// expand 走完整管线并返回展开后的文档。
func expand(t *testing.T, src string) *contracts.DocumentIR {
	t.Helper()
	r := mustOK(t, src)
	return r.Doc
}

// pageChild 返回展开后页面的第 i 个渲染节点。
func pageChild(t *testing.T, doc *contracts.DocumentIR, i int) *contracts.NodeIR {
	t.Helper()
	if doc.Page == nil {
		t.Fatal("Page 为 nil")
	}
	if i >= len(doc.Page.Children) {
		t.Fatalf("页面子节点数 = %d, 索引 %d 越界", len(doc.Page.Children), i)
	}
	return doc.Page.Children[i]
}

// ---------------------------------------------------------------------------
// 基本展开：Def 调用内联为渲染子树
// ---------------------------------------------------------------------------

func TestExpandBasic(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Title:string">
    <Column Padding="8"><Text Value="{Title}"/></Column>
  </Def></Defs>
  <Page><Card Title="你好"/></Page>
</Goxml>`)

	// 调用点被替换为 Def 体的 Column。
	col := pageChild(t, doc, 0)
	if col.Tag != "Column" {
		t.Fatalf("展开后标签 = %q, 期望 Column", col.Tag)
	}
	if col.Padding.Top != 8 {
		t.Errorf("Padding = %+v", col.Padding)
	}
	text := col.Children[0]
	if text.Tag != "Text" {
		t.Fatalf("子节点 = %q", text.Tag)
	}
	// Prop 静态值被替换进 Value。
	v, ok := text.Attr("Value")
	if !ok || v.Value.IsBinding || v.Value.Raw != "你好" {
		t.Errorf("Value = %+v", v.Value)
	}
}

func TestExpandPropBindingSubstitution(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Title:string"><Text Value="{Title}"/></Def></Defs>
  <Page><Card Title="{User.Name}"/></Page>
</Goxml>`)

	text := pageChild(t, doc, 0)
	v, _ := text.Attr("Value")
	if !v.Value.IsBinding {
		t.Fatalf("应为绑定替换: %+v", v.Value)
	}
	if got := v.Value.Path(); got != "User.Name" {
		t.Errorf("Path = %q, 期望 User.Name", got)
	}
}

func TestExpandPropDefaultValue(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Accent:color=$Theme.Primary">
    <Column Background="{Accent}"><Text Value="x"/></Column>
  </Def></Defs>
  <Page><Card/></Page>
</Goxml>`)

	col := pageChild(t, doc, 0)
	if col.Background == nil {
		t.Fatal("Background 未设置")
	}
	// 默认值是主题引用，替换后应成为主题颜色。
	if !col.Background.IsThemeRef || col.Background.ThemeField != "Primary" {
		t.Errorf("Background = %+v", col.Background)
	}
}

func TestExpandPropOverrideDefault(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="Accent:color=$Theme.Primary">
    <Column Background="{Accent}"><Text Value="x"/></Column>
  </Def></Defs>
  <Page><Card Accent="#FF0000"/></Page>
</Goxml>`)

	col := pageChild(t, doc, 0)
	if col.Background == nil || col.Background.R != 0xFF {
		t.Errorf("Background = %+v", col.Background)
	}
}

// ---------------------------------------------------------------------------
// 嵌套属性路径：{Title.Length} 之类的组合
// ---------------------------------------------------------------------------

func TestExpandNestedPropPath(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="User:string"><Text Value="{User.Name}"/></Def></Defs>
  <Page><Card User="{Account}"/></Page>
</Goxml>`)

	text := pageChild(t, doc, 0)
	v, _ := text.Attr("Value")
	if got := v.Value.Path(); got != "Account.Name" {
		t.Errorf("Path = %q, 期望 Account.Name", got)
	}
}

// ---------------------------------------------------------------------------
// Slot
// ---------------------------------------------------------------------------

func TestExpandSlotReplacement(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Frame" Props="T:string">
    <Column><Text Value="{T}"/><Slot/></Column>
  </Def></Defs>
  <Page><Frame T="标题"><Text Value="内容"/></Frame></Page>
</Goxml>`)

	col := pageChild(t, doc, 0)
	if col.Tag != "Column" {
		t.Fatalf("标签 = %q", col.Tag)
	}
	if len(col.Children) != 2 {
		t.Fatalf("子节点数 = %d, 期望 2（Text + 插槽内容）", len(col.Children))
	}
	if col.Children[0].Tag != "Text" {
		t.Errorf("[0] = %q", col.Children[0].Tag)
	}
	// <Slot/> 被替换为调用方子节点。
	if col.Children[1].Tag != "Text" {
		t.Fatalf("[1] = %q", col.Children[1].Tag)
	}
	v, _ := col.Children[1].Attr("Value")
	if v.Value.Raw != "内容" {
		t.Errorf("插槽内容 Value = %q", v.Value.Raw)
	}
}

func TestExpandSlotEmptyBody(t *testing.T) {
	// Def 体就是 <Slot/>。
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Wrap"><Slot/></Def></Defs>
  <Page><Wrap><Text Value="a"/></Wrap></Page>
</Goxml>`)

	n := pageChild(t, doc, 0)
	if n.Tag != "Text" {
		t.Fatalf("标签 = %q", n.Tag)
	}
}

func TestExpandChildrenWithoutSlotRejected(t *testing.T) {
	// 未声明 <Slot/> 的组件不能接收子节点。
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x"><Text Value="extra"/></Card></Page>
</Goxml>`, CodeSlotNotAllowed)
}

func TestExpandSlotMultipleChildrenRejected(t *testing.T) {
	// 单个 <Slot/> 位置插入多个子节点：v1 无法表达，明确报错。
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Frame"><Column><Slot/></Column></Def></Defs>
  <Page><Frame><Text Value="a"/><Text Value="b"/></Frame></Page>
</Goxml>`, CodeSlotNotAllowed)
}

// ---------------------------------------------------------------------------
// 组件调用的属性校验
// ---------------------------------------------------------------------------

func TestExpandUnknownPropRejected(t *testing.T) {
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Bogus="y"/></Page>
</Goxml>`, CodePropUnknown)
}

func TestExpandMissingRequiredProp(t *testing.T) {
	mustCode(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><Card/></Page>
</Goxml>`, CodePropMissing)
}

// ---------------------------------------------------------------------------
// 递归检测
// ---------------------------------------------------------------------------

func TestExpandRecursiveDefRejected(t *testing.T) {
	// 间接自引用：A -> B -> A。
	mustCode(t, `<Goxml Version="1">
  <Defs>
    <Def Name="A" Props="T:string"><Column><B T="{T}"/></Column></Def>
    <Def Name="B" Props="T:string"><Column><A T="{T}"/></Column></Def>
  </Defs>
  <Page><A T="x"/></Page>
</Goxml>`, CodeRecursiveDef)
}

// ---------------------------------------------------------------------------
// 多次调用必须互不影响（克隆正确性）
// ---------------------------------------------------------------------------

func TestExpandSameDefTwiceIsIndependent(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page>
    <Card T="first"/>
    <Card T="second"/>
  </Page>
</Goxml>`)

	if len(doc.Page.Children) != 2 {
		t.Fatalf("子节点数 = %d", len(doc.Page.Children))
	}
	first, _ := doc.Page.Children[0].Attr("Value")
	second, _ := doc.Page.Children[1].Attr("Value")
	if first.Value.Raw != "first" {
		t.Errorf("第一次调用 = %q", first.Value.Raw)
	}
	if second.Value.Raw != "second" {
		t.Errorf("第二次调用 = %q（克隆失败会得到 first）", second.Value.Raw)
	}
}

// ---------------------------------------------------------------------------
// 嵌套组件调用
// ---------------------------------------------------------------------------

func TestExpandNestedComponentCalls(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs>
    <Def Name="Inner" Props="V:string"><Text Value="{V}"/></Def>
    <Def Name="Outer" Props="V:string"><Column><Inner V="{V}"/></Column></Def>
  </Defs>
  <Page><Outer V="{User.Name}"/></Page>
</Goxml>`)

	col := pageChild(t, doc, 0)
	if col.Tag != "Column" {
		t.Fatalf("外层展开 = %q", col.Tag)
	}
	// 内层组件也必须被展开。
	text := col.Children[0]
	if text.Tag != "Text" {
		t.Fatalf("内层未展开: %q", text.Tag)
	}
	v, _ := text.Attr("Value")
	if got := v.Value.Path(); got != "User.Name" {
		t.Errorf("Path = %q", got)
	}
}

// ---------------------------------------------------------------------------
// 展开后的节点仍保持分类正确
// ---------------------------------------------------------------------------

func TestExpandedNodesKeepClassification(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Btn" Props="T:string"><Button Text="{T}" Click="Go"/></Def></Defs>
  <Page><Btn T="确定"/></Page>
</Goxml>`)

	btn := pageChild(t, doc, 0)
	if btn.Tag != "Button" {
		t.Fatalf("标签 = %q", btn.Tag)
	}
	if _, ok := btn.Event("Click"); !ok {
		t.Error("展开后 Click 事件应保留")
	}
}

// ---------------------------------------------------------------------------
// 在 If / For 内调用组件
// ---------------------------------------------------------------------------

func TestExpandInsideIf(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><If Condition="{X}"><Then><Card T="a"/></Then></If></Page>
</Goxml>`)

	ifNode := pageChild(t, doc, 0)
	if len(ifNode.If.Then) != 1 || ifNode.If.Then[0].Tag != "Text" {
		t.Fatalf("Then 内未正确展开: %+v", ifNode.If.Then)
	}
}

func TestExpandInsideFor(t *testing.T) {
	doc := expand(t, `<Goxml Version="1">
  <Defs><Def Name="Row2" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><For Each="item" In="{Items}" Key="{item.ID}"><Row2 T="{item.Name}"/></For></Page>
</Goxml>`)

	forNode := pageChild(t, doc, 0)
	if len(forNode.Children) != 1 || forNode.Children[0].Tag != "Text" {
		t.Fatalf("For 内未正确展开: %+v", forNode.Children)
	}
	v, _ := forNode.Children[0].Attr("Value")
	if got := v.Value.Path(); got != "item.Name" {
		t.Errorf("Path = %q", got)
	}
}

// ---------------------------------------------------------------------------
// 事件契约：v1 不支持自定义组件事件冒泡
// ---------------------------------------------------------------------------

func TestCustomComponentEventsAreContractOnly(t *testing.T) {
	// v1 的 <Def Events> 只声明契约：Def 体内没有语法「发出」事件，
	// 因此调用点传入的处理器在展开时不会传播到渲染树。
	//
	// 这是已知的 v1 限制，不是缺陷：真正的事件转发需要新的语言语法
	// （如事件冒泡或回调插槽），属于 v2。
	r := mustOK(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string" Events="Open"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Open="DoOpen"/></Page>
</Goxml>`)

	text := r.Doc.Page.Children[0]
	if text.Tag != "Text" {
		t.Fatalf("标签 = %q", text.Tag)
	}
	if len(text.Events) != 0 {
		t.Errorf("v1 不应把调用点事件传播到展开结果，实际 %d 个", len(text.Events))
	}
}

// ---------------------------------------------------------------------------
// 大小写敏感与未定义组件
// ---------------------------------------------------------------------------

func TestExpandIsCaseSensitive(t *testing.T) {
	// card 与 Card 是不同标签：card 未定义，其属性无法分类。
	r := run(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string"><Text Value="{T}"/></Def></Defs>
  <Page><card T="x"/></Page>
</Goxml>`)
	found := false
	for _, d := range r.Diags {
		if d.Code == CodeUnknownName && strings.Contains(d.Message, "card") {
			found = true
		}
	}
	if !found {
		t.Errorf("小写 card 应按未知标签处理: %v", join(r.Diags))
	}
}
