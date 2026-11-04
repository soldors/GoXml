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

// R12-B：VM 声明在源目录之外（页面在 edit 包、VM 在 model 包）时，限定符必须取
// VM 目录解析出的真实包名（VMPackageName），不得沿用源目录包名（ViewModelPackageName）。
// 基线行为是 alias=ViewModelPackageName（页面包名）→ `import edit ".../model"`，
// 可编译但误导阅读（R10 反例）。
func TestVMExprCrossDirUsesVMPackageNameNotSourceName(t *testing.T) {
	g := vmGen(Options{
		ModulePath:           "example.com/m",
		ViewModelPackageDir:  "view/edit",
		OutputPackageDir:     "view/edit",
		ViewModelPackageName: "edit", // 源目录真实包名——R12-B 前被错误用于 VM 限定符
	}, analyzer.ViewModelRef{PkgDir: "model", Name: "VM", PackageName: "vmodel"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "vmodel.VM" {
		t.Fatalf("跨目录 VM 限定符应取 VM 包真实名 vmodel.VM，实际 %q", got)
	}
	rendered := g.imports.render()
	if !strings.Contains(rendered, `vmodel "example.com/m/model"`) {
		t.Fatalf("应强制别名 import vmodel \"example.com/m/model\":\n%s", rendered)
	}
	if strings.Contains(rendered, `edit "`) || strings.Contains(rendered, `edit "example.com/m/model"`) {
		t.Fatalf("源目录包名不得泄漏为 VM 限定符（R12-B 反例）:\n%s", rendered)
	}
}

// VMPackageName 未接线（直接调用 Codegen 的库用户）时回退目录名：
// 与 GXML 声明文法的限定符一致（用户写的就是 model.VM）。
func TestVMExprCrossDirFallsBackToDirBase(t *testing.T) {
	g := vmGen(Options{ModulePath: "example.com/m", OutputPackageDir: "out"},
		analyzer.ViewModelRef{PkgDir: "model", Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "model.VM" || !strings.Contains(g.imports.render(), `model "example.com/m/model"`) {
		t.Fatalf("无 VMPackageName 应回退目录名: %s\n%s", got, g.imports.render())
	}
}

// 同包 VM（含 --out-dir 迁移）：ViewModelPackageName（源目录真实包名）仍是
// VM 包名，行为不变——它只在同包分支使用。
func TestVMExprSamePkgStillUsesSourcePkgName(t *testing.T) {
	g := vmGen(Options{
		ModulePath:           "example.com/m",
		ViewModelPackageDir:  "view/edit",
		OutputPackageDir:     "gen",
		ViewModelPackageName: "editpkg",
	}, analyzer.ViewModelRef{Name: "VM"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "editpkg.VM" || !strings.Contains(g.imports.render(), `editpkg "example.com/m/view/edit"`) {
		t.Fatalf("同包 VM 应沿用源包名: %s\n%s", got, g.imports.render())
	}
}
