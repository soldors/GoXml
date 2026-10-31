package analyzer

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// R3 切片2：真实构建上下文一致性。
// 用真实临时 module + go/packages 验证 Analyzer 的加载选择：
//   - //go:build ignore 文件不参与编译；
//   - 平台后缀/_GOOS 约束按当前平台生效；
//   - _test.go 不进入普通包加载（Tests:false）；
//   - GOFLAGS 自定义 -tags 生效（隔离式 t.Setenv，自动还原，不改全局遗留）。
// 不手写 Go 标签解析器，全部复用 go/packages。

func TestBuildContextIgnoresBuildIgnoreFiles(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "p", "ignored.go"),
		"//go:build ignore\n\npackage p\n\ntype Ignored struct{}\n")
	writeFile(t, filepath.Join(root, "p", "visible.go"), "package p\n\ntype Visible struct{}\n")

	if _, cerr := env.ResolveViewModel(decl("p.Ignored")); cerr == nil {
		t.Fatal("//go:build ignore 的类型不应被解析到")
	} else if !strings.Contains(cerr.Message, "not found") {
		t.Fatalf("应为 not found（而非其它失败）: %q", cerr.Message)
	}
	if _, cerr := env.ResolveViewModel(decl("p.Visible")); cerr != nil {
		t.Fatalf("普通文件类型应可解析: %v", cerr)
	}
}

func TestBuildContextPlatformSuffix(t *testing.T) {
	root, env := newModEnv(t)
	// 当前平台文件
	writeFile(t, filepath.Join(root, "p", "cur.go"),
		"//go:build "+runtime.GOOS+"\n\npackage p\n\ntype CurOS struct{}\n")
	// 非当前平台文件
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	writeFile(t, filepath.Join(root, "p", "other.go"),
		"//go:build "+other+"\n\npackage p\n\ntype OtherOS struct{}\n")

	if _, cerr := env.ResolveViewModel(decl("p.CurOS")); cerr != nil {
		t.Fatalf("当前平台类型应可解析: %v", cerr)
	}
	if _, cerr := env.ResolveViewModel(decl("p.OtherOS")); cerr == nil {
		t.Fatalf("非当前平台类型不应被解析到（GOOS=%s, other=%s）", runtime.GOOS, other)
	}
}

func TestBuildContextExcludesTestFiles(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "p", "x.go"), "package p\n\ntype Base struct{}\n")
	writeFile(t, filepath.Join(root, "p", "x_test.go"), "package p\n\ntype TestOnly struct{}\n")

	if _, cerr := env.ResolveViewModel(decl("p.Base")); cerr != nil {
		t.Fatalf("普通类型应可解析: %v", cerr)
	}
	if _, cerr := env.ResolveViewModel(decl("p.TestOnly")); cerr == nil {
		t.Fatal("仅存在于 _test.go 的类型不应进入普通包加载（Tests:false）")
	}
}

func TestBuildContextHonorsGOFLAGSTags(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "p", "tagged.go"),
		"//go:build custom\n\npackage p\n\ntype Tagged struct{}\n")

	// 默认（无 -tags）：不可见。
	if _, cerr := env.ResolveViewModel(decl("p.Tagged")); cerr == nil {
		t.Fatal("无 -tags 时自定义 tag 类型不应可见")
	}

	// 隔离式设置 GOFLAGS（t.Setenv 自动还原）；新建 TypeEnv 以免命中缓存。
	t.Setenv("GOFLAGS", "-tags=custom")
	env2, err := NewTypeEnv(Config{ModuleRoot: root})
	if err != nil {
		t.Fatalf("NewTypeEnv(tags): %v", err)
	}
	if _, cerr := env2.ResolveViewModel(decl("p.Tagged")); cerr != nil {
		t.Fatalf("-tags=custom 时类型应可见: %v", cerr)
	}
}

// 别名行为探针：type Alias = Real。Go1.22+ 的 obj.Type() 可能是 *types.Alias，
// 需要显式 Unalias 才能取到命名类型。此测试固定“别名应可解析为其目标命名类型”。
func TestResolveViewModelExportedAlias(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "p", "a.go"),
		"package p\n\ntype Real struct{}\n\ntype Alias = Real\n")

	got, cerr := env.ResolveViewModel(contracts.ViewModelDecl{Value: "p.Alias"})
	if cerr != nil {
		t.Fatalf("导出别名应可解析为命名类型: %v", cerr)
	}
	if got == nil || got.Named == nil || got.Named.Obj().Name() != "Real" {
		t.Fatalf("别名应解析到 Real，实际 %+v", got)
	}
}
