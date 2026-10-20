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

// flexFieldValues 取所有 Flex 字面量中指定字段的值表达式。
func flexFieldValues(t *testing.T, src []byte, field string) []ast.Expr {
	t.Helper()
	var out []ast.Expr
	for _, cl := range flexLits(t, src) {
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != field {
				continue
			}
			out = append(out, kv.Value)
		}
	}
	return out
}

// gapValues 取所有 Flex 字面量中 Gap 字段的值表达式。
func gapValues(t *testing.T, src []byte) []ast.Expr {
	t.Helper()
	return flexFieldValues(t, src, "Gap")
}

// selectorName 取 `pkg.Name` 形式表达式的成员名；非选择器时返回空串。
//
// 用于断言 Alignment 写的是哪个 Gio 常量——只判断「字段存在」会被
// 「改成 layout.Start 兜底」蒙混过关，必须看到具体的成员名。
func selectorName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	return sel.Sel.Name
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
// TASK-CODEGEN-001-8：`Align` 交叉轴对齐
//
// 契约依据：规范 §3.1.2（取值、轴向感知默认值）、§3.1.3（Baseline 限定）、
// ADR-0008 §2.1/§2.3（Gio 映射与默认值的实现责任）、§2.6.1（无 AlignSelf）。
// ============================================================================

// flexWithField 返回第一个含指定字段的 Flex 字面量。
//
// 不能直接取 flexLits[1]：外层 <Page> 与本容器都是 Flex，
// 靠下标假设顺序会把「字段写到了错误的 Flex 上」也判为通过。
func flexWithField(t *testing.T, src []byte, field string) *ast.CompositeLit {
	t.Helper()
	for _, cl := range flexLits(t, src) {
		if flexField(cl, field) != nil {
			return cl
		}
	}
	t.Fatalf("没有找到含 %s 字段的 layout.Flex\n%s", field, src)
	return nil
}

// flexField 取 Flex 字面量中指定字段的值表达式，不存在时返回 nil。
func flexField(cl *ast.CompositeLit, name string) ast.Expr {
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if ok && key.Name == name {
			return kv.Value
		}
	}
	return nil
}

// TestGenerateAlign 是 -8 的主验收：四个取值在两个轴向上的映射。
//
// 关键陷阱（ADR-0008 §2.1）：GXML 的 `Center` 对应 Gio 的 `layout.Middle`——
// Gio 的常量是 Start/End/Middle/Baseline，**没有 Center**。
func TestGenerateAlign(t *testing.T) {
	cases := []struct {
		tag      string
		align    string
		wantAxis string
		want     string
	}{
		{"Column", "Start", "Vertical", "Start"},
		{"Column", "Center", "Vertical", "Middle"},
		{"Column", "End", "Vertical", "End"},
		{"Row", "Start", "Horizontal", "Start"},
		{"Row", "Center", "Horizontal", "Middle"},
		{"Row", "End", "Horizontal", "End"},
		// Baseline 仅 <Row> 合法（规范 §3.1.3）；<Column> 的拒绝见 TestAlignBaselineOnlyOnRow。
		{"Row", "Baseline", "Horizontal", "Baseline"},
	}

	for _, tc := range cases {
		t.Run(tc.tag+"/"+tc.align, func(t *testing.T) {
			body := `<` + tc.tag + ` Align="` + tc.align + `">` +
				`<Text Value="a"/><Text Value="b"/></` + tc.tag + `>`
			out := mustGen(t, body, defaultOpts())

			// 恰好一个 Flex 带 Alignment：外层 <Page> 不消费 Align
			// （契约 §3.1/§4.7.2：Page 等价无布局属性的 Column）。
			aligns := flexFieldValues(t, out, "Alignment")
			if len(aligns) != 1 {
				t.Fatalf("Alignment 字段应恰好出现 1 次，实际 %d 次\n%s", len(aligns), out)
			}
			if got := selectorName(aligns[0]); got != tc.want {
				t.Errorf("<%s Align=%q> 应生成 layout.%s，实际: %s",
					tc.tag, tc.align, tc.want, exprText(aligns[0]))
			}

			// 同一字面量上的轴向必须正确：对齐是交叉轴概念，写错轴向的 Flex 等于对空气对齐。
			cl := flexWithField(t, out, "Alignment")
			if got := selectorName(flexField(cl, "Axis")); got != tc.wantAxis {
				t.Errorf("<%s> 的 Axis 应为 layout.%s，实际: %s",
					tc.tag, tc.wantAxis, exprText(flexField(cl, "Axis")))
			}
		})
	}
}

