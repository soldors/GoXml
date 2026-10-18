package parser

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// mustParse 解析并断言无诊断。
func mustParse(t *testing.T, src string) *contracts.DocumentIR {
	t.Helper()
	doc, diags := Parse("test.gxml", []byte(src))
	if len(diags) > 0 {
		t.Fatalf("意外诊断:\n%v", joinDiags(diags))
	}
	if doc == nil {
		t.Fatal("doc 为 nil")
	}
	return doc
}

// mustFail 解析并断言产生指定诊断码。
func mustFail(t *testing.T, src, wantCode string) contracts.CompilerError {
	t.Helper()
	_, diags := Parse("test.gxml", []byte(src))
	if len(diags) == 0 {
		t.Fatalf("期望诊断 %s，但解析成功", wantCode)
	}
	for _, d := range diags {
		if d.Code == wantCode {
			return d
		}
	}
	t.Fatalf("期望诊断码 %s，实际:\n%v", wantCode, joinDiags(diags))
	return contracts.CompilerError{}
}

func joinDiags(ds []contracts.CompilerError) string {
	var b strings.Builder
	for _, d := range diags(ds) {
		b.WriteString("  " + d.Error() + "\n")
	}
	return b.String()
}

func diags(in []contracts.CompilerError) []contracts.CompilerError { return in }

// ---------------------------------------------------------------------------
// 基础文档结构
// ---------------------------------------------------------------------------

func TestParseMinimalDocument(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1">
  <Page Route="/">
    <Text Value="hello"/>
  </Page>
</Goxml>`)

	if doc.Version != "1" {
		t.Errorf("Version = %q", doc.Version)
	}
	if doc.Page == nil {
		t.Fatal("Page 为 nil")
	}
	if doc.Page.RoutePath != "/" {
		t.Errorf("RoutePath = %q", doc.Page.RoutePath)
	}
	if len(doc.Page.Children) != 1 {
		t.Fatalf("Page 子节点数 = %d", len(doc.Page.Children))
	}
	text := doc.Page.Children[0]
	if text.Tag != "Text" || text.Kind != contracts.KindElement {
		t.Errorf("子节点 = %+v", text)
	}
}

func TestParseViewModel(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1" ViewModel="demo.ViewModel"><Page><Text Value="a"/></Page></Goxml>`)
	if doc.ViewModel != "demo.ViewModel" {
		t.Errorf("ViewModel = %q", doc.ViewModel)
	}
}

// ---------------------------------------------------------------------------
// 错误定位：这是 Parser 的核心价值
// ---------------------------------------------------------------------------

func TestParseErrorPosition(t *testing.T) {
	src := `<Goxml Version="1">
  <Page>
    <Text Value="{a < b}"/>
  </Page>
</Goxml>`
	_, ds := Parse("test.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("期望 XML 语法错误")
	}
	d := ds[0]
	if d.Line != 3 {
		t.Errorf("错误行 = %d, 期望 3", d.Line)
	}
	if !strings.HasPrefix(d.Code, "GXML-") {
		t.Errorf("错误码 = %q", d.Code)
	}
}

func TestParseAttributeSpan(t *testing.T) {
	// Align 在第 2 行，从第 8 列开始（1 基）。
	src := "<Goxml Version=\"1\">\n<Page>\n  <Row Align=\"Bad\"><Text Value=\"a\"/></Row>\n</Page>\n</Goxml>"
	_, ds := Parse("test.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("期望 Align 取值错误")
	}
	d := ds[0]
	if d.Line != 3 {
		t.Errorf("行 = %d, 期望 3", d.Line)
	}
	if d.Col < 7 || d.Col > 9 {
		t.Errorf("列 = %d, 期望约 8（Align 所在列）", d.Col)
	}
}

func TestParseChineseColumnIsRuneBased(t *testing.T) {
	// 中文占 3 字节但算 1 列，列号必须按 rune 计。
	src := "<Goxml Version=\"1\">\n<Page>\n  <Row Align=\"坏\"><Text Value=\"a\"/></Row>\n</Page>\n</Goxml>"
	_, ds := Parse("test.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("期望诊断")
	}
	if got := ds[0].Col; got < 7 || got > 9 {
		t.Errorf("含中文时列 = %d, 期望约 8（按 rune 计数）", got)
	}
}

// ---------------------------------------------------------------------------
// 根节点规则
// ---------------------------------------------------------------------------

func TestRootMustBeGoxml(t *testing.T) {
	mustFail(t, `<Root Version="1"><Page/></Root>`, CodeRoot)
}

func TestRootRequiresVersion(t *testing.T) {
	mustFail(t, `<Goxml><Page><Text Value="a"/></Page></Goxml>`, CodeRoot)
}

