package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TASK-CODEGEN-NAMING-001 切片 C3：诊断码的实际发射
//
// 目录 §5 第 5 条：新增码必须「至少一个用例锁定该输入产生该码」。
// 因此这里的码值、消息与位置字段都是**逐字锁定**的——
// 与命名符号那边「一律派生」的纪律不冲突：符号名会随规则演进，
// 而**码值是对外承诺**（日志与文档按它匹配），必须是固定字面量。
// ============================================================================

// TestNamingCodeValues 锁定两个码的字面值（对外承诺，不得漂移）。
func TestNamingCodeValues(t *testing.T) {
	cases := map[string]string{
		"CODEGEN-NAME-001": CodeNamingExisting,
		"CODEGEN-NAME-002": CodeNamingSameBatch,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("常量值 = %q，期望 %q（码值发布后不得改语义）", got, want)
		}
	}
	if CodeNamingExisting == CodeNamingSameBatch {
		t.Error("两个码不得相同（一个码只有一个含义，见目录 §1.2 第 1 条）")
	}
}

// TestNamingDiagnosticsExistingSymbol 包内既有声明占用生成符号 → CODEGEN-NAME-001。
func TestNamingDiagnosticsExistingSymbol(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.go")
	if err := os.WriteFile(userFile, []byte("package demo\n\ntype goxmlRenderKeyHome struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags, err := namingDiagnostics(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(diags) != 1 {
		t.Fatalf("应有 1 条诊断；实际 %d 条: %v", len(diags), diags)
	}
	d := diags[0]

	if d.Code != CodeNamingExisting {
		t.Errorf("码 = %q，期望 %q", d.Code, CodeNamingExisting)
	}
	// 消息必须同时给出符号、触发源、占用者——三者缺一，用户无法定位。
	for _, want := range []string{"goxmlRenderKeyHome", "home.gxml", userFile} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("消息应含 %q；实际: %s", want, d.Message)
		}
	}
	if !strings.HasPrefix(d.Message, `generated symbol "goxmlRenderKeyHome" (from `) {
		t.Errorf("消息前缀与冻结文案不一致: %s", d.Message)
	}
	// 位置字段为零值：包级事实不是源码某个行列。
	if d.Line != 0 || d.Col != 0 || d.EndLine != 0 || d.EndCol != 0 {
		t.Errorf("位置字段应为零值；实际 line=%d col=%d", d.Line, d.Col)
	}
	if !strings.HasSuffix(d.File, "home.gxml") {
		t.Errorf("File 应是触发方的 .gxml；实际 %q", d.File)
	}
}

// TestNamingDiagnosticsSameBatch 两个源派生同一符号（pascal 塌缩）→ CODEGEN-NAME-002。
func TestNamingDiagnosticsSameBatch(t *testing.T) {
	dir := t.TempDir()

	diags, err := namingDiagnostics(namingTargets(dir, "a-b.gxml", "a_b.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("塌缩的两个源应产生诊断")
	}

	// 塌缩的两个源派生出**完全相同**的符号集合，因此每个符号各一条冲突。
	// 期望集合由生产函数派生，不硬编码——但条数与覆盖面必须精确核对。
	wantSyms, err := generatedPackageSymbols("a-b.gxml")
	if err != nil {
		t.Fatalf("generatedPackageSymbols: %v", err)
	}
	if len(diags) != len(wantSyms) {
		t.Errorf("诊断条数 = %d，期望与派生符号数一致（%d）: %v", len(diags), len(wantSyms), diags)
	}

	covered := map[string]bool{}
	for _, d := range diags {
		if d.Code != CodeNamingSameBatch {
			t.Errorf("码 = %q，期望 %q", d.Code, CodeNamingSameBatch)
			continue
		}
		for _, want := range []string{"a-b.gxml", "a_b.gxml"} {
			if !strings.Contains(d.Message, want) {
				t.Errorf("消息应含双方源 %q；实际: %s", want, d.Message)
			}
		}
		for _, sym := range wantSyms {
			if strings.Contains(d.Message, `"`+sym+`"`) {
				covered[sym] = true
			}
		}
	}
	for _, sym := range wantSyms {
		if !covered[sym] {
			t.Errorf("没有一条诊断提到被撞的符号 %q；实际: %v", sym, diags)
		}
	}
}

// TestNamingDiagnosticsNoConflict 不同源不撞名 → 零诊断（防过度报警）。
func TestNamingDiagnosticsNoConflict(t *testing.T) {
	dir := t.TempDir()

	diags, err := namingDiagnostics(namingTargets(dir, "home.gxml", "detail.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("不同源不应产生诊断；实际 %d 条: %v", len(diags), diags)
	}
}

// TestNamingDiagnosticErrorString 锁定 Error() 的整体输出格式。
//
// 目录 §6 与 contracts.CompilerError 约定格式为 `file:line:col: CODE: message`；
// 命名冲突的行列是零值，因此输出形如 `home.gxml:0:0: CODEGEN-NAME-001: ...`。
func TestNamingDiagnosticErrorString(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.go")
	if err := os.WriteFile(userFile, []byte("package demo\n\ntype goxmlRenderKeyHome struct{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diags, err := namingDiagnostics(namingTargets(dir, "home.gxml"))
	if err != nil {
		t.Fatalf("预检报错: %v", err)
	}
	got := diags[0].Error()
	if !strings.Contains(got, ":0:0: "+CodeNamingExisting+": ") {
		t.Errorf("Error() 格式不符（期望含 `:0:0: %s: `）: %s", CodeNamingExisting, got)
	}
	t.Logf("诊断输出样例: %s", got)
}

// TestNamingDiagnosticUsesContractsPrefix 码必须来自 contracts 前缀常量，不得内联拼接。
//
// 目录 §5 第 4 条要求"定义常量（不得内联拼接）"。这条把该约束变成可执行判据：
// 若有人直接写 `CodeNamingExisting = "CODEGEN-NAME-001"`，本测试会红。
func TestNamingDiagnosticUsesContractsPrefix(t *testing.T) {
	if !strings.HasPrefix(CodeNamingExisting, contracts.CodePrefixCodegenName) {
		t.Errorf("%q 应以 %q 为前缀", CodeNamingExisting, contracts.CodePrefixCodegenName)
	}
	if !strings.HasPrefix(CodeNamingSameBatch, contracts.CodePrefixCodegenName) {
		t.Errorf("%q 应以 %q 为前缀", CodeNamingSameBatch, contracts.CodePrefixCodegenName)
	}
}
