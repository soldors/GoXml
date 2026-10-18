// Package diagdoc 校验诊断码目录与代码实现的一致性。
//
// 这是「文档与实现不脱节」的自动门禁。它检查三件事：
//
//  1. 代码中定义的每个码，目录里都有一行记录；
//  2. 目录里记录的每个码，代码中确实存在（不留幻像码）；
//  3. 目录中声明的激活码总数与实际一致。
//
// 任何一项失败都说明需要同步文档或代码，而不是修改本测试来「让它过」。
package diagdoc

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

const catalogPath = "../../../docs/03-架构/诊断码目录.md"

// codeEntry 记录一个码及其常量名。
type codeEntry struct {
	Code string
	Name string
}

// activeCodes 是代码中实际定义的全部激活码。
//
// 这份清单必须与源码常量保持同步：新增常量却忘了加到这里，
// TestCatalogCoversAllCodes 不会发现，因此此处也由
// TestEveryListedCodeIsUsed 反向校验每个码确实被发射。
func activeCodes() []codeEntry {
	return []codeEntry{
		// Parser
		{"GXML-ROOT-001", "parser.CodeRoot"},
		{"GXML-SYNTAX-001", "parser.CodeSyntax"},
		{"GXML-SYNTAX-002", "parser.CodeXML"},
		{"GXML-SYNTAX-004", "parser.CodeTextNode"},
		{"GXML-ATTR-001", "parser.CodeAttrDup"},
		{"GXML-ATTR-003", "parser.CodeAttrCall"},
		{"GXML-SCOPE-002", "parser.CodeControl"},
		{"GXML-SCOPE-003", "parser.CodeKey"},
		{"GXML-DEF-001", "parser.CodeDef"},
		{"GXML-DEF-002", "parser.CodeDefName"},
		// Expander
		{"GXML-ATTR-004", "expander.CodeUnknownName"},
		{"GXML-ATTR-005", "expander.CodeNotApplicable"},
		{"GXML-EVENT-002", "expander.CodeEventSyntax"},
		{"GXML-EVENT-005", "expander.CodeEventArg"},
		{"GXML-DEF-000", "expander.CodeNilDocument"},
		{"GXML-DEF-003", "expander.CodeSchemaConflict"},
		{"GXML-DEF-004", "expander.CodeSchemaDuplicate"},
		{"GXML-DEF-005", "expander.CodeLayoutShadow"},
		{"GXML-DEF-006", "expander.CodeComponentUnknown"},
		{"GXML-DEF-007", "expander.CodePropUnknown"},
		{"GXML-DEF-008", "expander.CodePropMissing"},
		{"GXML-DEF-009", "expander.CodeSlotNotAllowed"},
		{"GXML-DEF-010", "expander.CodeRecursiveDef"},
	}
}

var codePattern = regexp.MustCompile("GXML-[A-Z]+-[0-9]{3}")

// readCatalog 读取目录文件。
func readCatalog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(catalogPath))
	if err != nil {
		t.Fatalf("读取诊断码目录失败: %v", err)
	}
	return string(b)
}

