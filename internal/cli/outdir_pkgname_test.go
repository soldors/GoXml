package cli

// R4-A 片1：--out-dir 目标包名解析回归（源包与目标包不同、目标未初始化拒绝、多包拒绝、同目录不变）。
// 纯 resolveInput 层（不写盘、不跑 Codegen），断言包名来源与 fail-closed 行为。

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSrcFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 源包 a / 目标包 b：--out-dir 时包名必须取目标目录的 b，而非源目录的 a。
func TestResolveInputOutDirUsesTargetPackageName(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.24\n")
	srcDir := filepath.Join(dir, "src")
	outDir := filepath.Join(dir, "out")
	writeSrcFile(t, filepath.Join(srcDir, "a.go"), "package a\n")
	writeSrcFile(t, filepath.Join(outDir, "b.go"), "package b\n")
	gxml := filepath.Join(srcDir, "home.gxml")
	writeSrcFile(t, gxml, `<Goxml Version="1"><Page Route="/"><Column Padding="1"><Text Value="x"/></Column></Page></Goxml>`)

	in := discovered{sourceDir: srcDir, abs: gxml, rel: "home.gxml"}
	r, err := resolveInput(in, dir, outDir, envBuildContext(t))
	if err != nil {
		t.Fatalf("resolveInput: %v", err)
	}
	if r.pkgName != "b" {
		t.Fatalf("--out-dir 包名应取目标目录 b，实际 %q", r.pkgName)
	}
	if r.targetDir != outDir {
		t.Fatalf("targetDir = %q, want %q", r.targetDir, outDir)
	}
}

// 无 --out-dir：包名仍取源目录（既有行为不变）。
func TestResolveInputSameDirKeepsSourcePackageName(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.24\n")
	ui := filepath.Join(dir, "ui")
	writeSrcFile(t, filepath.Join(ui, "a.go"), "package a\n")
	gxml := filepath.Join(ui, "home.gxml")
	writeSrcFile(t, gxml, `<Goxml Version="1"><Page Route="/"><Column Padding="1"><Text Value="x"/></Column></Page></Goxml>`)
	in := discovered{sourceDir: ui, abs: gxml, rel: "home.gxml"}
	r, err := resolveInput(in, dir, "", envBuildContext(t))
	if err != nil {
		t.Fatalf("resolveInput: %v", err)
	}
	if r.pkgName != "a" {
		t.Fatalf("同目录应保持源包名 a，实际 %q", r.pkgName)
	}
}

// --out-dir 目标目录无 Go 源文件（空/未初始化）：保留既有兼容行为——回退源包名
// （首次生成到新目录仍成功，TestGenerateCreatesOutDirOnSuccess 守住该边界）。
func TestResolveInputOutDirEmptyTargetFallsBackToSourcePkg(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.24\n")
	srcDir := filepath.Join(dir, "src")
	outDir := filepath.Join(dir, "out")
	writeSrcFile(t, filepath.Join(srcDir, "a.go"), "package a\n")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gxml := filepath.Join(srcDir, "home.gxml")
	writeSrcFile(t, gxml, `<Goxml Version="1"><Page Route="/"><Column Padding="1"><Text Value="x"/></Column></Page></Goxml>`)
	in := discovered{sourceDir: srcDir, abs: gxml, rel: "home.gxml"}
	r, err := resolveInput(in, dir, outDir, envBuildContext(t))
	if err != nil {
		t.Fatalf("空目标目录应回退源包名而非拒绝（兼容首建新目录）: %v", err)
	}
	if r.pkgName != "a" {
		t.Fatalf("空目标目录应回退源包名 a，实际 %q", r.pkgName)
	}
}

// --out-dir 目标目录存在多个生产 package：fail-closed。
func TestResolveInputOutDirMultiPackageFailsClosed(t *testing.T) {
	dir := t.TempDir()
	writeSrcFile(t, filepath.Join(dir, "go.mod"), "module example.com/t\n\ngo 1.24\n")
	srcDir := filepath.Join(dir, "src")
	outDir := filepath.Join(dir, "out")
	writeSrcFile(t, filepath.Join(srcDir, "a.go"), "package a\n")
	writeSrcFile(t, filepath.Join(outDir, "b.go"), "package b\n")
	writeSrcFile(t, filepath.Join(outDir, "c.go"), "package c\n")
	gxml := filepath.Join(srcDir, "home.gxml")
	writeSrcFile(t, gxml, `<Goxml Version="1"><Page Route="/"><Column Padding="1"><Text Value="x"/></Column></Page></Goxml>`)
	in := discovered{sourceDir: srcDir, abs: gxml, rel: "home.gxml"}
	if _, err := resolveInput(in, dir, outDir, envBuildContext(t)); err == nil {
		t.Fatal("多包目标目录应 fail-closed")
	}
}
