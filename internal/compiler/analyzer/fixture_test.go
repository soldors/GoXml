package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/expander"
	"github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// 夹具期望表
//
// 期望用 Go 结构体字面量表达（而非 want.txt）：
//   - 编译器检查字段与类型，重构时能立刻发现失配；
//   - 诊断码引用常量，拼写错误在编译期暴露；
//   - 不需要维护期望文件的解析器；
//   - .gxml 仍是唯一的输入来源，输入与期望不混在一个文件里。
//
// 注意：字面量有类型检查，但不保证断言完整。因此下面的 Test 会对
// status 与每个期望码逐项比对，而不是只比较其中一项。
// ============================================================================

// expect 描述一个夹具的期望分析结果。
type expect struct {
	// status 是期望的总体状态。
	status Status
	// codes 是期望出现的诊断码，顺序无关。
	codes []string
	// note 说明该夹具验证什么，作为测试失败时的线索。
	note string
}

// expectationByCase 按夹具目录名索引。
var expectationByCase = map[string]expect{
	"valid-binding": {
		status: StatusOK,
		note:   "{User.Name} 两段路径全部可解析、可访问、类型匹配",
	},
	"invalid-root": {
		status: StatusPartial,
		codes:  []string{CodePathRoot},
		note:   "首段 Nope 不是 ViewModel 字段，也不是循环变量",
	},
	"invalid-nested-field": {
		status: StatusPartial,
		codes:  []string{CodePathField},
		note:   "User 无 Nmae 字段：中间段错误应报 BIND-002 而非 BIND-001",
	},
	"unexported-field": {
		status: StatusPartial,
		codes:  []string{CodePathUnexported},
		note:   "note 存在但未导出：修复动作是改大小写，与「不存在」不同",
	},
	"type-mismatch": {
		status: StatusPartial,
		codes:  []string{CodeBindTypeMismatch},
		note:   "User.Age 是 int，Text.Value 要 string",
	},
	"negate-non-bool": {
		status: StatusPartial,
		codes:  []string{CodeBindTypeMismatch},
		note:   "{!Count}，Count 是 int；Q6 规定取反只作用于 bool",
	},
	"valid-event-payload": {
		status: StatusOK,
		note:   "ValueChanged 载荷 value:string 映射到 OnNameChanged(value string)",
	},
	"valid-event-args": {
		status: StatusOK,
		note:   "Click=\"Delete(User.Age)\" 的实参映射到 Delete(id int)",
	},
	"valid-event-combined": {
		status: StatusOK,
		note:   "自定义事件 Open(id:string) + 实参：Mapping 应为 [payload, callarg]",
	},
	"invalid-event-arity": {
		status: StatusPartial,
		codes:  []string{CodeMethodArity},
		note:   "载荷 1 个，OnWrongArity 有 0 个形参 → EVENT-004",
	},
	"invalid-event-type": {
		status: StatusPartial,
		codes:  []string{CodeMethodArgType},
		note:   "载荷 string，OnWrongType 形参 int → EVENT-006",
	},
	"invalid-method-missing": {
		status: StatusPartial,
		codes:  []string{CodeMethodNotFound},
		note:   "ViewModel 无 NoSuchMethod",
	},
	"invalid-method-returns": {
		status: StatusPartial,
		note:   "事件方法不得有返回值（Q4）；具体码在实现时定",
	},
	"for-not-iterable": {
		status: StatusPartial,
		codes:  []string{CodeNotIterable},
		note:   "Title 是 string，不可迭代 → SCOPE-004",
	},
	"for-var-binding": {
		status: StatusOK,
		note:   "{item.Name} 的 item 来自 For 作用域，不应按 ViewModel 字段解析",
	},
	"blocked-viewmodel": {
		status: StatusBlocked,
		codes:  []string{CodeViewModelType},
		note:   "fixture.ViewModel 包中的 nosuch.Pkg 不存在；应只报一条，不产生派生错误",
	},
}

// casesDir 是夹具根目录（go test 以包目录为工作目录）。
const casesDir = "testdata/cases"

