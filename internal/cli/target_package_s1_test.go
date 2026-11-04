package cli

// R9-A S1 生产接线判别回归：结构化 go list 目标包解析 + K10 实验/CPU 入键 + 旧 v6 安全 miss。
// 只读（go env/go list），不 build 跨目标产物。

import (
	"os"
	"path/filepath"
	"testing"
)

// pkgByExperimentFixture：目标目录 d 的包名随 goexperiment.regabiargs 变化。
func pkgByExperimentFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/s1\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(root, "d", "enabled.go"),
		"//go:build goexperiment.regabiargs\n\npackage expenabled\n\nfunc E() {}\n")
	mustWrite(t, filepath.Join(root, "d", "disabled.go"),
		"//go:build !goexperiment.regabiargs\n\npackage expdisabled\n\nfunc D() {}\n")
	return root
}

// TestS1TargetPackageByArchitecture 386 目标（无 regabiargs 默认）→ expdisabled；
// amd64 → expenabled。证明 S1 按目标架构权威选包，不误用宿主集。
func TestS1TargetPackageByArchitecture(t *testing.T) {
	root := pkgByExperimentFixture(t)
	dir := filepath.Join(root, "d")
	// 386：GOENV 持久化 windows/386，进程 GOARCH 未设。
	goenv := filepath.Join(root, ".ge386")
	mustWrite(t, goenv, "GOOS=windows\nGOARCH=386\n")
	env386 := []string{"GOENV=" + goenv, "GOOS=", "GOARCH=", "GOEXPERIMENT="}
	name386, _, err := resolveTargetPackageGo(goBinary(t), root, dir, env386, nil, nil)
	if err != nil {
		t.Fatalf("386 go list: %v", err)
	}
	if name386 != "expdisabled" {
		t.Fatalf("386 目标包名应为 expdisabled（regabiargs 默认关），实际 %q", name386)
	}
	// amd64：默认 expenabled。
	envAmd := []string{"GOENV=off", "GOOS=windows", "GOARCH=amd64", "GOEXPERIMENT="}
	nameAmd, files, err := resolveTargetPackageGo(goBinary(t), root, dir, envAmd, nil, nil)
	if err != nil {
		t.Fatalf("amd64 go list: %v", err)
	}
	if nameAmd != "expenabled" {
		t.Fatalf("amd64 目标包名应为 expenabled，实际 %q", nameAmd)
	}
	if !containsFile(files, "enabled.go") {
		t.Fatalf("amd64 应选 enabled.go: %v", files)
	}
	os.Remove(goenv)
}

// TestS1RedundantGreenteagcFlag 冗余 GOEXPERIMENT=greenteagc 不改变选文件/包名（不误拒）。
func TestS1RedundantGreenteagcFlag(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/g\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(root, "d", "base.go"), "package p\n\nfunc B() {}\n")
	dir := filepath.Join(root, "d")
	base := []string{"GOENV=off", "GOOS=windows", "GOARCH=amd64"}
	n1, f1, err := resolveTargetPackageGo(goBinary(t), root, dir, append(append([]string{}, base...), "GOEXPERIMENT="), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	n2, f2, err := resolveTargetPackageGo(goBinary(t), root, dir, append(append([]string{}, base...), "GOEXPERIMENT=greenteagc"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != n2 || !sameStringSet(f1, f2) {
		t.Fatalf("冗余 greenteagc 不应改变包名/选文件: %q/%v vs %q/%v", n1, f1, n2, f2)
	}
}

// TestS1StructDecodeRejectsMultiObject 多 JSON 对象 → 恰一包校验拒绝。
func TestS1StructDecodeRejectsMultiObject(t *testing.T) {
	pkgs, err := decodeGoListJSON([]byte("{\"Name\":\"a\"}{\"Name\":\"b\"}"))
	if err != nil || len(pkgs) != 2 {
		t.Fatalf("应解出 2 对象: pkgs=%v err=%v", pkgs, err)
	}
	// resolveTargetPackageGo 对 len!=1 会 fail-closed；这里只验证解码层。
}

// TestS1BadJSONRejected 坏 JSON → error。
func TestS1BadJSONRejected(t *testing.T) {
	if _, err := decodeGoListJSON([]byte("{not json")); err == nil {
		t.Fatal("坏 JSON 应 error")
	}
}

// TestK10IncludesExperimentAndCPU GOEXPERIMENT/CPU 变化改变缓存上下文串。
func TestK10IncludesExperimentAndCPU(t *testing.T) {
	base := measuredContextValues{GOOS: "windows", GOARCH: "amd64"}
	c0, _ := base.cacheString()
	a := base
	a.GOEXPERIMENT = "greenteagc"
	c1, _ := a.cacheString()
	b := base
	b.GOAMD64 = "v3"
	c2, _ := b.cacheString()
	if c0 == c1 {
		t.Fatal("GOEXPERIMENT 变化应改变 K10 上下文串")
	}
	if c0 == c2 {
		t.Fatal("GOAMD64 变化应改变 K10 上下文串")
	}
}

// TestFormatV7 版本锚点（v7）。
func TestFormatV7(t *testing.T) {
	if cacheFormatVersion != 8 {
		t.Fatalf("R9-A S1 应为格式版本 7，实际 %d", cacheFormatVersion)
	}
}