func TestRootRejectsBadVersion(t *testing.T) {
	mustFail(t, `<Goxml Version="2"><Page><Text Value="a"/></Page></Goxml>`, CodeRoot)
}

func TestRootRequiresPage(t *testing.T) {
	mustFail(t, `<Goxml Version="1"></Goxml>`, CodeRoot)
}

func TestRootRejectsDuplicatePage(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Text Value="a"/></Page><Page><Text Value="b"/></Page></Goxml>`, CodeRoot)
}

func TestRootRejectsUnknownChild(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Bogus/></Goxml>`, CodeRoot)
}

// ---------------------------------------------------------------------------
// 属性值：静态 / 绑定 / 混合
// ---------------------------------------------------------------------------

func TestAttrStaticLiteral(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Text Value="保存"/></Page></Goxml>`)
	v, ok := doc.Page.Children[0].RawAttrs.Lookup("Value")
	if !ok {
		t.Fatal("Value 缺失")
	}
	if v.Value.IsBinding || v.Value.Raw != "保存" {
		t.Errorf("Value = %+v", v.Value)
	}
}

func TestAttrBinding(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Text Value="{User.Profile.Name}"/></Page></Goxml>`)
	v, _ := doc.Page.Children[0].RawAttrs.Lookup("Value")
	if !v.Value.IsBinding {
		t.Fatal("应为绑定")
	}
	want := []string{"User", "Profile", "Name"}
	if len(v.Value.Segments) != len(want) {
		t.Fatalf("Segments = %v", v.Value.Segments)
	}
	for i := range want {
		if v.Value.Segments[i] != want[i] {
			t.Errorf("Segments[%d] = %q, want %q", i, v.Value.Segments[i], want[i])
		}
	}
}

func TestAttrNegatedBinding(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Button Text="a" Enabled="{!CanSave}"/></Page></Goxml>`)
	v, _ := doc.Page.Children[0].RawAttrs.Lookup("Enabled")
	if !v.Value.IsBinding || !v.Value.IsNegated {
		t.Errorf("Enabled = %+v", v.Value)
	}
}

func TestAttrRejectsMixed(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Text Value="你好 {User.Name}"/></Page></Goxml>`, CodeAttrCall)
}

func TestAttrRejectsBadPath(t *testing.T) {
	for _, bad := range []string{"{User.}", "{.Name}", "{1abc}", "{User..Name}", "{}"} {
		src := `<Goxml Version="1"><Page><Text Value="` + bad + `"/></Page></Goxml>`
		mustFail(t, src, CodeAttrCall)
	}
}

func TestAttrRejectsDuplicate(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Text Value="a" Value="b"/></Page></Goxml>`, CodeAttrDup)
}

// ---------------------------------------------------------------------------
// 布局属性
// ---------------------------------------------------------------------------

func TestParsePaddingVariants(t *testing.T) {
	cases := []struct {
		src  string
		want contracts.EdgeInsets
	}{
		{`Padding="10"`, contracts.EdgeInsets{Top: 10, Right: 10, Bottom: 10, Left: 10}},
		{`Padding="10 20"`, contracts.EdgeInsets{Top: 10, Right: 20, Bottom: 10, Left: 20}},
		{`Padding="1 2 3 4"`, contracts.EdgeInsets{Top: 1, Right: 2, Bottom: 3, Left: 4}},
	}
	for _, c := range cases {
		doc := mustParse(t, `<Goxml Version="1"><Page><Column `+c.src+`><Text Value="a"/></Column></Page></Goxml>`)
		got := doc.Page.Children[0].Padding
		if got != c.want {
			t.Errorf("%s => %+v, want %+v", c.src, got, c.want)
		}
	}
}

func TestParsePaddingRejects(t *testing.T) {
	for _, bad := range []string{`Padding="1 2 3"`, `Padding="-1"`, `Padding=""`, `Padding="1 2 3 4 5"`} {
		src := `<Goxml Version="1"><Page><Column ` + bad + `><Text Value="a"/></Column></Page></Goxml>`
		mustFail(t, src, CodeAttrCall)
	}
}

func TestParseAlign(t *testing.T) {
	cases := map[string]contracts.CrossAlign{
		"Start": contracts.AlignStart, "Center": contracts.AlignCenter,
		"End": contracts.AlignEnd, "Baseline": contracts.AlignBaseline,
	}
	for in, want := range cases {
		doc := mustParse(t, `<Goxml Version="1"><Page><Row Align="`+in+`"><Text Value="a"/></Row></Page></Goxml>`)
		if got := doc.Page.Children[0].Align; got != want {
			t.Errorf("Align=%s => %v, want %v", in, got, want)
		}
	}
}

func TestParseAlignUnsetByDefault(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Row><Text Value="a"/></Row></Page></Goxml>`)
	if got := doc.Page.Children[0].Align; got != contracts.AlignUnset {
		t.Errorf("未设置时 Align = %v, 期望 AlignUnset（默认值由 Codegen 补）", got)
	}
}