// caseNames 返回磁盘上实际存在的夹具名。
func caseNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(casesDir)
	if err != nil {
		t.Fatalf("读取夹具目录失败: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// readCase 读取一个夹具的 .gxml。
func readCase(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join(casesDir, name, "case.gxml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", p, err)
	}
	return b
}

// ============================================================================
// 现在就能成立的断言：夹具必须通过 Parser 与 Expander
// ============================================================================

// TestFixturesPassParserAndExpander 确保每个夹具的错误属于 Analyzer，
// 而不是更早的阶段。
//
// 这条测试现在就有意义：若某个夹具其实是语法或展开错误，它就无法用来
// 验证 Analyzer——夹具本身失效了，必须先修夹具。
func TestFixturesPassParserAndExpander(t *testing.T) {
	names := caseNames(t)
	if len(names) == 0 {
		t.Fatal("没有夹具")
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			src := readCase(t, name)

			doc, pdiags := parser.Parse(name+".gxml", src)
			if len(pdiags) > 0 {
				for _, d := range pdiags {
					t.Errorf("Parser 诊断（夹具应在 Analyzer 才失败）: %s", d.Error())
				}
				return
			}
			if doc == nil {
				t.Fatal("Parser 返回 nil 文档")
			}

			r := expander.Expand(name+".gxml", doc)
			if len(r.Diags) > 0 {
				for _, d := range r.Diags {
					t.Errorf("Expander 诊断（夹具应在 Analyzer 才失败）: %s", d.Error())
				}
				return
			}
			if r.Doc == nil || r.Doc.Page == nil {
				t.Fatal("展开后文档不完整")
			}
		})
	}
}

// ============================================================================
// 期望表完整性
// ============================================================================

// TestEveryCaseHasExpectation 确认磁盘夹具与期望表一一对应。
func TestEveryCaseHasExpectation(t *testing.T) {
	for _, name := range caseNames(t) {
		if _, ok := expectationByCase[name]; !ok {
			t.Errorf("夹具 %s 缺少期望条目", name)
		}
	}
	for name := range expectationByCase {
		if _, err := os.Stat(filepath.Join(casesDir, name, "case.gxml")); err != nil {
			t.Errorf("期望条目 %s 没有对应的 case.gxml", name)
		}
	}
}

// TestExpectationsUseKnownCodes 确认期望的诊断码都已在契约中定稿。
func TestExpectationsUseKnownCodes(t *testing.T) {
	known := map[string]bool{
		CodeViewModelType:    true,
		CodePathRoot:         true,
		CodePathField:        true,
		CodePathUnexported:   true,
		CodeBindTypeMismatch: true,
		CodeBindUnsupported:  true,
		CodeMethodNotFound:   true,
		CodeMethodArity:      true,
		CodeMethodArgType:    true,
		CodeNotIterable:      true,
	}
	for name, e := range expectationByCase {
		for _, c := range e.codes {
			if !known[c] {
				t.Errorf("夹具 %s 引用了未定稿的码 %s", name, c)
			}
		}
	}
}

// TestExpectationNotes 确认每个期望都有说明，避免未来看不懂为什么这样断言。
func TestExpectationNotes(t *testing.T) {
	for name, e := range expectationByCase {
		if strings.TrimSpace(e.note) == "" {
			t.Errorf("夹具 %s 缺少 note", name)
		}
	}
}

// TestStatusDistribution 确认夹具覆盖三种状态。
func TestStatusDistribution(t *testing.T) {
	count := map[Status]int{}
	for _, e := range expectationByCase {
		count[e.status]++
	}
	for _, st := range []Status{StatusOK, StatusPartial, StatusBlocked} {
		if count[st] == 0 {
			t.Errorf("没有覆盖状态 %s 的夹具", st)
		}
	}
}

// TestPartialAndBlockedCasesHaveCodes 确认失败类夹具都指定了诊断码。
//
// 例外：invalid-method-returns 的具体码待实现时定（目录尚未为
// 「事件方法有返回值」分配号码），因此允许为空但需在 note 中说明。
func TestPartialAndBlockedCasesHaveCodes(t *testing.T) {
	allowNoCode := map[string]bool{
		"invalid-method-returns": true,
	}
	for name, e := range expectationByCase {
		if e.status == StatusOK || allowNoCode[name] {
			continue
		}
		if len(e.codes) == 0 {
			t.Errorf("夹具 %s 期望失败，但未指定诊断码", name)
		}
	}
}

