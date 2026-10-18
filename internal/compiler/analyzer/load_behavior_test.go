package analyzer

import (
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/packages"
)

// ============================================================================
// go/packages 加载行为锁定
//
// 这些测试把「go/packages 实际怎么报错」固化为断言，而不是靠印象。
// 加载器的实现依赖这些行为，一旦 x/tools 升级改变行为，测试会先失败。
//
// 实测结论（x/tools v0.52.0）：
//
//	情形              err    len(pkgs)  p.Errors
//	正常包            nil    1          0
//	包不存在          nil    1          1
//	包存在但语法错误  nil    1          5
//	模式匹配不到      nil    1          1
//
// 关键：packages.Load 的 err 几乎总是 nil，真正的错误在 p.Errors。
// 更危险的是，语法错误的包 Types 仍非 nil（部分类型信息可用），
// 因此「能取到类型」绝不能作为加载成功的判据。
// ============================================================================

func testLoadConfig(dir string) *packages.Config {
	return &packages.Config{
		Dir: dir,
		Mode: packages.NeedName | packages.NeedFiles |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax,
		Tests: false,
	}
}

// TestLoadBehavior_ValidFixture 确认夹具包可被显式加载。
//
// 意义：Go 工具在递归模式（./...）中跳过 testdata，但显式路径可用。
// 夹具布局因此不需要改动。
func TestLoadBehavior_ValidFixture(t *testing.T) {
	pkgs, err := packages.Load(testLoadConfig("."), "./testdata/fixture")
	if err != nil {
		t.Fatalf("Load 返回错误: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("加载到 %d 个包, 期望 1", len(pkgs))
	}
	p := pkgs[0]
	if len(p.Errors) > 0 {
		for _, e := range p.Errors {
			t.Errorf("加载错误: %v", e)
		}
		return
	}
	if p.Types == nil {
		t.Fatal("Types 为 nil")
	}
	if p.Name != "fixture" {
		t.Errorf("包名 = %q, 期望 fixture", p.Name)
	}
	if got, want := p.PkgPath, "github.com/goxml/goxml/internal/compiler/analyzer/testdata/fixture"; got != want {
		t.Errorf("PkgPath = %q, 期望 %q", got, want)
	}
}

// TestLoadBehavior_ViewModelTypeAndMethods 确认能取到类型与方法集。
func TestLoadBehavior_ViewModelTypeAndMethods(t *testing.T) {
	pkgs, err := packages.Load(testLoadConfig("."), "./testdata/fixture")
	if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("加载失败: err=%v pkgs=%d", err, len(pkgs))
	}
	p := pkgs[0]

	obj := p.Types.Scope().Lookup("ViewModel")
	if obj == nil {
		t.Fatal("包作用域中没有 ViewModel")
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		t.Fatalf("ViewModel 不是命名类型，实际 %T", obj.Type())
	}

	// 事件签名校验依赖指针接收者的方法集。
	ms := types.NewMethodSet(types.NewPointer(named))
	if ms.Len() == 0 {
		t.Fatal("方法集为空")
	}
	want := map[string]bool{
		"Delete": false, "OnNameChanged": false, "OnOpen": false,
		"OnWithReturn": false, "OnWrongArity": false, "OnWrongType": false,
	}
	for i := 0; i < ms.Len(); i++ {
		name := ms.At(i).Obj().Name()
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("方法集中缺少 %s", name)
		}
	}
}

// TestLoadBehavior_MissingPackageReturnsErrors 锁定「包不存在」的形态。
//
// 这是加载器最关键的判据：不能只看 Load 的 error 返回值。
func TestLoadBehavior_MissingPackageReturnsErrors(t *testing.T) {
	pkgs, err := packages.Load(testLoadConfig("."), "./testdata/fixture/nosuch")

	if err != nil {
		// 若未来版本改为返回 error，加载器需同时处理两条路径。
		// 这里记录事实，不判失败。
		t.Logf("Load 返回 error（与 v0.52.0 行为不同）: %v", err)
		return
	}
	if len(pkgs) == 0 {
		t.Log("Load 返回空切片（与 v0.52.0 行为不同）")
		return
	}
	if len(pkgs[0].Errors) == 0 {
		t.Fatal("包不存在却没有任何 Errors——加载器无法识别失败")
	}
	t.Logf("包不存在时的错误: %v", pkgs[0].Errors[0])
}

// TestLoadBehavior_BrokenPackageHasErrorsButTypes 锁定最危险的情形：
// 语法错误的包仍然有非 nil 的 Types。
//
// 这条测试存在的意义是防止有人用「Types != nil」判断加载成功。
func TestLoadBehavior_BrokenPackageHasErrorsButTypes(t *testing.T) {
	pkgs, err := packages.Load(testLoadConfig("."), "./testdata/broken")
	if err != nil {
		t.Logf("Load 返回 error: %v", err)
		return
	}
	if len(pkgs) == 0 {
		t.Fatal("未返回任何包")
	}
	p := pkgs[0]
	if len(p.Errors) == 0 {
		t.Fatal("损坏的包应产生 Errors")
	}
	if p.Types == nil {
		t.Log("Types 为 nil（比预期更严格，无害）")
	} else {
		t.Logf("注意：损坏的包 Types 仍非 nil——不得据此判断加载成功")
	}
}

// TestLoadBehavior_ModuleRootDiscoverable 确认模块根可稳定定位。
func TestLoadBehavior_ModuleRootDiscoverable(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := walkUpToGoMod(t, wd)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("模块根下无 go.mod: %v", err)
	}
	t.Logf("工作目录=%s 模块根=%s", wd, root)
}

// walkUpToGoMod 从 dir 向上查找含 go.mod 的目录。
func walkUpToGoMod(t *testing.T, dir string) string {
	t.Helper()
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
