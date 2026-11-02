package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// R7-B B3：真实 CLI 缓存 × 有效构建上下文闭环。
//
// 固定基线 CLI vs 修复后 CLI 的分叉已在 raw/repro-b1 用真实二进制复现；
// 本文件用进程内真实 Run()（同一管线、真实 go env/go list 子进程）钉死
// 修复后的闭环：命中 → 只改有效上下文 → 正确 miss/重分析 → 还原恢复
// → 错误保旧。所有环境只经 t.Setenv(GOENV=自有文件) 进入自有子进程，
// 父环境/用户 GOENV 不变。
// ============================================================================

// writeContextPairFixture 写互斥标签对（各带 VM，页面可生成）。
func writeContextPairFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir, "pair_base.go", "//go:build !goxmlr3\n\npackage pkgbase\n\ntype VM struct{ Title string }\n")
	writeGoFile(t, dir, "pair_r3.go", "//go:build goxmlr3\n\npackage pkgtag\n\ntype VM struct{ Title string }\n")
}

func fileModTime(t *testing.T, p string) int64 {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime().UnixNano()
}

// goRepoRoot 从测试进程 cwd（internal/cli）向上定位本仓根（go.mod 模块行
// 是 github.com/goxml/goxml），用于复制依赖清单到 scratch module。
func goRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if b, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil &&
			strings.HasPrefix(string(b), "module github.com/goxml/goxml") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到本仓 go.mod（cwd 漂移）")
		}
		dir = parent
	}
}

