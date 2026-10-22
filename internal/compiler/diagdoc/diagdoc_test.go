// Package diagdoc 校验诊断码目录与代码实现的一致性。
//
// 这是「文档与实现不脱节」的自动门禁。它检查四件事：
//
//  1. 代码中定义的每个码，目录里都有一行记录；
//  2. 目录里记录的每个码，要么有实现、要么标记为「已定稿未实现」、要么空置；
//  3. 目录声明的各类计数与实际一致；
//  4. 同一个码不被两个常量占用。
//
// 三态划分很重要：
//
//	实现态   —— 代码里有常量且被发射
//	定稿态   —— 目录里已定稿语义，但实现属于尚未开始的阶段（如 Analyzer）
//	空置态   —— 保留号码，禁止重新分配
//
// 定稿态是本次引入的：Analyzer 的诊断码在契约评审中已定稿，
// 但 Analyzer 尚未实现。若把它们混入「实现态」，门禁会误报；
// 若标为「空置」，又会丢掉「语义已定、只是没实现」这个事实。
package diagdoc

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/codegen"
	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

const catalogPath = "../../../docs/03-架构/诊断码目录.md"

// codeEntry 记录一个码及其常量名。
type codeEntry struct {
	Code string
	Name string
}

// implementedCodes 是代码中**已实现且会发射**的码。
func implementedCodes() []codeEntry {
	return []codeEntry{
		// Analyzer
		{"GXML-ROOT-002", "analyzer.CodeViewModelType"},
		{"GXML-BIND-001", "analyzer.CodePathRoot"},
		{"GXML-BIND-002", "analyzer.CodePathField"},
		{"GXML-BIND-003", "analyzer.CodePathUnexported"},
		{"GXML-BIND-004", "analyzer.CodeBindTypeMismatch"},
		{"GXML-BIND-005", "analyzer.CodeBindUnsupported"},
		{"GXML-EVENT-003", "analyzer.CodeMethodNotFound"},
		{"GXML-EVENT-004", "analyzer.CodeMethodArity"},
		{"GXML-EVENT-006", "analyzer.CodeMethodArgType"},
		{"GXML-EVENT-007", "analyzer.CodeMethodReturns"},
		{"GXML-SCOPE-004", "analyzer.CodeNotIterable"},
		{"GXML-SCOPE-005", "parser.CodeInputInFor"},

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

		// Codegen（生成/后端层，第二类前缀 CODEGEN-*，见卡 §10.5.2）
		{"CODEGEN-NAME-001", "codegen.CodeNamingExisting"},
		{"CODEGEN-NAME-002", "codegen.CodeNamingSameBatch"},
	}
}

// frozenCodes 是**已定稿但尚未实现**的码。
//
// 它们属于 Analyzer 阶段（尚未开始）。目录已记录其语义，
// 但代码中没有常量、也不会发射。
//
// 实现 Analyzer 时：把这些条目从 frozenCodes 移到 implementedCodes，
// 并补上常量与测试。门禁会在两边都不匹配时报错，因此不会漏掉。
var frozenCodes = []codeEntry{

	//
	// 这个清单会随阶段推进而清空，但**不能只靠维护它来保证正确性**——
	// 它曾经声称 Analyzer 未实现，而实际早已发射（因为旧门禁只校验
	// 两个手写清单互不重叠，从不校验真实代码）。
	// 真正的约束见 emission_test.go 的 AST 校验。
}

// vacantCodes 是目录中明确记录为「空置」的码：保留号码，禁止重新分配。
var vacantCodes = map[string]bool{
	"GXML-SYNTAX-003": true, // 原 CodeTag，从未被发射
	"GXML-ATTR-002":   true, // 规范曾误用该号，从未定义常量
	"GXML-EVENT-001":  true, // 原 CodeEventName，从未被发射
	"GXML-SCOPE-001":  true, // 原 CodeScope，从未被发射
}

// codePattern 匹配两类前缀：
//
//	GXML-*      面向 .gxml 源文本（Parser/Expander/Analyzer 发射）
//	CODEGEN-*   面向生成物与目标 Go 包（Codegen 发射，见卡 §10.5.2）
//
// 不扩展这个正则的话，目录里登记的 CODEGEN-NAME-* 不会被 codesIn 提取，
// 于是「已实现但目录未记录」与「目录里的幻像码」两边都检查不到它——
// 门禁会对整组新码失明。
// 前导 `[^A-Za-z-]` 不是可选项：本仓库的任务卡编号形如
// `TASK-CODEGEN-NAMING-001`，若只匹配 `(?:GXML|CODEGEN)-[A-Z]+-[0-9]{3}`，
// 会把卡号里的 `CODEGEN-NAMING-001` 误当成诊断码（实测报幻像码）。
// 诊断码不会是某个更长编号的后缀，因此要求前一个字符不是字母或连字符
// （Go 的 RE2 不支持后行断言，只能用捕获组表达）。
var codePattern = regexp.MustCompile(`(?:^|[^A-Za-z-])((?:GXML|CODEGEN)-[A-Z]+-[0-9]{3})`)

func readCatalog(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(catalogPath))
	if err != nil {
		t.Fatalf("读取诊断码目录失败: %v", err)
	}
	return string(b)
}