// TestAlignDefaultsAreAxisAware 任务卡 §1.3：轴向感知默认值是 **Codegen** 的责任，
// IR 层保持 AlignUnset。
//
// 这一条不能省：`<Row>` 的默认值（Center→Middle）与 Gio 零值（Start）**不一致**
// （ADR-0008 §2.3 称其为「唯一难以回退的决定」）。若不显式生成，
// 未写 Align 的 <Row> 会静默变成顶部对齐，且没有任何报错可循。
func TestAlignDefaultsAreAxisAware(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{"Row", "Middle"},
		{"Column", "Start"},
	}
	for _, tc := range cases {
		t.Run(tc.tag, func(t *testing.T) {
			body := `<` + tc.tag + `><Text Value="a"/><Text Value="b"/></` + tc.tag + `>`
			out := mustGen(t, body, defaultOpts())

			aligns := flexFieldValues(t, out, "Alignment")
			if len(aligns) != 1 {
				t.Fatalf("未写 Align 的 <%s> 也应显式生成默认对齐，实际 %d 处\n%s",
					tc.tag, len(aligns), out)
			}
			if got := selectorName(aligns[0]); got != tc.want {
				t.Errorf("<%s> 默认对齐应为 layout.%s，实际: %s",
					tc.tag, tc.want, exprText(aligns[0]))
			}
		})
	}
}

// TestAlignBaselineOnlyOnRow 规范 §3.1.3：Baseline 只在水平排列中有意义，
// 其余容器上是编译错误，Codegen **不应收到**这种输入。
func TestAlignBaselineOnlyOnRow(t *testing.T) {
	// 合法：只有 <Row>。
	rowSrc := `<Goxml Version="1">
  <Page Route="/"><Row Align="Baseline"><Text Value="a"/><Text Value="b"/></Row></Page>
</Goxml>`
	if diags := frontendDiags(t, rowSrc); len(diags) != 0 {
		t.Errorf("<Row Align=\"Baseline\"> 是规范允许的，不应有诊断: %v", diags)
	}

	for _, tag := range []string{"Column", "Grid"} {
		src := `<Goxml Version="1">
  <Page Route="/"><` + tag + ` Align="Baseline"><Text Value="a"/></` + tag + `></Page>
</Goxml>`
		diags := frontendDiags(t, src)
		if len(diags) == 0 {
			t.Errorf("<%s Align=\"Baseline\"> 必须被前端拒绝（规范 §3.1.3），实际无诊断", tag)
			continue
		}
		// 必须是针对 Baseline 的诊断，而不是碰巧别的错误（例如 Grid 缺 Columns）。
		joined := strings.Join(diags, "\n")
		if !strings.Contains(joined, "Baseline") {
			t.Errorf("<%s> 的诊断应指出 Baseline 不合法，实际: %s", tag, joined)
		}
	}
}

