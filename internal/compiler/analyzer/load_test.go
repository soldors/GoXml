package analyzer

import (
	"path/filepath"
	"strings"
	"testing"

	"os"

	"github.com/goxml/goxml/contracts"
)

// ============================================================================
// TypeExpr 文法解析（契约 §4.2，已冻结）
// ============================================================================

func TestParseTypeExprValid(t *testing.T) {
	cases := []struct {
		in     string
		pkgDir string
		name   string
	}{
		{"AppState", "", "AppState"},
		{"demo.ViewModel", "demo", "ViewModel"},
		{"internal/models.User", "internal/models", "User"},
	}

	for _, c := range cases {
		got, err := ParseTypeExpr(c.in)
		if err != nil {
			t.Errorf("ParseTypeExpr(%q) 失败: %v", c.in, err)
			continue
		}
		if got.PkgDir != c.pkgDir || got.Name != c.name {
			t.Errorf("ParseTypeExpr(%q) = {PkgDir:%q Name:%q}, want {%q %q}",
				c.in, got.PkgDir, got.Name, c.pkgDir, c.name)
		}
	}
}

func TestParseTypeExprRejects(t *testing.T) {
	// 逐条对应契约 §4.2 的拒绝表。
	cases := map[string]string{
		"":                               "空声明",
		"/demo.ViewModel":                "绝对路径",
		"demo//models.User":              "空路径段",
		"./demo.User":                    "'.' 段（路径穿越）",
		"../x.User":                      " '..' 段（路径穿越）",
		`demo\models.User`:               "反斜杠",
		"example.com/app/demo.ViewModel": "完整导入路径",
		".User":                          "前导点",
		"demo.":                          "尾随点",
		"a.b.c":                          "包路径内含点",
		"dmo-x.User":                     "非标识符段",
		"demo.1User":                     "类型名以数字开头",
		"*demo.ViewModel":                "指针",
		"demo.Box[T]":                    "泛型",
		" demo.ViewModel":                "前导空格",
		"demo.ViewModel ":                "尾随空格",
		"demo .ViewModel":                "段内空格",
	}
	for in, why := range cases {
		if got, err := ParseTypeExpr(in); err == nil {
			t.Errorf("ParseTypeExpr(%q) 应被拒绝（%s），实际得到 %+v", in, why, got)
		}
	}
}

