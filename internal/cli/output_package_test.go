package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// PRODUCT-R2 切片 2A：目标包的定名（R2-qcode-02）
//
// 契约依据：
//   - `代码生成输出契约.md` §3.2：输出目录必须解析为明确的 Go package；
//     「如果输出目录存在多个非测试 package，生成失败」。
//   - §4.1 与 §13 验收 4：包名要能「从目标目录正确解析」，缺失或冲突时
//     返回明确错误。
//
// 老实现按 ReadDir 顺序取**第一个** `.go` 的 package 子句，测试文件也在候选里。
// 「先写外部测试文件、再生成页面」是普通工程形态，不是构造出来的边界：
//
//	ui/home_test.go  → package ui_test   （字母序在前，被老实现抓走）
//	ui/view.go       → package ui
//	产物写成 package ui_test，CLI 报「已生成」、退出码 0
//	go build ./ui    → found packages ui_test (home_gen.go) and ui (view.go)
//
// 本文件里每条断言都成对写：正例（真实工程应得到什么）+ 能拒绝错实现的对照
// （名字/顺序刻意让"抓第一个"的实现落到错误分支）。
// ============================================================================

// writeGoFile 在 dir 写一个 Go 源文件，返回其路径。
func writeGoFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPackageDirNameIgnoresTestFiles 外部测试包不得决定产物包名。
//
// 对照性在文件名上：测试文件叫 `a_home_test.go`，排在 `b_view.go` 之前，
// 「取第一个 .go」的实现会返回 ui_test。
func TestPackageDirNameIgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "a_home_test.go", "package ui_test\n\nimport \"testing\"\n\nfunc TestHome(*testing.T) {}\n")
	writeGoFile(t, dir, "b_view.go", "package ui\n\ntype VM struct{ Title string }\n")

	got, err := packageDirName(dir)
	if err != nil {
		t.Fatalf("有明确非测试包时应成功: %v", err)
	}
	if got != "ui" {
		t.Fatalf("包名 = %q，期望 ui（外部测试包 ui_test 不得参与定名）", got)
	}
}

// TestPackageDirNameRejectsMultipleNonTestPackages 两个非测试包 → 明确失败（§3.2）。
//
// 老实现在这里返回 "one" 并继续生成，产物落进一个 go build 拒绝的目录。
func TestPackageDirNameRejectsMultipleNonTestPackages(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "a_one.go", "package one\n")
	writeGoFile(t, dir, "b_two.go", "package two\n")

	_, err := packageDirName(dir)
	if err == nil {
		t.Fatalf("多个非测试 package 必须报错，实际成功")
	}
	// 两个包名都要出现在诊断里，且顺序确定（ReadDir 的文件名序）。
	msg := err.Error()
	for _, want := range []string{"one", "two"} {
		if !strings.Contains(msg, want) {
			t.Errorf("诊断应列出冲突包名 %q，实际: %s", want, msg)
		}
	}
	if !strings.Contains(msg, "多个非测试 package") {
		t.Errorf("诊断应说明失败原因是多包冲突（契约 §3.2），实际: %s", msg)
	}
	if strings.Contains(msg, "ui_test") {
		t.Errorf("诊断不应把无关内容拼进来: %s", msg)
	}
	// 确定性：同一目录重复调用给出同一串（诊断顺序也是契约的一部分）。
	_, err2 := packageDirName(dir)
	if err2 == nil || err2.Error() != msg {
		t.Errorf("重复调用的诊断必须一致: %v vs %v", err, err2)
	}
}

// TestPackageDirNameRejectsTestOnlyDir 只有测试文件时不能拿它当包名。
func TestPackageDirNameRejectsTestOnlyDir(t *testing.T) {
	dir := t.TempDir()
	writeGoFile(t, dir, "home_test.go", "package ui_test\n")

	_, err := packageDirName(dir)
	if err == nil {
		t.Fatalf("目录没有可定名的非测试源文件时必须报错")
	}
	if !strings.Contains(err.Error(), "非测试") {
		t.Errorf("诊断应说明「非测试」口径，实际: %s", err)
	}
}

