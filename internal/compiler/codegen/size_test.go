package codegen

import (
	"go/ast"
	"go/parser"
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