func codesIn(s string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range codePattern.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// ---------------------------------------------------------------------------
// 正向：代码里的每个码都要在目录里
// ---------------------------------------------------------------------------

func TestCatalogCoversImplementedCodes(t *testing.T) {
	catalog := codesIn(readCatalog(t))
	for _, c := range implementedCodes() {
		if !catalog[c.Code] {
			t.Errorf("码 %s（%s）已实现，但目录未记录", c.Code, c.Name)
		}
	}
}

func TestCatalogCoversFrozenCodes(t *testing.T) {
	catalog := codesIn(readCatalog(t))
	for _, c := range frozenCodes {
		if !catalog[c.Code] {
			t.Errorf("码 %s（%s）已定稿，但目录未记录", c.Code, c.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// 反向：目录里提到的每个码都要有归属
// ---------------------------------------------------------------------------

func TestNoPhantomCodesInCatalog(t *testing.T) {
	known := make(map[string]bool)
	for _, c := range implementedCodes() {
		known[c.Code] = true
	}
	for _, c := range frozenCodes {
		known[c.Code] = true
	}
	for c := range vacantCodes {
		known[c] = true
	}

	for code := range codesIn(readCatalog(t)) {
		if !known[code] {
			t.Errorf("目录中出现幻像码 %s：既未实现、未定稿，也未标记为空置", code)
		}
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

// TestFrozenCodesHaveNoImplementation 确认定稿码确实还没实现。
func TestFrozenCodesHaveNoImplementation(t *testing.T) {
	impl := make(map[string]bool)
	for _, c := range implementedCodes() {
		impl[c.Code] = true
	}
	for _, c := range frozenCodes {
		if impl[c.Code] {
			t.Errorf("%s 已被标记为定稿，却出现在实现清单（%s）", c.Code, c.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// 计数
// ---------------------------------------------------------------------------

func TestCodeCounts(t *testing.T) {
	const (
		wantImpl   = 37
		wantFrozen = 0
		wantTotal  = wantImpl + wantFrozen
	)
	if got := len(implementedCodes()); got != wantImpl {
		t.Errorf("已实现码 = %d, 期望 %d", got, wantImpl)
	}
	if got := len(frozenCodes); got != wantFrozen {
		t.Errorf("已定稿码 = %d, 期望 %d", got, wantFrozen)
	}

	body := readCatalog(t)
	// 36 = 37 个实现态 − 1 个归集码（GXML-ATTR-003，见目录 §3）。
	if !strings.Contains(body, "共 **36 个**激活码。") {
		t.Errorf("目录未声明激活码总数为 %d", wantTotal-1)
	}
}

// ---------------------------------------------------------------------------
// 唯一性
// ---------------------------------------------------------------------------

func TestCodesUnique(t *testing.T) {
	seen := make(map[string]string)
	check := func(list []codeEntry, kind string) {
		for _, c := range list {
			if prev, dup := seen[c.Code]; dup {
				t.Errorf("码 %s 同时被 %s 与 %s 占用", c.Code, prev, c.Name)
			}
			seen[c.Code] = kind + ":" + c.Name
		}
	}
	check(implementedCodes(), "impl")
	check(frozenCodes, "frozen")
}

// ---------------------------------------------------------------------------
// 常量实际值必须与清单一致
// ---------------------------------------------------------------------------

func TestConstantValuesMatchCatalog(t *testing.T) {
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

		// Codegen：第二类前缀（CODEGEN-*），见卡 §10.5.2。
		"CODEGEN-NAME-001": codegen.CodeNamingExisting,
		"CODEGEN-NAME-002": codegen.CodeNamingSameBatch,
	}
	for want, got := range actual {
		if want != got {
			t.Errorf("清单写 %s，常量实际值 %s", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 归集码与空置约定
// ---------------------------------------------------------------------------

func TestAttr002StaysVacant(t *testing.T) {
	for _, c := range implementedCodes() {
		if c.Code == "GXML-ATTR-002" {
			t.Error("GXML-ATTR-002 应为空置；若确需启用，先更新目录 §2.3 与 §4")
		}
	}
	if !strings.Contains(readCatalog(t), "`GXML-ATTR-002` **空置**") {
		t.Error("目录未说明 GXML-ATTR-002 为空置")
	}
}

func TestBindCodesAreImplemented(t *testing.T) {
	// GXML-BIND 的 5 个码已实现。
	//
	// 这条测试曾经断言相反的内容（「已定稿但未实现」）。Analyzer 落地后
	// 它仍在守着旧事实——说明「靠人工维护断言」本身就会滞后。
	// 现在的真实约束由 emission_test.go 的 AST 校验保证。
	impl := make(map[string]bool)
	for _, c := range implementedCodes() {
		impl[c.Code] = true
	}
	for _, code := range []string{
		"GXML-BIND-001", "GXML-BIND-002", "GXML-BIND-003",
		"GXML-BIND-004", "GXML-BIND-005",
	} {
		if !impl[code] {
			t.Errorf("%s 应在实现清单中", code)
		}
	}
}

func TestCatalogDeclaresStage(t *testing.T) {
	body := readCatalog(t)
	for _, s := range []string{"Parser", "Expander", "Analyzer"} {
		if !strings.Contains(body, s) {
			t.Errorf("目录未提及阶段 %s", s)
		}
	}
}

// ---------------------------------------------------------------------------
// 分组计数
// ---------------------------------------------------------------------------

func TestCodesAreGrouped(t *testing.T) {
	groups := make(map[string]int)
	count := func(list []codeEntry) {
		for _, c := range list {
			parts := strings.SplitN(c.Code, "-", 3)
			if len(parts) != 3 {
				t.Fatalf("码格式异常: %s", c.Code)
			}
			groups[parts[1]]++
		}
	}
	count(implementedCodes())
	count(frozenCodes)

	want := map[string]int{
		"ROOT": 2, "SYNTAX": 3, "ATTR": 4, "EVENT": 6, "DEF": 11, "SCOPE": 4, "BIND": 5,
		"NAME": 2, // CODEGEN-NAME-*（生成层，见卡 §10.5.2）
	}
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if groups[k] != want[k] {
			t.Errorf("GXML-%s 码数 = %d, 目录声明 %d", k, groups[k], want[k])
		}
	}
}