// newScratchModuleWithRepoDeps 建一个带本仓依赖（gioui 等）的独立 scratch
// module：go.mod/go.sum 从本仓复制（模块名改为 scratch），使产物可离线
// go build——依赖解析全部走本机模块缓存，GOPROXY=off 不触网。
func newScratchModuleWithRepoDeps(t *testing.T) string {
	t.Helper()
	root := newScratchModule(t)
	repo := goRepoRoot(t)
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.Replace(string(mod), "module github.com/goxml/goxml", "module scratch", 1)
	if rel == string(mod) {
		t.Fatal("本仓 go.mod 模块行漂移，复制前提失效")
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(rel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// goBuildCmd 构造离线 go build 子进程：固定只读 mod、本地工具链、
// GOENV=off（隔离用户配置），CGO=0（本轮所有构建约束）。
func goBuildCmd(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(goBinary(t), append([]string{"build"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(goEnvFor(""),
		"CGO_ENABLED=0", "GOTOOLCHAIN=local", "GOENV=off", "GOFLAGS=-mod=readonly")
	return cmd
}

// TestGenerateCacheBuildContextClosure 命中→上下文变化 miss→还原恢复→错误保旧。
func TestGenerateCacheBuildContextClosure(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	writeContextPairFixture(t, dir)
	gxml := filepath.Join(dir, "home.gxml")
	page := `<Goxml Version="1" ViewModel="VM"><Page Route="/home"><Text Value="{Title}"/></Page></Goxml>`
	if err := os.WriteFile(gxml, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	envA := writeGOENV(t, t.TempDir(), "GOFLAGS=-tags=goxmlr3\n")
	envB := writeGOENV(t, t.TempDir(), "\n") // 无 GOFLAGS：回默认标签集
	gen := filepath.Join(dir, "home_gen.go")
	manifest := manifestPath(root)

	// 空值显式存在不遮蔽 GOENV 文件（go env 实测语义），压掉宿主命令注入
	// 的 GOFLAGS=-mod=readonly，保证被测上下文只由 GOENV 文件决定。
	t.Setenv("GOFLAGS", "")
	run := func() (string, int) {
		var out, errb bytes.Buffer
		code := Run([]string{"generate", dir}, &out, &errb)
		return out.String() + errb.String(), code
	}

	// 1) 首次生成（GOENV 持久化 -tags）：包名必须是 go list 权威的 pkgtag。
	t.Setenv("GOENV", envA)
	log1, code := run()
	if code != ExitOK {
		t.Fatalf("首次生成失败: %s", log1)
	}
	if got := productPackage(t, gen); got != "pkgtag" {
		t.Fatalf("GOENV 持久化 -tags 下产物应为 pkgtag，实际 %q", got)
	}
	bytes1, mtime1 := mustRead(t, gen), fileModTime(t, gen)
	manifest1 := mustRead(t, manifest)

	// 2) 再次生成：真命中——字节/mtime/Manifest 全部不变。
	log2, code := run()
	if code != ExitOK || !strings.Contains(log2, "跳过") || !strings.Contains(log2, "缓存命中") {
		t.Fatalf("第二次应缓存命中: code=%d log=%s", code, log2)
	}
	if mustRead(t, gen) == nil || fileModTime(t, gen) != mtime1 {
		t.Fatal("命中不得重写产物（mtime 变化）")
	}
	if !bytes.Equal(mustRead(t, gen), bytes1) || !bytes.Equal(mustRead(t, manifest), manifest1) {
		t.Fatal("命中不得改动产物字节或 Manifest")
	}

	// 3) 只改有效构建上下文（GOENV 文件切到无 tags）：正确 miss + 重分析，
	//    产物包名随上下文切到 pkgbase（go list 同环境权威一致）。
	t.Setenv("GOENV", envB)
	log3, code := run()
	if code != ExitOK {
		t.Fatalf("上下文变化后生成失败: %s", log3)
	}
	if strings.Contains(log3, "缓存命中") {
		t.Fatalf("有效上下文变化必须 miss，实际命中: %s", log3)
	}
	if !strings.Contains(log3, "缓存未命中") {
		t.Fatalf("上下文变化应有未命中原因行: %s", log3)
	}
	if got := productPackage(t, gen); got != "pkgbase" {
		t.Fatalf("GOENV 切到无 tags 后产物应为 pkgbase，实际 %q", got)
	}
	manifest3 := mustRead(t, manifest)

	// 4) 还原 GOENV：恢复到 pkgtag 内容（内容与首次一致）。
	t.Setenv("GOENV", envA)
	log4, code := run()
	if code != ExitOK {
		t.Fatalf("还原后生成失败: %s", log4)
	}
	if !bytes.Equal(mustRead(t, gen), bytes1) {
		t.Fatalf("还原后产物字节应恢复为 pkgtag 版本，实际:\n%s", mustRead(t, gen)[:200])
	}
	if !bytes.Equal(mustRead(t, manifest), manifest3) {
		t.Fatal("还原后 Manifest（输入哈希/输出/版本）应保持一致")
	}

	// 5) 错误保旧：坏 .gxml 使管线失败，旧产物/Manifest 原样。
	if err := os.WriteFile(gxml, []byte("<Goxml><Page"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeBytes, beforeMtime := mustRead(t, gen), fileModTime(t, gen)
	log5, code := run()
	if code == ExitOK {
		t.Fatalf("坏输入必须失败: %s", log5)
	}
	if !bytes.Equal(mustRead(t, gen), beforeBytes) || fileModTime(t, gen) != beforeMtime {
		t.Fatal("失败批次不得改动旧产物")
	}
	if !bytes.Equal(mustRead(t, manifest), manifest3) {
		t.Fatal("失败批次不得改动 Manifest")
	}
}

// TestGenerateCrossPackageOutputGoBuildPasses 合法跨包输出实际 go build 通过：
// VM 在独立包，页面经 --out-dir 生成进已初始化的目标包（包名取目标包），
// 产物与 vm 包一起真实编译。
func TestGenerateCrossPackageOutputGoBuildPasses(t *testing.T) {
	root := newScratchModuleWithRepoDeps(t)
	vmDir := filepath.Join(root, "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, vmDir, "vm.go", "package vm\n\ntype VM struct{ Title string }\n")
	srcDir := filepath.Join(root, "pages")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "home.gxml"),
		[]byte(`<Goxml Version="1" ViewModel="vm.VM"><Page Route="/home"><Text Value="{Title}"/></Page></Goxml>`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "uiout")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, outDir, "doc.go", "package uiout\n")

	var out, errb bytes.Buffer
	if code := Run([]string{"generate", filepath.Join(srcDir, "home.gxml"), "--out-dir", outDir}, &out, &errb); code != ExitOK {
		t.Fatalf("跨包生成失败: %s%s", out.String(), errb.String())
	}
	if got := productPackage(t, filepath.Join(outDir, "home_gen.go")); got != "uiout" {
		t.Fatalf("跨包输出包名应取目标包 uiout，实际 %q", got)
	}
	// 真实 go build（离线：依赖来自本机模块缓存，go.mod/go.sum 复制自本仓）。
	cmd := goBuildCmd(t, root, "./...")
	var bOut, bErr bytes.Buffer
	cmd.Stdout = &bOut
	cmd.Stderr = &bErr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build ./... 失败: %v\nstdout:%s\nstderr:%s", err, bOut.String(), bErr.String())
	}
}
