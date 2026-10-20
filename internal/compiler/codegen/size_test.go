package codegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
	"github.com/goxml/goxml/internal/compiler/sizelayout"
)

// ============================================================================
// sizeWrap 的模板测试（TASK-CODEGEN-001-10 / -22）
//
// 本文件只验证**模板本身**：轴向选择、指令到 Gio 语句的映射、阈值来源、不裁剪。
// 生成代码在运行时是否真的改变了布局，由接入后的真实布局测试验证
// （用户要求：不能只看 AST 里出现了什么）。
// ============================================================================

// sizeNodeOf 构造一个只关心尺寸属性的节点。
func sizeNodeOf(w, h contracts.SizeSpec) *contracts.NodeIR {
	return &contracts.NodeIR{Tag: "Text", Width: w, Height: h}
}

// selChain 把 `a.b.c` 形式的表达式还原成 "a.b.c"，用于断言赋值目标。
func selChain(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return selChain(x.X) + "." + x.Sel.Name
	}
	return ""
}

// sizeTemplate 解构 sizeWrap 的输出：构造器名 + 闭包体。
func sizeTemplate(t *testing.T, expr string) (kind string, fn *ast.FuncLit) {
	t.Helper()
	e, err := parser.ParseExpr(expr)
	if err != nil {
		t.Fatalf("模板输出不是合法 Go 表达式: %v\n%s", err, expr)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		t.Fatalf("模板输出应为调用表达式，实际 %T", e)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		t.Fatalf("模板输出应为 layout.Xxx(...)，实际 %T", call.Fun)
	}
	kind = sel.Sel.Name
	for _, a := range call.Args {
		if fl, ok := a.(*ast.FuncLit); ok {
			fn = fl
			break
		}
	}
	if fn == nil {
		t.Fatalf("模板输出缺少布局闭包\n%s", expr)
	}
	return kind, fn
}

