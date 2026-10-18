package parser

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// P0-1：v1 不提供 AlignSelf（子项级对齐），只保留容器级 Align
// ---------------------------------------------------------------------------

func TestAlignSelfIsNotLayoutAttr(t *testing.T) {
	// v1 移除了 AlignSelf：Gio 的 FlexChild 没有对齐字段，实现它需要额外
	// 包一层 Stack，等价于「用嵌套换嵌套」，净收益为零且破坏静态子树固化。
	//
	// Parser 不再把它识别为布局属性，因此它原样进入 RawAttrs，
	// 由 Expander 按「未知属性」拒绝（见 expander 的对应测试）。
	doc := mustParse(t, wrapSrc(`<Row><Text Value="a" AlignSelf="Center"/></Row>`))
	n := doc.Page.Children[0].Children[0]

	if _, ok := n.RawAttrs.Lookup("AlignSelf"); !ok {
		t.Error("AlignSelf 应作为未分类属性进入 RawAttrs")
	}
	// 关键：不能被解析进任何专用字段。
	if n.Align != contracts.AlignUnset {
		t.Errorf("Align 不应被 AlignSelf 影响，实际 %v", n.Align)
	}
}

func TestAlignSelfDoesNotOverrideAlign(t *testing.T) {
	// 容器级 Align 仍正常工作，与子项写法无关。
	doc := mustParse(t, wrapSrc(`<Row Align="End"><Text Value="a"/></Row>`))
	if got := doc.Page.Children[0].Align; got != contracts.AlignEnd {
		t.Errorf("Align = %v, want AlignEnd", got)
	}
}

// ---------------------------------------------------------------------------
// P0-3：Baseline 的适用范围收紧到 <Row>
// ---------------------------------------------------------------------------

func TestBaselineAllowedOnRow(t *testing.T) {
	mustParse(t, wrapSrc(`<Row Align="Baseline"><Text Value="a"/></Row>`))
}

func TestBaselineRejectedOnColumn(t *testing.T) {
	mustFail(t, wrapSrc(`<Column Align="Baseline"><Text Value="a"/></Column>`), CodeAttrCall)
}

func TestBaselineRejectedOnGrid(t *testing.T) {
	mustFail(t, wrapSrc(`<Grid Columns="*" Align="Baseline"><Text Value="a"/></Grid>`), CodeAttrCall)
}

func TestBaselineErrorMessageNamesTarget(t *testing.T) {
	_, ds := Parse("t.gxml", []byte(wrapSrc(`<Column Align="Baseline"><Text Value="a"/></Column>`)))
	if len(ds) == 0 {
		t.Fatal("期望诊断")
	}
	msg := ds[0].Message
	if !contains(msg, "Row") || !contains(msg, "Column") {
		t.Errorf("诊断应指明仅 Row 可用、当前是 Column: %q", msg)
	}
}

// ---------------------------------------------------------------------------
// P0-2：Fill 是语言层语义，IR 不应把它绑定到某个具体布局调用
// ---------------------------------------------------------------------------

func TestSizeFillIsSemanticNotConcrete(t *testing.T) {
	doc := mustParse(t, wrapSrc(`<Row><Text Value="a" Width="Fill"/></Row>`))
	w := doc.Page.Children[0].Children[0].Width
	if w.Mode != contracts.SizeFill {
		t.Fatalf("Width = %+v", w)
	}
	// FillSpec 只表达「占满」，不携带任何具体布局参数。
	// 若将来有人给 SizeSpec 加了 Flexed 之类的字段，这个断言会提醒更新规范。
	if w.Value != 0 {
		t.Errorf("SizeFill 不应携带固定数值，实际 Value=%v", w.Value)
	}
}

func TestFillSizeHelper(t *testing.T) {
	if got := contracts.FillSize(); got.Mode != contracts.SizeFill || got.IsSet() != true {
		t.Errorf("FillSize() = %+v", got)
	}
}

// ---------------------------------------------------------------------------
// 尺寸数值格式（规范 §3.1.5）
// ---------------------------------------------------------------------------

func TestSizeAcceptsDecimal(t *testing.T) {
	// 规范允许小数（如 Width="80.5"）。
	doc := mustParse(t, wrapSrc(`<Text Value="a" Width="80.5"/>`))
	w := doc.Page.Children[0].Width
	if w.Mode != contracts.SizeFixed || w.Value != 80.5 {
		t.Errorf("Width = %+v", w)
	}
}

func TestSizeAcceptsZero(t *testing.T) {
	doc := mustParse(t, wrapSrc(`<Text Value="a" Height="0"/>`))
	h := doc.Page.Children[0].Height
	if h.Mode != contracts.SizeFixed || h.Value != 0 {
		t.Errorf("Height = %+v", h)
	}
}

func TestSizeRejectsNegativeAndGarbage(t *testing.T) {
	for _, bad := range []string{"-1", "1e5", "80px", "50%", "abc"} {
		src := wrapSrc(`<Text Value="a" Width="` + bad + `"/>`)
		_, ds := Parse("t.gxml", []byte(src))
		if len(ds) == 0 {
			t.Errorf("Width=%q 应被拒绝", bad)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
