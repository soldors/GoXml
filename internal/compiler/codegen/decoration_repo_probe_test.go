package codegen

// R5-A 补充（同根修复轮）：让需要真实跨包 ViewModel import 的叶子（Button/Input）
// 也能在探针里编译运行 —— 探针临时 module 增加 `require github.com/goxml/goxml`
// + `replace => repoRoot`，复用本树锁定依赖（不联网）。
//
// 不改既有 genModuleWithProbe（其用例无 goxml import，保持最小依赖），
// 本 helper 仅服务本线需要真实 VM 类型的运行时几何/命中探针。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func genModuleWithRepo(t *testing.T, genSource []byte, probe string) (string, error) {
	t.Helper()
	repoRoot := repoRootOf(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gen.go"), genSource, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe_test.go"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	// 主仓 module 路径 + replace 到 repoRoot，使生成的跨包 VM import 可解析。
	mod, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	gomod := strings.Replace(string(mod), "module github.com/goxml/goxml", "module github.com/goxml/goxml/gotestprobe", 1) + "\nrequire github.com/goxml/goxml v0.0.0\nreplace github.com/goxml/goxml => " + strconv.Quote(filepath.ToSlash(repoRoot)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "test", "-v", "-run", "TestProbe", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=readonly", "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// sizeRuntimeRepo 与 sizeRuntime 同构，但走 genModuleWithRepo（支持真实 VM import）。
func sizeRuntimeRepo(t *testing.T, opts Options, gxmlBody, probeBody string) {
	t.Helper()
	src := mustGen(t, gxmlBody, opts)
	out, err := genModuleWithRepo(t, src, sizeProbeHeader+probeBody)
	if strings.Contains(out, "NO_HEADLESS") {
		t.Skipf("本机无 headless 后端，尺寸几何验证跳过:\n%s", out)
	}
	if err != nil {
		t.Fatalf("运行时探测失败: %v\n%s", err, out)
	}
	if strings.Contains(out, "--- FAIL") || strings.Contains(out, "FAIL\t") {
		t.Fatalf("探针断言失败:\n%s", out)
	}
	t.Logf("%s", lastLogLines(out, 8))
}
