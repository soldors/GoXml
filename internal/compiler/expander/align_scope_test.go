package expander

import (
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ---------------------------------------------------------------------------
// v1 不提供 AlignSelf：出现即为未知属性
// ---------------------------------------------------------------------------

func TestAlignSelfRejectedAsUnknown(t *testing.T) {
	// v1 移除了 AlignSelf。Parser 不再识别它，Expander 也不在布局属性表里，
	// 因此它落到「未知名称」分支——这是正确的失败方式：
	// 用户会看到「unknown attribute or event "AlignSelf"」，
	// 而不是一个默默无效的属性。
	d := mustCode(t, wrap(`<Row><Text Value="a" AlignSelf="Center"/></Row>`), CodeUnknownName)
	if d.Message == "" {
		t.Error("诊断应包含消息")
	}
}

func TestAlignSelfOnContainerRejected(t *testing.T) {
	// 即使在容器上写也应被拒绝（它不是布局属性）。
	mustCode(t, wrap(`<Row AlignSelf="Center"><Text Value="a"/></Row>`), CodeUnknownName)
}

// ---------------------------------------------------------------------------
// 容器级 Align 仍然完整可用
// ---------------------------------------------------------------------------

func TestContainerAlignStillWorks(t *testing.T) {
	r := mustOK(t, wrap(`<Row Align="End"><Text Value="a"/></Row>`))
	if got := r.Doc.Page.Children[0].Align; got != contracts.AlignEnd {
		t.Errorf("Align = %v, want AlignEnd", got)
	}
}

func TestColumnAlignStillWorks(t *testing.T) {
	r := mustOK(t, wrap(`<Column Align="Center"><Text Value="a"/></Column>`))
	if got := r.Doc.Page.Children[0].Align; got != contracts.AlignCenter {
		t.Errorf("Align = %v, want AlignCenter", got)
	}
}

// ---------------------------------------------------------------------------
// Baseline 适用范围（规范 §3.1.3）
//
// 校验在 Parser 层完成（见 parser 的 TestBaselineRejectedOnColumn/Grid），
// 这里只确认合法用法能通过。
// ---------------------------------------------------------------------------

func TestBaselineAllowedOnRow(t *testing.T) {
	mustOK(t, wrap(`<Row Align="Baseline"><Text Value="a"/></Row>`))
}

// ---------------------------------------------------------------------------
// 用嵌套替代 AlignSelf 是可行路径（规范 §3.1.2 的建议）
// ---------------------------------------------------------------------------

func TestNestedLayoutReplacesAlignSelf(t *testing.T) {
	// 想让 Row 里的一个标签右对齐，用嵌套 Row + Justify 表达。
	r := mustOK(t, wrap(`
      <Row Gap="8">
        <Row Justify="End" Width="72"><Text Value="标签"/></Row>
        <Text Value="值" Width="Fill"/>
      </Row>`))

	outer := r.Doc.Page.Children[0]
	if outer.Tag != "Row" {
		t.Fatalf("外层 = %q", outer.Tag)
	}
	inner := outer.Children[0]
	if inner.Tag != "Row" || inner.Justify != contracts.JustifyEnd {
		t.Errorf("内层应为 Justify=End 的 Row，实际 %q / %v", inner.Tag, inner.Justify)
	}
}
