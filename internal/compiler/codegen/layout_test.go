package codegen

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/expander"
	gxmlparser "github.com/goxml/goxml/internal/compiler/parser"
)

// ============================================================================
// TASK-CODEGEN-001-11：`Gap` 使用原生 Flex.Gap
//
// 验收要求（任务卡）：AST 断言无逐项 Inset。
// 这里刻意**不**用 `strings.Contains(out, "Gap")`——那只证明字符串出现过，
// 不证明值进了 `Flex.Gap`、也不证明单位换算正确。
// ============================================================================

// flexLit 收集生成源码中所有 `layout.Flex{...}` 复合字面量。
func flexLits(t *testing.T, src []byte) []*ast.CompositeLit {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("生成代码无法被 Go 解析（说明生成结果不是合法源码）: %v", err)
	}
	var out []*ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := cl.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "layout" || sel.Sel.Name != "Flex" {
			return true
		}
		out = append(out, cl)
		return true
	})
	return out
}

// gapValues 取所有 Flex 字面量中 Gap 字段的值表达式。
func gapValues(t *testing.T, src []byte) []ast.Expr {
	t.Helper()
	var out []ast.Expr
	for _, cl := range flexLits(t, src) {
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Gap" {
				continue
			}
			out = append(out, kv.Value)
		}
	}
	return out
}

// isDpCall 判断表达式是否为 `gtx.Dp(<数值字面量>)`。
//
// 单位纪律：Gio 的 Flex.Gap 是像素，而 GXML 的 Gap 语义是 dp。
// 直接写 `Gap: 8` 在 DPI≠1 的屏幕上会整体偏小，且只在高分屏暴露。
// 因此断言**必须**是 gtx.Dp 调用，裸整数字面量视为缺陷。
func isDpCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok || x.Name != "gtx" || sel.Sel.Name != "Dp" {
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	_, ok = call.Args[0].(*ast.BasicLit)
	return ok
}

// TestGapUsesNative 是 -11 的主验收：Gap 必须落到原生 Flex.Gap，且不靠 Inset 模拟。
func TestGapUsesNative(t *testing.T) {
	cases := []struct {
		name string
		tag  string
	}{
		{"Column", "Column"},
		{"Row", "Row"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `<` + tc.tag + ` Gap="8"><Text Value="a"/><Text Value="b"/></` + tc.tag + `>`
			out := mustGen(t, body, defaultOpts())

			// 1. 恰好一个 Flex 带 Gap（外层 <Page> 不应有）。
			gaps := gapValues(t, out)
			if len(gaps) != 1 {
				t.Fatalf("Gap 字段应恰好出现 1 次，实际 %d 次\n%s", len(gaps), out)
			}

			// 2. 值必须是 gtx.Dp(<数值>)，不能是裸整数或字符串。
			if !isDpCall(gaps[0]) {
				t.Errorf("Gap 必须是 gtx.Dp(<数值>) 调用（dp→px 换算），实际: %s",
					exprText(gaps[0]))
			}

			// 3. 不得用逐子项 Inset 模拟间距。
			if n := insetCount(out); n != 0 {
				t.Errorf("不应生成 layout.Inset 模拟间距，实际出现 %d 处\n%s", n, out)
			}

			// 4. Gap 不得引入额外的布局层（契约 §3「不产生新轴」）。
			//    Flex 数应恒为 2：外层 <Page> + 本容器。
			if got := len(flexLits(t, out)); got != 2 {
				t.Errorf("Flex 层数应为 2（<Page> + <%s>），实际 %d\n%s", tc.tag, got, out)
			}
		})
	}
}

// TestGapAbsentOmitsField 未指定 Gap 时不写字段（与「默认 0」等价，
// 但保持生成结果最小，不引入无意义字段）。
func TestGapAbsentOmitsField(t *testing.T) {
	out := mustGen(t, `<Column><Text Value="a"/><Text Value="b"/></Column>`, defaultOpts())
	if gaps := gapValues(t, out); len(gaps) != 0 {
		t.Errorf("未指定 Gap 时不应生成 Gap 字段，实际 %d 处\n%s", len(gaps), out)
	}
}

// TestGapZeroIsExplicit 显式 Gap="0" 与缺省在语义上都是 0，
// 但 HasGap 区分二者，因此显式 0 应生成字段（保证「写了就生效」可被观测）。
func TestGapZeroIsExplicit(t *testing.T) {
	out := mustGen(t, `<Column Gap="0"><Text Value="a"/></Column>`, defaultOpts())
	gaps := gapValues(t, out)
	if len(gaps) != 1 {
		t.Fatalf("显式 Gap=0 应生成字段，实际 %d 处\n%s", len(gaps), out)
	}
	if !isDpCall(gaps[0]) {
		t.Errorf("Gap=0 同样应经 gtx.Dp 换算: %s", exprText(gaps[0]))
	}
}