// TestAlignInvalidValuesRejected 映射边界的非法值防御。
//
// `ParseCrossAlign` 大小写敏感，且只接受 GXML 的四个取值名。
// 这里特别包含 Gio 侧的 `Middle`——它是 Gio 常量名，但不是 GXML 取值：
// 若有人误把 Gio 名字当 GXML 名字接受，语言层与实现层就耦合了。
func TestAlignInvalidValuesRejected(t *testing.T) {
	bad := []string{
		"Middle",       // Gio 常量名，不是 GXML 取值
		"middle",       // 大小写敏感
		"center",       // 大小写敏感
		"CENTER",       // 大小写敏感
		"Stretch",      // CSS 惯性
		"SpaceBetween", // 那是 Justify 的取值
		"",             // 空
		"{User.Align}", // 绑定（v1 不支持动态对齐）
		"Start; End",   // 多值
	}
	for _, raw := range bad {
		src := `<Goxml Version="1">
  <Page Route="/"><Row Align="` + raw + `"><Text Value="a"/></Row></Page>
</Goxml>`
		if diags := frontendDiags(t, src); len(diags) == 0 {
			t.Errorf("Align=%q 必须被前端拒绝，实际无诊断", raw)
		}
	}
}

// TestAlignWithGap 两个布局字段必须落在**同一个** Flex 字面量上。
//
// 单独验证 Gap 与 Align 各自存在是不够的：若二者被写到不同的 Flex
// （例如给 Align 额外套一层容器），间距与对齐就会作用在不同层级，
// 视觉上「间距对了、对齐丢了」，而两者各自的断言都是绿的。
func TestAlignWithGap(t *testing.T) {
	out := mustGen(t, `<Row Gap="8" Align="Center"><Text Value="a"/><Text Value="b"/></Row>`,
		defaultOpts())
	cl := flexWithField(t, out, "Alignment")
	if flexField(cl, "Gap") == nil {
		t.Errorf("Gap 与 Alignment 应在同一个 layout.Flex 上\n%s", out)
	}
	if got := selectorName(flexField(cl, "Alignment")); got != "Middle" {
		t.Errorf("Align=Center 应生成 layout.Middle，实际: %s",
			exprText(flexField(cl, "Alignment")))
	}
}

// TestAlignNoAlignSelf ADR-0008 §2.6.1：v1 不提供子项级对齐，
// 因此不得为对齐引入任何包装层（子项必须仍是 Flex 的直接 Rigid 子项）。
//
// 包装层（layout.Stack）会破坏静态子树固化并带来每帧闭包分配，
// 与 INV-PERF-1/2 冲突——这正是 v1 宁可不提供 AlignSelf 的原因。
// TestAlignNoAlignSelf 的断言不能只数全文件的 Rigid：外层 <Page> 自己也会
// 用一个 Rigid 包住本容器，总数因此是 3 而非 2——只数总数会把「子项被多包一层」
// 与「父容器多一个子项」混为一谈。这里数的是**带 Alignment 的那个 Flex** 的直接子项。
func TestAlignNoAlignSelf(t *testing.T) {
	out := mustGen(t, `<Row Align="End"><Text Value="a"/><Text Value="b"/></Row>`, defaultOpts())
	if n := selCount(out, "Stack"); n != 0 {
		t.Errorf("对齐不得引入 layout.Stack 包装层，实际 %d 处\n%s", n, out)
	}
	if got := flexRigidChildren(t, out, "Alignment"); got != 2 {
		t.Errorf("两个直接子项应仍是带 Alignment 的 Flex 的直接 Rigid 子项，"+
			"实际 %d 个\n%s", got, out)
	}
}

// TestAlignGeneratedCodeCompiles 真实编译验证。
//
// 枚举映射最容易出的错是「写了一个不存在的常量名」（例如直觉写 layout.Center）。
// AST 断言能查到成员名，但只有真实编译能证明这个名字确实存在于 Gio。
func TestAlignGeneratedCodeCompiles(t *testing.T) {
	for _, src := range []string{
		`<Column Align="Center"><Text Value="a"/><Text Value="b"/></Column>`,
		`<Row Align="Baseline"><Text Value="a"/><Text Value="b"/></Row>`,
		`<Row Gap="8" Align="End"><Text Value="a"/></Row>`,
	} {
		buildInTempModule(t, mustGen(t, src, defaultOpts()))
	}
}

