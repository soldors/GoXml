package parser

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// 文档级错误路径（parseDocument 45.5%）
// ---------------------------------------------------------------------------

func TestEmptyDocument(t *testing.T) {
	_, ds := Parse("t.gxml", nil)
	if len(ds) == 0 {
		t.Fatal("空文档应报错")
	}
}

func TestWhitespaceOnlyDocument(t *testing.T) {
	_, ds := Parse("t.gxml", []byte("  \n\t  \n"))
	if len(ds) == 0 {
		t.Fatal("仅空白应报错")
	}
}

func TestTextBeforeRoot(t *testing.T) {
	_, ds := Parse("t.gxml", []byte("stray<Goxml Version=\"1\"><Page><Text Value=\"a\"/></Page></Goxml>"))
	if len(ds) == 0 {
		t.Fatal("根节点前的文本应报错")
	}
	if !strings.Contains(ds[0].Message, "unexpected text") {
		t.Errorf("诊断 = %q", ds[0].Message)
	}
}

func TestCommentBeforeRootAllowed(t *testing.T) {
	mustParse(t, `<!-- 注释 -->
<Goxml Version="1"><Page><Text Value="a"/></Page></Goxml>`)
}

func TestDirectiveRejected(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(`<!DOCTYPE foo><Goxml Version="1"><Page><Text Value="a"/></Page></Goxml>`))
	if len(ds) == 0 {
		t.Fatal("DOCTYPE 指令应被拒绝")
	}
}

// ---------------------------------------------------------------------------
// parseNode 分派错误路径（40%）
// ---------------------------------------------------------------------------

func TestThenOutsideIf(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Then><Text Value="a"/></Then>`)))
	if len(ds) == 0 {
		t.Fatal("<Then> 在 <If> 外应报错")
	}
	if ds[0].Code != CodeControl {
		t.Errorf("诊断码 = %s, 期望 %s", ds[0].Code, CodeControl)
	}
}

func TestElseOutsideIf(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Else><Text Value="a"/></Else>`)))
	if len(ds) == 0 {
		t.Fatal("<Else> 在 <If> 外应报错")
	}
}

func TestDefOutsideDefs(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Def Name="A"><Text Value="a"/></Def>`)))
	if len(ds) == 0 {
		t.Fatal("<Def> 在 <Defs> 外应报错")
	}
}

func TestNestedPageRejected(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Page><Text Value="a"/></Page>`)))
	if len(ds) == 0 {
		t.Fatal("嵌套 <Page> 应报错")
	}
}

func TestNestedGoxmlRejected(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Goxml Version="1"/>`)))
	if len(ds) == 0 {
		t.Fatal("嵌套 <Goxml> 应报错")
	}
}

// ---------------------------------------------------------------------------
// parseDefs 错误路径（66.7%）
// ---------------------------------------------------------------------------

func TestDefsRejectsAttributes(t *testing.T) {
	src := `<Goxml Version="1"><Defs Bad="1"><Def Name="A"><Text Value="a"/></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("<Defs> 带属性应报错")
	}
}

func TestDefsRejectsNonDefChild(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Text Value="a"/></Defs><Page><Text Value="b"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("<Defs> 内非 <Def> 应报错")
	}
}

func TestDefsRequiresAtLeastOneDef(t *testing.T) {
	src := `<Goxml Version="1"><Defs></Defs><Page><Text Value="a"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("空 <Defs> 应报错")
	}
}

// ---------------------------------------------------------------------------
// parseDef 错误路径（68.9%）
// ---------------------------------------------------------------------------

func TestDefRejectsUnknownAttr(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="A" Bogus="1"><Text Value="a"/></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("未知 Def 属性应报错")
	}
}

func TestDefRequiresName(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def><Text Value="a"/></Def></Defs><Page><Text Value="b"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("缺少 Name 应报错")
	}
}

func TestDefRejectsBadName(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="1Bad"><Text Value="a"/></Def></Defs><Page><Text Value="b"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("非法 Name 应报错")
	}
}

func TestDefRejectsPropsBinding(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="A" Props="{X}"><Text Value="a"/></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("Props 为绑定应报错")
	}
}

func TestDefRejectsMultipleSlots(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="A"><Column><Slot/><Slot/></Column></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("多个 <Slot/> 应报错")
	}
}

func TestDefSlotWithChildrenRejected(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="A"><Column><Slot><Text Value="x"/></Slot></Column></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("<Slot> 带子节点应报错")
	}
}

func TestDefSlotWithAttributesRejected(t *testing.T) {
	src := `<Goxml Version="1"><Defs><Def Name="A"><Column><Slot Name="x"/></Column></Def></Defs><Page><A/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("<Slot> 带属性应报错")
	}
}

