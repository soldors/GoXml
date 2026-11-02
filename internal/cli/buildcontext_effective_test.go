package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// R7-B B1：有效构建上下文闭环——GOENV 持久化配置下 CLI 与 go 权威对照。
//
// 复现（night-product-r7/zcode/raw/repro-b1，真实 CLI 二进制）：
//   GOENV 文件写 `GOFLAGS=-tags=goxmlr3` → `go list` 选 pkgtag，
//   基线 CLI 生成 `package pkgbase`（rc=0 静默分叉）；
//   GOENV 文件写 `GOOS=linux` → `go list`=pkglinux，基线 CLI=package pkgwin。
//
// 根因：packageDirName 走 build.Default + 进程环境 GOFLAGS，而 go 自身
// 还消费 GOENV 持久化值；缓存 K10 早已用 go env 实测——同一有效上下文
// 两个视图。修复后 packageDirName 与 K10 同源（measureContextValues）。
//
// 本文件全部用自有 scratch module 与自有 GOENV 文件；不改系统/父进程
// GOENV，子进程环境逐项显式（goEnvFor + GOENV 覆盖）。
// ============================================================================

// goListPackageEnv 与 goListPackage 相同，但允许追加子进程环境
// （如 GOENV=<自有文件>），用于让权威对照物与 CLI 看到同一份持久化配置。
func goListPackageEnv(t *testing.T, moduleRoot, dir string, extraEnv ...string) (name string, incomplete bool) {
	t.Helper()
	gobin := goBinary(t)
	rel, err := filepath.Rel(moduleRoot, dir)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(gobin, "list", "-e", "-f", "{{.Name}}\t{{.Incomplete}}", "./"+filepath.ToSlash(rel))
	cmd.Dir = moduleRoot
	cmd.Env = append(goEnvFor(""), extraEnv...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list 执行失败: %v\nstdout:%s\nstderr:%s", err, out.String(), errb.String())
	}
	line := strings.TrimRight(out.String(), "\r\n")
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	name = line
	inc := false
	if j := strings.Index(line, "\t"); j >= 0 {
		name = line[:j]
		inc = strings.TrimSpace(line[j+1:]) == "true"
	}
	return strings.TrimSpace(name), inc
}

// writeGOENV 写一枚自有 GOENV 文件并返回其路径。
func writeGOENV(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "goenv-test-file")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPackageDirNameFollowsGOENVPersistedTags GOENV 持久化 GOFLAGS：
// CLI 包名判定必须与同环境下的 go list 一致（修复前分叉，见文件头）。
func TestPackageDirNameFollowsGOENVPersistedTags(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "p")
	writePairFixture(t, dir)
	goenv := writeGOENV(t, t.TempDir(), "GOFLAGS=-tags=goxmlr3\n")
	// "GOFLAGS="（空值显式存在）不遮蔽 GOENV 文件（go env 实测语义），
	// 用于压掉宿主命令注入的 GOFLAGS=-mod=readonly，让被测配置生效。
	extra := []string{"GOENV=" + goenv, "GOFLAGS="}

	// 权威：go list 在同一 GOENV 文件下选 pkgtag。
	authName, inc := goListPackageEnv(t, root, dir, extra...)
	if inc || authName != "pkgtag" {
		t.Fatalf("对照前提：go list 应得 pkgtag（GOENV 持久化 -tags），实际 %q inc=%v", authName, inc)
	}
	// CLI：走与 K10 同源的实测上下文。
	m, err := measureContextValues(goBinary(t), root, extra...)
	if err != nil {
		t.Fatalf("measureContextValues: %v", err)
	}
	if m.GOFLAGS != "-tags=goxmlr3" {
		t.Fatalf("实测 GOFLAGS 应回读持久化值，实际 %q", m.GOFLAGS)
	}
	ctxt, err := buildContextFromMeasured(m)
	if err != nil {
		t.Fatalf("buildContextFromMeasured: %v", err)
	}
	got, err := packageDirName(dir, ctxt)
	if err != nil {
		t.Fatalf("packageDirName: %v", err)
	}
	if got != authName {
		t.Errorf("CLI 包名 %q 与 go list %q 不一致（GOENV 持久化 -tags）", got, authName)
	}

	// 对照：GOENV=off 且无进程 GOFLAGS → 应回到 pkgbase（go list 同环境一致）。
	authOff, incOff := goListPackageEnv(t, root, dir, "GOENV=off")
	if incOff || authOff != "pkgbase" {
		t.Fatalf("对照前提：GOENV=off 时 go list 应得 pkgbase，实际 %q inc=%v", authOff, incOff)
	}
	mOff, err := measureContextValues(goBinary(t), root, "GOENV=off", "GOFLAGS=")
	if err != nil {
		t.Fatal(err)
	}
	if mOff.GOFLAGS != "" {
		t.Fatalf("GOENV=off 时实测 GOFLAGS 应为空，实际 %q", mOff.GOFLAGS)
	}
	ctxtOff, err := buildContextFromMeasured(mOff)
	if err != nil {
		t.Fatal(err)
	}
	if gotOff, err := packageDirName(dir, ctxtOff); err != nil || gotOff != authOff {
		t.Errorf("GOENV=off：CLI %q err=%v，go list %q", gotOff, err, authOff)
	}
}

