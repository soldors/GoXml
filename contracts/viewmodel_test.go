package contracts

import "testing"

// ---------------------------------------------------------------------------
// ViewModelDecl：Parser 保留声明，Analyzer 解析类型
// ---------------------------------------------------------------------------

func TestViewModelDeclIsZero(t *testing.T) {
	var zero ViewModelDecl
	if !zero.IsZero() {
		t.Error("零值应判为缺省")
	}

	// 只有 Value 有内容 → 不是零值。
	withValue := ViewModelDecl{Value: "AppState"}
	if withValue.IsZero() {
		t.Error("有 Value 时不应判为缺省")
	}

	// 只有 Span 有内容 → 也不算零值（说明用户写了，只是文本为空）。
	withSpan := ViewModelDecl{Span: SourceSpan{
		Start: Position{Line: 1, Col: 1},
		End:   Position{Line: 1, Col: 10},
	}}
	if withSpan.IsZero() {
		t.Error("有 Span 时不应判为缺省")
	}
}

func TestViewModelDeclString(t *testing.T) {
	d := ViewModelDecl{Value: "internal/models.User"}
	if got := d.String(); got != "internal/models.User" {
		t.Errorf("String() = %q", got)
	}
	var zero ViewModelDecl
	if got := zero.String(); got != "" {
		t.Errorf("零值 String() = %q, 期望空串", got)
	}
}

func TestViewModelDeclKeepsRawText(t *testing.T) {
	// 合约：Parser 不解析文法，只保留原文。
	// 即使写法在 Analyzer 阶段会被拒绝，Value 也原样保存。
	raw := "example.com/app/demo.ViewModel"
	d := ViewModelDecl{Value: raw, Span: SourceSpan{}}
	if d.Value != raw {
		t.Errorf("Value = %q, 期望原样保留 %q", d.Value, raw)
	}
}

func TestDocumentIRViewModelDefaultsToZero(t *testing.T) {
	// 纯静态页面的 DocumentIR 不应有 ViewModel。
	var doc DocumentIR
	if !doc.ViewModel.IsZero() {
		t.Error("新建 DocumentIR 的 ViewModel 应为零值")
	}
}