// ---------------------------------------------------------------------------
// parsePage 错误路径（66.7%）
// ---------------------------------------------------------------------------

func TestPageRejectsUnknownAttr(t *testing.T) {
	src := `<Goxml Version="1"><Page Bogus="1"><Text Value="a"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("Page 未知属性应报错")
	}
}

func TestPageRejectsRouteBinding(t *testing.T) {
	src := `<Goxml Version="1"><Page Route="{R}"><Text Value="a"/></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("Route 为绑定应报错")
	}
}

func TestPageRequiresChildren(t *testing.T) {
	src := `<Goxml Version="1"><Page></Page></Goxml>`
	_, ds := Parse("t.gxml", []byte(src))
	if len(ds) == 0 {
		t.Fatal("空 Page 应报错")
	}
}

// ---------------------------------------------------------------------------
// parseFor 错误路径（61%）
// ---------------------------------------------------------------------------

func TestForRequiresEach(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For In="{Items}"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("缺少 Each 应报错")
	}
}

func TestForRequiresIn(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("缺少 In 应报错")
	}
}

func TestForInMustBeBinding(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i" In="Items"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("In 非绑定应报错")
	}
}

func TestForRejectsUnknownAttr(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i" In="{X}" Bogus="1"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("For 未知属性应报错")
	}
}

func TestForRejectsBadVirtualize(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i" In="{X}" Virtualize="yes"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("Virtualize 非布尔应报错")
	}
}

func TestForRejectsZeroItemExtent(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i" In="{X}" Virtualize="true" ItemExtent="0"><Text Value="a"/></For>`)))
	if len(ds) == 0 {
		t.Fatal("ItemExtent 为 0 应报错")
	}
}

func TestForRequiresChildren(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<For Each="i" In="{X}"></For>`)))
	if len(ds) == 0 {
		t.Fatal("空 For 应报错")
	}
}

// ---------------------------------------------------------------------------
// 其它
// ---------------------------------------------------------------------------

func TestParseDpRejectsBinding(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(`<Goxml Version="1"><Page><Row Gap="{G}"><Text Value="a"/></Row></Page></Goxml>`))
	if len(ds) == 0 {
		t.Fatal("Gap 为绑定应报错")
	}
}

func TestParseSizeRejectsBinding(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Text Value="a" Width="{W}"/>`)))
	if len(ds) == 0 {
		t.Fatal("Width 为绑定应报错")
	}
}

func TestParseSizeRejectsNegative(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Text Value="a" Height="-5"/>`)))
	if len(ds) == 0 {
		t.Fatal("负高度应报错")
	}
}

func TestGridColumnsRejectsBinding(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Grid Columns="{C}"><Text Value="a"/></Grid>`)))
	if len(ds) == 0 {
		t.Fatal("Columns 为绑定应报错")
	}
}

func TestGridColumnsRejectsZeroWeight(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Grid Columns="0*"><Text Value="a"/></Grid>`)))
	if len(ds) == 0 {
		t.Fatal("0* 权值应报错")
	}
}

func TestGridColumnsRejectsGarbage(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Grid Columns="abc"><Text Value="a"/></Grid>`)))
	if len(ds) == 0 {
		t.Fatal("非法列定义应报错")
	}
}

func TestGridColumnsEmpty(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Grid Columns=""><Text Value="a"/></Grid>`)))
	if len(ds) == 0 {
		t.Fatal("空 Columns 应报错")
	}
}

func TestSpacerRejectsNonSizeAttrs(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Column><Spacer Padding="8"/></Column>`)))
	if len(ds) == 0 {
		t.Fatal("<Spacer> 只接受 Width/Height")
	}
}

// wrapSrc 把片段包成完整文档。
func wrapSrc(body string) string {
	return `<Goxml Version="1"><Page>` + body + `</Page></Goxml>`
}

// 确保 contracts 包被使用（SourceSpan 断言）。
var _ = contracts.SourceSpan{}