// TestPackageDirNameFollowsGOENVPersistedGOOS GOENV 持久化 GOOS：
// 平台选择（含平台后缀文件）必须与 go list 一致。
func TestPackageDirNameFollowsGOENVPersistedGOOS(t *testing.T) {
	root := newScratchModule(t)
	dir := filepath.Join(root, "plat")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 平台互斥对：linux 激活 pkglinux，其余平台激活 pkghost。
	writeGoFile(t, dir, "pair_linux.go", "//go:build linux\n\npackage pkglinux\n\ntype L struct{ Title string }\n")
	writeGoFile(t, dir, "pair_host.go", "//go:build !linux\n\npackage pkghost\n\ntype H struct{ Title string }\n")
	goenv := writeGOENV(t, t.TempDir(), "GOOS=linux\n")
	extra := []string{"GOENV=" + goenv}

	authName, inc := goListPackageEnv(t, root, dir, extra...)
	if inc || authName != "pkglinux" {
		t.Fatalf("对照前提：go list 应得 pkglinux（GOENV 持久化 GOOS），实际 %q inc=%v", authName, inc)
	}
	m, err := measureContextValues(goBinary(t), root, extra...)
	if err != nil {
		t.Fatal(err)
	}
	if m.GOOS != "linux" {
		t.Fatalf("实测 GOOS 应回读持久化值，实际 %q", m.GOOS)
	}
	ctxt, err := buildContextFromMeasured(m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := packageDirName(dir, ctxt)
	if err != nil {
		t.Fatalf("packageDirName: %v", err)
	}
	if got != authName {
		t.Errorf("CLI 包名 %q 与 go list %q 不一致（GOENV 持久化 GOOS）", got, authName)
	}

	// 平台后缀文件：仅有 _linux.go 时，host 上下文应判「未初始化」，
	// linux 上下文应能定名——与 go 的文件选择一致。
	dir2 := filepath.Join(root, "suf")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	writeGoFile(t, dir2, "only_linux.go", "package onlylinux\n")
	ctxtLinux := ctxt
	if _, err := packageDirName(dir2, ctxtLinux); err != nil {
		t.Errorf("linux 上下文应能经平台后缀定名: %v", err)
	}
	mHost, err := measureContextValues(goBinary(t), root, "GOENV=off")
	if err != nil {
		t.Fatal(err)
	}
	ctxtHost, err := buildContextFromMeasured(mHost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packageDirName(dir2, ctxtHost); err == nil {
		t.Errorf("host(%s) 上下文下 only_linux.go 不参与构建，应判未初始化", ctxtHost.GOOS)
	}
}

// TestMeasureContextValuesInvalidGOENVFailsClosed GOENV 文件里的非法
// GOFLAGS：go env 只回显不校验（实测 rc=0 返回 "garbage"），因此
// fail-closed 点在 buildContextFromMeasured 的 tagsFromGOFLAGS（与
// 进程 GOFLAGS 非法同一语义）；go list 同配置下拒绝加载，权威一致。
func TestMeasureContextValuesInvalidGOENVFailsClosed(t *testing.T) {
	root := newScratchModule(t)
	goenv := writeGOENV(t, t.TempDir(), "GOFLAGS=garbage\n")
	m, err := measureContextValues(goBinary(t), root, "GOENV="+goenv, "GOFLAGS=")
	if err != nil {
		t.Fatalf("go env 对非法 GOFLAGS 只回显: %v", err)
	}
	if m.GOFLAGS != "garbage" {
		t.Fatalf("实测 GOFLAGS 应回读持久化原值, got %q", m.GOFLAGS)
	}
	if _, err := buildContextFromMeasured(m); err == nil {
		t.Fatal("非法持久化 GOFLAGS 必须在上下文构造处 fail-closed")
	} else if !strings.Contains(err.Error(), "GOFLAGS") {
		t.Errorf("错误应指向 $GOFLAGS，实际: %v", err)
	}
	// 权威对照：go list 在同一配置下同样以非零退出（非法 GOFLAGS 不是
	// Incomplete 标记而是命令失败；goListPackageEnv 遇非零会 t.Fatalf，
	// 所以这里独立验证非零退出）。
	cmd := exec.Command(goBinary(t), "list", "-e", ".")
	cmd.Dir = root
	cmd.Env = append(goEnvFor(""), "GOENV="+goenv, "GOFLAGS=")
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if runErr := cmd.Run(); runErr == nil {
		t.Fatalf("对照前提：非法持久化 GOFLAGS 下 go list 也应失败（stderr=%s）", errb.String())
	}
}