// ============================================================================
// TASK-CODEGEN-001-12：`Baseline` 禁止零值兜底
//
// 规范 §3.1.3 把 `Baseline == 0` 定为**合法值**。Gio 的
// `layout.Dimensions.Baseline` 是「从底边上量到首行基线」的距离
// （`go doc gioui.org/layout.Dimensions`），取 0 即基线压在控件底边。
// `layout.Flex` 正是这样消费它的（gioui.org@v0.10.3/layout/flex.go）：
//
//	:197    b := dims.Size.Y - dims.Baseline
//	:204    case Baseline: cross = maxBaseline - b
//
// 所以 `if d.Baseline == 0 { d.Baseline = d.Size.Y }` 不是「补齐缺失信息」，
// 而是把一个确定的语义**推翻**成相反的（基线挪到顶边）。这种错误在单字号
// 排版下几乎看不出来，只在多字号混排时才显现——正是最该用测试钉住的一类。
// ============================================================================

// baselineScan 是一次 Baseline 扫描的结果。
type baselineScan struct {
	writes  []string // 违规写点：赋值 / 自增自减 / 零值比较
	assigns int      // 全文件赋值语句总数，用于证明扫描确实走到了赋值节点
	selRefs int      // 以 `.Baseline` 结尾的选择器次数（含 layout.Baseline 常量）
}

// isBaselineSel 判断表达式是否形如 `xxx.Baseline`。
func isBaselineSel(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Baseline"
}

// scanBaseline 扫描源码中的 Baseline 写点。
//
// 只把「写」与「零值比较」算违规：`layout.Dimensions{Baseline: x}` 这类
// 结构体字段**键**是合法的基线声明（将来控件实现要走这条路），它不是
// 赋值语句，因此这里不会误判——这是本检测器必须区分的地方。
func scanBaseline(t *testing.T, src []byte) baselineScan {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("源码不合法: %v", err)
	}
	var out baselineScan
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if x.Sel.Name == "Baseline" {
				out.selRefs++
			}
		case *ast.AssignStmt:
			out.assigns++
			for _, lhs := range x.Lhs {
				if isBaselineSel(lhs) {
					out.writes = append(out.writes, "赋值 "+selChain(lhs))
				}
			}
		case *ast.IncDecStmt:
			if isBaselineSel(x.X) {
				out.writes = append(out.writes, "自增/自减 "+selChain(x.X))
			}
		case *ast.BinaryExpr:
			if x.Op == token.EQL || x.Op == token.NEQ {
				if isBaselineSel(x.X) || isBaselineSel(x.Y) {
					out.writes = append(out.writes, "零值比较 "+exprText(x))
				}
			}
		}
		return true
	})
	return out
}

