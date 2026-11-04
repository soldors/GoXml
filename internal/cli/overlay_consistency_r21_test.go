package cli

// R21-A 判别回归：用户 GOFLAGS/GOENV 的 -overlay 在「目标定名」阶段与
// go list / go build / --check 口径一致（不再被工具自有 -overlay 覆盖丢弃）。
//
// 历史反例（未修前）：用户 overlay 把目标包的 a.go 换成 package q 时，
//   - `go list`（env overlay, 无工具 argv overlay）看到 q；
//   - 定名阶段工具传 argv -overlay（内部无用户映射）→ go list 看到磁盘 p，
//     而逐文件校验又读磁盘 → 假报「多个非测试 package（q / p）」。
// 修复：effectiveTargetOverlay 合并用户 overlay（工具自有优先）+ 校验 overlay 优先。
//
// 变异红：禁用 effectiveTargetOverlay（退回 targetNameOverlay）后，
// TestUserOverlayRenamesPackageConsistent 会再次假失败。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func r21Go(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available")
	}
	return p
}

func r21Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func r21Mod(t *testing.T, root, mod string) {
	t.Helper()
	repo, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	repo = filepath.Dir(repo) // internal/cli -> repo root
	gomod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, ln := range strings.Split(string(gomod), "\n") {
		if strings.HasPrefix(ln, "module ") {
			ln = "module " + mod
		}
		lines = append(lines, ln)
	}
	r21Write(t, filepath.Join(root, "go.mod"), strings.TrimRight(strings.Join(lines, "\n"), "\n")+
		"\n\nrequire github.com/goxml/goxml v0.0.0\n\nreplace github.com/goxml/goxml => "+filepath.ToSlash(repo)+"\n")
	sum, _ := os.ReadFile(filepath.Join(repo, "go.sum"))
	r21Write(t, filepath.Join(root, "go.sum"), string(sum))
}

// TestEffectiveTargetOverlayMergesUserAndOwned 单元：用户 overlay 并入且工具自有优先。
func TestEffectiveTargetOverlayMergesUserAndOwned(t *testing.T) {
	root := t.TempDir()
	r21Write(t, filepath.Join(root, "p", "a.go"), "package p\n")
	// 用户 overlay：p/a.go -> repl(q)
	r21Write(t, filepath.Join(root, "repl.go"), "package q\n")
	r21Write(t, filepath.Join(root, "user.json"), `{"Replace":{"p/a.go":"repl.go"}}`)
	eff, err := effectiveTargetOverlay(root, nil, filepath.Join(root, "p"), `-mod=readonly "-overlay=user.json"`)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(root, "p", "a.go")
	data, ok := eff[full]
	if !ok {
		t.Fatalf("用户 overlay 应并入 effective 映射；got keys=%v", keysOf(eff))
	}
	if !strings.Contains(string(data), "package q") {
		t.Fatalf("用户 overlay 字节应生效，got=%q", string(data))
	}
	// 无用户 overlay -> 空
	eff2, err := effectiveTargetOverlay(root, nil, filepath.Join(root, "p"), "-mod=readonly")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := eff2[full]; ok {
		t.Fatal("无用户 overlay 时不应有该条目")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestUserOverlayRenamesPackageConsistent 真实 go list 基准：CLI 定名 = 用户 overlay 视角。
func TestUserOverlayRenamesPackageConsistent(t *testing.T) {
	goExe := r21Go(t)
	root := t.TempDir()
	r21Mod(t, root, "example.com/r21ovl")
	r21Write(t, filepath.Join(root, "p", "a.go"), "package p\n\nfunc A(){}\n")
	r21Write(t, filepath.Join(root, "repl.go"), "package q\n\nfunc A(){}\n")
	r21Write(t, filepath.Join(root, "user.json"), `{"Replace":{"p/a.go":"repl.go"}}`)

	// 基准：go list 加用户 overlay -> package q（不带工具 argv overlay）
	cmd := exec.Command(goExe, "list", "-f", "{{.Name}}", "-overlay", filepath.Join(root, "user.json"), "./p")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOENV=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("基准 go list 失败: %v\n%s", err, out)
	}
	baseline := strings.TrimSpace(string(out))
	if baseline != "q" {
		t.Fatalf("基准应看到 package q，got %q", baseline)
	}

	// CLI 定名（effective overlay 应得到同样的 q）
	name, _, err := resolveTargetPackageGo(goExe, root, filepath.Join(root, "p"),
		[]string{"GOENV=off", "GOTOOLCHAIN=local"}, nil)
	// 注意：resolveTargetPackageGo 直接收 overlay map；这里用 effective 构造。
	eff, eerr := effectiveTargetOverlay(root, nil, filepath.Join(root, "p"), `-mod=readonly "-overlay=user.json"`)
	if eerr != nil {
		t.Fatal(eerr)
	}
	name, _, err = resolveTargetPackageGo(goExe, root, filepath.Join(root, "p"),
		[]string{"GOENV=off", "GOTOOLCHAIN=local"}, eff)
	if err != nil {
		t.Fatalf("定名失败: %v", err)
	}
	if name != baseline {
		t.Fatalf("CLI 定名 %q 与 go list 基准 %q 不一致（用户 overlay 未生效）", name, baseline)
	}
}