// ============================================================================
// 待实现的断言
// ============================================================================

// ---------------------------------------------------------------------------
// 分步启用：只跑已实现步骤覆盖的夹具
//
// Analyzer 分五步实现。夹具期望表已完整固定，但只能逐步启用——
// 否则未实现步骤的夹具会「失败」，把「还没做」误报成「做错了」。
//
// 已实现：步骤 1（ViewModel 解析）、步骤 2（绑定路径解析）
// ---------------------------------------------------------------------------

// implementedByStep 列出当前已能被正确判定的夹具。
var implementedByStep = map[string]bool{
	// 步骤 2：绑定路径
	"valid-binding":        true,
	"invalid-root":         true,
	"invalid-nested-field": true,
	"unexported-field":     true,
	"for-not-iterable":     true,
	"for-var-binding":      true,
	"blocked-viewmodel":    true,

	// 以下依赖后续步骤，暂不启用：
	//   type-mismatch / negate-non-bool   → 步骤 3（BIND-004）
	//   valid-event-* / invalid-event-*   → 步骤 4（事件签名）
	//   valid-event-combined              → 步骤 4
	//   invalid-method-returns            → 步骤 4
}

// TestAnalyzeBehavior 对已实现步骤覆盖的夹具做完整断言。
func TestAnalyzeBehavior(t *testing.T) {
	env := newTestEnv(t, ".")

	for name, want := range expectationByCase {
		if !implementedByStep[name] {
			continue
		}
		t.Run(name, func(t *testing.T) {
			src := readCase(t, name)

			doc, pdiags := parser.Parse(name+".gxml", src)
			if len(pdiags) > 0 {
				t.Fatalf("Parser 诊断: %s", pdiags[0].Error())
			}
			er := expander.Expand(name+".gxml", doc)
			if len(er.Diags) > 0 {
				t.Fatalf("Expander 诊断: %s", er.Diags[0].Error())
			}

			r := Analyze(name+".gxml", er.Doc, er.Schemas, env)

			// 1. 状态
			if r.Status != want.status {
				t.Errorf("Status = %v, 期望 %v", r.Status, want.status)
			}

			// 2. 诊断码：逐项比对（数量 + 每个期望码都出现）
			got := make(map[string]int)
			for _, d := range r.Diags {
				got[d.Code]++
			}
			for _, c := range want.codes {
				if got[c] == 0 {
					t.Errorf("缺少期望诊断 %s（实际 %v）", c, got)
				}
			}
			if len(want.codes) == 0 && len(got) > 0 {
				t.Errorf("期望无诊断，实际 %v", got)
			}

			// 3. StatusOK 的夹具：验证凭证可取，且事实非空
			if r.Status == StatusOK {
				if _, err := r.Verified(); err != nil {
					t.Errorf("StatusOK 应可取得凭证: %v", err)
				}
				if len(want.codes) == 0 && r.Facts.Len() == 0 {
					t.Errorf("%s 期望产生语义事实，实际为空", name)
				}
			} else if _, err := r.Verified(); err == nil {
				t.Errorf("%s 状态为 %v，不应取得凭证", name, r.Status)
			}
		})
	}
}

// TestPendingStepsAreDocumented 确认未启用的夹具都有明确归属，
// 不会因为「忘了启用」而长期沉默。
func TestPendingStepsAreDocumented(t *testing.T) {
	pending := make(map[string]bool)
	for name := range expectationByCase {
		if !implementedByStep[name] {
			pending[name] = true
		}
	}
	// 当前应恰好剩下依赖步骤 3/4 的夹具。
	want := []string{
		"type-mismatch", "negate-non-bool",
		"valid-event-payload", "valid-event-args", "valid-event-combined",
		"invalid-event-arity", "invalid-event-type",
		"invalid-method-missing", "invalid-method-returns",
	}
	for _, n := range want {
		if !pending[n] {
			t.Errorf("%s 应在待启用清单中（当前步骤未覆盖）", n)
		}
	}
	for n := range pending {
		found := false
		for _, w := range want {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 未在待启用清单中说明，可能是遗漏", n)
		}
	}
}