// TestPackageDirNameDeduplicatesSamePackage 多文件同包是正常形态，不得误判冲突。
func TestPackageDirNameDeduplicatesSamePackage(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a_view.go", "b_vm.go", "c_page.go"} {
		writeGoFile(t, dir, n, "package ui\n")
	}
	got, err := packageDirName(dir)
	if err != nil {
		t.Fatalf("同包多文件不应冲突: %v", err)
	}
	if got != "ui" {
		t.Fatalf("包名 = %q，期望 ui", got)
	}
}

// productPackage 取产物里的 package 子句（跳过保护头注释）。
func productPackage(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取产物: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "package "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("产物没有 package 子句:\n%.200s", data)
	return ""
}

// writePageInPackage 写一页可成功生成的 .gxml 与其 ViewModel（包名为 pkg）。
//
// Route 带上 pkg 前缀：同一批次里多个同名 `home.gxml`（不同目录）是真实工程
// 形态，路由不能撞在一起，否则测的就不是本轮关心的那一层。
func writePageInPackage(t *testing.T, dir, pkg, name string) {
	t.Helper()
	writeGoFile(t, dir, "vm.go", "package "+pkg+"\n\ntype VM struct{ Title string }\n")
	gxml := `<Goxml Version="1" ViewModel="VM">
  <Page Route="/` + pkg + "/" + name + `"><Text Value="{Title}"/></Page>
</Goxml>`
	writeGoFile(t, dir, name+".gxml", gxml)
}

// TestGenerateProductUsesBuildPackageNotTestPackage CLI 级正例：
// 真实工程形态（源目录同时有外部测试文件）下产物必须写构建包名。
//
// 断言打在**产物内容**上而不是错误串上：这条缺陷的原始症状是「CLI 报成功、
// go build 拒绝」，只有产物包名能证明故障没有发生。
func TestGenerateProductUsesBuildPackageNotTestPackage(t *testing.T) {
	root := tempProjectInRepo(t)
	writePageInPackage(t, root, "ui", "home")
	// 外部测试文件排在 vm.go 之前（a_ 前缀），专门踩"取第一个 .go"的实现。
	writeGoFile(t, root, "a_home_test.go", "package ui_test\n\nimport \"testing\"\n\nfunc TestHome(t *testing.T) {}\n")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", root}, &out, &errb); code != ExitOK {
		t.Fatalf("退出码 = %d；stdout:\n%s\nstderr:\n%s", code, out.String(), errb.String())
	}
	product := filepath.Join(root, "home_gen.go")
	if got := productPackage(t, product); got != "ui" {
		t.Fatalf("产物包名 = %q，期望 ui；外部测试包名写进产物会让 go build 直接失败", got)
	}
	// 产物里不得再出现 ui_test（codegen 的包引用也只应有一种包名）。
	data, err := os.ReadFile(product)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ui_test") {
		t.Errorf("产物混入外部测试包名 ui_test:\n%.200s", data)
	}
}

// TestGenerateFailsOnAmbiguousTargetPackage 目标目录多包时生成前就失败，
// 且磁盘零痕迹（契约 §3.2「生成失败」，不是"生成后再被 go build 拒绝"）。
func TestGenerateFailsOnAmbiguousTargetPackage(t *testing.T) {
	root := tempProjectInRepo(t)
	writePageInPackage(t, root, "ui", "home")
	// 同目录第二个**非测试**包：go build 本来也不接受这个目录。
	writeGoFile(t, root, "zz_other.go", "package other\n")

	var out, errb bytes.Buffer
	code := Run([]string{"generate", root}, &out, &errb)
	if code != ExitFailure {
		t.Fatalf("退出码 = %d，期望 %d；stdout:\n%s", code, ExitFailure, out.String())
	}
	if !strings.Contains(errb.String(), "多个非测试 package") {
		t.Errorf("stderr 应指明多包冲突: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(root, "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("包名歧义必须在写盘前拦住: %v", err)
	}
	if residue := countTempResidue(t, root); len(residue) > 0 {
		t.Errorf("失败后残留临时文件: %v", residue)
	}
}
