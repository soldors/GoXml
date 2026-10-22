package diagdoc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ============================================================================
// 真实发射校验（AST）
//
// 起因：本包原有的三态清单（implementedCodes / frozenCodes）是**手写的**，
// 旧测试只校验两个清单互不重叠——从不校验真实代码。
//
// 结果：Analyzer 的 11 个码全部已实现并发射，门禁却仍声称它们
// 「尚未实现、不会发射」。测试一直是绿的。
//
// 下面的测试直接解析源码，验证：
//  1. 每个 Code* 常量都在所在包内有调用点（不是死常量）；
//  2. 每个 Code* 常量都登记在 implementedCodes 里（不漏登记）；
//  3. implementedCodes 里登记的符号都真实存在（不虚报）。
//
// 这样「是否实现」由代码决定，而不是由某个人的记性决定。
// ============================================================================

// stagePkg 描述一个需要校验的阶段包。
type stagePkg struct {
	// dir 是相对仓库根的目录。
	dir string
	// importPath 是包导入路径（用于定位 go.mod 根）。
	importPath string
}

// stagePkgs 是所有发射诊断码的阶段包。
var stagePkgs = []stagePkg{
	{"internal/compiler/parser", "github.com/goxml/goxml/internal/compiler/parser"},
	{"internal/compiler/expander", "github.com/goxml/goxml/internal/compiler/expander"},
	{"internal/compiler/analyzer", "github.com/goxml/goxml/internal/compiler/analyzer"},
	// Codegen 自 C3 起纳入：CODEGEN-NAME-001/002 定义在本包。
	// 若不加进来，这两个常量就享受不到「必须被发射 / 必须被登记 /
	// 登记的符号必须存在」三条 AST 校验——那正是旧门禁失明的原因。
	{"internal/compiler/codegen", "github.com/goxml/goxml/internal/compiler/codegen"},
}

// pkgConstants 返回包内定义的所有 Code* 常量名。
func pkgConstants(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(absDir(t, dir))
	if err != nil {
		t.Fatalf("读取目录 %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, absDir(t, filepath.Join(dir, e.Name())), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", e.Name(), err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for _, name := range vs.Names {
				if strings.HasPrefix(name.Name, "Code") && name.IsExported() {
					out = append(out, name.Name)
				}
			}
			return true
		})
	}
	sort.Strings(out)
	return out
}

// pkgEmitted 返回包内作为**调用实参**出现的 Code* 标识符。
//
// 只认「作为参数传给某个函数」的出现，避免把常量声明本身
// （CodeX = contracts.CodePrefixY + "-001"）算作发射。
func pkgEmitted(t *testing.T, dir string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(absDir(t, dir))
	if err != nil {
		t.Fatalf("读取目录 %s: %v", dir, err)
	}
	out := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, absDir(t, filepath.Join(dir, e.Name())), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", e.Name(), err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, arg := range call.Args {
				if id, ok := arg.(*ast.Ident); ok && strings.HasPrefix(id.Name, "Code") {
					out[id.Name] = true
				}
			}
			return true
		})
	}
	return out
}

// TestEveryConstantIsEmitted 确保没有「定义了却从不发射」的死常量。
//
// 死常量是最容易误导后来者的形态：它看起来像已实现，实际永远不会出现。
func TestEveryConstantIsEmitted(t *testing.T) {
	for _, sp := range stagePkgs {
		t.Run(sp.dir, func(t *testing.T) {
			consts := pkgConstants(t, sp.dir)
			if len(consts) == 0 {
				t.Fatalf("未找到任何 Code* 常量")
			}
			emitted := pkgEmitted(t, sp.dir)
			for _, name := range consts {
				if !emitted[name] {
					t.Errorf("%s.%s 已定义但从未作为诊断发射", sp.importPath, name)
				}
			}
		})
	}
}

// TestEveryConstantIsRegistered 确保代码里新增的码都登记在实现清单。
func TestEveryConstantIsRegistered(t *testing.T) {
	registered := make(map[string]bool)
	for _, c := range implementedCodes() {
		registered[c.Name] = true
	}
	for _, sp := range stagePkgs {
		t.Run(sp.dir, func(t *testing.T) {
			for _, name := range pkgConstants(t, sp.dir) {
				full := shortPkg(sp.dir) + "." + name
				if !registered[full] {
					t.Errorf("%s 未登记在 implementedCodes 中", full)
				}
			}
		})
	}
}

// TestRegisteredSymbolsExist 确保 implementedCodes 不虚报不存在的符号。
func TestRegisteredSymbolsExist(t *testing.T) {
	byPkg := make(map[string]map[string]bool)
	for _, sp := range stagePkgs {
		set := make(map[string]bool)
		for _, name := range pkgConstants(t, sp.dir) {
			set[name] = true
		}
		byPkg[shortPkg(sp.dir)] = set
	}
	for _, c := range implementedCodes() {
		i := strings.LastIndex(c.Name, ".")
		if i < 0 {
			continue // contracts 里的前缀常量，不在此校验
		}
		pkg, sym := c.Name[:i], c.Name[i+1:]
		set, ok := byPkg[pkg]
		if !ok {
			continue // 非阶段包
		}
		if !set[sym] {
			t.Errorf("implementedCodes 登记的 %s 在包中不存在", c.Name)
		}
	}
}

// TestFrozenCodesAreTrulyUnimplemented 校验定稿清单里的码确实没有实现。
func TestFrozenCodesAreTrulyUnimplemented(t *testing.T) {
	// 收集所有阶段包中真实存在的常量。
	real := make(map[string]bool)
	for _, sp := range stagePkgs {
		for _, name := range pkgConstants(t, sp.dir) {
			real[shortPkg(sp.dir)+"."+name] = true
		}
	}
	for _, c := range frozenCodes {
		if real[c.Name] {
			t.Errorf("frozenCodes 声称 %s 未实现，但代码中已有该常量", c.Name)
		}
	}
}

// shortPkg 从相对目录得到包名（最后一段）。
func shortPkg(dir string) string {
	return filepath.Base(dir)
}

// repoRoot 从测试工作目录向上定位模块根。
//
// 测试进程的 cwd 是包目录（internal/compiler/diagdoc），因此
// stagePkgs 里相对仓库根的路径必须先补上根前缀。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("向上查找未找到 go.mod")
		}
		dir = parent
	}
}

// absDir 把仓库根相对目录解析为绝对路径。
func absDir(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), filepath.FromSlash(rel))
}
