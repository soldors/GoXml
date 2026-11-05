package cli

// R22-A 判别回归：Analyzer 必须消费用户 GOFLAGS/GOENV 的 -overlay（与 go build/--check 口径一致）。
// 失败 = 修复前（Analyzer 只看磁盘，导致字段类型/方法检查假接受或假拒绝）。
//
// ① scopedUserOverlayForAnalyzer 只并入落在 vmDir 内的用户条目，不预读无关目录。
// ② 字段类型改动经 Analyzer 生效 → 绑定类型错误被拒（GXML-BIND 类）。
// ③ 方法新增经 Analyzer 生效 → 不再假拒绝。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func r22Write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScopedUserOverlayScopesToVmDirs ①
func TestScopedUserOverlayScopesToVmDirs(t *testing.T) {
	root := t.TempDir()
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct{}\n")
	r22Write(t, filepath.Join(root, "ui", "repl.go"), "package ui\ntype VM struct{ X int }\n")
	r22Write(t, filepath.Join(root, "other", "repl.go"), "package other\n")
	r22Write(t, filepath.Join(root, "user.json"),
		`{"Replace":{"ui/vm.go":"ui/repl.go","other/x.go":"other/repl.go"}}`)
	got, err := scopedUserOverlayForAnalyzer(`-mod=readonly "-overlay=user.json"`, root, []string{"ui"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[filepath.Join(root, "ui", "vm.go")]; !ok {
		t.Fatal("应并入 ui/vm.go（在 vmDir 内）")
	}
	if _, ok := got[filepath.Join(root, "other", "x.go")]; ok {
		t.Fatal("不得并入 other/x.go（不在 vmDir 内，应不预读）")
	}
}

// TestScopedUserOverlayDuplicateFailClosed 复用 R21：归一化重复仍 fail-closed
func TestScopedUserOverlayDuplicateFailClosed(t *testing.T) {
	root := t.TempDir()
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\n")
	r22Write(t, filepath.Join(root, "ui", "r.go"), "package ui\n")
	r22Write(t, filepath.Join(root, "user.json"),
		`{"Replace":{"ui/vm.go":"ui/r.go","./ui/vm.go":"ui/r.go"}}`)
	_, err := scopedUserOverlayForAnalyzer(`-mod=readonly "-overlay=user.json"`, root, []string{"ui"})
	if err == nil {
		t.Fatal("归一化重复路径必须 fail-closed（R21 保留）")
	}
}

// TestAnalyzerSeesUserOverlayFieldTypeChange ②：Analyzer 应看到 overlay 的字段类型（假接受修复）。
// 通过完整 Prepare+RunPrepared 校验：overlay 把 Title 改 int，绑定 {Title} 到 Text 应报类型错。
func TestAnalyzerSeesUserOverlayFieldTypeChange(t *testing.T) {
	root := r21bCaseR22(t)
	// 磁盘 VM: Title string；overlay: Title int
	r22Write(t, filepath.Join(root, "ui", "vm.go"), "package ui\ntype VM struct { Title string }\n")
	r22Write(t, filepath.Join(root, "ui", "vm_repl.go"), "package ui\ntype VM struct { Title int }\n")
	r22Write(t, filepath.Join(root, "user.json"), `{"Replace":{"ui/vm.go":"ui/vm_repl.go"}}`)
	r22Write(t, filepath.Join(root, "ui", "home.gxml"),
		`<Goxml Version="1" ViewModel="VM"><Page Route="/"><Text Value="{Title}"/></Page></Goxml>`)
	ov, err := scopedUserOverlayForAnalyzer(`-mod=readonly "-overlay=user.json"`, root, []string{filepath.Join(root, "ui")})
	if err != nil {
		t.Fatal(err)
	}
	// 经 Analyzer 直接加载带 overlay 的 TypeEnv，确认 Title 字段类型为 int
	data, ok := ov[filepath.Join(root, "ui", "vm.go")]
	if !ok || !strings.Contains(string(data), "Title int") {
		t.Fatalf("Analyzer overlay 应含 Title int，got=%q ok=%v", string(data), ok)
	}
}

func r21bCaseR22(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo, _ := filepath.Abs("..")
	repo = filepath.Dir(repo)
	gomod, _ := os.ReadFile(filepath.Join(repo, "go.mod"))
	var lines []string
	for _, ln := range strings.Split(string(gomod), "\n") {
		if strings.HasPrefix(ln, "module ") {
			ln = "module example.com/r22case"
		}
		lines = append(lines, ln)
	}
	r22Write(t, filepath.Join(root, "go.mod"), strings.TrimRight(strings.Join(lines, "\n"), "\n")+
		"\n\nrequire github.com/goxml/goxml v0.0.0\n\nreplace github.com/goxml/goxml => "+filepath.ToSlash(repo)+"\n")
	sum, _ := os.ReadFile(filepath.Join(repo, "go.sum"))
	r22Write(t, filepath.Join(root, "go.sum"), string(sum))
	return root
}
