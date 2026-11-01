package codegen

// R4-A 片2：VM 跨包类型表达式与 import 一致性（Options.ViewModelPackageDir/OutputPackageDir）。
// 直接构造 generator（不依赖 fixture 磁盘），精准断言 vmTypeExpr 的限定符与 import 登记。

import (
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

func vmGen(opts Options, vm analyzer.ViewModelRef) *generator {
	return &generator{opts: opts, imports: newImportSet(), vmRef: vm}
}

// 同源 VM（PkgDir 空）搬到别的包（src → out）：必须 import 源目录并加 src. 限定符。
func TestVMExprSamePkgMovedToOtherPackage(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/m", ViewModelPackageDir: "src", OutputPackageDir: "out"},
		analyzer.ViewModelRef{Name: "VM"}) // PkgDir 空 = 同源
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "src.VM" {
		t.Fatalf("限定符应为 src.VM，实际 %q", got)
	}
	if !strings.Contains(g.imports.render(), `src "example.com/m/src"`) {
		t.Fatalf("应强制别名 import src \"example.com/m/src\":\n%s", g.imports.render())
	}
}

// 同源 VM 且产物仍在其包（src → src）：无自导入，裸 VM。
func TestVMExprSamePkgSameOutput(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/m", ViewModelPackageDir: "src", OutputPackageDir: "src"},
		analyzer.ViewModelRef{Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "VM" {
		t.Fatalf("同包应为裸 VM，实际 %q", got)
	}
	if g.imports.render() != "" {
		t.Fatalf("同包不应产生 import:\n%s", g.imports.render())
	}
}

// 显式跨包（PkgDir 非空），产物在别的包：import 该包，限定符=目录名并强制别名。
func TestVMExprExplicitCrossPkg(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/m", OutputPackageDir: "out"},
		analyzer.ViewModelRef{PkgDir: "model", Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "model.VM" {
		t.Fatalf("应为 model.VM，实际 %q", got)
	}
	if !strings.Contains(g.imports.render(), `model "example.com/m/model"`) {
		t.Fatalf("应强制别名 import model:\n%s", g.imports.render())
	}
}

// 包名≠目录名：目录 vmdir → 强制显式别名 vmdir，限定符与别名绑定一致。
func TestVMExprPkgNameDiffersFromDirForcedAlias(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/m", OutputPackageDir: "out"},
		analyzer.ViewModelRef{PkgDir: "vmdir", Name: "Alias"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "vmdir.Alias" {
		t.Fatalf("应为 vmdir.Alias，实际 %q", got)
	}
	if !strings.Contains(g.imports.render(), `vmdir "example.com/m/vmdir"`) {
		t.Fatalf("包名≠目录名应写显式别名:\n%s", g.imports.render())
	}
}

// 缺 ModulePath 且需要跨包 → 明确报错（不静默裸名）。
func TestVMExprCrossPkgMissingModulePathErrors(t *testing.T) {
	g := vmGen(Options{ViewModelPackageDir: "src", OutputPackageDir: "out"},
		analyzer.ViewModelRef{Name: "VM"})
	if _, err := g.vmTypeExpr(); err == nil {
		t.Fatal("跨包缺 ModulePath 应报错")
	}
}
