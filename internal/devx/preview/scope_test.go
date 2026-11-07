package preview

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// span 构造一个非零 SourceSpan（诊断位置必须可回指）。
func span(line, col int) contracts.SourceSpan {
	return contracts.SourceSpan{
		Start: contracts.Position{Line: line, Col: col},
		End:   contracts.Position{Line: line, Col: col + 5},
	}
}

func staticNode(tag string) *contracts.NodeIR {
	return &contracts.NodeIR{
		Kind: contracts.KindElement,
		Tag:  tag,
		Span: span(3, 2),
		Attrs: []contracts.Attr{
			{Name: "Value", Value: contracts.BindingVal{Raw: "静态"}},
		},
	}
}

// TestScopeGateAcceptsCleanStaticPage 纯静态树（含 Def 替换后的静态
// props、Slot 内容）不产生任何拒绝。
func TestScopeGateAcceptsCleanStaticPage(t *testing.T) {
	page := &contracts.NodeIR{Kind: contracts.KindPage, Tag: "Page"}
	col := &contracts.NodeIR{Kind: contracts.KindElement, Tag: "Column", Span: span(4, 3)}
	col.Children = []*contracts.NodeIR{staticNode("Text"), staticNode("Spacer")}
	page.Children = []*contracts.NodeIR{col}

	doc := &contracts.DocumentIR{Page: page}
	if out := checkStaticScope("x.gxml", doc); len(out) != 0 {
		t.Fatalf("纯静态树不应被拒：%v", out)
	}
	if out := checkStaticScope("x.gxml", nil); len(out) != 0 {
		t.Fatalf("nil 文档不应被拒：%v", out)
	}
}

// TestScopeGateBindingRejection 属性绑定 → PREVIEW-002。
// 这是纵深防御：现行标准前端已先拒无 VM 绑定；门禁独立于前端可测。
func TestScopeGateBindingRejection(t *testing.T) {
	n := staticNode("Text")
	n.Attrs = []contracts.Attr{{
		Name: "Value",
		Value: contracts.BindingVal{
			IsBinding: true,
			Raw:       "{Title}",
			Segments:  []string{"Title"},
		},
		Span: span(5, 10),
	}}
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{n},
	}}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 1 || out[0].Code != CodePreviewBind {
		t.Fatalf("应恰 1 条 PREVIEW-002，实得：%v", out)
	}
	if !strings.Contains(out[0].Message, "Value") || !strings.Contains(out[0].Message, "Title") {
		t.Fatalf("诊断应点名属性与路径：%v", out[0])
	}
	if out[0].Line != 5 {
		t.Fatalf("诊断位置应取属性 span（line 5），实得 %d", out[0].Line)
	}
}

// TestScopeGateEventRejection 事件绑定 → PREVIEW-003。
func TestScopeGateEventRejection(t *testing.T) {
	// 事件挂在非交互节点上（真实事件多在 Button 上，但那会先触发
	// PREVIEW-005；这里单独验证事件维度的判定与诊断形态）。
	n := staticNode("Text")
	n.Events = []contracts.EventBinding{{
		Name:   "Click",
		Method: "OnTap",
		Span:   span(6, 20),
	}}
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{n},
	}}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 1 || out[0].Code != CodePreviewEvent {
		t.Fatalf("应恰 1 条 PREVIEW-003，实得：%v", out)
	}
	if !strings.Contains(out[0].Message, "OnTap") {
		t.Fatalf("诊断应点名处理器方法：%v", out[0])
	}
}

// TestScopeGateThemeRefRejection $Theme 背景 → PREVIEW-004。
// 现行 codegen 对主题引用背景静默不绘制；预览必须显式拒绝。
func TestScopeGateThemeRefRejection(t *testing.T) {
	n := staticNode("Text")
	n.Background = &contracts.NRGBAColor{IsThemeRef: true, ThemeField: "Primary"}
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{n},
	}}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 1 || out[0].Code != CodePreviewTheme {
		t.Fatalf("应恰 1 条 PREVIEW-004，实得：%v", out)
	}
	if !strings.Contains(out[0].Message, "$Theme.Primary") && !strings.Contains(out[0].Message, "Primary") {
		t.Fatalf("诊断应点名主题字段：%v", out[0])
	}
}

