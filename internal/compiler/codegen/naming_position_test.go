package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R3 F1 回归：包扫描错误必须使用**真实文件名**，不得出现伪文件名 x.go。
// 只改错误定位，不改诊断阶段/码/是否 ROOT-002。

func TestPackageLevelNamesUsesRealFilename(t *testing.T) {
	bad := []byte("package p\n\nfunc Broken( {\n") // 语法错误在第 3 行
	cases := []string{"bad.go", "有 空格 bad.go", "unicodé-文件.go"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := packageLevelNames(name, bad)
			if err == nil {
				t.Fatalf("语法错误应报错")
			}
			s := err.Error()
			if strings.Contains(s, "x.go") {
				t.Fatalf("仍出现伪文件名 x.go: %q", s)
			}
			if !strings.Contains(s, name+":3:") {
				t.Fatalf("错误应包含真实文件名与行号 %q:3:，实际 %q", name, s)
			}
		})
	}
}

func TestExistingPackageSymbolsReportsRealFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "broken.go")
	if err := os.WriteFile(bad, []byte("package p\n\nfunc Broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := existingPackageSymbols(dir, nil)
	if err == nil {
		t.Fatal("坏文件应使包级符号扫描失败")
	}
	s := err.Error()
	if strings.Contains(s, "x.go") {
		t.Fatalf("仍出现伪文件名 x.go: %q", s)
	}
	if !strings.Contains(s, "broken.go:3:") {
		t.Fatalf("应含真实文件与行号 broken.go:3:，实际 %q", s)
	}
	if !strings.Contains(s, "预检解析") || !strings.Contains(s, dir) {
		t.Fatalf("应保留预检阶段与绝对路径上下文: %q", s)
	}
}

// 正对照：正常声明仍被正确收集（修复不绕过解析、不忽略文件）。
func TestPackageLevelNamesValidDeclarations(t *testing.T) {
	src := []byte("package p\n\ntype A struct{}\nvar B int\nfunc F() {}\nfunc (A) M() {}\n")
	names, err := packageLevelNames("ok.go", src)
	if err != nil {
		t.Fatalf("正常源码不应报错: %v", err)
	}
	for _, want := range []string{"A", "B", "F"} {
		if !names[want] {
			t.Errorf("应收集包级符号 %q，实际 %v", want, names)
		}
	}
	if names["M"] {
		t.Errorf("方法 M 不应计入包级符号: %v", names)
	}
}