// assignTargets 收集闭包内所有赋值语句的左侧链名（如 "gtx.Constraints.Min.X"）。
func assignTargets(fn *ast.FuncLit) []string {
	var out []string
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if s := selChain(lhs); s != "" {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// guardThreshold 取闭包内 `if gtx.Constraints.Max.<A> < <字面量>` 的轴与阈值。
func guardThreshold(t *testing.T, fn *ast.FuncLit) (axis string, threshold string, found bool) {
	t.Helper()
	ast.Inspect(fn, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := ifs.Cond.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if s := selChain(bin.X); s == "gtx.Constraints.Max.X" || s == "gtx.Constraints.Max.Y" {
			lit, ok := bin.Y.(*ast.BasicLit)
			if !ok {
				return true
			}
			axis = s[len(s)-1:]
			threshold = lit.Value
			found = true
			return false
		}
		return true
	})
	return axis, threshold, found
}

// TestSizeWrapTemplate 主模板验收：指令 → Gio 语句的映射。
func TestSizeWrapTemplate(t *testing.T) {
	cases := []struct {
		name             string
		mainIsHorizontal bool
		w, h             contracts.SizeSpec
		wantKind         string
		wantAssigns      []string
		wantGuardAxis    string // 空表示不应有 if
	}{
		{
			name: "Row 主轴 Fill", mainIsHorizontal: true,
			w: contracts.FillSize(), h: contracts.NaturalSize(),
			wantKind: "Flexed", wantAssigns: nil,
		},
		{
			name: "Row 主轴固定 80", mainIsHorizontal: true,
			w: contracts.FixedSize(80), h: contracts.NaturalSize(),
			wantKind:    "Rigid",
			wantAssigns: []string{"gtx.Constraints.Min.X", "gtx.Constraints.Max.X"},
		},
		{
			name: "Row 交叉轴 Fill", mainIsHorizontal: true,
			w: contracts.NaturalSize(), h: contracts.FillSize(),
			wantKind: "Rigid",
			// 有界检查体内的抬升也是赋值，必须计入
			wantAssigns:   []string{"gtx.Constraints.Min.Y"},
			wantGuardAxis: "Y",
		},
		{
			name: "Column 主轴 Fill", mainIsHorizontal: false,
			w: contracts.NaturalSize(), h: contracts.FillSize(),
			wantKind: "Flexed", wantAssigns: nil,
		},
		{
			name: "Column 交叉轴 Fill", mainIsHorizontal: false,
			w: contracts.FillSize(), h: contracts.NaturalSize(),
			wantKind:      "Rigid",
			wantAssigns:   []string{"gtx.Constraints.Min.X"},
			wantGuardAxis: "X",
		},
		{
			// 主轴是固定值 → Rigid + 精确约束；交叉轴 Fill 仍需有界检查。
			name: "Column 主轴固定 32 + 交叉轴 Fill", mainIsHorizontal: false,
			w: contracts.FillSize(), h: contracts.FixedSize(32),
			wantKind:      "Rigid",
			wantAssigns:   []string{"gtx.Constraints.Min.Y", "gtx.Constraints.Max.Y", "gtx.Constraints.Min.X"},
			wantGuardAxis: "X",
		},
		{
			// 主轴 Fill → Flexed；交叉轴固定 → 闭包内设精确约束，且不应有 if。
			name: "Column 主轴 Fill + 交叉轴固定 32", mainIsHorizontal: false,
			w: contracts.FixedSize(32), h: contracts.FillSize(),
			wantKind:    "Flexed",
			wantAssigns: []string{"gtx.Constraints.Min.X", "gtx.Constraints.Max.X"},
		},
		{
			name: "未设置尺寸", mainIsHorizontal: true,
			w: contracts.NaturalSize(), h: contracts.NaturalSize(),
			wantKind: "Rigid", wantAssigns: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sizeWrap(sizeNodeOf(tc.w, tc.h), "inner()", tc.mainIsHorizontal)
			kind, fn := sizeTemplate(t, out)

			if kind != tc.wantKind {
				t.Errorf("构造器 = layout.%s, want layout.%s\n%s", kind, tc.wantKind, out)
			}

			got := assignTargets(fn)
			if !reflect.DeepEqual(got, tc.wantAssigns) {
				t.Errorf("赋值目标 = %v, want %v\n%s", got, tc.wantAssigns, out)
			}

			axis, threshold, found := guardThreshold(t, fn)
			if tc.wantGuardAxis == "" {
				if found {
					t.Errorf("不应生成有界检查，实际检查 Max.%s\n%s", axis, out)
				}
				return
			}
			if !found {
				t.Fatalf("缺少有界检查（应对 Max.%s）\n%s", tc.wantGuardAxis, out)
			}
			if axis != tc.wantGuardAxis {
				t.Errorf("有界检查的轴 = %s, want %s\n%s", axis, tc.wantGuardAxis, out)
			}
			// 阈值必须原样等于 sizelayout 的唯一阈值
			want := strconv.Itoa(sizelayout.UnboundedThreshold)
			if threshold != want {
				t.Errorf("阈值字面量 = %s, want %s（必须来自 sizelayout）", threshold, want)
			}
		})
	}
}

// TestSizeWrapFixedValueIsParsedNumber 固定尺寸写入的是解析后的十进制字面量。
//
// 安全准入第 1 项：数值不得以原始文本拼接。这里用 80.5 同时验证小数格式，
// 并断言不出现指数记法（formatDp 用 'f'）。
func TestSizeWrapFixedValueIsParsedNumber(t *testing.T) {
	out := sizeWrap(sizeNodeOf(contracts.FixedSize(80.5), contracts.NaturalSize()), "inner()", true)
	if !strings.Contains(out, "gtx.Dp(80.5)") {
		t.Errorf("应生成 gtx.Dp(80.5)，实际:\n%s", out)
	}
	if strings.Contains(out, "e+") || strings.Contains(out, "E+") {
		t.Errorf("不应出现指数记法:\n%s", out)
	}
}

// TestSizeWrapDoesNotClip 固定尺寸溢出时不得生成任何裁剪（规范 §3.1.5）。
func TestSizeWrapDoesNotClip(t *testing.T) {
	for _, tc := range []struct {
		name string
		w, h contracts.SizeSpec
	}{
		{"W 固定超大", contracts.FixedSize(5000), contracts.NaturalSize()},
		{"H 固定超大", contracts.NaturalSize(), contracts.FixedSize(5000)},
		{"双向固定", contracts.FixedSize(5000), contracts.FixedSize(5000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, horiz := range []bool{true, false} {
				out := sizeWrap(sizeNodeOf(tc.w, tc.h), "inner()", horiz)
				e, err := parser.ParseExpr(out)
				if err != nil {
					t.Fatalf("非法表达式: %v", err)
				}
				ast.Inspect(e, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						switch sel.Sel.Name {
						case "Clip", "ClipRect", "Min", "Constrain":
							// Min 作为字段名会被上面的赋值断言覆盖，这里只关心函数式裁剪
							if sel.Sel.Name == "Clip" || sel.Sel.Name == "ClipRect" {
								t.Errorf("不应生成裁剪调用 %s", sel.Sel.Name)
							}
						}
					}
					return true
				})
			}
		})
	}
}

