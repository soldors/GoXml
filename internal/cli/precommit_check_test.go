package cli

// R11-A 提交前检查（--check）判别回归。
//
// 覆盖三件事，每件都有可编译行为红：
//  1. 检查消费**本批候选内容**而不是磁盘旧产物（成对断言：同一文件磁盘坏/候选好 → 通过；
//     磁盘好/候选坏或按命中读盘 → 失败）。若 overlay 未生效，第一条断言直接变红。
//  2. CLI `--check` 拒绝会让目标包编译失败的候选，且不落产物/Manifest/缓存。
//  3. 全命中批次仍然执行检查（不因无未命中而跳过）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkFixture 建一个最小模块（无外部依赖，纯 Go 标准库编译）。
func checkFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/chk\n\ngo 1.26\n")
	for name, body := range files {
		mustWrite(t, filepath.Join(root, name), body)
	}
	return root
}

// TestCheckUsesCandidateContentNotDisk 成对证明：检查用本批候选字节，不看磁盘旧内容。
func TestCheckUsesCandidateContentNotDisk(t *testing.T) {
	good := "package pkgx\n\nfunc Gen() {}\n"
	// 磁盘上是坏的（引用未定义符号），候选是好的 → 检查必须通过（overlay 生效）。
	root := checkFixture(t, map[string]string{
		"pkgx/out_gen.go": "package pkgx\n\nvar _ = undefinedSymbolOnDisk()\n",
	})
	out := filepath.Join(root, "pkgx", "out_gen.go")
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: out, content: []byte(good)},
	}); err != nil {
		t.Fatalf("候选内容应覆盖磁盘旧产物并通过检查，实际失败（overlay 未生效？）: %v", err)
	}

	// 同一磁盘内容、但本批按「命中」处理（content=nil ⇒ 读盘）→ 必须失败。
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: out, content: nil},
	}); err == nil {
		t.Fatal("命中路径应读磁盘坏内容并失败，实际通过")
	}

	// 反向：磁盘好、候选坏 → 必须失败（证明不是只看磁盘）。
	root2 := checkFixture(t, map[string]string{"pkgx/out_gen.go": good})
	out2 := filepath.Join(root2, "pkgx", "out_gen.go")
	if err := checkPrecommitCandidates(goBinary(t), root2, []precommitCandidate{
		{dir: filepath.Join(root2, "pkgx"), output: out2, content: []byte("package pkgx\n\nvar _ = undefinedInCandidate()\n")},
	}); err == nil {
		t.Fatal("候选内容坏时检查应失败，实际通过")
	}
}

// TestCheckRejectsCandidateBreakingTargetPackage CLI 级：--check 拒绝坏候选且不落盘。
func TestCheckRejectsCandidateBreakingTargetPackage(t *testing.T) {
	root := standaloneCLIProject(t)
	// 生成视图类型 NewGoxmlHomeView 由 home.gxml 产生（无 ViewModel ⇒ 零参数）。
	// 用户文件按「有 ViewModel」的签名调用 ⇒ 候选与目标包组合编译失败。
	writeCLIFile(t, filepath.Join(root, "use.go"), []byte("package demo\n\nvar _ = NewGoxmlHomeView(nil)\n"))
	source := filepath.Join(root, "home.gxml")

	code, stdout, stderr := runStandaloneCLI(t, source, "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("--check 应拒绝编译失败的候选，实际 rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "提交前检查失败") {
		t.Fatalf("诊断应说明提交前检查失败，实际: %s", stderr)
	}
	// 失败不提交：产物、Manifest、成功缓存都不落盘。
	if _, err := os.Stat(filepath.Join(root, "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("检查失败时不得留下产物: %v", err)
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Errorf("检查失败时不得创建 Manifest: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".goxml", "cache")); err == nil && len(entries) > 0 {
		t.Errorf("检查失败时不得写成功缓存记录: %d 条", len(entries))
	}
	if strings.Contains(stdout, "已生成") {
		t.Errorf("检查失败时不得声称已生成: %s", stdout)
	}
}

// TestCheckAbsentKeepsDefaultBehavior 不带 --check 时默认行为不变（判据：产物照常生成）。
func TestCheckAbsentKeepsDefaultBehavior(t *testing.T) {
	root := standaloneCLIProject(t)
	writeCLIFile(t, filepath.Join(root, "use.go"), []byte("package demo\n\nvar _ = NewGoxmlHomeView(nil)\n"))
	source := filepath.Join(root, "home.gxml")
	code, stdout, stderr := runStandaloneCLI(t, source, "--no-cache")
	if code != ExitOK {
		t.Fatalf("默认（无 --check）应照旧生成，rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "home_gen.go")); err != nil {
		t.Fatalf("默认行为应写出产物: %v", err)
	}
	if strings.Contains(stdout, "提交前检查通过") {
		t.Errorf("未请求 --check 时不应执行提交前检查: %s", stdout)
	}
}

// TestCheckRunsOnAllCacheHits 全命中批次仍必须执行检查。
// 变异红：若实现把检查放在「有未命中才做」的分支里，第二次运行的 stdout 就不会有
// 「提交前检查通过」这行，本测试失败。
func TestCheckRunsOnAllCacheHits(t *testing.T) {
	root := standaloneCLIProject(t)
	source := filepath.Join(root, "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("首次生成应成功: rc=%d %s", code, stderr)
	}
	code, stdout, stderr := runStandaloneCLI(t, source, "--check")
	if code != ExitOK {
		t.Fatalf("全命中 + --check 应通过: rc=%d\nstdout:%s\nstderr:%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "缓存命中") {
		t.Fatalf("第二次运行应为全命中: %s", stdout)
	}
	if !strings.Contains(stdout, "提交前检查通过") {
		t.Fatalf("全命中批次也必须执行提交前检查，实际 stdout: %s", stdout)
	}
}
