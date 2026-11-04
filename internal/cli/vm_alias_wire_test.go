package cli

// R12-B：跨包 ViewModel 的包名事实接线。
//
// 反例（基线 b12980c 真实 CLI 复现）：页面在 view/edit（package edit）、
// ViewModel 在 model/（真实包名 vmodel），生成代码却写
// `import edit "example.com/x/model"`——限定符是页面包名，可编译但误导阅读。
//
// 本文件在 CLI 层验收：Codegen 消费 Analyzer 已加载的 ViewModel 声明包名，
// 命名预检提供产物包的已有符号供别名消歧。

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVMAliasProject 建一个最小跨包工程：
//
//	model/vm.go        真实包名 vmPkg（目录固定 model，可 ≠ 包名）
//	view/edit/doc.go   package edit
//	view/edit/home.gxml  ViewModel="model.VM"，绑定 {Title}
func writeVMAliasProject(t *testing.T, root, vmPkg string) {
	t.Helper()
	writeGoFile(t, root, "go.mod", "module example.com/aliaswire\n\ngo 1.26\n")
	writeGoFile(t, filepath.Join(root, "model"), "vm.go",
		"package "+vmPkg+"\n\ntype VM struct{ Title string }\n")
	writeGoFile(t, filepath.Join(root, "view", "edit"), "doc.go", "package edit\n")
	gxml := `<Goxml Version="1" ViewModel="model.VM">
  <Page Route="/edit"><Text Value="{Title}"/></Page>
</Goxml>`
	writeGoFile(t, filepath.Join(root, "view", "edit"), "home.gxml", gxml)
}

// TestWireVMPackageNameDirDiffersFromPackage CLI 级主验收：
// VM 目录 model/ 声明 package vmodel 时，生成 import 必须以真实包名 vmodel
// 作限定符/别名，不得出现页面包名 edit。
func TestWireVMPackageNameDirDiffersFromPackage(t *testing.T) {
	root := tempProjectInRepo(t)
	writeVMAliasProject(t, root, "vmodel")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(root, "view", "edit")}, &out, &errb); code != ExitOK {
		t.Fatalf("退出码 = %d；stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	product := filepath.Join(root, "view", "edit", "home_gen.go")
	data, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, `vmodel "example.com/aliaswire/model"`) {
		t.Fatalf("生成 import 应以 VM 真实包名 vmodel 作显式别名:\n%s", src)
	}
	if !strings.Contains(src, "vmodel.VM") {
		t.Fatalf("VM 类型引用应为 vmodel.VM:\n%s", src)
	}
	if strings.Contains(src, `edit "example.com/aliaswire/model"`) {
		t.Fatalf("页面包名 edit 不得作为 VM 包限定符（R12-B 反例）:\n%s", src)
	}
}

// 对照：VM 目录与包名一致（model/ 声明 package model）时，未接线也要回退到
// 目录名——与 GXML 声明文法的限定符一致；接线后解析出的真实名同样是 model，
// 行为不变。
func TestWireVMPackageNameDirEqualsPackage(t *testing.T) {
	root := tempProjectInRepo(t)
	writeVMAliasProject(t, root, "model")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(root, "view", "edit")}, &out, &errb); code != ExitOK {
		t.Fatalf("退出码 = %d；stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "view", "edit", "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if !strings.Contains(src, `model "example.com/aliaswire/model"`) {
		t.Fatalf("包名=目录名时应写 model 别名:\n%s", src)
	}
	if strings.Contains(src, `edit "example.com/aliaswire/model"`) {
		t.Fatalf("页面包名不得作为 VM 包限定符:\n%s", src)
	}
}

// 同包 VM（ViewModel 与页面同目录）不引入 import，也不受接线影响。
func TestWireVMPackageNameSamePkgNoImport(t *testing.T) {
	root := tempProjectInRepo(t)
	writeGoFile(t, root, "go.mod", "module example.com/aliaswire\n\ngo 1.26\n")
	writeGoFile(t, root, "vm.go", "package edit\n\ntype VM struct{ Title string }\n")
	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/edit"><Text Value="{Title}"/></Page>
</Goxml>`
	writeGoFile(t, root, "home.gxml", gxml)

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("退出码 = %d；stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "home_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	// 动态页仍会 import gio 包；这里只断言**不出现 VM 自引用**：
	// 无本模块 import，VM 类型是裸名 VM（构造函数参数/字段）。
	if strings.Contains(src, `"example.com/aliaswire"`) {
		t.Fatalf("同包 VM 不应 import 本模块:\n%s", src)
	}
	if strings.Contains(src, "edit.VM") {
		t.Fatalf("同包 VM 应为裸类型名 VM，不得带限定符:\n%s", src)
	}
}

// VM 目录解析失败必须 fail-closed：VM 目录被删（或非 Go 包）时生成整体失败，
// 不静默回退页面包名。
func TestWireVMPackageNameMissingVMDirFailsClosed(t *testing.T) {
	root := tempProjectInRepo(t)
	writeVMAliasProject(t, root, "vmodel")
	if err := os.Remove(filepath.Join(root, "model", "vm.go")); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(root, "view", "edit")}, &out, &errb); code == ExitOK {
		t.Fatalf("VM 目录缺 Go 包时必须失败；stdout:\n%s", out.String())
	}
}

func TestWireVMPackageNameDisambiguatesDeclarations(t *testing.T) {
	cases := []struct {
		packageName string
		userDecl    string
		reexport    bool
		alias       string
	}{
		{packageName: "init", alias: "init_vm2"},
		{packageName: "string", alias: "string_vm2"},
		{packageName: "GoxmlHomeView", alias: "vm_GoxmlHomeView"},
		{packageName: "NewGoxmlHomeView", alias: "vm_NewGoxmlHomeView"},
		{packageName: "vmodel", userDecl: "var vmodel, vmodel_vm2 int", alias: "vmodel_vm3", reexport: true},
	}
	for _, tc := range cases {
		t.Run(tc.packageName, func(t *testing.T) {
			root := t.TempDir()
			writeVMAliasProject(t, root, tc.packageName)
			if tc.reexport {
				writeGoFile(t, filepath.Join(root, "core"), "real.go", "package core\ntype Real struct{ Title string }\n")
				writeGoFile(t, filepath.Join(root, "model"), "vm.go", "package "+tc.packageName+"\nimport \"example.com/aliaswire/core\"\ntype VM = core.Real\n")
			}
			goMod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			mod := strings.Replace(string(goMod), "module github.com/goxml/goxml", "module example.com/aliaswire", 1)
			writeGoFile(t, root, "go.mod", mod)
			goSum, err := os.ReadFile(filepath.Join("..", "..", "go.sum"))
			if err != nil {
				t.Fatal(err)
			}
			writeGoFile(t, root, "go.sum", string(goSum))
			if tc.userDecl != "" {
				writeGoFile(t, filepath.Join(root, "view", "edit"), "collision.go", "package edit\n"+tc.userDecl+"\n")
			}
			var out, errb bytes.Buffer
			if code := Run([]string{"generate", "--check", filepath.Join(root, "view", "edit")}, &out, &errb); code != ExitOK {
				t.Fatalf("generate --check = %d\nstdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
			}
			data, err := os.ReadFile(filepath.Join(root, "view", "edit", "home_gen.go"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), tc.alias+" \"example.com/aliaswire/model\"") {
				t.Fatalf("expected alias %q:\n%s", tc.alias, data)
			}
		})
	}
}
