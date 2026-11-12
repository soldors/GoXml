package widgets

// R28-B3 选择器守卫：从 RUNBOOK §2 提取实际运行选择器，验证锚定形式
// 且命中唯一 native fixture 测试。RUNBOOK 选择器退化为旧前缀形式时
// 本守卫 FAIL（翻红）。

import (
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
	runbook := string(b)

	// 提取 §2 的 -test.run 参数
	runRe := regexp.MustCompile(`-test\.run\s+(\S+)`)
	matches := runRe.FindAllStringSubmatch(runbook, -1)
	if len(matches) == 0 {
		t.Fatal("RUNBOOK §2 缺少 -test.run 选择器")
	}
	// 取最后一个（§2 是实际运行段），去引号
	selector := strings.Trim(matches[len(matches)-1][1], "'\"")

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
	nativeTests := []string{
		"TestDataGridKeyboardNativeFixture",
		"TestNativeMenuClosingFrameDropsHitAreas", // R26 nativewindow（如存在）
		"TestNativeModalClosingFrameDropsHitAreas",
	}
	hits := 0
	for _, name := range nativeTests {
		if anchorRe.MatchString(name) {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("锚定选择器应恰好命中 1 个测试，命中 %d", hits)
	}
}
