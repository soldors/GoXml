package cli

// R14-A CHECK-MUST-COMPILE 判别回归。
//
// 核心：`--check` 的实际 `go build` 必须在用户 GOFLAGS/GOENV 带 `-n`（dry-run）时
// **仍然真编译**——否则只打印命令、以 0 退出，坏候选会假通过并提交。
// 断言：坏目标在 -n 下仍被拒绝且不落产物/Manifest/缓存；合法目标在 -n 下仍通过；
// 显式 -n=false 与正常 -x 不改变判定；多包一个坏整批拒绝；含空格 -modfile + overlay 仍成立。
// 若移除 argv 的 `-n=false` 修复，本组必然红。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// r14Project 建 n 个目标包（n=1 时目录名 ui），每包 doc.go + home.gxml。
func r14Project(t *testing.T, n int) string {
	t.Helper()
	root := standaloneCLIProject(t)
	_ = os.Remove(filepath.Join(root, "home.gxml")) // 去掉自带目标，便于计数
	for i := 0; i < n; i++ {
		d := "ui"
		if n > 1 {
			d = "pkg" + twoDigitR13(i)
		}
		writeCLIFileMkdir(t, filepath.Join(root, d, "doc.go"), []byte("package "+d+"\n"))
		writeCLIFileMkdir(t, filepath.Join(root, d, "home.gxml"),
			[]byte(`<Goxml Version="1"><Page Route="/"><Text Value="p`+twoDigitR13(i)+`"/></Page></Goxml>`))
	}
	return root
}

func breakTargetPkg(t *testing.T, root, d string) {
	t.Helper()
	writeCLIFileMkdir(t, filepath.Join(root, d, "use.go"),
		[]byte("package "+d+"\n\nvar _ = NewGoxmlHomeView(nil)\n"))
}

// TestCheckMustCompileDespiteDashN 进程 GOFLAGS=-n 时坏目标仍被拒绝且不落盘。
func TestCheckMustCompileDespiteDashN(t *testing.T) {
	root := r14Project(t, 1)
	breakTargetPkg(t, root, "ui")
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -n")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	code, stdout, stderr := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("-n 下坏目标必须被拒绝（否则假通过），实际 rc=0\nstdout:%s\nstderr:%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "提交前检查失败") {
		t.Fatalf("应报提交前检查失败，实际: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "ui", "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("失败时不得写入产物")
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Errorf("失败时不得创建 Manifest")
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".goxml", "cache")); err == nil && len(entries) > 0 {
		t.Errorf("失败时不得写缓存记录: %d", len(entries))
	}
}

// TestCheckMustCompileDespiteGOENVDashN GOENV 持久化 -n 时坏目标仍被拒绝。
func TestCheckMustCompileDespiteGOENVDashN(t *testing.T) {
	root := r14Project(t, 1)
	breakTargetPkg(t, root, "ui")
	cfg := filepath.Join(t.TempDir(), "goenv")
	writeCLIFileMkdir(t, cfg, []byte("GOFLAGS=-mod=readonly -n\n"))
	t.Setenv("GOENV", cfg)
	t.Setenv("GOFLAGS", "")

	code, _, stderr := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("GOENV -n 下坏目标必须被拒绝，实际 rc=0，stderr:%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "ui", "home_gen.go")); !os.IsNotExist(err) {
		t.Errorf("失败时不得写入产物")
	}
}

// TestCheckPassesWithDashNForValidTarget 合法目标 + -n 仍通过（-n 不误伤正常路径）。
func TestCheckPassesWithDashNForValidTarget(t *testing.T) {
	root := r14Project(t, 1)
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -n")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	code, stdout, stderr := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check", "--no-cache")
	if code != ExitOK {
		t.Fatalf("合法目标在 -n 下应通过: rc=%d\nstderr:%s", code, stderr)
	}
	if !strings.Contains(stdout, "提交前检查通过") {
		t.Fatalf("应记录提交前检查通过: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "ui", "home_gen.go")); err != nil {
		t.Fatalf("应写出产物: %v", err)
	}
}