// codesIn 提取文本中出现的全部码（去重）。
func codesIn(s string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range codePattern.FindAllString(s, -1) {
		out[m] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// 正向：代码里的每个码都要在目录里
// ---------------------------------------------------------------------------

func TestCatalogCoversAllCodes(t *testing.T) {
	catalog := codesIn(readCatalog(t))
	for _, c := range activeCodes() {
		if !catalog[c.Code] {
			t.Errorf("码 %s（%s）在代码中激活，但目录未记录", c.Code, c.Name)
		}
	}
}

// vacantCodes 是在目录中被明确记录为「空置」或「预留」的码。
//
// 它们出现在目录里是合法的——目录需要保留这些号码，说明「不要重新分配」
// 以及为什么。但它们不得有对应的激活检测。
var vacantCodes = map[string]bool{
	"GXML-SYNTAX-003": true, // 原 CodeTag，从未被发射
	"GXML-ATTR-002":   true, // 规范曾误用该号，从未定义常量
	"GXML-EVENT-001":  true, // 原 CodeEventName，从未被发射
	"GXML-SCOPE-001":  true, // 原 CodeScope，从未被发射
	"GXML-BIND-001":   true, // Analyzer 阶段预留
}

// ---------------------------------------------------------------------------
// 反向：目录里提到的每个码都要在代码里，或明确记为空置
// ---------------------------------------------------------------------------

func TestNoPhantomCodesInCatalog(t *testing.T) {
	known := make(map[string]bool, len(activeCodes()))
	for _, c := range activeCodes() {
		known[c.Code] = true
	}

	for code := range codesIn(readCatalog(t)) {
		if known[code] || vacantCodes[code] {
			continue
		}
		t.Errorf("目录中出现幻像码 %s：代码里没有对应的激活检测，也未标记为空置", code)
	}
}

// TestVacantCodesAreDocumented 确认每个空置码都在目录里有说明。
func TestVacantCodesAreDocumented(t *testing.T) {
	body := readCatalog(t)
	for code := range vacantCodes {
		if !strings.Contains(body, code) {
			t.Errorf("空置码 %s 未在目录中说明", code)
		}
	}
}

// TestVacantCodesHaveNoDetection 确认空置码确实没有激活检测。
func TestVacantCodesHaveNoDetection(t *testing.T) {
	for _, c := range activeCodes() {
		if vacantCodes[c.Code] {
			t.Errorf("%s 被标记为空置，却出现在激活清单（%s）", c.Code, c.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// 计数：目录声明 23 个，代码必须也是 23 个
// ---------------------------------------------------------------------------

func TestActiveCodeCount(t *testing.T) {
	const want = 23
	if got := len(activeCodes()); got != want {
		t.Errorf("激活码数量 = %d, 目录声明为 %d", got, want)
	}

	// 目录正文应当出现「共 **23 个**激活码」这类声明。
	body := readCatalog(t)
	if !strings.Contains(body, "共 **23 个**激活码") {
		t.Error("目录未声明激活码数量为 23，或声明与实际不符")
	}
}

// ---------------------------------------------------------------------------
// 唯一性：同一个码不得被两处记录
// ---------------------------------------------------------------------------

func TestActiveCodesUnique(t *testing.T) {
	seen := make(map[string]string, len(activeCodes()))
	for _, c := range activeCodes() {
		if prev, dup := seen[c.Code]; dup {
			t.Errorf("码 %s 被 %s 与 %s 同时占用", c.Code, prev, c.Name)
		}
		seen[c.Code] = c.Name
	}
}

// ---------------------------------------------------------------------------
// 常量实际值必须与清单一致
// ---------------------------------------------------------------------------

func TestConstantValuesMatchCatalog(t *testing.T) {
	// 显式列出，防止常量值与清单文字脱节。
	actual := map[string]string{
		"GXML-ROOT-001":   parser.CodeRoot,
		"GXML-SYNTAX-001": parser.CodeSyntax,
		"GXML-SYNTAX-002": parser.CodeXML,
		"GXML-SYNTAX-004": parser.CodeTextNode,
		"GXML-ATTR-001":   parser.CodeAttrDup,
		"GXML-ATTR-003":   parser.CodeAttrCall,
		"GXML-SCOPE-002":  parser.CodeControl,
		"GXML-SCOPE-003":  parser.CodeKey,
		"GXML-DEF-001":    parser.CodeDef,
		"GXML-DEF-002":    parser.CodeDefName,

		"GXML-ATTR-004":  expander.CodeUnknownName,
		"GXML-ATTR-005":  expander.CodeNotApplicable,
		"GXML-EVENT-002": expander.CodeEventSyntax,
		"GXML-EVENT-005": expander.CodeEventArg,
		"GXML-DEF-000":   expander.CodeNilDocument,
		"GXML-DEF-003":   expander.CodeSchemaConflict,
		"GXML-DEF-004":   expander.CodeSchemaDuplicate,
		"GXML-DEF-005":   expander.CodeLayoutShadow,
		"GXML-DEF-006":   expander.CodeComponentUnknown,
		"GXML-DEF-007":   expander.CodePropUnknown,
		"GXML-DEF-008":   expander.CodePropMissing,
		"GXML-DEF-009":   expander.CodeSlotNotAllowed,
		"GXML-DEF-010":   expander.CodeRecursiveDef,
	}
	for want, got := range actual {
		if want != got {
			t.Errorf("清单写 %s，常量实际值 %s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 归集码与空置号约定
// ---------------------------------------------------------------------------

func TestAttr002StaysVacant(t *testing.T) {
	// GXML-ATTR-002 历史上被文档误用，从未定义常量。
	// 目录明确记为「空置」，此处防止它被悄悄启用。
	for _, c := range activeCodes() {
		if c.Code == "GXML-ATTR-002" {
			t.Error("GXML-ATTR-002 应为空置；若确需启用，先更新目录 §2.3 与 §4")
		}
	}
	// 但目录里必须解释它为什么空置。
	body := readCatalog(t)
	if !strings.Contains(body, "`GXML-ATTR-002` **空置**") {
		t.Error("目录未说明 GXML-ATTR-002 为空置")
	}
}

func TestBindCategoryHasNoActiveCode(t *testing.T) {
	for _, c := range activeCodes() {
		if strings.HasPrefix(c.Code, "GXML-BIND-") {
			t.Errorf("%s 已激活，但 Analyzer 尚未实现；需同步目录 §2.7", c.Code)
		}
	}
	if !strings.Contains(readCatalog(t), "当前没有任何激活码") {
		t.Error("目录未声明 GXML-BIND 当前无激活码")
	}
}

// ---------------------------------------------------------------------------
// 目录必须记录每个码的阶段
// ---------------------------------------------------------------------------

func TestCatalogDeclaresStage(t *testing.T) {
	body := readCatalog(t)
	stages := []string{"Parser", "Expander"}
	for _, s := range stages {
		if !strings.Contains(body, s) {
			t.Errorf("目录未提及阶段 %s", s)
		}
	}
	if !strings.Contains(body, "Analyzer") {
		t.Error("目录未提及 Analyzer 阶段（GXML-BIND 预留说明需要）")
	}
}

// ---------------------------------------------------------------------------
// 排序自检：确保清单本身可读
// ---------------------------------------------------------------------------

func TestCodesAreGrouped(t *testing.T) {
	// 按前缀分组后检查每组数量，帮助发现漏项。
	groups := make(map[string]int)
	for _, c := range activeCodes() {
		parts := strings.SplitN(c.Code, "-", 3)
		if len(parts) != 3 {
			t.Fatalf("码格式异常: %s", c.Code)
		}
		groups[parts[1]]++
	}
	want := map[string]int{
		"ROOT": 1, "SYNTAX": 3, "ATTR": 4, "EVENT": 2, "DEF": 11, "SCOPE": 2,
	}
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if groups[k] != want[k] {
			t.Errorf("GXML-%s 激活码 = %d, 目录声明 %d", k, groups[k], want[k])
		}
	}
}