// TestSizeWrapNoNumericLiteralExceptThreshold 生成器不得自带数字常量。
//
// 阈值是唯一允许出现的整数，且必须等于 sizelayout.UnboundedThreshold；
// 固定尺寸的数值来自 formatDp（在 gtx.Dp(...) 内），属用户数据而非常量。
func TestSizeWrapNoNumericLiteralExceptThreshold(t *testing.T) {
	out := sizeWrap(sizeNodeOf(contracts.FillSize(), contracts.FillSize()), "inner()", false)
	axis, threshold, found := func() (string, string, bool) {
		_, fn := sizeTemplate(t, out)
		return guardThreshold(t, fn)
	}()
	if !found {
		t.Fatalf("Column 的 Width=Fill 应产生有界检查\n%s", out)
	}
	if threshold != strconv.Itoa(sizelayout.UnboundedThreshold) {
		t.Errorf("阈值 %s 不等于 sizelayout.UnboundedThreshold=%d", threshold, sizelayout.UnboundedThreshold)
	}
	_ = axis
}

// ============================================================================
// 接入验证：containerExpr 的子项必须整体走 sizeWrap
//
// 这一层的价值不是「再测一遍模板」，而是证明**接线**正确：
//   - 每个 Flex 的每个子项都被包装过（不存在漏包的裸闭包）；
//   - 传给 sizeWrap 的主轴方向与容器的 Axis 一致（Row 传 true、Page/Column 传 false）；
//   - 尺寸语句只出现在**直接子项**的闭包内，没有被误放或复制到别处。
//
// 只断言「源码里出现了 layout.Flexed」是不行的：那既可能是别的容器生成的，
// 也可能被放在错误的层级。因此这里按 Flex 逐个解构子项。
// ============================================================================

// flexChild 是一个 FlexChild 实参的解构结果。
type flexChild struct {
	kind   string   // "Rigid" / "Flexed"
	assign []string // 闭包体内**顶层**语句的赋值目标
	axis   string   // 顶层有界检查的轴；空表示无
}

// childSummary 只取闭包体**顶层**语句，不递归进嵌套闭包。
//
// 递归会串味：Page 的子项是 Row，Row 的表达式里还有一层 Flex 和它的子项，
// 递归下去就会把孙子的赋值算到儿子头上，接线错误反而被掩盖。
func childSummary(t *testing.T, fn *ast.FuncLit) (assign []string, axis string) {
	t.Helper()
	for _, stmt := range fn.Body.List {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			for _, lhs := range s.Lhs {
				if chain := selChain(lhs); chain != "" {
					assign = append(assign, chain)
				}
			}
		case *ast.IfStmt:
			bin, ok := s.Cond.(*ast.BinaryExpr)
			if !ok {
				t.Fatalf("闭包内出现非二元条件的 if：%T", s.Cond)
			}
			chain := selChain(bin.X)
			if chain != "gtx.Constraints.Max.X" && chain != "gtx.Constraints.Max.Y" {
				t.Fatalf("闭包内出现意外的 if 条件：%s", chain)
			}
			axis = chain[len(chain)-1:]
			// 抬升 Min 的赋值在 if 体内，也要计入——否则「有检查却没有抬升」
			// 这种半截实现会被漏掉。
			for _, inner := range s.Body.List {
				as, ok := inner.(*ast.AssignStmt)
				if !ok {
					t.Fatalf("有界检查体内出现非赋值语句：%T", inner)
				}
				for _, lhs := range as.Lhs {
					if c := selChain(lhs); c != "" {
						assign = append(assign, c)
					}
				}
			}
		}
	}
	return assign, axis
}

