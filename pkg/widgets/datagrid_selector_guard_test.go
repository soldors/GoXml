package widgets

// R28-B3 选择器守卫：从 RUNBOOK §2 提取实际运行选择器，验证锚定形式
// 且命中唯一 native fixture 测试。RUNBOOK 选择器退化为旧前缀形式时
// 本守卫 FAIL（翻红）。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestKBSelectorGuard 从 RUNBOOK §2 提取 -test.run 选择器并验证：
//  1. 选择器必须是锚定正则 ^TestDataGridKeyboardNativeFixture$
//  2. （隐式）锚定正则不会匹配其他 nativewindow 测试
//
// 旧手册使用未锚定的 TestNativeDataGridKeyboardFixture（前缀匹配），
// 本守卫通过 RUNBOOK 文本锚定检查使其翻红。
func TestKBSelectorGuard(t *testing.T) {
	b, err := os.ReadFile("testdata/datagrid-keyboard/native/RUNBOOK.md")
	if err != nil {
		t.Fatal("读取 RUNBOOK 失败:", err)
	}
	_, afterHeading, found := strings.Cut(string(b), "\n## 2. ")
	if !found {
		t.Fatal("RUNBOOK 缺少 §2")
	}
	section, _, _ := strings.Cut(afterHeading, "\n## ")

	// 提取 §2 的 -test.run 参数
	runRe := regexp.MustCompile(`-test\.run\s+(\S+)`)
	matches := runRe.FindAllStringSubmatch(section, -1)
	if len(matches) != 1 {
		t.Fatalf("RUNBOOK §2 应有一个 -test.run 选择器，实得 %d", len(matches))
	}
	selector := strings.Trim(matches[0][1], "'\"")

	// 验证锚定形式
	if !strings.HasPrefix(selector, "^") || !strings.HasSuffix(selector, "$") {
		t.Fatalf("RUNBOOK §2 选择器必须锚定（^...$），得到 %q", selector)
	}
	if selector != "^TestDataGridKeyboardNativeFixture$" {
		t.Fatalf("RUNBOOK §2 选择器应为 ^TestDataGridKeyboardNativeFixture$，得到 %q", selector)
	}

	// 验证锚定正则确实只命中一个测试名（同一文件中的唯一 native 测试）
	anchorRe, err := regexp.Compile(selector)
	if err != nil {
		t.Fatalf("选择器正则编译失败: %v", err)
	}
	src, err := parser.ParseFile(token.NewFileSet(), "datagrid_keyboard_native_test.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, decl := range src.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && anchorRe.MatchString(fn.Name.Name) {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("锚定选择器应恰好命中 1 个测试，命中 %d", hits)
	}
}
