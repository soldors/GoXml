package analyzer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// R3 切片3 补充：真实 Analyze 入口的集成验证（GXML 输入即 DocumentIR）。
// 不改 analyze.go；仅从公开 Analyze 入口验证：
//   - 包加载失败时 Status=Blocked、只报一条 ROOT-002、保留 GXML span、消息含真实 Go 文件:行:列；
//   - 修好源码后同一 env 再分析可恢复 Status=OK 并拿到生成准入（Verified）。
func TestAnalyzeLoadFailurePositionAndRecovery(t *testing.T) {
	root, env := newModEnv(t)
	bad := filepath.Join(root, "bad", "bad.go")
	writeFile(t, bad, "package bad\n\nfunc Broken( {\n")

	span := contracts.SourceSpan{Start: contracts.Position{Line: 9, Col: 13}, End: contracts.Position{Line: 9, Col: 28}}
	doc := &contracts.DocumentIR{
		FilePath:  "app.gxml",
		Version:   "1",
		ViewModel: contracts.ViewModelDecl{Value: "bad.VM", Span: span},
	}

	r := Analyze("app.gxml", doc, nil, env)
	if r.Status != StatusBlocked {
		t.Fatalf("加载失败应 Status=Blocked，实际 %v", r.Status)
	}
	if len(r.Diags) != 1 {
		t.Fatalf("应只报一条诊断（不派生），实际 %d: %+v", len(r.Diags), r.Diags)
	}
	d := r.Diags[0]
	if d.Code != CodeViewModelType {
		t.Fatalf("码 = %s, want %s", d.Code, CodeViewModelType)
	}
	// 归属：GXML 属性位置保留。
	if d.Line != 9 || d.Col != 13 {
		t.Fatalf("GXML 位置 = %d:%d, want 9:13", d.Line, d.Col)
	}
	// 真实 Go 位置必须出现在消息里（来自 go/packages），且不能伪造 GXML 行号。
	if !strings.Contains(d.Message, "failed to load") || !strings.Contains(d.Message, "bad.go:") {
		t.Fatalf("消息应含真实 Go 文件位置: %q", d.Message)
	}
	// 真实 Go 位置应为可定位的绝对路径（含 module root），而非仅编译器相对片段。
	if !strings.Contains(d.Message, root) {
		t.Fatalf("消息应含绝对可定位路径 %q: %q", root, d.Message)
	}
	if strings.Contains(d.Message, "app.gxml:") {
		t.Fatalf("不得把 Go 加载错误伪装成 GXML 行号: %q", d.Message)
	}
	// 阻断则无生成准入。
	if _, verr := r.Verified(); verr == nil {
		t.Fatal("Blocked 结果不应发放生成准入")
	}

	// 修好源码后同一 env 再分析：应恢复并拿到准入（doc.Page=nil → 无绑定需校验）。
	writeFile(t, bad, "package bad\n\ntype VM struct{}\n")
	r2 := Analyze("app.gxml", doc, nil, env)
	if r2.Status != StatusOK || len(r2.Diags) != 0 {
		t.Fatalf("修好后应 Status=OK 且无诊断，实际 %v %+v", r2.Status, r2.Diags)
	}
	if _, verr := r2.Verified(); verr != nil {
		t.Fatalf("恢复后应发放生成准入: %v", verr)
	}
}