// TestBaselineNoFallback 是 -12 的主验收：生成代码不得对 `d.Baseline` 做零值兜底。
//
// 三个子用例缺一不可：
//   - 第一个覆盖「只有对齐、没有尺寸」的普通形态；
//   - 第二个刻意让文件里**确实有**赋值语句，否则「没有 Baseline 赋值」
//     可能只是因为整份代码压根没有赋值——那种绿是假绿；
//   - 第三个是检测器自检：手写的违规片段必须被同一个检测器抓到。
//     没有它，检测器漏看 if 体也会让前两条通过。
func TestBaselineNoFallback(t *testing.T) {
	reportWrites := func(t *testing.T, src []byte, got baselineScan) {
		t.Helper()
		if len(got.writes) != 0 {
			t.Errorf("生成代码出现 Baseline 写点（规范 §3.1.3 禁止零值兜底）:\n  %s\n\n%s",
				strings.Join(got.writes, "\n  "), src)
		}
	}

	t.Run("Row/Baseline 只产出对齐常量", func(t *testing.T) {
		src := mustGen(t, `<Row Align="Baseline"><Text Value="a"/><Text Value="b"/></Row>`,
			defaultOpts())
		got := scanBaseline(t, src)
		reportWrites(t, src, got)

		// 全文件只应有**一个** Baseline 选择器：对齐常量本身。
		// 多出来的引用必然是新增的兜底逻辑或被写死的基线值。
		if got.selRefs != 1 {
			t.Errorf("Baseline 选择器出现 %d 次, want 1（只应有 layout.Baseline）\n%s",
				got.selRefs, src)
		}

		aligns := flexFieldValues(t, src, "Alignment")
		if len(aligns) != 1 {
			t.Fatalf("应恰好一处 Alignment，实际 %d 处\n%s", len(aligns), src)
		}
		if name := selectorName(aligns[0]); name != "Baseline" {
			t.Errorf("<Row Align=\"Baseline\"> 应生成 layout.Baseline，实际 %s",
				exprText(aligns[0]))
		}
	})

	t.Run("含尺寸约束的容器内同样没有兜底", func(t *testing.T) {
		// 固定宽度 → 精确约束赋值；Height="Fill" 在 Row 上是交叉轴 →
		// 有界检查 + 抬 Min。两者都会产生赋值语句。
		src := mustGen(t,
			`<Column><Row Align="Baseline"><Text Value="a" Width="120"/>`+
				`<Text Value="b" Height="Fill"/></Row></Column>`, defaultOpts())
		got := scanBaseline(t, src)
		if got.assigns == 0 {
			t.Fatalf("本例应产生约束赋值；扫描到 0 条赋值说明断言无效\n%s", src)
		}
		reportWrites(t, src, got)
	})

	t.Run("检测器自检：手写违规必须被抓到", func(t *testing.T) {
		bad := []string{
			// 任务卡点名禁止的写法
			"func f(d *layout.Dimensions) {\n\tif d.Baseline == 0 {\n\t\td.Baseline = d.Size.Y\n\t}\n}",
			// 换一种比较方向
			"func f(d *layout.Dimensions) {\n\tif d.Baseline != 0 {\n\t\treturn\n\t}\n\td.Baseline = d.Size.Y\n}",
			// 不走比较，直接累加
			"func f(d *layout.Dimensions) {\n\td.Baseline += d.Size.Y\n}",
		}
		for _, src := range bad {
			got := scanBaseline(t, []byte("package p\n\n"+src))
			if len(got.writes) == 0 {
				t.Errorf("检测器漏报违规片段:\n%s", src)
			}
		}
		// 反向自检：合法的结构体字段键不得被误判，否则检测器会把将来的
		// 正确实现（由控件自己声明基线）也拦下来。
		ok := "func f() layout.Dimensions {\n\treturn layout.Dimensions{Baseline: 3}\n}"
		if got := scanBaseline(t, []byte("package p\n\n"+ok)); len(got.writes) != 0 {
			t.Errorf("结构体字段键 Baseline: 被误判为兜底: %v", got.writes)
		}
	})
}

// ============================================================================
// TASK-CODEGEN-001-9：`Justify` 主轴剩余空间分布
//
// 契约依据：规范 §3.1.2（六个取值、缺省 Start）、ADR-0008 §2.4（映射表与反转警告）。
// ============================================================================

// TestJustifyMapping 是 -9 的主验收：六个取值在两个轴向上的映射。
//
// 映射表（ADR-0008 §2.4）：
//
//	Start→SpaceEnd、Center→SpaceSides、End→SpaceStart，
//	SpaceBetween/SpaceAround/SpaceEvenly 同名。
func TestJustifyMapping(t *testing.T) {
	cases := []struct {
		justify string
		want    string
	}{
		{"Start", "SpaceEnd"},
		{"Center", "SpaceSides"},
		{"End", "SpaceStart"},
		{"SpaceBetween", "SpaceBetween"},
		{"SpaceAround", "SpaceAround"},
		{"SpaceEvenly", "SpaceEvenly"},
	}

	for _, tag := range []string{"Column", "Row"} {
		for _, tc := range cases {
			t.Run(tag+"/"+tc.justify, func(t *testing.T) {
				body := `<` + tag + ` Justify="` + tc.justify + `">` +
					`<Text Value="a"/><Text Value="b"/></` + tag + `>`
				out := mustGen(t, body, defaultOpts())

				spacings := flexFieldValues(t, out, "Spacing")
				if len(spacings) != 1 {
					t.Fatalf("Spacing 字段应恰好出现 1 次（外层 <Page> 不消费 Justify），"+
						"实际 %d 次\n%s", len(spacings), out)
				}
				if got := selectorName(spacings[0]); got != tc.want {
					t.Errorf("<%s Justify=%q> 应生成 layout.%s，实际: %s",
						tag, tc.justify, tc.want, exprText(spacings[0]))
				}

				// 同一字面量上的轴向：主轴方向错了，分布作用的就是另一个轴。
				wantAxis := "Vertical"
				if tag == "Row" {
					wantAxis = "Horizontal"
				}
				cl := flexWithField(t, out, "Spacing")
				if got := selectorName(flexField(cl, "Axis")); got != wantAxis {
					t.Errorf("<%s> 的 Axis 应为 layout.%s，实际: %s",
						tag, wantAxis, exprText(flexField(cl, "Axis")))
				}
			})
		}
	}
}