// TestCheckExplicitNFalseStillCompiles 显式 -n=false 时坏目标仍被拒绝。
func TestCheckExplicitNFalseStillCompiles(t *testing.T) {
	root := r14Project(t, 1)
	breakTargetPkg(t, root, "ui")
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -n=false")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	code, _, stderr := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("显式 -n=false 下坏目标必须被拒绝，stderr:%s", stderr)
	}
}

// TestCheckNormalXStillCompiles 正常 -x（打印命令但仍编译）下坏目标被拒绝。
func TestCheckNormalXStillCompiles(t *testing.T) {
	root := r14Project(t, 1)
	breakTargetPkg(t, root, "ui")
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -x")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	code, _, stderr := runStandaloneCLI(t, filepath.Join(root, "ui", "home.gxml"), "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("正常 -x 下坏目标必须被拒绝，stderr:%s", stderr)
	}
}

// TestCheckMultiBadRejectedUnderDashN 多包一个坏 + -n => 整批拒绝，无任何产物/Manifest。
func TestCheckMultiBadRejectedUnderDashN(t *testing.T) {
	root := r14Project(t, 3)
	breakTargetPkg(t, root, "pkg01")
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -n")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	code, stdout, _ := runStandaloneCLI(t, root, "--check", "--no-cache")
	if code == ExitOK {
		t.Fatalf("多包一个坏应整批拒绝: rc=0\nstdout:%s", stdout)
	}
	for i := 0; i < 3; i++ {
		if _, err := os.Stat(filepath.Join(root, "pkg"+twoDigitR13(i), "home_gen.go")); !os.IsNotExist(err) {
			t.Fatalf("pkg%d 不应写产物", i)
		}
	}
	if _, err := os.Stat(manifestPath(root)); !os.IsNotExist(err) {
		t.Fatalf("不应创建 Manifest: %v", err)
	}
}

// TestCheckAllHitUnderDashN 全缓存命中 + -n 仍通过（且检查确实执行）。
func TestCheckAllHitUnderDashN(t *testing.T) {
	root := r14Project(t, 1)
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", "-mod=readonly -n")
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })
	source := filepath.Join(root, "ui", "home.gxml")
	if code, _, stderr := runStandaloneCLI(t, source); code != ExitOK {
		t.Fatalf("首次生成应成功: %s", stderr)
	}
	code, stdout, stderr := runStandaloneCLI(t, source, "--check")
	if code != ExitOK {
		t.Fatalf("全命中 + -n 应通过: rc=%d %s", code, stderr)
	}
	if !strings.Contains(stdout, "缓存命中") || !strings.Contains(stdout, "提交前检查通过") {
		t.Fatalf("应为全命中且执行检查: %s", stdout)
	}
}

// TestCheckSpaceModfileAndUserOverlay 含空格 -modfile + 用户 overlay：坏候选仍被拒绝且候选优先。
func TestCheckSpaceModfileAndUserOverlay(t *testing.T) {
	root := checkFixture(t, map[string]string{"pkgx/doc.go": "package pkgx\n"})
	mustWrite(t, filepath.Join(root, "repl.go"), "package pkgx\n")
	mustWrite(t, filepath.Join(root, "user ov.json"), `{"Replace":{"pkgx/doc.go":"repl.go"}}`)
	mustWrite(t, filepath.Join(root, "alt 模.mod"), "module example.com/chk\n\ngo 1.26\n")
	prev := os.Getenv("GOFLAGS")
	os.Setenv("GOFLAGS", `-mod=readonly "-modfile=alt 模.mod" "-overlay=user ov.json"`)
	t.Cleanup(func() { os.Setenv("GOFLAGS", prev) })

	good := filepath.Join(root, "pkgx", "gen.go")
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: good, content: []byte("package pkgx\nfunc Gen(){}\n")},
	}); err != nil {
		t.Fatalf("含空格 modfile + 用户 overlay 下合法候选应通过: %v", err)
	}
	// 坏候选不得被用户 overlay 掩盖。
	if err := checkPrecommitCandidates(goBinary(t), root, []precommitCandidate{
		{dir: filepath.Join(root, "pkgx"), output: good, content: []byte("package pkgx\nvar X int=\"badCandidate\"\n")},
	}); err == nil {
		t.Fatal("坏候选不应被用户 overlay 掩盖")
	}
}