// TestScopeGateLoopAndConditional For/If → PREVIEW-002（循环/条件文案）。
func TestScopeGateLoopAndConditional(t *testing.T) {
	forNode := &contracts.NodeIR{
		Kind: contracts.KindFor,
		Tag:  "For",
		Span: span(7, 4),
		For: &contracts.ForClause{
			Each: "item",
			In:   contracts.BindingVal{IsBinding: true, Segments: []string{"items"}},
		},
	}
	ifNode := &contracts.NodeIR{
		Kind: contracts.KindIf,
		Tag:  "If",
		Span: span(9, 4),
		If: &contracts.IfClause{
			Condition: contracts.BindingVal{IsBinding: true, Segments: []string{"flag"}},
		},
	}
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{forNode, ifNode},
	}}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 2 {
		t.Fatalf("For 与 If 各应 1 条，实得 %d 条：%v", len(out), out)
	}
	var loopMsg, condMsg bool
	for _, d := range out {
		switch {
		case strings.Contains(d.Message, "loops"):
			loopMsg = true
		case strings.Contains(d.Message, "conditionals"):
			condMsg = true
		}
	}
	if !loopMsg || !condMsg {
		t.Fatalf("循环/条件诊断应各自点名：%v", out)
	}
}

// TestScopeGateViewModelDefense 纵深防御：展开 IR 上的 ViewModel 声明
// 也要能被拒（正常流程 Run 在 pre-parse 已拦，这里是独立防线）。
func TestScopeGateViewModelDefense(t *testing.T) {
	doc := &contracts.DocumentIR{
		ViewModel: contracts.ViewModelDecl{Value: "vm.Model", Span: span(1, 20)},
		Page:      &contracts.NodeIR{Kind: contracts.KindPage},
	}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 1 || out[0].Code != CodePreviewVM {
		t.Fatalf("应恰 1 条 PREVIEW-001，实得：%v", out)
	}
}

// TestScopeGateInteractiveRejection 交互控件（Button/Input）→ PREVIEW-005。
// 镜像 codegen.isInteractiveTag：交互控件被现行生成语义判为动态页。
func TestScopeGateInteractiveRejection(t *testing.T) {
	for _, tag := range []string{"Button", "Input"} {
		n := staticNode(tag)
		doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
			Kind:     contracts.KindPage,
			Children: []*contracts.NodeIR{n},
		}}
		out := checkStaticScope("x.gxml", doc)
		if len(out) != 1 || out[0].Code != CodePreviewInteractive {
			t.Fatalf("%s 应恰 1 条 PREVIEW-005，实得：%v", tag, out)
		}
		if !strings.Contains(out[0].Message, tag) {
			t.Fatalf("诊断应点名标签 %s：%v", tag, out[0])
		}
	}
	// Def 实例克隆出的交互控件同样要被拒（Tag 保留原始标签）。
	clone := staticNode("Button")
	clone.Kind = contracts.KindElement
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{&contracts.NodeIR{Kind: contracts.KindElement, Tag: "Column", Children: []*contracts.NodeIR{clone}}},
	}}
	if out := checkStaticScope("x.gxml", doc); len(out) != 1 || out[0].Code != CodePreviewInteractive {
		t.Fatalf("克隆出的交互控件应被拒：%v", out)
	}
}

// TestScopeGateWalksNestedChildren 拒绝要能穿透任意深度的子树
// （含 Slot 替换内容与 Def 实例克隆）。
func TestScopeGateWalksNestedChildren(t *testing.T) {
	deep := staticNode("Text")
	deep.Attrs = []contracts.Attr{{
		Name:  "Value",
		Value: contracts.BindingVal{IsBinding: true, Segments: []string{"deep"}},
		Span:  span(11, 8),
	}}
	l3 := &contracts.NodeIR{Kind: contracts.KindElement, Tag: "Column", Children: []*contracts.NodeIR{deep}}
	l2 := &contracts.NodeIR{Kind: contracts.KindElement, Tag: "Row", Children: []*contracts.NodeIR{l3}}
	l1 := &contracts.NodeIR{Kind: contracts.KindElement, Tag: "Column", Children: []*contracts.NodeIR{l2}}
	doc := &contracts.DocumentIR{Page: &contracts.NodeIR{
		Kind:     contracts.KindPage,
		Children: []*contracts.NodeIR{l1},
	}}
	out := checkStaticScope("x.gxml", doc)
	if len(out) != 1 || out[0].Code != CodePreviewBind {
		t.Fatalf("深层绑定应被拒：%v", out)
	}
}