// TestJustifySpacingDirectionIsReversed 专门锁住 ADR-0008 §2.4 的反转警告。
//
// 与 TestJustifyMapping 的区别：那条证明「映射正确」，这条证明「不是同名赋值」——
// 后者才是真实世界里会犯的错，因为 `layout.SpaceStart` / `layout.SpaceEnd` 两个
// 常量**确实存在**，按名字直译能编译、能跑，只是视觉上前后颠倒。
func TestJustifySpacingDirectionIsReversed(t *testing.T) {
	// 键是 GXML 取值，值是「按名字直译会得到的错值」。
	naive := map[string]string{
		"Start":  "SpaceStart",
		"End":    "SpaceEnd",
		"Center": "SpaceCenter", // Gio 无此常量，直译会编译失败（较好的失败）
	}
	for justify, wrong := range naive {
		body := `<Row Justify="` + justify + `"><Text Value="a"/><Text Value="b"/></Row>`
		out := mustGen(t, body, defaultOpts())
		spacings := flexFieldValues(t, out, "Spacing")
		if len(spacings) != 1 {
			t.Fatalf("Justify=%q 应生成 1 处 Spacing，实际 %d 处", justify, len(spacings))
		}
		if got := selectorName(spacings[0]); got == wrong {
			t.Errorf("Justify=%q 被按名字直译为 layout.%s——"+
				"Gio 的 Spacing 描述空白的位置，方向相反（ADR-0008 §2.4）", justify, wrong)
		}
	}
}

// TestJustifyDefaultsToStart 规范 §3.1.2：Justify 缺省为 Start（剩余空间留在末尾），
// 即 layout.SpaceEnd。
//
// 这里刻意**显式生成**默认值，尽管 SpaceEnd 恰是 Gio 的零值、省略也等价——
// 理由见 flexFieldsExpr 的注释：不把语言语义藏进「碰巧等于零值」的实现细节。
func TestJustifyDefaultsToStart(t *testing.T) {
	for _, tag := range []string{"Column", "Row"} {
		out := mustGen(t, `<`+tag+`><Text Value="a"/><Text Value="b"/></`+tag+`>`,
			defaultOpts())
		spacings := flexFieldValues(t, out, "Spacing")
		if len(spacings) != 1 {
			t.Fatalf("未写 Justify 的 <%s> 也应显式生成默认分布，实际 %d 处\n%s",
				tag, len(spacings), out)
		}
		if got := selectorName(spacings[0]); got != "SpaceEnd" {
			t.Errorf("<%s> 默认 Justify 应为 layout.SpaceEnd，实际: %s",
				tag, exprText(spacings[0]))
		}
	}
}