// TestGapValueIsParsedNumber 安全准入第 1 项（TASK-CODEGEN-001 §3.5）：
// 数值属性不得以原始文本拼接。本测试证明写入源码的是**解析后的数值字面量**。
//
// 反向验证方式：若实现改成把 `a.Value.Raw` 直接拼进源码，下列输入会产出
// 非数值字面量或非法源码，测试即失败。
func TestGapValueIsParsedNumber(t *testing.T) {
	for _, raw := range []string{"8", "0", "12.5"} {
		out := mustGen(t, `<Column Gap="`+raw+`"><Text Value="a"/></Column>`, defaultOpts())
		gaps := gapValues(t, out)
		if len(gaps) != 1 {
			t.Fatalf("Gap=%q 应生成字段，实际 %d 处", raw, len(gaps))
		}
		call, ok := gaps[0].(*ast.CallExpr)
		if !ok {
			t.Fatalf("Gap=%q 的值应为调用表达式: %s", raw, exprText(gaps[0]))
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			t.Fatalf("Gap=%q 的实参应为数值字面量，实际: %s", raw, exprText(call.Args[0]))
		}
		if lit.Kind != token.FLOAT && lit.Kind != token.INT {
			t.Errorf("Gap=%q 的字面量种类应为数值，实际 %v", raw, lit.Kind)
		}
		// 十进制形式：不得出现指数记法（规范只接受十进制）。
		if strings.ContainsAny(lit.Value, "eE") {
			t.Errorf("Gap=%q 的字面量含指数记法: %s", raw, lit.Value)
		}
	}
}

// TestGapGeneratedCodeCompiles 真实编译验证（`buildInTempModule` 在临时模块执行 go build）。
//
// 这一条不是冗余：静态页面走的是「录制到 rec」路径，而 Gap 换算用的是 `gtx.Dp`。
// 若 gtx 在该作用域不可用，或类型不匹配（Flex.Gap 是 int 而传了 unit.Dp），
// 只有真实编译能发现——单元测试的字符串/AST 断言看不出来。
func TestGapGeneratedCodeCompiles(t *testing.T) {
	// 只验静态路径：它才是作用域最容易出错的地方（布局录到 rec，换算却用 gtx）。
	// 含绑定的动态路径需要 Options.ModulePath，该集成属另一条线的开放项，
	// 不在这里顺带引入（避免把无关失败混进本任务的验收）。
	buildInTempModule(t, mustGen(t,
		`<Column Gap="8"><Text Value="a"/><Text Value="b"/></Column>`, defaultOpts()))
	buildInTempModule(t, mustGen(t,
		`<Row Gap="12.5"><Text Value="a"/><Text Value="b"/></Row>`, defaultOpts()))
}

// TestGapInjectionInputsRejected 安全准入第 5 项：每个新生成的字面量都要有注入测试。
//
// 这里证明**恶意值到不了生成器**——Parser 已拒绝，因此 Codegen 不可能收到原始文本。
// 若这些输入有任何一个被放行，说明准入第 1 项的前提被破坏。
func TestGapInjectionInputsRejected(t *testing.T) {
	bad := []string{
		`8); panic("x`,      // 跳出表达式
		`1); os.Exit(1); (`, // 注入语句
		`" + "`,             // 字符串拼接
		`-1`,                // 负数
		`1e5`,               // 指数形式
		`8px`,               // 单位后缀
		`{User.Gap}`,        // 绑定
		`Inf`,               // 非有限值
		`NaN`,               // 非有限值
	}
	for _, raw := range bad {
		src := `<Goxml Version="1">
  <Page Route="/"><Column Gap="` + raw + `"><Text Value="a"/></Column></Page>
</Goxml>`
		if diags := frontendDiags(t, src); len(diags) == 0 {
			t.Errorf("Gap=%q 必须被前端拒绝（安全准入第 1 项），实际无诊断", raw)
		}
	}
}

// ============================================================================
// 辅助
// ============================================================================

// frontendDiags 跑 Parser + Expander 并返回诊断；不要求通过。
//
// codegen_test.go 的 runFrontend 在失败时直接 t.Fatal，无法用于负向用例，
// 因此这里单独实现一份「收集诊断」的版本。
func frontendDiags(t *testing.T, src string) []string {
	t.Helper()
	doc, pdiags := gxmlparser.Parse("case.gxml", []byte(src))
	if len(pdiags) > 0 {
		out := make([]string, 0, len(pdiags))
		for _, d := range pdiags {
			out = append(out, d.Error())
		}
		return out
	}
	er := expander.Expand("case.gxml", doc)
	out := make([]string, 0, len(er.Diags))
	for _, d := range er.Diags {
		out = append(out, d.Error())
	}
	return out
}

// insetCount 统计生成源码中 layout.Inset 的出现次数（AST 级，非字符串）。
func insetCount(src []byte) int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		return -1
	}
	n := 0
	ast.Inspect(f, func(node ast.Node) bool {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Inset" {
			n++
		}
		return true
	})
	return n
}

// exprText 把 AST 表达式回显为源码文本，仅用于失败信息。
func exprText(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "<无法回显>"
	}
	return b.String()
}
