package parser

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// 颜色解析（规范 §4.4）
// ---------------------------------------------------------------------------

func TestParseColorHex6(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Column Background="#FF8000"><Text Value="a"/></Column></Page></Goxml>`)
	bg := doc.Page.Children[0].Background
	if bg == nil {
		t.Fatal("Background 为空")
	}
	if bg.R != 0xFF || bg.G != 0x80 || bg.B != 0x00 || bg.A != 0xFF {
		t.Errorf("Background = %+v", bg)
	}
}

func TestParseColorHex8(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Column Background="#FF800080"><Text Value="a"/></Column></Page></Goxml>`)
	bg := doc.Page.Children[0].Background
	if bg.R != 0xFF || bg.G != 0x80 || bg.B != 0x00 || bg.A != 0x80 {
		t.Errorf("Background = %+v", bg)
	}
}

func TestParseColorThemeRef(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Column Background="$Theme.Primary"><Text Value="a"/></Column></Page></Goxml>`)
	bg := doc.Page.Children[0].Background
	if !bg.IsThemeRef || bg.ThemeField != "Primary" {
		t.Errorf("Background = %+v", bg)
	}
}

func TestParseColorRejects(t *testing.T) {
	cases := []string{
		`Background="#FFF"`,    // 3 位不支持
		`Background="#GGGGGG"`, // 非十六进制
		`Background="red"`,     // 命名色不支持
		`Background="$Theme."`, // 空字段
	}
	for _, c := range cases {
		src := `<Goxml Version="1"><Page><Column ` + c + `><Text Value="a"/></Column></Page></Goxml>`
		mustFail(t, src, CodeAttrCall)
	}
}

// TestParseColorAcceptsBinding 说明为什么颜色属性允许绑定语法。
//
// <Def> 内 Background="{Accent}" 是对 color 类型 Prop 的引用，
// Parser 不知道哪些标识符是 Prop（那是 Expander 的知识），因此放行；
// 展开后若仍是绑定，由 Expander 报「颜色属性不接受任意绑定」。
func TestParseColorAcceptsBinding(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Column Background="{User.Color}"><Text Value="a"/></Column></Page></Goxml>`)
	col := doc.Page.Children[0]
	if col.Background != nil {
		t.Error("绑定形式的颜色不应在 Parser 阶段解析出具体颜色")
	}
	ra, ok := col.RawAttrs.Lookup("Background")
	if !ok || !ra.Value.IsBinding {
		t.Fatalf("绑定应保留在 RawAttrs 中: %+v", ra)
	}
}

// ---------------------------------------------------------------------------
// <Def Events> 声明（ADR-0008 之后新增）
// ---------------------------------------------------------------------------

func TestParseDefEventsNoParams(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string" Events="Click; Open"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Click="Go"/></Page>
</Goxml>`)

	d := doc.Defs[0]
	if len(d.Def.Events) != 2 {
		t.Fatalf("Events = %d", len(d.Def.Events))
	}
	if d.Def.Events[0].Name != "Click" || len(d.Def.Events[0].Params) != 0 {
		t.Errorf("event0 = %+v", d.Def.Events[0])
	}
}

func TestParseDefEventsWithParams(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string" Events="Open(id:string, count:int)"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Open="Go"/></Page>
</Goxml>`)

	ev := doc.Defs[0].Def.Events[0]
	if ev.Name != "Open" || len(ev.Params) != 2 {
		t.Fatalf("event = %+v", ev)
	}
	if ev.Params[0].Name != "id" || ev.Params[0].Type != contracts.PropString {
		t.Errorf("param0 = %+v", ev.Params[0])
	}
	if ev.Params[1].Name != "count" || ev.Params[1].Type != contracts.PropInt {
		t.Errorf("param1 = %+v", ev.Params[1])
	}
}

func TestParseDefEventsEmptyParens(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1">
  <Defs><Def Name="Card" Props="T:string" Events="Click()"><Text Value="{T}"/></Def></Defs>
  <Page><Card T="x" Click="Go"/></Page>
</Goxml>`)
	if len(doc.Defs[0].Def.Events[0].Params) != 0 {
		t.Error("空括号应表示无参数")
	}
}

func TestParseDefEventsRejects(t *testing.T) {
	cases := []string{
		`Events=""`,                        // 必须至少一个
		`Events="1Click"`,                  // 非法标识符
		`Events="Open(id)"`,                // 缺少类型
		`Events="Open(id:unknown)"`,        // 未知类型
		`Events="Open(id:string"`,          // 缺右括号
		`Events="Open(id:string, id:int)"`, // 重复参数
	}
	for _, c := range cases {
		src := `<Goxml Version="1"><Defs><Def Name="Card" Props="T:string" ` + c + `><Text Value="{T}"/></Def></Defs><Page><Card T="x"/></Page></Goxml>`
		_, ds := Parse("t.gxml", []byte(src))
		if len(ds) == 0 {
			t.Errorf("%s 应被拒绝", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Parser 不做分类：所有属性（含布局属性）都进入 RawAttrs
// ---------------------------------------------------------------------------

func TestRawAttrsIncludesLayoutAttrs(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1"><Page><Column Padding="8" Gap="4"><Text Value="a"/></Column></Page></Goxml>`)
	col := doc.Page.Children[0]

	// 布局属性既被解析进专用字段，也保留在 RawAttrs 供 Expander 做适用性检查。
	if col.Padding.IsZero() {
		t.Error("Padding 应已解析")
	}
	if _, ok := col.RawAttrs.Lookup("Padding"); !ok {
		t.Error("Padding 应同时存在于 RawAttrs")
	}
	if _, ok := col.RawAttrs.Lookup("Gap"); !ok {
		t.Error("Gap 应存在于 RawAttrs")
	}
}
