package analyzer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/contracts"
)

// R3 切片3：失败加载 / 修好源码 / 重新加载 的隔离与恢复。
// 断言错误对象与真实位置（非只看字符串）；证明诊断/VM 类型/源位置/缓存不串用。

func TestRecoveryAfterFixingSamePackage(t *testing.T) {
	root, env := newModEnv(t)
	p := filepath.Join(root, "p", "p.go")
	writeFile(t, p, "package p\n\nfunc Broken( {\n") // 语法错

	// 失败：结构化错误 + 真实位置。
	_, err := env.load("example.com/m/p")
	le, ok := err.(*PackageLoadError)
	if !ok || !le.HasPos {
		t.Fatalf("初次应为带位置的 PackageLoadError，实际 %T %+v", err, err)
	}
	if !samePath(le.File, p) || le.Line != 3 {
		t.Fatalf("初次定位 = %s:%d, want %s:3", le.File, le.Line, p)
	}

	// 修好源码（同一路径，内容合法）。
	writeFile(t, p, "package p\n\ntype VM struct{}\n")

	// 同一 TypeEnv 重新加载：失败未被缓存，应恢复成功。
	got, cerr := env.ResolveViewModel(contracts.ViewModelDecl{Value: "p.VM"})
	if cerr != nil {
		t.Fatalf("修好源码后同一环境应能恢复，实际 %v", cerr)
	}
	if got == nil || got.Named == nil || got.Named.Obj().Name() != "VM" {
		t.Fatalf("恢复后应解析到 p.VM，实际 %+v", got)
	}
	// 类型可用（可作为后续分析准入）：作用域里确实存在。
	if obj := got.Named.Obj(); obj.Pkg().Path() != "example.com/m/p" {
		t.Fatalf("类型归属包错误: %s", obj.Pkg().Path())
	}
}

func TestDifferentPackageFailureDoesNotPollute(t *testing.T) {
	root, env := newModEnv(t)
	writeFile(t, filepath.Join(root, "p", "p.go"), "package p\n\ntype VM struct{}\n")
	writeFile(t, filepath.Join(root, "q", "q.go"), "package q\n\nvar Bad int = \"s\"\n") // 类型错

	vm, cerr := env.ResolveViewModel(contracts.ViewModelDecl{Value: "p.VM"})
	if cerr != nil {
		t.Fatalf("p.VM 应成功: %v", cerr)
	}
	wantNamed := vm.Named

	// q 失败：错误位置属于 q。
	_, err := env.load("example.com/m/q")
	le, ok := err.(*PackageLoadError)
	if !ok || !le.HasPos {
		t.Fatalf("q 应为带位置错误，实际 %T %+v", err, err)
	}
	if !strings.Contains(le.File, "q"+string(filepath.Separator)+"q.go") {
		t.Fatalf("q 错误位置不归属 q: %s", le.File)
	}

	// 再次解析 p.VM：仍成功，且类型身份未变（未被 q 的失败污染/替换）。
	vm2, cerr2 := env.ResolveViewModel(contracts.ViewModelDecl{Value: "p.VM"})
	if cerr2 != nil {
		t.Fatalf("q 失败后 p.VM 仍应成功: %v", cerr2)
	}
	if vm2.Named != wantNamed {
		t.Fatal("p.VM 的类型对象被替换（缓存/类型环境被污染）")
	}
}

func TestFreshEnvIsolationAfterFailure(t *testing.T) {
	root := writeModule(t)
	writeFile(t, filepath.Join(root, "p", "p.go"), "package p\n\ntype VM struct{}\n")
	writeFile(t, filepath.Join(root, "q", "q.go"), "package q\n\nfunc Broken( {\n")

	envA, err := NewTypeEnv(Config{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, cerr := envA.ResolveViewModel(contracts.ViewModelDecl{Value: "p.VM"}); cerr != nil {
		t.Fatalf("envA p.VM 应成功: %v", cerr)
	}

	// envB 在 q 上失败，不得影响 envA 的已加载类型。
	envB, err := NewTypeEnv(Config{ModuleRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := envB.load("example.com/m/q"); err == nil {
		t.Fatal("envB q 应失败")
	}
	vm, cerr := envA.ResolveViewModel(contracts.ViewModelDecl{Value: "p.VM"})
	if cerr != nil || vm == nil || vm.Named == nil {
		t.Fatalf("envA 被 envB 的失败污染: vm=%v err=%v", vm, cerr)
	}
}

func TestMultiErrorOrderDeterministic(t *testing.T) {
	root := writeModule(t)
	writeFile(t, filepath.Join(root, "p", "p.go"),
		"package p\n\nvar A int = \"s\"\nvar B bool = 1\n") // 两处类型错

	type sig struct {
		file string
		line int
		col  int
		msg  string
	}
	var got []sig
	for i := 0; i < 3; i++ {
		env, err := NewTypeEnv(Config{ModuleRoot: root})
		if err != nil {
			t.Fatal(err)
		}
		_, lerr := env.load("example.com/m/p")
		le, ok := lerr.(*PackageLoadError)
		if !ok || !le.HasPos {
			t.Fatalf("第 %d 次：应为带位置错误，实际 %T %+v", i, lerr, lerr)
		}
		got = append(got, sig{le.File, le.Line, le.Col, le.Msg})
	}
	for i := 1; i < len(got); i++ {
		if got[i] != got[0] {
			t.Fatalf("多错误顺序/内容不确定: %+v vs %+v", got[0], got[i])
		}
	}
	if got[0].line <= 0 || got[0].file == "" {
		t.Fatalf("确定性签名位置不可用: %+v", got[0])
	}
}
