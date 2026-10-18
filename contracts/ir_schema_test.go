package contracts

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CompilerError：格式必须满足 GXML 语言规范 §8 的 file:line:col: CODE: message
// ---------------------------------------------------------------------------

func TestCompilerErrorFormat(t *testing.T) {
	err := NewError("ui/dashboard.gxml",
		SourceSpan{Start: Position{Line: 12, Col: 18}},
		"GXML-BIND-001", "unknown binding path %q", "User.Nmae")

	got := err.Error()
	want := `ui/dashboard.gxml:12:18: GXML-BIND-001: unknown binding path "User.Nmae"`
	if got != want {
		t.Fatalf("Error() = %q\nwant        %q", got, want)
	}
}

func TestCompilerErrorWithoutCode(t *testing.T) {
	err := CompilerError{File: "a.gxml", Line: 1, Col: 2, Message: "boom"}
	if got, want := err.Error(), "a.gxml:1:2: boom"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestCompilerErrorSpanRoundTrip(t *testing.T) {
	span := SourceSpan{
		Start: Position{Line: 3, Col: 7},
		End:   Position{Line: 3, Col: 19},
	}
	err := NewError("x.gxml", span, "GXML-ATTR-002", "bad value")

	if got := err.Span(); got != span {
		t.Fatalf("Span() = %+v, want %+v", got, span)
	}
	// 错误必须能被当作 error 使用。
	var _ error = err
}

// ---------------------------------------------------------------------------
// SourceSpan
// ---------------------------------------------------------------------------

func TestSourceSpanIsZero(t *testing.T) {
	if !(SourceSpan{}).IsZero() {
		t.Fatal("空 span 应为 IsZero")
	}
	if (SourceSpan{Start: Position{Line: 1, Col: 1}}).IsZero() {
		t.Fatal("有起始位置的 span 不应为 IsZero")
	}
}

func TestSourceSpanString(t *testing.T) {
	cases := []struct {
		span SourceSpan
		want string
	}{
		{SourceSpan{}, "?"},
		{SourceSpan{Start: Position{5, 2}}, "5:2"},
		{SourceSpan{Start: Position{5, 2}, End: Position{5, 8}}, "5:2-5:8"},
	}
	for _, c := range cases {
		if got := c.span.String(); got != c.want {
			t.Errorf("%+v.String() = %q, want %q", c.span, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// EdgeInsets：1/2/4 参数规则的语义载体
// ---------------------------------------------------------------------------

func TestEdgeInsetsIsZero(t *testing.T) {
	if !(EdgeInsets{}).IsZero() {
		t.Fatal("零值 EdgeInsets 应为 IsZero")
	}
	if (EdgeInsets{Left: 1}).IsZero() {
		t.Fatal("有任一边非零时不应为 IsZero")
	}
}

// ---------------------------------------------------------------------------
// BindingVal：绑定路径必须是分段存储，不能只留整串
// ---------------------------------------------------------------------------

func TestBindingValPath(t *testing.T) {
	b := BindingVal{
		IsBinding: true,
		Segments:  []string{"User", "Profile", "Name"},
	}
	if got, want := b.Path(), "User.Profile.Name"; got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestBindingValStaticLiteralPath(t *testing.T) {
	b := BindingVal{Raw: "保存"}
	if got, want := b.Path(), "保存"; got != want {
		t.Fatalf("静态值 Path() = %q, want %q", got, want)
	}
}

func TestBindingValIsZero(t *testing.T) {
	if !(BindingVal{}).IsZero() {
		t.Fatal("零值 BindingVal 应为 IsZero")
	}
	if (BindingVal{Raw: "x"}).IsZero() {
		t.Fatal("有 Raw 的 BindingVal 不应为 IsZero")
	}
	if (BindingVal{IsBinding: true}).IsZero() {
		t.Fatal("IsBinding 为真时不应为 IsZero")
	}
}

// ---------------------------------------------------------------------------
// NodeKind
// ---------------------------------------------------------------------------

func TestNodeKindString(t *testing.T) {
	cases := map[NodeKind]string{
		KindUnknown: "Unknown",
		KindGoxml:   "Goxml",
		KindDefs:    "Defs",
		KindDef:     "Def",
		KindSlot:    "Slot",
		KindPage:    "Page",
		KindElement: "Element",
		KindIf:      "If",
		KindFor:     "For",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("NodeKind(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestNodeKindIsStructural(t *testing.T) {
	// 只在编译期存在、不直接渲染的种类。
	structural := []NodeKind{KindDefs, KindDef, KindIf, KindFor, KindSlot}
	for _, k := range structural {
		if !k.IsStructural() {
			t.Errorf("%v 应为结构节点", k)
		}
	}
	// 产生直接渲染的种类。
	rendering := []NodeKind{KindUnknown, KindGoxml, KindPage, KindElement}
	for _, k := range rendering {
		if k.IsStructural() {
			t.Errorf("%v 不应为结构节点", k)
		}
	}
}

// ---------------------------------------------------------------------------
// AttrList
// ---------------------------------------------------------------------------

func TestAttrListLookup(t *testing.T) {
	list := AttrList{
		{Name: "Value", Value: BindingVal{Raw: "hi"}},
		{Name: "Color", Value: BindingVal{IsBinding: true, Segments: []string{"Theme", "Primary"}}},
	}

	a, ok := list.Lookup("Value")
	if !ok || a.Value.Raw != "hi" {
		t.Fatalf("Lookup(Value) = %+v, %v", a, ok)
	}
	if !list.Has("Color") {
		t.Fatal("Has(Color) 应为 true")
	}
	if _, ok := list.Lookup("Missing"); ok {
		t.Fatal("不存在的属性不应返回 ok")
	}
	// 名称区分大小写。
	if list.Has("value") {
		t.Fatal("属性名应区分大小写")
	}
}

// ---------------------------------------------------------------------------
// ForClause：Key 缺失是状态串位的根因，必须可判别
// ---------------------------------------------------------------------------

func TestForClauseKeyPresence(t *testing.T) {
	// Key 零值本身无法区分「未提供」与「提供了空值」，必须依赖 HasKey。
	without := ForClause{Each: "item", In: BindingVal{IsBinding: true, Segments: []string{"Items"}}}
	if without.HasKey {
		t.Fatal("未提供 Key 时 HasKey 应为 false")
	}

	with := ForClause{
		Each:   "item",
		In:     BindingVal{IsBinding: true, Segments: []string{"Items"}},
		Key:    BindingVal{IsBinding: true, Segments: []string{"item", "ID"}},
		HasKey: true,
	}
	if !with.HasKey {
		t.Fatal("提供 Key 时 HasKey 应为 true")
	}
	if got, want := with.Key.Path(), "item.ID"; got != want {
		t.Fatalf("Key.Path() = %q, want %q", got, want)
	}
}

func TestForClauseVirtualizeDefaults(t *testing.T) {
	// v1 基线：未显式开启虚拟化时行为不变。
	f := ForClause{Each: "item"}
	if f.Virtualize {
		t.Fatal("Virtualize 缺省应为 false")
	}
	if f.HasItemExtent {
		t.Fatal("ItemExtent 缺省应未设置")
	}
}

// ---------------------------------------------------------------------------
// PropType：Def Props 类型解析
// ---------------------------------------------------------------------------

func TestParsePropType(t *testing.T) {
	cases := map[string]PropType{
		"string":   PropString,
		"bool":     PropBool,
		"int":      PropInt,
		"float":    PropFloat,
		"color":    PropColor,
		"insets":   PropInsets,
		" string ": PropString,  // 允许首尾空白
		"number":   PropUnknown, // v1 不支持的类型
		"":         PropUnknown,
	}
	for in, want := range cases {
		if got := ParsePropType(in); got != want {
			t.Errorf("ParsePropType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPropTypeString(t *testing.T) {
	if got := PropColor.String(); got != "color" {
		t.Fatalf("PropColor.String() = %q", got)
	}
	if got := PropUnknown.String(); got != "unknown" {
		t.Fatalf("PropUnknown.String() = %q", got)
	}
}

// ---------------------------------------------------------------------------
// NodeIR 辅助方法
// ---------------------------------------------------------------------------

func TestNodeIRAttrAndEvent(t *testing.T) {
	n := &NodeIR{
		Kind:  KindElement,
		Tag:   "Button",
		Attrs: AttrList{{Name: "Text", Value: BindingVal{Raw: "保存"}}},
		Events: []EventBinding{
			{Name: "Click", Method: "Save"},
			{Name: "Hover", Method: "OnHover"},
		},
	}

	if a, ok := n.Attr("Text"); !ok || a.Value.Raw != "保存" {
		t.Fatalf("Attr(Text) = %+v, %v", a, ok)
	}
	if e, ok := n.Event("Click"); !ok || e.Method != "Save" {
		t.Fatalf("Event(Click) = %+v, %v", e, ok)
	}
	if _, ok := n.Event("click"); ok {
		t.Fatal("事件名应区分大小写")
	}
}

func TestNodeIRNilSafety(t *testing.T) {
	var n *NodeIR
	if _, ok := n.Attr("x"); ok {
		t.Fatal("nil 节点 Attr 应返回 false")
	}
	if _, ok := n.Event("x"); ok {
		t.Fatal("nil 节点 Event 应返回 false")
	}
}

// ---------------------------------------------------------------------------
// 诊断码前缀：保证错误码格式可被工具稳定匹配
// ---------------------------------------------------------------------------

func TestDiagnosticCodePrefixes(t *testing.T) {
	prefixes := []string{
		CodePrefixSyntax, CodePrefixProp, CodePrefixBind,
		CodePrefixEvent, CodePrefixDef, CodePrefixScope, CodePrefixRoot,
	}
	seen := make(map[string]bool, len(prefixes))
	for _, p := range prefixes {
		if !strings.HasPrefix(p, "GXML-") {
			t.Errorf("错误码前缀 %q 应以 GXML- 开头", p)
		}
		if seen[p] {
			t.Errorf("错误码前缀 %q 重复", p)
		}
		seen[p] = true
	}
}
