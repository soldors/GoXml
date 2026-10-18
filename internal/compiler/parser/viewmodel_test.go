package parser

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// ViewModelDecl：Parser 只保留声明，不解析 Go 类型
// ---------------------------------------------------------------------------

func TestViewModelDeclKeepsRawText(t *testing.T) {
	// Parser 不校验文法：即使写法在 Analyzer 阶段会被拒绝，
	// Parser 也只负责原样保留，让 Analyzer 报出精确诊断。
	cases := []string{
		"AppState",
		"demo.ViewModel",
		"internal/models.User",
		"example.com/app/demo.ViewModel", // 文法非法，但 Parser 不判断
		"",
	}
	for _, v := range cases {
		src := `<Goxml Version="1" ViewModel="` + v + `"><Page><Text Value="a"/></Page></Goxml>`
		doc := mustParse(t, src)
		if doc.ViewModel.Value != v {
			t.Errorf("ViewModel.Value = %q, 期望 %q", doc.ViewModel.Value, v)
		}
	}
}

func TestViewModelDeclSpanPointsToAttribute(t *testing.T) {
	// Span 必须覆盖属性本身，否则 ROOT-002 只能笼统指向文档根。
	src := "<Goxml Version=\"1\" ViewModel=\"demo.ViewModel\">\n" +
		"  <Page><Text Value=\"a\"/></Page>\n" +
		"</Goxml>"
	doc := mustParse(t, src)

	sp := doc.ViewModel.Span
	if sp.IsZero() {
		t.Fatal("Span 不应为零值")
	}
	if sp.Start.Line != 1 {
		t.Errorf("起始行 = %d, 期望 1", sp.Start.Line)
	}
	// 第 1 行：`<Goxml Version="1" ViewModel=...`
	//   <Goxml␣        → 第 1–7 列
	//   Version="1"    → 第 8–18 列
	//   空格           → 第 19 列
	//   ViewModel      → 第 20 列起
	if sp.Start.Col != 20 {
		t.Errorf("起始列 = %d, 期望 20", sp.Start.Col)
	}
}

func TestViewModelDeclAbsentIsZero(t *testing.T) {
	doc := mustParse(t, wrapSrc(`<Text Value="a"/>`))
	if !doc.ViewModel.IsZero() {
		t.Errorf("缺省时应为零值，实际 %+v", doc.ViewModel)
	}
	if doc.ViewModel.Value != "" {
		t.Errorf("Value = %q", doc.ViewModel.Value)
	}
}

func TestViewModelDeclRejectsBinding(t *testing.T) {
	// 绑定形式不是静态声明，Parser 直接拒绝。
	mustFail(t, `<Goxml Version="1" ViewModel="{VM}"><Page><Text Value="a"/></Page></Goxml>`, CodeAttrCall)
}

func TestViewModelDeclString(t *testing.T) {
	doc := mustParse(t, `<Goxml Version="1" ViewModel="demo.ViewModel"><Page><Text Value="a"/></Page></Goxml>`)
	if got := doc.ViewModel.String(); got != "demo.ViewModel" {
		t.Errorf("String() = %q", got)
	}
}

// ---------------------------------------------------------------------------
// 纯静态页面：无 ViewModel 仍合法
// ---------------------------------------------------------------------------

func TestStaticPageWithoutViewModel(t *testing.T) {
	doc := mustParse(t, wrapSrc(`<Column><Text Value="标题"/></Column>`))
	if !doc.ViewModel.IsZero() {
		t.Error("纯静态页面不应有 ViewModel")
	}
	if doc.Page == nil {
		t.Error("页面应存在")
	}
}

// 确保契约类型可用（编译期检查）。
var _ contracts.ViewModelDecl = contracts.ViewModelDecl{}