// TestJustifyInvalidValuesRejected 映射边界的非法值防御。
func TestJustifyInvalidValuesRejected(t *testing.T) {
	bad := []string{
		"spacebetween",  // 大小写敏感
		"SPACEBETWEEN",  // 大小写敏感
		"space-between", // CSS 惯性
		"FlexStart",     // CSS 惯性
		"Middle",        // 那是 Align 的语义
		"Stretch",       // CSS 惯性
		"",              // 空
		"{User.J}",      // 绑定
		"Start End",     // 多值
	}
	for _, raw := range bad {
		src := `<Goxml Version="1">
  <Page Route="/"><Row Justify="` + raw + `"><Text Value="a"/></Row></Page>
</Goxml>`
		if diags := frontendDiags(t, src); len(diags) == 0 {
			t.Errorf("Justify=%q 必须被前端拒绝，实际无诊断", raw)
		}
	}
}

// TestJustifyWithAlignAndGap 三个布局字段必须落在**同一个** Flex 字面量上。
//
// 分开写会各自断言全绿而视觉全错（间距/对齐/分布作用在错误的层级）。
func TestJustifyWithAlignAndGap(t *testing.T) {
	out := mustGen(t,
		`<Row Gap="8" Align="Center" Justify="SpaceBetween">`+
			`<Text Value="a"/><Text Value="b"/></Row>`, defaultOpts())
	cl := flexWithField(t, out, "Spacing")
	for _, field := range []string{"Alignment", "Gap"} {
		if flexField(cl, field) == nil {
			t.Errorf("%s 应与 Spacing 在同一个 layout.Flex 上\n%s", field, out)
		}
	}
	if got := selectorName(flexField(cl, "Spacing")); got != "SpaceBetween" {
		t.Errorf("Justify=SpaceBetween 应生成 layout.SpaceBetween，实际: %s",
			exprText(flexField(cl, "Spacing")))
	}
	if got := selectorName(flexField(cl, "Alignment")); got != "Middle" {
		t.Errorf("Align=Center 应生成 layout.Middle，实际: %s",
			exprText(flexField(cl, "Alignment")))
	}
}

// TestJustifyGeneratedCodeCompiles 真实编译验证：证明 SpaceEvenly 等常量确实存在。
func TestJustifyGeneratedCodeCompiles(t *testing.T) {
	for _, src := range []string{
		`<Column Justify="SpaceEvenly"><Text Value="a"/><Text Value="b"/></Column>`,
		`<Row Justify="End"><Text Value="a"/><Text Value="b"/></Row>`,
		`<Row Gap="8" Align="Center" Justify="SpaceAround"><Text Value="a"/></Row>`,
	} {
		buildInTempModule(t, mustGen(t, src, defaultOpts()))
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

// flexRigidChildren 数「含指定字段的 layout.Flex」的直接 Rigid 子项个数。
//
// 判断对象是 Flex 字面量所在的 `.Layout(...)` 调用的实参，
// 因此统计到的必定是该容器自己的直接子项，不含外层容器的包装。
func flexRigidChildren(t *testing.T, src []byte, field string) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("生成代码无法被 Go 解析: %v", err)
	}
	got := -1
	ast.Inspect(f, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Layout" {
			return true
		}
		cl, ok := sel.X.(*ast.CompositeLit)
		if !ok || !isLayoutFlex(cl) || flexField(cl, field) == nil {
			return true
		}
		n := 0
		for _, a := range call.Args {
			if call2, ok := a.(*ast.CallExpr); ok {
				if sel2, ok := call2.Fun.(*ast.SelectorExpr); ok && sel2.Sel.Name == "Rigid" {
					n++
				}
			}
		}
		got = n
		return false
	})
	return got
}

// isLayoutFlex 判断复合字面量是否为 `layout.Flex{...}`。
func isLayoutFlex(cl *ast.CompositeLit) bool {
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "layout" && sel.Sel.Name == "Flex"
}

// selCount 统计生成源码中指定选择器成员名的出现次数（AST 级，非字符串）。
func selCount(src []byte, name string) int {
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
		if sel.Sel.Name == name {
			n++
		}
		return true
	})
	return n
}

// insetCount 统计 layout.Inset 的出现次数。
func insetCount(src []byte) int { return selCount(src, "Inset") }

// exprText 把 AST 表达式回显为源码文本，仅用于失败信息。
func exprText(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		return "<无法回显>"
	}
	return b.String()
}
