package codegen

// R14-B：生成文件导入限定符与目标包已有包级符号的冲突消歧。
//
// 反例（基线 03c1f8a 真实 CLI 复现）：目标包用户 Go 文件合法声明
// `type layout struct{}` 等，生成文件 `import layout "gioui.org/layout"`
// ——Go 规范禁止同一标识符同时出现在文件块与包块，go build 直接失败：
//
//	layout already declared through import of package layout
//
// 修复：importSet 登记时按 Options.PackageNames（命名预检事实，编排层传入）
// 与已登记名取第一个空闲名（pkg、pkg_2、pkg_3……升序），重命名即强制写别名；
// 全部限定符发射点经 g.q 取同一有效名——import 块与实际引用同步，
// 不做任何文本替换（用户字符串与事件实参天然保真）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxml/goxml/internal/compiler/analyzer"
)

// takenPage 是带绑定与静态岛的最小动态页（覆盖 signature/text/flex/op 等发射点）。
const takenPage = `<Column Padding="4">
  <Text Value="{Title}"/>
  <Column>
    <Text Value="静态甲"/>
    <Text Value="静态乙"/>
  </Column>
  <Button Text="点" Click="Tap"/>
</Column>`

// takenGen 以给定 taken 事实跑完真实前端+生成，返回生成源码。
func takenGen(t *testing.T, taken map[string]string) string {
	t.Helper()
	dir := filepath.Join(repoRootOf(t), "internal", "compiler", "codegen",
		"testdata", "tmprt"+randomSuffix())
	vmDir := filepath.Join(dir, "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(vmDir, "vm.go"),
		[]byte("package vm\n\ntype VM struct{ Title string }\n\nfunc (vm *VM) Tap() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := "internal/compiler/codegen/testdata/" + filepath.Base(dir)
	src := `<Goxml Version="1" ViewModel="` + rel + `/vm.VM">
  <Page Route="/">` + takenPage + `</Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	// taken 一律显式写入（nil 与空表都表示「无冲突事实」），不再继承
	// bindingOpts 的默认值；生成对**同一份凭证**执行，路径事实相同。
	opts := bindingOpts()
	opts.PackageNames = taken
	out, err := Generate(v, opts)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	return string(out)
}

// takenDoc 跑真实前端+Analyzer，返回同一份凭证（供多种 taken 事实对比）。
func takenDoc(t *testing.T) *analyzer.VerifiedDoc {
	t.Helper()
	dir := filepath.Join(repoRootOf(t), "internal", "compiler", "codegen",
		"testdata", "tmprt"+randomSuffix())
	vmDir := filepath.Join(dir, "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(vmDir, "vm.go"),
		[]byte("package vm\n\ntype VM struct{ Title string }\n\nfunc (vm *VM) Tap() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := "internal/compiler/codegen/testdata/" + filepath.Base(dir)
	src := `<Goxml Version="1" ViewModel="` + rel + `/vm.VM">
  <Page Route="/">` + takenPage + `</Page>
</Goxml>`
	p := runFrontend(t, src)
	r := analyzer.Analyze("case.gxml", p.doc, p.schemas, p.env)
	if r.Status != analyzer.StatusOK {
		t.Fatalf("Analyzer 未通过: %v", codes(r))
	}
	v, err := r.Verified()
	if err != nil {
		t.Fatalf("取凭证失败: %v", err)
	}
	return v
}

// genWith 对**同一份凭证**以给定 taken 事实生成（同输入的字节对比用）。
func genWith(t *testing.T, v *analyzer.VerifiedDoc, taken map[string]string) string {
	t.Helper()
	opts := bindingOpts()
	opts.PackageNames = taken
	out, err := Generate(v, opts)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	return string(out)
}

// importBlock 取生成源码的 import 块（便于断言）。
func importBlock(t *testing.T, src string) string {
	t.Helper()
	i := strings.Index(src, "import (")
	j := strings.Index(src, ")\n")
	if i < 0 || j < 0 || j < i {
		t.Fatalf("生成源码无 import 块:\n%.400s", src)
	}
	return src[i : j+2]
}

// TestImportTakenNamesForceAliases 多个导入名同时冲突：
// layout/material/op 均被目标包包级声明占用 → 三个 import 全部改
// layout_2/material_2/op_2，且**实际引用同步**（签名/Body/Button 全用新名），
// 旧名不再以限定符形式出现。
func TestImportTakenNamesForceAliases(t *testing.T) {
	src := takenGen(t, map[string]string{
		"layout": "user.go", "material": "user.go", "op": "user.go",
	})
	ib := importBlock(t, src)
	for _, want := range []string{
		`layout_2 "gioui.org/layout"`,
		`material_2 "gioui.org/widget/material"`,
		`op_2 "gioui.org/op"`,
	} {
		if !strings.Contains(ib, want) {
			t.Fatalf("import 块缺 %s:\n%s", want, ib)
		}
	}
	// 引用同步：构造函数签名、bindText、Button 均用新限定符。
	for _, want := range []string{"layout_2.Context", "material_2.Theme", "material_2.Body1", "material_2.Button", "op_2.Record", "op_2.Ops"} {
		if !strings.Contains(src, want) {
			t.Fatalf("生成引用未同步 %s（R14-B 违反）:\n%s", want, src[:800])
		}
	}
	// 旧名不得再以限定符形式出现（用户字符串除外——本页无此类文本）。
	for _, bad := range []string{"layout.Context", "material.Theme", "material.Body1", "op.Record", "op.Ops"} {
		if strings.Contains(src, bad) {
			t.Fatalf("旧限定符残留 %s:\n%s", bad, src[:800])
		}
	}
}

// TestImportTakenCandidateEscalation 消歧候选名也被占用：
// layout 与 layout_2 均被声明 → 取 layout_3（升序确定性）。
func TestImportTakenCandidateEscalation(t *testing.T) {
	src := takenGen(t, map[string]string{
		"layout": "user.go", "layout_2": "user.go",
		"material": "user.go", "material_2": "user.go",
	})
	ib := importBlock(t, src)
	if !strings.Contains(ib, `layout_3 "gioui.org/layout"`) {
		t.Fatalf("候选 layout_2 被占用时应取 layout_3:\n%s", ib)
	}
	if !strings.Contains(ib, `material_3 "gioui.org/widget/material"`) {
		t.Fatalf("候选 material_2 被占用时应取 material_3:\n%s", ib)
	}
	if strings.Contains(ib, `layout_2 "gioui.org/layout"`) {
		t.Fatalf("被占用的候选名不得作为别名:\n%s", ib)
	}
}

// TestImportNoTakenBytesUnchanged 无冲突（PackageNames 为空/nil）时输出与
// 基线逐字节一致——限定符保持默认名、裸 import 形态不变。
func TestImportNoTakenBytesUnchanged(t *testing.T) {
	v := takenDoc(t)
	a := genWith(t, v, nil)
	b := genWith(t, v, map[string]string{})
	if a != b {
		t.Fatal("taken 为空与 nil 的输出应逐字节一致")
	}
	if !strings.Contains(importBlock(t, a), "\t\"gioui.org/layout\"\n") {
		t.Fatalf("无冲突时应保持裸 import:\n%s", importBlock(t, a))
	}
	if strings.Contains(importBlock(t, a), "_2 \"") {
		t.Fatalf("无冲突时不得出现消歧别名:\n%s", importBlock(t, a))
	}
}

// TestImportDeterminismWithTaken 相同输入+相同 taken 事实 → 生成字节确定。
func TestImportDeterminismWithTaken(t *testing.T) {
	v := takenDoc(t)
	taken := map[string]string{"layout": "u.go", "op": "u.go", "material": "u.go"}
	if genWith(t, v, taken) != genWith(t, v, taken) {
		t.Fatal("相同输入与 taken 事实应产生逐字节相同输出（契约 §6）")
	}
}

// TestVMAliasAvoidsTakenAndGio VM 别名与 Gio 导入竞争 + taken：
// VM 包真实名 layout（被用户声明占用，且 Gio layout 已登记）→
// VM 别名落 layout_vm2（既有 R12 环 + taken 判据）。
func TestVMAliasAvoidsTakenAndGio(t *testing.T) {
	g := vmGen(Options{
		ModulePath:          "example.com/m",
		OutputPackageDir:    "ui",
		ViewModelPackageDir: "vmdir",
		PackageNames:        map[string]string{"layout": "user.go"},
	}, analyzer.ViewModelRef{PkgDir: "vmdir", Name: "VM", PackageName: "layout"})
	got, err := g.vmTypeExpr()
	if err != nil {
		t.Fatal(err)
	}
	if got != "layout_vm2.VM" {
		t.Fatalf("VM 包名 layout 与 taken/Gio 冲突时应取 layout_vm2，实际 %q", got)
	}
	ib := g.imports.render()
	if !strings.Contains(ib, `layout_vm2 "example.com/m/vmdir"`) {
		t.Fatalf("应强制别名 layout_vm2:\n%s", ib)
	}
}
