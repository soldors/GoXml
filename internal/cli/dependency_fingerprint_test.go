package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// PRODUCT-R5-B：依赖闭包收集器的单元测试（B1/B2 语义）。
// 全部使用合成模块根（临时目录 + 自备 go.mod/go.sum），不触碰仓库 go.mod。

func depProject(t *testing.T) (root, srcDir string, write func(rel, content string)) {
	t.Helper()
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.org/r5b\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte("sum-v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcDir = filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write = func(rel, content string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/vm.go", "package vm\n\ntype VM struct { Names []string }\n\nfunc (vm *VM) Record(s string) {}\n")
	return root, srcDir, write
}

func depInput(root, srcDir, decl string, sourceText string) *resolvedInput {
	return &resolvedInput{
		source:     []byte(sourceText),
		moduleRoot: root,
		sourceDir:  srcDir,
		sourceRel:  "src/page.gxml",
	}
}

func depsContain(deps []cachePackageSource, want string) bool {
	for _, s := range deps {
		if s.Rel == want {
			return true
		}
	}
	return false
}

// 跨包形态：VM 类型在 <root>/vm/vm.go（PkgDir 相对模块根）。
func TestCollectDependencySourcesCrossPackage(t *testing.T) {
	root, srcDir, write := depProject(t)
	write("vm/vm.go", "package vm\n\ntype VM struct { Names []string }\n\nfunc (vm *VM) Record(s string) {}\n")
	r := depInput(root, srcDir, "vm.VM", `<Goxml Version="1" ViewModel="vm.VM"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !depsContain(deps, "vm/vm.go") {
		t.Fatalf("依赖闭包应含 vm/vm.go: %+v", deps)
	}
	if !depsContain(deps, "go.mod") || !depsContain(deps, "go.sum") {
		t.Fatalf("模块身份（go.mod/go.sum）应在列: %+v", deps)
	}
}

// 同包形态：VM 类型与 .gxml 同目录（src/）。
func TestCollectDependencySourcesSamePackage(t *testing.T) {
	root, srcDir, _ := depProject(t)
	r := depInput(root, srcDir, "VM", `<Goxml Version="1" ViewModel="VM"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !depsContain(deps, "src/vm.go") {
		t.Fatalf("同包 VM 的依赖闭包应含 src/vm.go: %+v", deps)
	}
}

// 传递闭包：vm 导入本模块 types 包，types 再导入 helper（两层传递）。
func TestCollectDependencySourcesTransitive(t *testing.T) {
	root, srcDir, write := depProject(t)
	write("vm/vm.go", "package vm\n\nimport \"example.org/r5b/types\"\n\ntype VM struct { T types.Thing }\n")
	write("types/types.go", "package types\n\ntype Thing struct { N int }\n")
	write("types/deep.go", "package types\n\nimport \"example.org/r5b/helper\"\n\nfunc Use() helper.H { return helper.H{} }\n")
	write("helper/helper.go", "package helper\n\ntype H struct { S string }\n")
	r := depInput(root, srcDir, "vm.VM", `<Goxml Version="1" ViewModel="vm.VM"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vm/vm.go", "types/types.go", "types/deep.go", "helper/helper.go"} {
		if !depsContain(deps, want) {
			t.Fatalf("传递闭包应含 %s: %+v", want, deps)
		}
	}
}

// 排除面：_test.go 与排除集产物不进指纹。
func TestCollectDependencySourcesExcludesTestsAndArtifact(t *testing.T) {
	root, srcDir, write := depProject(t)
	write("vm/vm.go", "package vm\ntype VM struct{}\n")
	write("vm/vm_extra_test.go", "package vm\nimport \"testing\"\nfunc TestX(t *testing.T) {}\n")
	write("vm/gen_artifact.go", "package vm\n// 模拟已登记产物\n")
	exclude := map[string]bool{filepath.Join(root, "vm", "gen_artifact.go"): true} // 与生产调用同构：原生分隔符
	r := depInput(root, srcDir, "vm.VM", `<Goxml Version="1" ViewModel="vm.VM"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", exclude)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range deps {
		if s.Rel == "vm/vm_extra_test.go" {
			t.Fatalf("_test.go 不应进指纹: %+v", deps)
		}
		if s.Rel == "vm/gen_artifact.go" {
			t.Fatalf("排除集产物不应进指纹: %+v", deps)
		}
	}
}

// 依赖目录缺失（被删除）：哨兵条目 → 保守 miss，让重分析报 ROOT-002。
func TestCollectDependencySourcesMissingDirSentinel(t *testing.T) {
	root, srcDir, _ := depProject(t)
	r := depInput(root, srcDir, "gone.VM", `<Goxml Version="1" ViewModel="gone.VM"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !depsContain(deps, "missing-vm-dir") {
		t.Fatalf("依赖目录缺失应有哨兵（保守 miss）: %+v", deps)
	}
}

// 纯静态页：无 ViewModel 声明 → 依赖快照为空（正确语义，非兜底）。
func TestCollectDependencySourcesNoViewModel(t *testing.T) {
	root, srcDir, _ := depProject(t)
	r := depInput(root, srcDir, "", `<Goxml Version="1"><Page/></Goxml>`)
	deps, err := collectDependencySources(r, "example.org/r5b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Fatalf("纯静态页无 VM 依赖，快照应为空: %+v", deps)
	}
}

// K8 变化必须改变缓存键；同输入键确定。
func TestCacheKeyDependencySourcesChangeKey(t *testing.T) {
	mk := func(depHash string) cacheKeyInput {
		return cacheKeyInput{
			SourceRel: "src/page.gxml", OutputRel: "ui/page_gen.go", SourcePath: "page_gen.go",
			SourceHash: "aaa", PackageName: "ui", CompilerVersion: "v1", BuildFlags: "",
			DependencySources: []cachePackageSource{{Rel: "vm/vm.go", Hash: depHash}},
		}
	}
	k1, err := cacheKey(mk("hash-old"))
	if err != nil {
		t.Fatal(err)
	}
	k2, err := cacheKey(mk("hash-new"))
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("依赖内容变化必须改变缓存键")
	}
	if k3, _ := cacheKey(mk("hash-old")); k3 != k1 {
		t.Fatal("同输入键必须确定")
	}
}

// v6失效旧v5 Input生成物；v5仍是R6静态背景升级的历史版本。
// 本轮v6来自已批准Input规则，与未接纳CPU特性候选的v6原因不同。
func TestFormatVersionEvolution(t *testing.T) {
	if cacheFormatVersion != 7 {
		t.Fatalf("R9-A S1 实验/CPU 上下文升级应使用格式版本 7, got %d", cacheFormatVersion)
	}
}

// PRODUCT-R5-B 切片 B3：有效构建上下文（GOOS/GOARCH/CGO_ENABLED）入键（K10）。
//
// 判别依据 [实测]：纯 Go 夹具包内有 //go:build 标签文件时，
// `go list -f {{.GoFiles}}` 在 GOARCH=amd64 与 arm64 下选择不同的文件——
// 「已选文件随上下文变化」实证成立，键不含上下文就会跨上下文假命中。

// TestBuildContextChangesKey：上下文三元组变化必须改变缓存键。
func TestBuildContextChangesKey(t *testing.T) {
	mk := func(ctx string) cacheKeyInput {
		return cacheKeyInput{
			SourceRel: "src/page.gxml", OutputRel: "ui/page_gen.go", SourcePath: "page_gen.go",
			SourceHash: "aaa", PackageName: "ui", CompilerVersion: "v1", BuildFlags: "",
			BuildContext: ctx,
		}
	}
	k1, err := cacheKey(mk("GOOS=,GOARCH=,CGO_ENABLED="))
	if err != nil {
		t.Fatal(err)
	}
	k2, err := cacheKey(mk("GOOS=,GOARCH=arm64,CGO_ENABLED="))
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Fatal("构建上下文变化必须改变缓存键")
	}
	if k3, _ := cacheKey(mk("GOOS=,GOARCH=,CGO_ENABLED=")); k3 != k1 {
		t.Fatal("同输入键必须确定")
	}
}

// TestGoListSelectsFilesByBuildContext [实测]：纯 Go 夹具 + build 标签，
// go list 的文件选择确实随 GOARCH 变化——K10 的判别依据。
func TestGoListSelectsFilesByBuildContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module example.org/r5b-ctx\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "ctxpkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a_amd64.go": "//go:build amd64\n\npackage ctxpkg\n\nconst Sel = \"amd64\"\n",
		"a_arm64.go": "//go:build arm64\n\npackage ctxpkg\n\nconst Sel = \"arm64\"\n",
		"common.go":  "package ctxpkg\n\nconst Common = true\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runList := func(goarch string) (string, error) {
		cmd := exec.Command("go", "list", "-f", "{{.GoFiles}}", "./ctxpkg")
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off", "GOWORK=off",
			"GOFLAGS=-mod=readonly", "GOOS=windows", "GOARCH="+goarch)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	r1, err1 := runList("amd64")
	if err1 != nil {
		t.Fatalf("go list (amd64): %v: %s", err1, r1)
	}
	r2, err2 := runList("arm64")
	if err2 != nil {
		t.Fatalf("go list (arm64): %v: %s", err2, r2)
	}
	if err2 != nil {
		t.Fatalf("go list (arm64): %v", err2)
	}
	s1, s2 := string(r1), string(r2)
	if s1 == s2 {
		t.Fatalf("两种 GOARCH 下文件选择相同（夹具无效）: %q", s1)
	}
	t.Logf("GOARCH 默认: %s | arm64: %s", s1, s2)
}