func TestParseJustify(t *testing.T) {
	cases := map[string]contracts.MainJustify{
		"Start": contracts.JustifyStart, "Center": contracts.JustifyCenter,
		"End": contracts.JustifyEnd, "SpaceBetween": contracts.JustifySpaceBetween,
		"SpaceAround": contracts.JustifySpaceAround, "SpaceEvenly": contracts.JustifySpaceEvenly,
	}
	for in, want := range cases {
		doc := mustParse(t, `<Goxml Version="1"><Page><Row Justify="`+in+`"><Text Value="a"/></Row></Page></Goxml>`)
		if got := doc.Page.Children[0].Justify; got != want {
			t.Errorf("Justify=%s => %v, want %v", in, got, want)
		}
	}
}

func TestParseGap(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Row Gap="8"><Text Value="a"/></Row></Page></Goxml>`)
	n := doc.Page.Children[0]
	if !n.HasGap || n.Gap != 8 {
		t.Errorf("Gap = %v, HasGap = %v", n.Gap, n.HasGap)
	}
}

func TestParseSize(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Text Value="a" Width="Fill" Height="32"/></Page></Goxml>`)
	n := doc.Page.Children[0]
	if n.Width.Mode != contracts.SizeFill {
		t.Errorf("Width = %+v", n.Width)
	}
	if n.Height.Mode != contracts.SizeFixed || n.Height.Value != 32 {
		t.Errorf("Height = %+v", n.Height)
	}
}

func TestParseSizeRejectsAuto(t *testing.T) {
	// v1 明确不支持 Auto 尺寸（ADR-0008 §2.2）。
	mustFail(t, `<Goxml Version="1"><Page><Text Value="a" Width="Auto"/></Page></Goxml>`, CodeAttrCall)
}

func TestParseColumns(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Grid Columns="72 * 96"><Text Value="a"/></Grid></Page></Goxml>`)
	cols := doc.Page.Children[0].Columns
	if len(cols) != 3 {
		t.Fatalf("列数 = %d", len(cols))
	}
	if cols[0].Mode != contracts.GridColumnFixed || cols[0].Value != 72 {
		t.Errorf("col0 = %+v", cols[0])
	}
	if cols[1].Mode != contracts.GridColumnFlex || cols[1].Value != 1 {
		t.Errorf("col1 = %+v", cols[1])
	}
	if cols[2].Mode != contracts.GridColumnFixed || cols[2].Value != 96 {
		t.Errorf("col2 = %+v", cols[2])
	}
}

func TestParseColumnsWeighted(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Grid Columns="2* 1*"><Text Value="a"/></Grid></Page></Goxml>`)
	cols := doc.Page.Children[0].Columns
	if cols[0].Value != 2 || cols[1].Value != 1 {
		t.Errorf("cols = %+v", cols)
	}
}

func TestGridRequiresColumns(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Grid><Text Value="a"/></Grid></Page></Goxml>`, CodeAttrCall)
}

// ---------------------------------------------------------------------------
// 控制流
// ---------------------------------------------------------------------------

func TestParseIf(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><If Condition="{LoggedIn}">
      <Then><Text Value="yes"/></Then>
      <Else><Text Value="no"/></Else>
    </If></Page></Goxml>`)

	n := doc.Page.Children[0]
	if n.Kind != contracts.KindIf || n.If == nil {
		t.Fatalf("节点 = %+v", n)
	}
	if !n.If.Condition.IsBinding {
		t.Error("Condition 应为绑定")
	}
	if len(n.If.Then) != 1 || len(n.If.Else) != 1 {
		t.Errorf("Then=%d Else=%d", len(n.If.Then), len(n.If.Else))
	}
}

func TestIfRequiresThen(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><If Condition="{X}"><Else><Text Value="a"/></Else></If></Page></Goxml>`, CodeControl)
}

func TestIfConditionMustBeBinding(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><If Condition="true"><Then><Text Value="a"/></Then></If></Page></Goxml>`, CodeControl)
}