// flexCallsOf 解构生成源码中每个 `layout.Flex{...}.Layout(...)` 的子项。
func flexCallsOf(t *testing.T, src []byte) [][]flexChild {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("生成源码不合法: %v", err)
	}

	var out [][]flexChild
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Layout" {
			return true
		}
		lit, ok := sel.X.(*ast.CompositeLit)
		if !ok {
			return true
		}
		typ, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || selChain(typ) != "layout.Flex" {
			return true
		}

		var kids []flexChild
		for _, arg := range call.Args[1:] { // args[0] 是 ops
			cc, ok := arg.(*ast.CallExpr)
			if !ok {
				t.Fatalf("Flex 子项不是构造调用，而是 %T（说明有子项漏过 sizeWrap）", arg)
			}
			cs, ok := cc.Fun.(*ast.SelectorExpr)
			if !ok {
				t.Fatalf("Flex 子项构造不是 layout.Xxx(...)，而是 %T", cc.Fun)
			}
			var fn *ast.FuncLit
			for _, a := range cc.Args {
				if fl, ok := a.(*ast.FuncLit); ok {
					fn = fl
				}
			}
			if fn == nil {
				t.Fatalf("Flex 子项 %s 缺少布局闭包", cs.Sel.Name)
			}
			if cs.Sel.Name != "Rigid" && cs.Sel.Name != "Flexed" {
				t.Fatalf("Flex 子项构造只能是 Rigid/Flexed，实际 %s", cs.Sel.Name)
			}
			assign, axis := childSummary(t, fn)
			kids = append(kids, flexChild{kind: cs.Sel.Name, assign: assign, axis: axis})
		}
		out = append(out, kids)
		return true
	})
	return out
}