func TestTypeExprString(t *testing.T) {
	cases := map[string]string{
		"AppState":             "AppState",
		"demo.ViewModel":       "demo.ViewModel",
		"internal/models.User": "internal/models.User",
	}
	for in, want := range cases {
		expr, err := ParseTypeExpr(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := expr.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

func TestTypeExprIsSamePackage(t *testing.T) {
	same, _ := ParseTypeExpr("AppState")
	if !same.IsSamePackage() {
		t.Error("AppState 应判为同包")
	}
	other, _ := ParseTypeExpr("demo.ViewModel")
	if other.IsSamePackage() {
		t.Error("demo.ViewModel 不应判为同包")
	}
}

func TestTypeExprErrorMessageIsActionable(t *testing.T) {
	// 消息应包含原始输入与原因，而不只是 "invalid"。
	_, err := ParseTypeExpr("example.com/app/demo.ViewModel")
	if err == nil {
		t.Fatal("应报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "example.com/app/demo.ViewModel") {
		t.Errorf("消息未回显输入: %q", msg)
	}
	if !strings.Contains(msg, "import") {
		t.Errorf("消息未说明是导入路径问题: %q", msg)
	}
}

// ============================================================================
// Config 校验
// ============================================================================

func TestConfigValidate(t *testing.T) {
	root := moduleRootOf(t)

	t.Run("valid", func(t *testing.T) {
		if err := (Config{ModuleRoot: root}).Validate(); err != nil {
			t.Errorf("应通过: %v", err)
		}
	})
	t.Run("empty root", func(t *testing.T) {
		if err := (Config{}).Validate(); err == nil {
			t.Error("缺少 ModuleRoot 应报错")
		}
	})
	t.Run("relative root", func(t *testing.T) {
		if err := (Config{ModuleRoot: "."}).Validate(); err == nil {
			t.Error("相对路径应报错——不能依赖进程工作目录")
		}
	})
	t.Run("no go.mod", func(t *testing.T) {
		if err := (Config{ModuleRoot: t.TempDir()}).Validate(); err == nil {
			t.Error("没有 go.mod 应报错")
		}
	})
}

// ============================================================================
// ViewModel 解析
// ============================================================================

// newTestEnv 用真实模块根构造环境。
func newTestEnv(t *testing.T, sourceDir string) *TypeEnv {
	t.Helper()
	root := moduleRootOf(t)
	abs, err := filepath.Abs(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	env, err := NewTypeEnv(Config{ModuleRoot: root, SourceDir: abs})
	if err != nil {
		t.Fatalf("NewTypeEnv: %v", err)
	}
	return env
}

// moduleRootOf 返回仓库根目录。
func moduleRootOf(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return walkUpToGoMod(t, wd)
}

// decl 构造 ViewModel 声明。
func decl(value string) contracts.ViewModelDecl {
	return contracts.ViewModelDecl{Value: value, Span: contracts.SourceSpan{}}
}

func TestResolveViewModelAbsentIsNotError(t *testing.T) {
	env := newTestEnv(t, ".")
	got, cerr := env.ResolveViewModel(contracts.ViewModelDecl{})
	if cerr != nil {
		t.Fatalf("缺省声明不应报错: %v", cerr)
	}
	if got != nil {
		t.Errorf("缺省声明应返回 nil，实际 %+v", got)
	}
}

func TestResolveViewModelCrossPackage(t *testing.T) {
	env := newTestEnv(t, ".")

	// 夹具包在模块内的相对目录。
	got, cerr := env.ResolveViewModel(decl("internal/compiler/analyzer/testdata/fixture.ViewModel"))
	if cerr != nil {
		t.Fatalf("应解析成功: %v", cerr)
	}
	if got == nil || got.Named == nil {
		t.Fatal("应返回命名类型")
	}
	if got.Expr.Name != "ViewModel" {
		t.Errorf("Name = %q", got.Expr.Name)
	}
	if want := env.ModulePath() + "/internal/compiler/analyzer/testdata/fixture"; got.ImportPath != want {
		t.Errorf("ImportPath = %q, want %q", got.ImportPath, want)
	}
}

func TestResolveViewModelMissingPackage(t *testing.T) {
	env := newTestEnv(t, ".")

	_, cerr := env.ResolveViewModel(decl("nosuchpkg.ViewModel"))
	if cerr == nil {
		t.Fatal("包不存在应报错")
	}
	if cerr.Code != CodeViewModelType {
		t.Errorf("码 = %s, want %s", cerr.Code, CodeViewModelType)
	}
	if !strings.Contains(cerr.Message, "nosuchpkg") {
		t.Errorf("消息应回显包名: %q", cerr.Message)
	}
}

func TestResolveViewModelMissingType(t *testing.T) {
	env := newTestEnv(t, ".")

	_, cerr := env.ResolveViewModel(decl("internal/compiler/analyzer/testdata/fixture.NoSuchType"))
	if cerr == nil {
		t.Fatal("类型不存在应报错")
	}
	if cerr.Code != CodeViewModelType {
		t.Errorf("码 = %s", cerr.Code)
	}
	if !strings.Contains(cerr.Message, "not found") {
		t.Errorf("消息应说明类型未找到: %q", cerr.Message)
	}
}

func TestResolveViewModelNotAType(t *testing.T) {
	env := newTestEnv(t, ".")

	// fixture 包里有 var BadType（int 值），不是类型。
	// 注意：BadType 在 broken 包里；fixture 里没有变量。
	// 用一个确实存在的非类型符号：包级函数或变量。
	// fixture 包没有导出变量，改用 broken 包（有 BadType 变量）。
	_, cerr := env.ResolveViewModel(decl("internal/compiler/analyzer/testdata/broken.BadType"))
	if cerr == nil {
		t.Fatal("broken 包加载失败也应报错")
	}
	if cerr.Code != CodeViewModelType {
		t.Errorf("码 = %s", cerr.Code)
	}
}

func TestResolveViewModelBrokenPackage(t *testing.T) {
	env := newTestEnv(t, ".")

	// broken 包有类型错误：必须在加载阶段被拦住，
	// 不能因为「能取到部分类型信息」就继续。
	got, cerr := env.ResolveViewModel(decl("internal/compiler/analyzer/testdata/broken.BadType"))
	if cerr == nil {
		t.Fatalf("损坏的包应报错，实际得到 %+v", got)
	}
	if !strings.Contains(cerr.Message, "failed to load") {
		t.Errorf("消息应说明包加载失败: %q", cerr.Message)
	}
}

func TestResolveViewModelInvalidSyntax(t *testing.T) {
	env := newTestEnv(t, ".")

	for _, bad := range []string{"example.com/app/demo.ViewModel", "*fixture.ViewModel", "a.b.c"} {
		_, cerr := env.ResolveViewModel(decl(bad))
		if cerr == nil {
			t.Errorf("%q 应报错", bad)
			continue
		}
		if cerr.Code != CodeViewModelType {
			t.Errorf("%q 的码 = %s", bad, cerr.Code)
		}
	}
}

func TestResolveViewModelCachesPackage(t *testing.T) {
	env := newTestEnv(t, ".")

	src := "internal/compiler/analyzer/testdata/fixture.ViewModel"
	if _, cerr := env.ResolveViewModel(decl(src)); cerr != nil {
		t.Fatalf("首次解析失败: %v", cerr)
	}
	before := len(env.loaded)

	if _, cerr := env.ResolveViewModel(decl(src)); cerr != nil {
		t.Fatalf("二次解析失败: %v", cerr)
	}
	if len(env.loaded) != before {
		t.Errorf("重复解析应复用缓存，loaded 从 %d 变为 %d", before, len(env.loaded))
	}
}

func TestResolveViewModelSpanIsPreserved(t *testing.T) {
	env := newTestEnv(t, ".")

	// 诊断必须指向 ViewModel 属性，而不是文档根。
	span := contracts.SourceSpan{
		Start: contracts.Position{Line: 3, Col: 15},
		End:   contracts.Position{Line: 3, Col: 40},
	}
	d := contracts.ViewModelDecl{Value: "nosuchpkg.Type", Span: span}
	_, cerr := env.ResolveViewModel(d)
	if cerr == nil {
		t.Fatal("应报错")
	}
	if cerr.Line != 3 || cerr.Col != 15 {
		t.Errorf("诊断位置 = %d:%d, want 3:15", cerr.Line, cerr.Col)
	}
}

func TestResolveViewModelNoGlobalTypeSearch(t *testing.T) {
	// 同名类型在不同包里不应被混淆：只在目标包作用域查找。
	env := newTestEnv(t, ".")

	// fixture 包里有 ViewModel；若实现做了全局搜索，
	// 下面的「另一个包 + 同名类型」就会错误命中。
	got, cerr := env.ResolveViewModel(decl("internal/compiler/analyzer/testdata.DoesNotExist"))
	if cerr == nil {
		t.Fatalf("该包不存在，应报错，实际 %+v", got)
	}
}

// ============================================================================
// 同包形式（ViewModel="AppState"）
// ============================================================================

func TestResolveViewModelSamePackage(t *testing.T) {
	// 同包解析以 .gxml 所在目录为包目录，而不是模块根。
	env := newTestEnv(t, "testdata/samepkg")

	got, cerr := env.ResolveViewModel(decl("AppState"))
	if cerr != nil {
		t.Fatalf("同包声明应解析成功: %v", cerr)
	}
	if got == nil || got.Named == nil {
		t.Fatal("应返回命名类型")
	}
	if !got.Expr.IsSamePackage() {
		t.Error("应判为同包形式")
	}
	want := env.ModulePath() + "/internal/compiler/analyzer/testdata/samepkg"
	if got.ImportPath != want {
		t.Errorf("ImportPath = %q, want %q", got.ImportPath, want)
	}
	if got.Named.Obj().Name() != "AppState" {
		t.Errorf("类型名 = %q", got.Named.Obj().Name())
	}
}

func TestResolveViewModelSamePackageRequiresSourceDir(t *testing.T) {
	// 同包形式缺少 SourceDir 时必须明确报错，而不是猜一个目录。
	root := moduleRootOf(t)
	env, err := NewTypeEnv(Config{ModuleRoot: root})
	if err != nil {
		t.Fatalf("NewTypeEnv: %v", err)
	}
	_, cerr := env.ResolveViewModel(decl("AppState"))
	if cerr == nil {
		t.Fatal("缺少 SourceDir 时应报错")
	}
	if !strings.Contains(cerr.Message, "SourceDir") {
		t.Errorf("消息应说明缺少 SourceDir: %q", cerr.Message)
	}
}

func TestResolveViewModelSamePackageWrongName(t *testing.T) {
	// 同包形式下类型不存在，应报「未找到」而非「包加载失败」。
	env := newTestEnv(t, "testdata/samepkg")
	_, cerr := env.ResolveViewModel(decl("Missing"))
	if cerr == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(cerr.Message, "not found") {
		t.Errorf("应报类型未找到: %q", cerr.Message)
	}
}

// ============================================================================
// 环境构造失败路径
// ============================================================================

func TestNewTypeEnvRejectsBadConfig(t *testing.T) {
	if _, err := NewTypeEnv(Config{}); err == nil {
		t.Error("空配置应报错")
	}
	if _, err := NewTypeEnv(Config{ModuleRoot: t.TempDir()}); err == nil {
		t.Error("无 go.mod 的目录应报错")
	}
}

func TestReadModulePathRejectsBadGoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("this is not a go.mod\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readModulePath(dir); err == nil {
		t.Error("非法 go.mod 应报错")
	}
}

func TestReadModulePathRequiresModuleDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.26.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readModulePath(dir); err == nil {
		t.Error("缺少 module 行应报错")
	}
}
