package codegen

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// A4 验收：静态 <Text> 节点缓存闭环（契约 §16.8）
//
// 验收原则：**不得**把「生成源码里出现 op.Record / op.CallOp」当作通过。
// 必须证明：字段覆盖、生成确定性、缓存确实回放、key 变化触发重录。
//
// 其中「确实回放」需要在**运行时**证明——见 runtime_cache_test.go，
// 那里把生成源码放进临时模块，用计数 View 观测实际录制次数。
// ============================================================================

// ---------------------------------------------------------------------------
// A4-T1 字段覆盖（静态检查：字段存在；运行时行为见 runtime 测试）
// ---------------------------------------------------------------------------

func TestCacheStructFields(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	f := parseGo(t, out)

	// 找到生成的 View 类型。
	structName := "GoxmlDashboardView"
	var st *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != structName {
			return true
		}
		s, ok := ts.Type.(*ast.StructType)
		if ok {
			st = s
		}
		return false
	})
	if st == nil {
		t.Fatalf("未找到 %s 结构体", structName)
	}

	want := map[string]string{
		"cacheOps":  "op.Ops",
		"cacheCall": "op.CallOp",
		"cacheDims": "layout.Dimensions",
		"cached":    "bool",
	}
	got := map[string]string{}
	for _, fld := range st.Fields.List {
		for _, name := range fld.Names {
			got[name.Name] = exprString(fld.Type)
		}
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("字段 %s 类型 = %q, 期望 %q", name, got[name], typ)
		}
	}
	// cacheKey 的类型名按源文件唯一（goxmlRenderKey<Suffix>，TASK-CODEGEN-NAMING-001 卡 §5.1）：
	// 只断言前缀，避免把某个具体源的派生名写死在这里。
	if !strings.HasPrefix(got["cacheKey"], "goxmlRenderKey") {
		t.Errorf("字段 cacheKey 类型 = %q, 期望以 goxmlRenderKey 开头", got["cacheKey"])
	}
}

// TestRenderKeyFields 验证 renderKey 覆盖 §16.5 全部四个输入。
func TestRenderKeyFields(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	f := parseGo(t, out)

	var st *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || !strings.HasPrefix(ts.Name.Name, "goxmlRenderKey") {
			return true
		}
		if s, ok := ts.Type.(*ast.StructType); ok {
			st = s
		}
		return false
	})
	if st == nil {
		t.Fatal("未找到 goxmlRenderKey 类型")
	}

	want := map[string]string{
		"constraints": "layout.Constraints",
		"metric":      "unit.Metric",
		"theme":       "*material.Theme",
		"locale":      "system.Locale",
	}
	if st.Fields.NumFields() != len(want) {
		t.Errorf("renderKey 字段数 = %d, 期望 %d（§16.5 规定全部四个）",
			st.Fields.NumFields(), len(want))
	}
	got := map[string]string{}
	for _, fld := range st.Fields.List {
		for _, name := range fld.Names {
			got[name.Name] = exprString(fld.Type)
		}
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("renderKey.%s 类型 = %q, 期望 %q", name, got[name], typ)
		}
	}
}

// TestRenderKeyUsesBitComparison 验证浮点按位比较（契约 §16.5 第 1 条）。
//
// 不允许用容差：容差会漏掉本该失效的微小 DPI 变化。
func TestRenderKeyUsesBitComparison(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	s := string(out)

	if !strings.Contains(s, "math.Float32bits") {
		t.Error("renderKey.equal 必须用 math.Float32bits 按位比较浮点字段")
	}
	// 不允许出现浮点直接比较的容差写法。
	for _, bad := range []string{"PxPerDp -", "PxPerDp <", "PxPerDp >", "math.Abs"} {
		if strings.Contains(s, bad) {
			t.Errorf("不应使用容差比较: 发现 %q", bad)
		}
	}
}

// ---------------------------------------------------------------------------
// A4-T2 生成确定性
// ---------------------------------------------------------------------------

func TestCacheGenerationDeterministic(t *testing.T) {
	a := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	b := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	if !bytes.Equal(a, b) {
		t.Error("相同 IR + 相同 Options 两次生成结果不一致")
	}
}

// TestCacheImportsSorted 验证新增 import 后仍确定性排序（契约 §7）。
func TestCacheImportsSorted(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	f := parseGo(t, out)

	var paths []string
	for _, imp := range f.Imports {
		paths = append(paths, strings.Trim(imp.Path.Value, `"`))
	}
	if len(paths) < 5 {
		t.Fatalf("期望至少 5 个 import，实际 %d: %v", len(paths), paths)
	}
	// 检查已按路径排序（§7：先按别名，再按路径；本文件无别名冲突）。
	for i := 1; i < len(paths); i++ {
		if paths[i-1] > paths[i] {
			t.Errorf("import 未排序: %q 在 %q 之前", paths[i-1], paths[i])
		}
	}
	// 必需的 import 一个都不能少。
	for _, want := range []string{
		`"gioui.org/io/system"`, `"gioui.org/layout"`, `"gioui.org/op"`,
		`"gioui.org/unit"`, `"gioui.org/widget/material"`, `"math"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("缺少 import %s", want)
		}
	}
}

// TestNoUnusedImports 确保生成文件里每个 import 都被引用。
func TestNoUnusedImports(t *testing.T) {
	out := mustGen(t, `<Text Value="Hello"/>`, defaultOpts())
	f := parseGo(t, out)

	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if path == "math" {
			name = "math"
		}
		if !used[name] {
			t.Errorf("import %q (%s) 未被使用", path, name)
		}
	}
}

// ---------------------------------------------------------------------------
// 辅助
// ---------------------------------------------------------------------------

// exprString 把类型表达式还原为可比较的字符串。
func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return "*" + exprString(v.X)
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	default:
		return "?"
	}
}

var (
	_ = parser.ParseFile
	_ = token.NewFileSet
	_ = os.ReadFile
)