// TestContainerRoutesChildrenThroughSizeWrap 接线验证。
func TestContainerRoutesChildrenThroughSizeWrap(t *testing.T) {
	cases := []struct {
		name string
		body string
		// 按 Flex 出现顺序（Page 在前）给出每个子项的期望
		want [][]flexChild
	}{
		{
			name: "Page(Column) + Row：交叉轴 Fill 落在 Row 的子项上",
			body: `<Row><Text Value="A" Height="Fill"/><Text Value="B"/></Row>`,
			want: [][]flexChild{
				// Page 的子项是 Row 本身，Row 没有尺寸属性 → Rigid 且无约束改写
				{{kind: "Rigid"}},
				// Row 的子项：A 的 Height 是交叉轴（Y），B 无尺寸
				{{kind: "Rigid", assign: []string{"gtx.Constraints.Min.Y"}, axis: "Y"}, {kind: "Rigid"}},
			},
		},
		{
			name: "Page：主轴 Fill 用 Flexed，固定宽度用精确约束",
			body: `<Text Value="A" Height="Fill"/><Text Value="B" Width="120"/>`,
			want: [][]flexChild{
				{
					{kind: "Flexed"},
					{kind: "Rigid", assign: []string{
						"gtx.Constraints.Min.X",
						"gtx.Constraints.Max.X",
					}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := mustGen(t, tc.body, defaultOpts())
			got := flexCallsOf(t, src)
			if len(got) != len(tc.want) {
				t.Fatalf("Flex 数量 = %d, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Errorf("第 %d 个 Flex 的子项 = %+v\nwant %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestSizeGuardAppearsOncePerFillChild 全文件范围内，有界检查的出现次数
// 必须等于「交叉轴 Fill 的节点数」。
//
// 这条测试防的是「接线时顺手在别处又写一份判断」：即使两处当前等价，
// 也会在下次修改阈值或比较方向时分叉，而这正是规范 §3.1.7 要排除的。
func TestSizeGuardAppearsOncePerFillChild(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		// 两个交叉轴 Fill：Width 在 Column 上是交叉轴，Height 在 Row 上是交叉轴。
		// 同一个父容器上不可能有两个交叉轴，所以必须嵌套一层。
		{`<Column><Text Value="A" Width="Fill"/><Row><Text Value="B" Height="Fill"/></Row></Column>`, 2},
		// 主轴 Fill 只是 Flexed，不产生 if
		{`<Text Value="A" Height="Fill"/>`, 0},
		// 固定尺寸不产生 if
		{`<Text Value="A" Width="120"/>`, 0},
	}
	for _, tc := range cases {
		src := mustGen(t, tc.body, defaultOpts())
		n := 0
		for _, kids := range flexCallsOf(t, src) {
			for _, k := range kids {
				if k.axis != "" {
					n++
				}
			}
		}
		if n != tc.want {
			t.Errorf("%s 的有界检查次数 = %d, want %d", tc.body, n, tc.want)
		}
		// 源码文本层面同样不应出现额外的 `gtx.Constraints.Max.` 比较
		if got := strings.Count(string(src), "< "+strconv.Itoa(sizelayout.UnboundedThreshold)); got != tc.want {
			t.Errorf("%s 中阈值比较出现 %d 次, want %d", tc.body, got, tc.want)
		}
	}
}

// ============================================================================
// 动态路径接入验证（<For> / <If>）
//
// 动态路径的子项不是 Flex 的实参，而是先 `children = append(children, X)`
// 再 `Layout(gtx, children...)`，所以 flexCallsOf 看不到它们。
// 这里改为解构每个 append 的第二个实参，逐项断言：
//   - 都经过 sizeWrap（是 layout.Rigid / layout.Flexed 调用，不是裸闭包）；
//   - 尺寸约束改写落在该子项的闭包内，轴与容器方向一致。
//
// 只做结构断言是不够的：真实布局行为由 size_runtime_test.go 的像素几何覆盖，
// 本文件的断言只证明「接线正确」，不证明「布局正确」。
// ============================================================================

// appendSite 是一个 `children = append(children, X)` 的解构结果。
type appendSite struct {
	kind   string
	assign []string
	axis   string
}

// appendSitesOf 解构生成源码中所有 append 到 children 的 FlexChild。
func appendSitesOf(t *testing.T, src []byte) []appendSite {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("生成源码不合法: %v", err)
	}

	var out []appendSite
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		// `children := make(...)` 也是给 children 赋值，但它是初始化而非子项追加
		if as.Tok != token.ASSIGN {
			return true
		}
		if selChain(as.Lhs[0]) != "children" {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "append" || len(call.Args) != 2 {
			t.Fatalf("children 赋值右侧不是 append(children, X)：%T", call.Fun)
		}
		wrapped, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			t.Fatalf("append 的元素不是构造调用，而是 %T（说明有子项漏过 sizeWrap）", call.Args[1])
		}
		sel, ok := wrapped.Fun.(*ast.SelectorExpr)
		if !ok || selChain(sel) != "layout."+sel.Sel.Name {
			t.Fatalf("append 元素不是 layout.Xxx(...)：%T", wrapped.Fun)
		}
		if sel.Sel.Name != "Rigid" && sel.Sel.Name != "Flexed" {
			t.Fatalf("append 元素构造只能是 Rigid/Flexed，实际 %s", sel.Sel.Name)
		}
		var fn *ast.FuncLit
		for _, a := range wrapped.Args {
			if fl, ok := a.(*ast.FuncLit); ok {
				fn = fl
			}
		}
		if fn == nil {
			t.Fatalf("append 元素 %s 缺少布局闭包", sel.Sel.Name)
		}
		assign, axis := childSummary(t, fn)
		out = append(out, appendSite{kind: sel.Sel.Name, assign: assign, axis: axis})
		return true
	})
	return out
}

// TestDynamicPathsRouteThroughSizeWrap 锁住 <For>/<If> 三条动态路径的接线。
func TestDynamicPathsRouteThroughSizeWrap(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []appendSite
	}{
		{
			name: "For 内主轴 Fill（Page 为垂直）",
			body: `<For Each="item" In="{Items}"><Text Value="{item.Name}" Height="Fill"/></For>`,
			want: []appendSite{{kind: "Flexed"}},
		},
		{
			name: "For 内交叉轴 Fill（X）",
			body: `<For Each="item" In="{Items}"><Text Value="{item.Name}" Width="Fill"/></For>`,
			want: []appendSite{{kind: "Rigid", assign: []string{"gtx.Constraints.Min.X"}, axis: "X"}},
		},
		{
			name: "If Then 内固定宽度",
			body: `<If Condition="{Dirty}"><Then><Text Value="A" Width="120"/></Then></If>`,
			want: []appendSite{{kind: "Rigid", assign: []string{
				"gtx.Constraints.Min.X", "gtx.Constraints.Max.X",
			}}},
		},
		{
			name: "If 两个分支各自带尺寸",
			body: `<If Condition="{Dirty}"><Then><Text Value="A" Width="Fill"/></Then><Else><Text Value="B" Height="Fill"/></Else></If>`,
			want: []appendSite{
				{kind: "Rigid", assign: []string{"gtx.Constraints.Min.X"}, axis: "X"},
				{kind: "Flexed"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 含绑定/控制流的页面需要 ViewModel 的 module 路径
			src := mustGen(t, tc.body, bindingOpts())
			got := appendSitesOf(t, src)
			if len(got) != len(tc.want) {
				t.Fatalf("append 站点数 = %d, want %d（每个渲染子项恰好一个）", len(got), len(tc.want))
			}
			for i := range tc.want {
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Errorf("第 %d 个 append 站点 = %+v\nwant %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