func TestIfRejectsDirectChild(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><If Condition="{X}"><Text Value="a"/></If></Page></Goxml>`, CodeControl)
}

func TestParseFor(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><For Each="item" In="{Items}" Index="i" Key="{item.ID}">
      <Text Value="{item.Name}"/>
    </For></Page></Goxml>`)

	n := doc.Page.Children[0]
	if n.Kind != contracts.KindFor || n.For == nil {
		t.Fatalf("节点 = %+v", n)
	}
	if n.For.Each != "item" || n.For.Index != "i" {
		t.Errorf("For = %+v", n.For)
	}
	if !n.For.HasKey || n.For.Key.Path() != "item.ID" {
		t.Errorf("Key = %v, HasKey = %v", n.For.Key.Path(), n.For.HasKey)
	}
}

func TestForStatefulRequiresKey(t *testing.T) {
	// 含 Button 的循环必须提供 Key（AOT Diff §5.1）。
	mustFail(t, `<Goxml Version="1"><Page><For Each="item" In="{Items}">
      <Button Text="{item.Name}"/>
    </For></Page></Goxml>`, CodeKey)
}

func TestForStatelessAllowsNoKey(t *testing.T) {
	mustParse(t, `<Goxml Version="1"><Page><For Each="item" In="{Items}">
      <Text Value="{item.Name}"/>
    </For></Page></Goxml>`)
}

func TestForVirtualizeRequiresItemExtent(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><For Each="i" In="{Items}" Virtualize="true">
      <Text Value="{i.name}"/>
    </For></Page></Goxml>`, CodeControl)
}

func TestForVirtualizeWithItemExtent(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><For Each="i" In="{Items}" Virtualize="true" ItemExtent="32">
      <Text Value="{i.name}"/>
    </For></Page></Goxml>`)
	n := doc.Page.Children[0]
	if !n.For.Virtualize || n.For.ItemExtent != 32 || !n.For.HasItemExtent {
		t.Errorf("For = %+v", n.For)
	}
}

// ---------------------------------------------------------------------------
// 事件
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Def / Slot
// ---------------------------------------------------------------------------

func TestParseDef(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1">
  <Defs>
    <Def Name="UserCard" Props="Name:string; Accent:color=$Theme.Primary">
      <Column Padding="12"><Text Value="{Name}"/><Slot/></Column>
    </Def>
  </Defs>
  <Page><UserCard Name="{User.Name}"><Text Value="x"/></UserCard></Page>
</Goxml>`)

	if len(doc.Defs) != 1 {
		t.Fatalf("Defs = %d", len(doc.Defs))
	}
	d := doc.Defs[0]
	if d.Kind != contracts.KindDef || d.Def == nil {
		t.Fatalf("Def 节点 = %+v", d)
	}
	if d.Def.Name != "UserCard" {
		t.Errorf("Name = %q", d.Def.Name)
	}
	if len(d.Def.Props) != 2 {
		t.Fatalf("Props = %d", len(d.Def.Props))
	}
	if d.Def.Props[0].Name != "Name" || d.Def.Props[0].Type != contracts.PropString {
		t.Errorf("prop0 = %+v", d.Def.Props[0])
	}
	if d.Def.Props[1].Type != contracts.PropColor || !d.Def.Props[1].HasDefault {
		t.Errorf("prop1 = %+v", d.Def.Props[1])
	}
	if !d.Def.HasSlot {
		t.Error("应检测到 <Slot/>")
	}
}

func TestDefRejectsReservedName(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Defs><Def Name="Text"><Text Value="a"/></Def></Defs><Page><Text Value="b"/></Page></Goxml>`, CodeDefName)
}

func TestDefRequiresSingleRoot(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Defs><Def Name="A"><Text Value="a"/><Text Value="b"/></Def></Defs><Page><Text Value="b"/></Page></Goxml>`, CodeDef)
}

func TestDefRejectsBadPropType(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Defs><Def Name="A" Props="X:number"><Text Value="a"/></Def></Defs><Page><Text Value="b"/></Page></Goxml>`, CodeDef)
}

// ---------------------------------------------------------------------------
// 文本节点与结构约束
// ---------------------------------------------------------------------------

func TestRejectsTextNode(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Column>裸文本</Column></Page></Goxml>`, CodeTextNode)
}

func TestSpacerRejectsChildren(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Column><Spacer><Text Value="a"/></Spacer></Column></Page></Goxml>`, CodeSyntax)
}

func TestExpandedRequiresOneChild(t *testing.T) {
	mustFail(t, `<Goxml Version="1"><Page><Column><Expanded/></Column></Page></Goxml>`, CodeSyntax)
}

func TestExpandedAcceptsOneChild(t *testing.T) {
	mustParse(t, `<Goxml Version="1"><Page><Column><Expanded><Text Value="a"/></Expanded></Column></Page></Goxml>`)
}

func TestRejectsProcInst(t *testing.T) {
	mustFail(t, `<?xml version="1.0"?><Goxml Version="1"><Page><Text Value="a"/></Page></Goxml>`, CodeSyntax)
}
